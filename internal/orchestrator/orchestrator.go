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

	"github.com/baikal/sysdiag/internal/collector"
	"github.com/baikal/sysdiag/internal/ebpf"
	"github.com/baikal/sysdiag/internal/executor"
	"github.com/baikal/sysdiag/internal/model"
	"github.com/baikal/sysdiag/internal/observer"
	"github.com/baikal/sysdiag/internal/output"
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
		progress:   output.NewProgress(!cfg.Quiet),
	}
}

// Run executes all collectors in parallel with timeout and signal handling.
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
	go func() {
		select {
		case sig := <-sigCh:
			o.progress.Log("Received %v, shutting down gracefully (partial report)...", sig)
			cancel()
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigCh)

	// Create PID tracker for observer-effect mitigation
	tracker := observer.NewPIDTracker()
	o.config.PIDTracker = tracker
	tracker.SnapshotBefore()

	// Filter collectors by availability and focus
	active := o.filterCollectors()
	o.progress.Log("Starting collection: profile=%s, duration=%s, collectors=%d",
		o.config.Profile, profile.Duration, len(active))

	// Run all collectors in parallel
	var (
		mu      sync.Mutex
		results = make(map[string][]*model.Result)
		wg      sync.WaitGroup
	)

	for _, c := range active {
		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()

			name := c.Name()
			o.progress.Log("  [%s] collecting...", name)
			start := time.Now()

			result, err := c.Collect(ctx, o.config)
			elapsed := time.Since(start)

			if err != nil {
				if ctx.Err() != nil {
					o.progress.Log("  [%s] interrupted (%s)", name, elapsed.Round(time.Millisecond))
				} else {
					o.progress.Log("  [%s] error: %v (%s)", name, err, elapsed.Round(time.Millisecond))
				}
				// Even on error, create a result with the error recorded
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

	// Sort results within each category by collector name for deterministic output
	categories := make(map[string][]model.Result)
	for cat, resultPtrs := range results {
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

	// Phase 4: compute USE metrics, anomalies, health score, recommendations
	report.Summary.Resources = model.ComputeUSEMetrics(report)
	report.Summary.Anomalies = model.DetectAnomalies(report)
	report.Summary.HealthScore = model.ComputeHealthScore(report.Summary.Resources, report.Summary.Anomalies)
	report.Summary.Recommendations = model.GenerateRecommendations(report)

	o.progress.Log("Collection complete. %d categories, health=%d/100, anomalies=%d",
		len(categories), report.Summary.HealthScore, len(report.Summary.Anomalies))
	return report, nil
}

// filterCollectors returns only available collectors,
// respecting focus area filtering.
func (o *Orchestrator) filterCollectors() []collector.Collector {
	var active []collector.Collector

	profile := GetProfile(o.config.Profile)

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

		active = append(active, c)
	}

	return active
}

// buildMetadata constructs Metadata from the current system and config.
func (o *Orchestrator) buildMetadata(profile ProfileConfig) model.Metadata {
	hostname, _ := os.Hostname()

	meta := model.Metadata{
		Tool:          "sysdiag",
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
