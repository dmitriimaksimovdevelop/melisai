# Chapter 12: Recommendations Engine

## Overview

After detecting anomalies, melisai goes further: it suggests specific commands to fix common issues. The recommendations engine (`internal/model/recommendations.go`) generates actionable tuning advice based on collected metrics.

## Recommendation Structure

```go
type Recommendation struct {
    Priority       int      // Execution order
    Category       string   // "cpu", "memory", "network", "disk"
    Title          string   // Short description
    Commands       []string // Exact commands to run (immediate effect)
    Persistent     []string // Commands to survive reboot
    ExpectedImpact string   // What improvement to expect
    Evidence       string   // Metric values that triggered this
    Source         string   // Reference (Gregg, kernel docs, etc.)
}
```

## Network Recommendations

### Enable TCP BBR
```go
if net.CongestionCtrl != "bbr" {
    // "Enable BBR congestion control"
    // Commands: sysctl -w net.core.default_qdisc=fq
    //           sysctl -w net.ipv4.tcp_congestion_control=bbr
}
```

### Increase Listen Backlog
```go
if net.SomaxConn < 4096 {
    // "Increase TCP listen backlog for high-traffic servers"
    // Commands: sysctl -w net.core.somaxconn=4096
}
```

### Conntrack Table Pressure
```go
if net.Conntrack != nil && net.Conntrack.UsagePct > 70 {
    // "Conntrack table approaching capacity"
    // Commands: sysctl -w net.netfilter.nf_conntrack_max=<current*2>
}
```

### Ring Buffer Overflow
```go
if iface.RingRxCur < iface.RingRxMax/2 && iface.RxDiscards > 0 {
    // "Increase ring buffer on <iface> (rx_discards detected)"
    // Commands: ethtool -G <iface> rx <max>
}
```

### Listen Queue Overflows
```go
if net.ListenOverflows > 0 {
    // "Listen queue overflows detected — increase backlog and enable SO_REUSEPORT"
    // Commands: sysctl -w net.core.somaxconn=65535
}
```

### TCP Memory Pressure
```go
if net.PruneCalled > 0 {
    // "TCP memory pressure detected (PruneCalled) — increase tcp_mem"
    // Commands: sysctl -w net.ipv4.tcp_mem='1048576 2097152 4194304'
}
```

### TCP Buffer Tuning

When RTT is high (cross-region), larger TCP buffers are needed for full throughput:
```
Bandwidth × RTT = required buffer size
1 Gbps × 100ms = 12.5 MB buffer needed
```

## Memory Recommendations

### Swappiness for Databases
```go
if mem.Swappiness > 30 && mem.SwapUsedBytes > 0 {
    // "Reduce swappiness for database/latency-sensitive workloads"
    // Commands: sysctl -w vm.swappiness=10
}
```

### Dirty Page Ratio
```go
if mem.DirtyRatio > 20 {
    // "Lower dirty_ratio to prevent write stalls"
    // Commands: sysctl -w vm.dirty_ratio=10
}
```

### THP Latency
```go
if mem.THPEnabled == "always" {
    // "Consider disabling THP for latency-sensitive workloads"
    // Commands: echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
}
```

## Disk Recommendations

### SSD Scheduler
```go
if !dev.Rotational && dev.Scheduler != "mq-deadline" {
    // "Switch <dev> to mq-deadline scheduler (SSD)"
}
```

## Persistence

All `sysctl` changes are temporary — they reset on reboot. Each recommendation includes `Persistent` commands:

```bash
# Temporary (immediate effect):
sysctl -w net.ipv4.tcp_congestion_control=bbr

# Permanent (survives reboot):
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.d/99-melisai.conf
sysctl -p /etc/sysctl.d/99-melisai.conf
```

---

*Next: [Chapter 13 — AI Integration](13-ai-integration.md)*
