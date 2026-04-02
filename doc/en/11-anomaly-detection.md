# Chapter 11: Anomaly Detection

## Overview

melisai's anomaly detection engine (`internal/model/anomaly.go`) applies **37 threshold rules** based on Brendan Gregg's recommended values and production best practices. Each rule evaluates a specific metric from collected data and flags it as `warning` or `critical`.

All rate-based rules use two-point sampling (delta/interval) to detect issues happening *right now*, not historical cumulative counters.

## How It Works

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

Each threshold has an evaluator function that extracts the metric from the report and returns `(value, found)`. If `found` is true and `value >= Critical`, a critical anomaly is created. If `value >= Warning`, a warning.

## The 37 Rules

### CPU (5 rules)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 1 | cpu_utilization | > 80% | > 95% | /proc/stat (delta) |
| 2 | cpu_iowait | > 10% | > 30% | /proc/stat (delta) |
| 3 | load_average | > 2x CPUs | > 4x CPUs | /proc/loadavg |
| 4 | runqlat_p99 | > 10ms | > 50ms | BCC runqlat histogram |
| 5 | cpu_psi_pressure | > 5% | > 25% | /proc/pressure/cpu |

### Memory (8 rules)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 6 | memory_utilization | > 85% | > 95% | /proc/meminfo MemAvailable |
| 7 | swap_usage | > 10% | > 50% | /proc/meminfo |
| 8 | memory_psi_pressure | > 5% | > 25% | /proc/pressure/memory |
| 9 | cache_miss_ratio | > 5% | > 15% | BCC cachestat |
| 10 | direct_reclaim_rate | > 10/s | > 1000/s | /proc/vmstat pgscan_direct (rate) |
| 11 | compaction_stall_rate | > 1/s | > 100/s | /proc/vmstat compact_stall (rate) |
| 12 | thp_split_rate | > 1/s | > 100/s | /proc/vmstat thp_split_page (rate) |
| 13 | numa_miss_ratio | > 5% | > 20% | /sys/devices/system/node numastat |

### Disk (5 rules)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 14 | disk_utilization | > 70% | > 90% | /proc/diskstats |
| 15 | disk_avg_latency | > 5ms | > 50ms | /proc/diskstats |
| 16 | biolatency_p99_ssd | > 5ms | > 25ms | BCC biolatency |
| 17 | biolatency_p99_hdd | > 50ms | > 200ms | BCC biolatency |
| 18 | io_psi_pressure | > 10% | > 50% | /proc/pressure/io |

### Network (15 rules)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 19 | tcp_retransmits | > 10/s | > 50/s | /proc/net/snmp (rate) |
| 20 | tcp_timewait | > 5000 | > 20000 | ss |
| 21 | network_errors_per_sec | > 10/s | > 100/s | /proc/net/dev (rate) |
| 22 | conntrack_usage_pct | > 70% | > 90% | /proc/sys/net/netfilter |
| 23 | softnet_dropped | > 1/s | > 100/s | /proc/net/softnet_stat (rate) |
| 24 | listen_overflows | > 1/s | > 100/s | /proc/net/netstat (rate) |
| 25 | nic_rx_discards | > 100 | > 10000 | ethtool -S |
| 26 | tcp_close_wait | > 1 | > 100 | ss (current state) |
| 27 | softnet_time_squeeze | > 1/s | > 100/s | /proc/net/softnet_stat (rate) |
| 28 | tcp_abort_on_memory | > 0.1/s | > 1/s | /proc/net/netstat (rate) |
| 29 | irq_imbalance | > 5x ratio | > 20x ratio | /proc/softirqs (rate delta) |
| 30 | udp_rcvbuf_errors | > 1/s | > 100/s | /proc/net/snmp (rate) |
| 31 | tcp_rcvq_drop | > 1/s | > 100/s | /proc/net/netstat (rate) |
| 32 | tcp_zero_window_drop | > 1/s | > 50/s | /proc/net/netstat (rate) |
| 33 | listen_queue_saturation | > 70% fill | > 90% fill | ss -tnl Recv-Q/Send-Q |

### Container (2 rules)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 34 | cpu_throttling | > 100 periods | > 1000 periods | cgroup cpu.stat |
| 35 | container_memory_usage | > 80% | > 95% | cgroup memory.current/max |

### System (1 rule)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 36 | gpu_nic_cross_numa | > 1 pair | > 1 pair | sysfs PCI NUMA node |

### Other (1 rule)

| # | Metric | Warning | Critical | Source |
|---|--------|---------|----------|--------|
| 37 | dns_latency_p99 | > 50ms | > 200ms | BCC gethostlatency |

## Rate-Based Detection

Rules marked `(rate)` use two-point sampling: the collector reads the counter before and after a 1-second interval, computes `delta / seconds`. This eliminates false positives from cumulative counters on long-uptime systems.

Rate-based rules: softnet_dropped, softnet_time_squeeze, listen_overflows, tcp_abort_on_memory, udp_rcvbuf_errors, tcp_rcvq_drop, tcp_zero_window_drop, direct_reclaim_rate, compaction_stall_rate, thp_split_rate.

## Health Score

The health score (0-100) combines USE metric deductions and anomaly deductions:

**USE deductions** (weighted by resource importance):
- CPU: 1.5x, Memory: 1.5x, Disk: 1.0x, Network: 1.0x, Container: 1.2x
- Utilization >= 95% → -15*weight; >= 85% → -8*weight; >= 70% → -3*weight
- Saturation > 50% → -15*weight; > 10% → -8*weight; > 1% → -3*weight
- Errors > 1000 → -10*weight; > 100 → -5*weight; > 0 → -2*weight

**Anomaly deductions** (flat, not weighted):
- Critical anomaly = **-10 points**
- Warning anomaly = **-5 points**

Score clamped to [0, 100].

---

*Next: [Chapter 12 — Recommendations Engine](12-recommendations.md)*
