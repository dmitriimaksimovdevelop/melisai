# Chapter 11: Anomaly Detection

## Overview

Collecting metrics is useful, but automatically flagging problems is better. melisai's anomaly detection engine (`internal/model/anomaly.go`) applies 11 threshold rules based on Brendan Gregg's recommended values.

## Source File: anomaly.go

- **Lines**: 260
- **Functions**: ~15 (thresholds + evaluators)

## How It Works

### 1. Define Thresholds

Each threshold has:
- **Name**: Human-readable description
- **Warning level**: Something to investigate
- **Critical level**: Immediate attention needed
- **Evaluator**: A function that extracts the metric from collected data

```go
type Threshold struct {
    Name     string
    Category string  // "cpu", "memory", "disk", "network"
    Warning  float64
    Critical float64
    Unit     string  // "percent", "count", "ms"
    Evaluate func(report *Report) float64
}
```

### 2. Default Thresholds

```go
func DefaultThresholds() []Threshold {
    return []Threshold{
        // CPU
        {Name: "CPU utilization",     Warning: 80,  Critical: 95, Unit: "percent"},
        {Name: "CPU saturation",      Warning: 50,  Critical: 100, Unit: "percent"},
        {Name: "Load per CPU",        Warning: 1.5, Critical: 3.0, Unit: "ratio"},

        // Memory
        {Name: "Memory utilization",  Warning: 85,  Critical: 95, Unit: "percent"},
        {Name: "Swap usage",          Warning: 20,  Critical: 50, Unit: "percent"},
        {Name: "Memory PSI (some)",   Warning: 10,  Critical: 25, Unit: "percent"},

        // Disk
        {Name: "Disk utilization",    Warning: 80,  Critical: 95, Unit: "percent"},
        {Name: "Disk saturation",     Warning: 8,   Critical: 32, Unit: "queue_depth"},

        // Network
        {Name: "TCP retransmissions", Warning: 1,   Critical: 5, Unit: "percent"},
        {Name: "CLOSE_WAIT count",    Warning: 1,   Critical: 100, Unit: "count"},
        {Name: "Interface errors",    Warning: 1,   Critical: 100, Unit: "count"},
    }
}
```

### 3. Evaluator Functions

Each threshold has an evaluator that extracts the relevant metric:

```go
// CPU utilization evaluator
func(report *Report) float64 {
    for _, r := range report.Categories["cpu"] {
        if cpu, ok := r.Data.(*CPUData); ok {
            return 100 - cpu.IdlePct
        }
    }
    return 0
}

// CLOSE_WAIT evaluator
func(report *Report) float64 {
    for _, r := range report.Categories["network"] {
        if net, ok := r.Data.(*NetworkData); ok && net.TCP != nil {
            return float64(net.TCP.CloseWaitCount)
        }
    }
    return 0
}
```

### 4. DetectAnomalies()

```go
func DetectAnomalies(report *Report, thresholds []Threshold) []Anomaly {
    var anomalies []Anomaly
    for _, t := range thresholds {
        value := t.Evaluate(report)
        if value >= t.Critical {
            anomalies = append(anomalies, Anomaly{
                Severity: "critical",
                Category: t.Category,
                Message:  fmt.Sprintf("%s at %.1f%s (critical: >%.1f%s)",
                    t.Name, value, t.Unit, t.Critical, t.Unit),
                Value:    value,
            })
        } else if value >= t.Warning {
            anomalies = append(anomalies, Anomaly{
                Severity: "warning",
                ...
            })
        }
    }
    return anomalies
}
```

## The 11 Rules

| # | Rule | Warning | Critical | Why |
|---|------|---------|----------|-----|
| 1 | CPU utilization | > 80% | > 95% | Near capacity |
| 2 | CPU saturation | > 50% | > 100% | Work queuing (load/CPUs ratio) |
| 3 | Load per CPU | > 1.5 | > 3.0 | Multiple tasks per core waiting |
| 4 | Memory utilization | > 85% | > 95% | Based on MemAvailable, not MemFree |
| 5 | Swap usage | > 20% | > 50% | Swap = memory thrashing |
| 6 | Memory PSI (some) | > 10% | > 25% | Tasks stalling for memory |
| 7 | Disk utilization | > 80% | > 95% | I/O channel near saturation |
| 8 | Disk queue depth | > 8 | > 32 | I/O requests backing up |
| 9 | TCP retransmissions | > 1% | > 5% | Network packet loss |
| 10 | CLOSE_WAIT > 0 | > 0 | > 100 | Application connection leak |
| 11 | Interface errors | > 0 | > 100 | Physical/driver issues |

---

*Next: [Chapter 12 — Recommendations Engine](12-recommendations.md)*
