// Package orchestrator manages parallel collector execution with timeout,
// graceful signal handling, and tier-based fallback.
package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/collector"
	"github.com/dmitriimaksimovdevelop/melisai/internal/ebpf"
	"github.com/dmitriimaksimovdevelop/melisai/internal/executor"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
	"github.com/dmitriimaksimovdevelop/melisai/internal/observer"
	"github.com/dmitriimaksimovdevelop/melisai/internal/output"
)

// Orchestrator coordinates all collectors and produces a Report.
type Orchestrator struct {
	collectors []collector.Collector
	config     collector.CollectConfig
	progress   *output.Progress
}

// New creates an Orchestrator with the given collectors and config.
func New(collectors []collector.Collector, cfg collector.CollectConfig) *Orchestrator {
	return &Orchestrator{
		collectors: collectors,
		config:     cfg,
		progress:   output.NewVerboseProgress(!cfg.Quiet, cfg.Verbose),
	}
}

// Run executes collectors in two phases to avoid observer effect:
//   Phase 1: Tier 1 (procfs) collectors run alone on a clean system
//   Phase 2: Tier 2/3 (BCC/eBPF) collectors run in parallel
// Returns a partial report if interrupted (SIGINT/SIGTERM).
func (o *Orchestrator) Run(ctx context.Context) (*model.Report, error) {
	// Set up timeouts first, then signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Apply profile timeout
	profile := GetProfile(o.config.Profile)
	timeout := profile.Duration + 30*time.Second
	ctx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	// Signal handling — started AFTER all context derivations to avoid data race
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sigDone := make(chan struct{})
	go func() {
		defer close(sigDone)
		select {
		case sig := <-sigCh:
			o.progress.Log("Received %v, shutting down gracefully (partial report)...", sig)
			cancel()
		case <-ctx.Done():
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		<-sigDone // wait for goroutine to exit
	}()

	// Create PID tracker for observer-effect mitigation
	tracker := observer.NewPIDTracker()
	o.config.PIDTracker = tracker
	tracker.SnapshotBefore()

	// Filter collectors by availability and focus
	active := o.filterCollectors()

	// Split into Tier 1 (procfs, lightweight) and Tier 2/3 (BCC/eBPF, heavy)
	var tier1, tier23 []collector.Collector
	for _, c := range active {
		if c.Available().Tier == 1 {
			tier1 = append(tier1, c)
		} else {
			tier23 = append(tier23, c)
		}
	}

	o.progress.Log("Starting collection: profile=%s, duration=%s, tier1=%d, tier2/3=%d",
		o.config.Profile, profile.Duration, len(tier1), len(tier23))

	// Phase 1: Run Tier 1 collectors on a clean system (no BCC overhead).
	// This gives accurate CPU, memory, disk, and network baseline metrics.
	o.progress.Log("Phase 1: System baseline (Tier 1, no BPF tools running)...")
	phase1Results := o.runCollectorsParallel(ctx, tier1)

	// Check for interruption between phases
	if ctx.Err() != nil {
		o.progress.Log("Interrupted after Phase 1, building partial report...")
	}

	// Phase 2: Run Tier 2/3 collectors (BCC/eBPF — CPU-intensive)
	var phase2Results map[string][]*model.Result
	if ctx.Err() == nil && len(tier23) > 0 {
		o.progress.Log("Phase 2: BCC/eBPF tools (%d collectors)...", len(tier23))
		phase2Results = o.runCollectorsParallel(ctx, tier23)
	}

	// Cancel context so the signal-handling goroutine exits promptly
	cancel()

	// Merge phase 1 and phase 2 results
	allResults := phase1Results
	for cat, resultPtrs := range phase2Results {
		allResults[cat] = append(allResults[cat], resultPtrs...)
	}

	// Sort results within each category by collector name for deterministic output
	categories := make(map[string][]model.Result)
	for cat, resultPtrs := range allResults {
		sort.Slice(resultPtrs, func(i, j int) bool {
			return resultPtrs[i].Collector < resultPtrs[j].Collector
		})
		for _, r := range resultPtrs {
			categories[cat] = append(categories[cat], *r)
		}
	}

	// Snapshot observer overhead after all collectors finish
	overhead := tracker.SnapshotAfter()

	// Build report
	meta := o.buildMetadata(profile)
	meta.ObserverOverhead = &model.ObserverOverhead{
		SelfPID:         overhead.SelfPID,
		ChildPIDs:       overhead.ChildPIDs,
		CPUUserMs:       overhead.CPUUserMs,
		CPUSystemMs:     overhead.CPUSystemMs,
		MemoryRSSBytes:  overhead.MemoryRSSBytes,
		DiskReadBytes:   overhead.DiskReadBytes,
		DiskWriteBytes:  overhead.DiskWriteBytes,
		ContextSwitches: overhead.ContextSwitches,
	}

	report := &model.Report{
		Metadata:   meta,
		Categories: categories,
		Summary: model.Summary{
			Anomalies: []model.Anomaly{},
			Resources: map[string]model.USEMetric{},
		},
	}

	// Compute USE metrics, anomalies, health score, recommendations
	report.Summary.Resources = model.ComputeUSEMetrics(report)
	report.Summary.Anomalies = model.DetectAnomalies(report)
	report.Summary.HealthScore = model.ComputeHealthScore(report.Summary.Resources, report.Summary.Anomalies)
	report.Summary.Recommendations = model.GenerateRecommendations(report)

	o.progress.Log("Collection complete. %d categories, health=%d/100, anomalies=%d",
		len(categories), report.Summary.HealthScore, len(report.Summary.Anomalies))
	return report, nil
}

// runCollectorsParallel runs a set of collectors in parallel and returns results by category.
func (o *Orchestrator) runCollectorsParallel(ctx context.Context, collectors []collector.Collector) map[string][]*model.Result {
	var (
		mu      sync.Mutex
		results = make(map[string][]*model.Result)
		wg      sync.WaitGroup
	)

	for _, c := range collectors {
		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()

			name := c.Name()
			start := time.Now()

			// Recover from panics in individual collectors
			defer func() {
				if r := recover(); r != nil {
					o.progress.Log("  [%s] panic: %v", name, r)
					mu.Lock()
					results[c.Category()] = append(results[c.Category()], &model.Result{
						Collector: name,
						Category:  c.Category(),
						Tier:      c.Available().Tier,
						StartTime: start,
						EndTime:   time.Now(),
						Errors:    []string{fmt.Sprintf("panic: %v", r)},
					})
					mu.Unlock()
				}
			}()

			o.progress.Log("  [%s] collecting...", name)

			result, err := c.Collect(ctx, o.config)
			elapsed := time.Since(start)

			if err != nil {
				if ctx.Err() != nil {
					o.progress.Log("  [%s] interrupted (%s)", name, elapsed.Round(time.Millisecond))
				} else {
					o.progress.Log("  [%s] error: %v (%s)", name, err, elapsed.Round(time.Millisecond))
				}
				result = &model.Result{
					Collector: name,
					Category:  c.Category(),
					Tier:      c.Available().Tier,
					StartTime: start,
					EndTime:   time.Now(),
					Errors:    []string{err.Error()},
				}
			} else {
				o.progress.Log("  [%s] done (%s)", name, elapsed.Round(time.Millisecond))
			}

			mu.Lock()
			results[c.Category()] = append(results[c.Category()], result)
			mu.Unlock()
		}(c)
	}

	wg.Wait()
	return results
}

// focusCategoryMap maps user-facing focus area names to collector categories.
var focusCategoryMap = map[string]string{
	"stacks":    "stacktrace",
	"cpu":       "cpu",
	"memory":    "memory",
	"disk":      "disk",
	"network":   "network",
	"process":   "process",
	"container": "container",
}

// filterCollectors returns only available collectors,
// respecting profile restrictions and focus area filtering.
// When --focus is set, Tier 1 collectors always run (baseline data),
// but Tier 2/3 collectors are limited to matching categories.
func (o *Orchestrator) filterCollectors() []collector.Collector {
	var active []collector.Collector

	profile := GetProfile(o.config.Profile)

	// Build focus category set
	focusCategories := make(map[string]bool)
	for _, f := range o.config.Focus {
		if cat, ok := focusCategoryMap[f]; ok {
			focusCategories[cat] = true
		} else {
			// If not in map, use as-is (e.g. "stacktrace" directly)
			focusCategories[f] = true
		}
	}
	hasFocus := len(focusCategories) > 0

	for _, c := range o.collectors {
		avail := c.Available()
		if avail.Tier == 0 {
			o.progress.Log("  [%s] skipped: %s", c.Name(), avail.Reason)
			continue
		}

		// If a profile restricts collectors, check the list
		if len(profile.Collectors) > 0 && profile.Collectors[0] != "all" {
			found := false
			for _, name := range profile.Collectors {
				if name == c.Name() {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Focus filtering: Tier 1 always included, Tier 2/3 only if category matches
		if hasFocus && avail.Tier > 1 {
			if !focusCategories[c.Category()] {
				o.progress.Log("  [%s] skipped: not in focus areas %v", c.Name(), o.config.Focus)
				continue
			}
		}

		active = append(active, c)
	}

	return active
}

// buildMetadata constructs Metadata from the current system and config.
func (o *Orchestrator) buildMetadata(profile ProfileConfig) model.Metadata {
	hostname, _ := os.Hostname()

	meta := model.Metadata{
		Tool:          "melisai",
		Version:       "0.1.0",
		SchemaVersion: "1.0.0",
		Hostname:      hostname,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Duration:      profile.Duration.String(),
		Profile:       o.config.Profile,
		FocusAreas:    o.config.Focus,
		Arch:          runtime.GOARCH,
		CPUs:          runtime.NumCPU(),
	}

	// Kernel version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			meta.KernelVersion = parts[2]
		}
	}

	// Total memory
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						meta.MemoryGB = int(kb / (1024 * 1024))
					}
				}
				break
			}
		}
	}

	return meta
}

// RegisterCollectors builds the list of collectors based on configuration.
// It always registers Tier 1 collectors, then adds BCC (Tier 2) tools
// from the Registry based on the active profile. For "all" profiles,
// every registered BCC tool is attempted. For explicit lists, only
// named tools are added.
func RegisterCollectors(cfg collector.CollectConfig) []collector.Collector {
	procRoot := cfg.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	sysRoot := cfg.SysRoot
	if sysRoot == "" {
		sysRoot = "/sys"
	}

	// Always register Tier 1 collectors
	tier1 := []collector.Collector{
		collector.NewSystemCollector(procRoot, sysRoot),
		collector.NewCPUCollector(procRoot),
		collector.NewMemoryCollector(procRoot, sysRoot),
		collector.NewDiskCollector(procRoot, sysRoot),
		collector.NewNetworkCollector(procRoot),
		collector.NewProcessCollector(procRoot),
		collector.NewContainerCollector(procRoot, sysRoot),
	}

	// Map Tier 1 names for deduplication
	tier1Names := make(map[string]bool)
	for _, c := range tier1 {
		tier1Names[c.Name()] = true
	}

	collectors := append([]collector.Collector{}, tier1...)

	// Initialize loaders for Tier 2 and 3
	ebpfLoader := ebpf.NewLoader(false)
	bccExec := executor.NewBCCExecutor(false)

	// Track which BCC tools have been added
	added := make(map[string]bool)

	// Helper to add BCC tool if available (skips duplicates and Tier 1 names)
	addBCC := func(tool string) {
		if added[tool] || tier1Names[tool] {
			return
		}
		added[tool] = true

		// Special case: try native eBPF Tier 3 for tcpretrans
		if tool == "tcpretrans" {
			nativeTcpretrans := collector.NewNativeTcpretransCollector(ebpfLoader)
			if nativeTcpretrans.Available().Tier > 0 {
				collectors = append(collectors, nativeTcpretrans)
				return
			}
		}

		bccCol, err := collector.NewBCCCollector(tool, bccExec)
		if err != nil {
			return
		}
		if avail := bccCol.Available(); avail.Tier > 0 {
			collectors = append(collectors, bccCol)
		}
	}

	profile := GetProfile(cfg.Profile)

	if len(profile.Collectors) > 0 && profile.Collectors[0] == "all" {
		// "all" profile: register every BCC tool from the Registry
		for name := range executor.Registry {
			addBCC(name)
		}
	} else {
		// Explicit list: add only the named BCC tools
		for _, name := range profile.Collectors {
			if !tier1Names[name] {
				addBCC(name)
			}
		}
	}

	// Deep profile Extra tools
	for _, name := range profile.Extra {
		addBCC(name)
	}

	return collectors
}

// BuildReport runs all registered collectors and produces a report.
// This is the high-level entry point used by the CLI.
func BuildReport(ctx context.Context, cfg collector.CollectConfig) (*model.Report, error) {
	collectors := RegisterCollectors(cfg)
	if len(collectors) == 0 {
		return nil, fmt.Errorf("no collectors available")
	}

	orch := New(collectors, cfg)
	return orch.Run(ctx)
}
