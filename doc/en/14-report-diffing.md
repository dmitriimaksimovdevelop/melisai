# Chapter 14: Report Diffing

## Overview

Comparing two reports answers: "Did my tuning improve things?" or "Is today's performance worse than yesterday?"

melisai's `diff` package (`internal/diff/diff.go`) compares USE metrics, histograms, and anomalies between a baseline and current report.

## Source File: diff.go

- **Lines**: ~150
- **Purpose**: Structured comparison of two melisai reports

## Diff Structure

```go
type DiffReport struct {
    Baseline    *Report             // Before
    Current     *Report             // After
    USEDeltas   map[string]USEDelta // Per-resource USE metric changes
    Histograms  []HistogramDiff     // Latency distribution changes
    NewAnomalies    []Anomaly       // Anomalies in current but not baseline
    ResolvedAnomalies []Anomaly    // Anomalies in baseline but not current
    Regressions []Regression        // Metrics that got significantly worse
    Improvements []Improvement      // Metrics that got significantly better
}
```

## USE Metric Comparison

```go
type USEDelta struct {
    Resource       string  // "cpu", "memory", "disk", "network"
    UtilizationOld float64
    UtilizationNew float64
    UtilizationDelta float64 // New - Old (positive = worse)
    SaturationDelta  float64
    ErrorsDelta      int
    Significance     string // "improvement", "regression", "stable"
}
```

Significance thresholds:
- **Stable**: delta < 5% absolute change
- **Improvement**: delta < -5% (metrics decreased)
- **Regression**: delta > 5% (metrics increased)

## Histogram P99 Comparison

```go
type HistogramDiff struct {
    Name    string
    P99Old  float64
    P99New  float64
    P99Delta float64
    P99PctChange float64 // percentage change
}
```

Example:
```
Histogram: biolatency
  p99 baseline:  850 μs
  p99 current:   2300 μs
  Δ:             +1450 μs (+170.6%)  ← REGRESSION
```

## Example Diff Output

```json
{
  "use_deltas": {
    "cpu": {
      "utilization_old": 45.2,
      "utilization_new": 38.7,
      "utilization_delta": -6.5,
      "significance": "improvement"
    },
    "disk": {
      "utilization_old": 30.0,
      "utilization_new": 78.5,
      "utilization_delta": 48.5,
      "significance": "regression"
    }
  },
  "new_anomalies": [
    {"severity": "warning", "message": "Disk utilization at 78.5%"}
  ],
  "resolved_anomalies": [
    {"severity": "warning", "message": "Swap usage at 22%"}
  ]
}
```

## Usage

```bash
melisai diff baseline.json current.json
melisai diff baseline.json current.json --json    # structured output
```

---

*Next: [Chapter 15 — Orchestrator](15-orchestrator.md)*
