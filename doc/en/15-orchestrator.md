# Chapter 15: Orchestrator

## Overview

The orchestrator (`internal/orchestrator/orchestrator.go`) is the brain of melisai. It runs all collectors in parallel, handles signals, manages timeouts, and assembles the final report.

## Source Files: orchestrator/ (3 files)

| File | Lines | Purpose |
|------|-------|---------|
| `orchestrator.go` | ~136 | Main execution loop |
| `profiles.go` | ~106 | Quick/standard/deep profile definitions |

## The Run() Method

```go
func (o *Orchestrator) Run(ctx context.Context) (*model.Report, error) {
    // 1. Create cancellation context for signal handling
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    // 2. Apply timeout from profile
    ctx, timeoutCancel := context.WithTimeout(ctx, profile.Timeout)
    defer timeoutCancel()

    // 3. Signal handler: graceful shutdown on SIGINT/SIGTERM
    go func() {
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        select {
        case <-sigCh:
            cancel()  // Cancel all collectors
        case <-ctx.Done():
            // Normal completion
        }
    }()

    // 4. Run collectors in parallel
    results := o.runCollectors(ctx, cfg)

    // 5. Assemble report
    report := &model.Report{
        Metadata:   metadata,
        System:     systemInfo,
        Categories: results,
    }

    // 6. Phase 4: Analysis
    report.Summary.Resources = model.ComputeUSEMetrics(report)
    report.Summary.Anomalies = model.DetectAnomalies(report, model.DefaultThresholds())
    report.Summary.HealthScore = model.ComputeHealthScore(report.Summary.Resources, report.Summary.Anomalies)
    report.Summary.Recommendations = model.GenerateRecommendations(report)

    return report, nil
}
```

### Signal Handling Design

The signal handler goroutine is started **after** `context.WithTimeout()` to ensure it uses the final context. This prevents a race condition where the signal handler might reference a context that's subsequently wrapped.

### Parallel Collector Execution

```go
func (o *Orchestrator) runCollectors(ctx context.Context, cfg collector.CollectConfig) map[string][]model.Result {
    var wg sync.WaitGroup
    var mu sync.Mutex
    results := make(map[string][]model.Result)

    for _, c := range o.collectors {
        wg.Add(1)
        go func(c collector.Collector) {
            defer wg.Done()
            result, err := c.Collect(ctx, cfg)
            if err != nil { return }
            mu.Lock()
            results[c.Category()] = append(results[c.Category()], *result)
            mu.Unlock()
        }(c)
    }
    wg.Wait()
    return results
}
```

**Key concurrency pattern**: Each collector runs in its own goroutine. Results are protected by a mutex when appending to the shared map. The `WaitGroup` ensures all collectors complete before proceeding to analysis.

## Collection Profiles

```go
var Profiles = map[string]Profile{
    "quick": {
        Duration:       10 * time.Second,
        SampleInterval: 1 * time.Second,
        Timeout:        30 * time.Second,
        Focus:          []string{},       // No BCC tools
        BCCTools:       []string{},
    },
    "standard": {
        Duration:       30 * time.Second,
        SampleInterval: 1 * time.Second,
        Timeout:        90 * time.Second,
        Focus:          []string{},
        BCCTools:       []string{"runqlat", "biolatency", "tcpconnlat", "tcpretrans"},
    },
    "deep": {
        Duration:       60 * time.Second,
        SampleInterval: 1 * time.Second,
        Timeout:        180 * time.Second,
        Focus:          []string{"stacks", "all"},
        BCCTools:       []string{
            "runqlat", "runqlen", "biolatency", "biosnoop",
            "tcpconnlat", "tcpretrans", "tcprtt", "profile",
            "offcputime", "cachestat", "execsnoop",
        },
    },
}
```

## Execution Phases

```
Phase 1: Initialize     → Create collectors, apply profile
Phase 2: Collect         → Run all collectors in parallel
Phase 3: Assemble        → Categorize results into report structure
Phase 4: Analyze         → USE metrics, anomalies, health score, recommendations
Phase 5: Output          → JSON file or stdout
```

---

*Next: [Chapter 16 — Output Formats](16-output-formats.md)*
