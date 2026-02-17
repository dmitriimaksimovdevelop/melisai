// Package orchestrator manages parallel collector execution with timeout,
// graceful signal handling, and tier-based fallback.
package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/baikal/sysdiag/internal/collector"
	"github.com/baikal/sysdiag/internal/model"
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

	// Build report
	report := &model.Report{
		Metadata:   o.buildMetadata(profile),
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

	return model.Metadata{
		Tool:          "sysdiag",
		Version:       "0.1.0",
		SchemaVersion: "1.0.0",
		Hostname:      hostname,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Duration:      profile.Duration.String(),
		Profile:       o.config.Profile,
		FocusAreas:    o.config.Focus,
	}
}

// RegisterCollectors builds the default list of all Tier 1 collectors.
func RegisterCollectors(cfg collector.CollectConfig) []collector.Collector {
	procRoot := cfg.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	sysRoot := cfg.SysRoot
	if sysRoot == "" {
		sysRoot = "/sys"
	}

	return []collector.Collector{
		collector.NewSystemCollector(procRoot, sysRoot),
		collector.NewCPUCollector(procRoot),
		collector.NewMemoryCollector(procRoot, sysRoot),
		collector.NewDiskCollector(procRoot, sysRoot),
		collector.NewNetworkCollector(procRoot),
		collector.NewProcessCollector(procRoot),
		collector.NewContainerCollector(procRoot, sysRoot),
	}
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
