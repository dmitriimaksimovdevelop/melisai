# Chapter 11: Anomaly Detection

## Overview

Collecting metrics is useful, but automatically flagging problems is better. melisai's anomaly detection engine (`internal/model/anomaly.go`) applies 29 threshold rules based on Brendan Gregg's recommended values.

## Source File: anomaly.go

- **Functions**: ~25 (thresholds + evaluators)

## How It Works

### 1. Define Thresholds

Each threshold has:
- **Metric**: Machine-readable identifier
- **Category**: Subsystem ("cpu", "memory", "disk", "network", "container")
- **Warning level**: Something to investigate
- **Critical level**: Immediate attention needed
- **Evaluator**: A function that extracts the metric from collected data

```go
type Threshold struct {
    Metric    string
    Category  string
    Warning   float64
    Critical  float64
    Evaluator func(report *Report) (float64, bool)
    Message   func(value float64) string
}
```

### 2. DetectAnomalies()

```go
func DetectAnomalies(report *Report, thresholds []Threshold) []Anomaly {
    for _, t := range thresholds {
        value, ok := t.Evaluator(report)
        if !ok { continue }
        if value >= t.Critical {
            anomalies = append(anomalies, Anomaly{Severity: "critical", ...})
        } else if value >= t.Warning {
            anomalies = append(anomalies, Anomaly{Severity: "warning", ...})
        }
    }
}
```

## The 24 Rules

### CPU (5 rules)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 1 | cpu_utilization | > 80% | > 95% | Near capacity |
| 2 | cpu_iowait | > 10% | > 30% | Waiting on I/O |
| 3 | load_average | > 2x CPUs | > 4x CPUs | Multiple tasks per core waiting |
| 4 | runqlat_p99 | > 10ms | > 50ms | BCC histogram — scheduler delay |
| 5 | cpu_psi_pressure | > 10% | > 25% | Tasks stalling for CPU |

### Memory (4 rules)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 6 | memory_utilization | > 85% | > 95% | Based on MemAvailable, not MemFree |
| 7 | swap_usage | > 10% | > 50% | Swap = memory thrashing |
| 8 | memory_psi_pressure | > 10% | > 25% | Tasks stalling for memory |
| 9 | cache_miss_ratio | > 50% | > 80% | BCC cachestat — poor cache usage |

### Disk (5 rules)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 10 | disk_utilization | > 70% | > 90% | I/O channel near saturation |
| 11 | disk_avg_latency | > 5ms | > 50ms | I/O latency too high |
| 12 | biolatency_p99_ssd | > 5ms | > 25ms | SSD latency outliers |
| 13 | biolatency_p99_hdd | > 50ms | > 200ms | HDD latency outliers |
| 14 | io_psi_pressure | > 10% | > 25% | Tasks stalling for I/O |

### Network (12 rules)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 15 | tcp_retransmits | > 10/s | > 50/s | Packet loss or congestion |
| 16 | tcp_timewait | > 5000 | > 20000 | Port exhaustion risk |
| 17 | network_errors_per_sec | > 1/s | > 100/s | Physical/driver issues |
| 18 | conntrack_usage_pct | > 70% | > 90% | Conntrack table approaching capacity |
| 19 | softnet_dropped | > 1 | > 10 | Kernel can't keep up with NIC rate |
| 20 | listen_overflows | > 1 | > 100 | Accept queue full — SYN drops |
| 21 | nic_rx_discards | > 100 | > 10000 | NIC ring buffer overflow |
| 22 | tcp_close_wait | > 1 | > 100 | Application not closing sockets |
| 23 | softnet_time_squeeze | > 1 | > 100 | NAPI budget exhausted |
| 24 | tcp_abort_on_memory | > 1 | > 10 | Connections killed by memory pressure |
| 25 | irq_imbalance | > 5x ratio | > 20x ratio | One CPU handling all NIC interrupts |
| 26 | udp_rcvbuf_errors | > 1 | > 100 | UDP receive buffer overflow |

### Container (2 rules)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 27 | cpu_throttling | > 100 | > 1000 | Cgroup CPU limit hit |
| 28 | container_memory_usage | > 80% | > 95% | Container memory limit proximity |

### Other (1 rule)

| # | Metric | Warning | Critical | Why |
|---|--------|---------|----------|-----|
| 29 | dns_latency_p99 | > 100ms | > 500ms | BCC gethostlatency — slow DNS |

## Health Score

The health score (0-100) is calculated from detected anomalies with category weights:

- **CPU**: 1.5x weight
- **Memory**: 1.5x weight
- **Disk**: 1.0x weight
- **Network**: 1.0x weight
- **Container**: 1.0x weight

Critical anomaly = -20 points, Warning = -10 points (scaled by weight).

---

*Next: [Chapter 12 — Recommendations Engine](12-recommendations.md)*
