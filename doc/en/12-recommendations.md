# Chapter 12: Recommendations Engine

## Overview

After detecting anomalies, melisai goes further: it suggests specific `sysctl` commands to fix common issues. The recommendations engine (`internal/model/recommendations.go`) generates actionable tuning advice based on collected metrics.

## How It Works

```go
func GenerateRecommendations(report *Report) []Recommendation {
    var recs []Recommendation

    // Check each category and generate relevant suggestions
    if cpu, ok := getCPUData(report); ok {
        recs = append(recs, cpuRecommendations(cpu)...)
    }
    if mem, ok := getMemData(report); ok {
        recs = append(recs, memoryRecommendations(mem)...)
    }
    if net, ok := getNetData(report); ok {
        recs = append(recs, networkRecommendations(net)...)
    }
    return recs
}
```

## Recommendation Structure

```go
type Recommendation struct {
    Title    string   // Short description
    Category string   // "cpu", "memory", "network"
    Impact   string   // "high", "medium", "low"
    Reason   string   // Why this recommendation applies
    Commands []string // Exact sysctl/system commands to run
    Persist  string   // How to make the change permanent
}
```

## Network Recommendations

### Enable TCP BBR

```go
if net.CongestionCtrl == "cubic" {
    recs = append(recs, Recommendation{
        Title:    "Enable TCP BBR congestion control",
        Impact:   "high",
        Reason:   "BBR achieves higher throughput with lower latency on lossy networks",
        Commands: []string{
            "sysctl -w net.core.default_qdisc=fq",
            "sysctl -w net.ipv4.tcp_congestion_control=bbr",
        },
        Persist: "echo 'net.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.d/99-bbr.conf",
    })
}
```

### Increase Listen Backlog

```go
if net.SomaxConn < 4096 {
    recs = append(recs, Recommendation{
        Title:  "Increase TCP listen backlog",
        Reason: fmt.Sprintf("somaxconn=%d may cause connection drops under load", net.SomaxConn),
        Commands: []string{"sysctl -w net.core.somaxconn=65535"},
    })
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
if mem.Swappiness > 10 {
    recs = append(recs, Recommendation{
        Title:   "Reduce swappiness for database workloads",
        Reason:  fmt.Sprintf("vm.swappiness=%d; databases manage their own cache", mem.Swappiness),
        Commands: []string{"sysctl -w vm.swappiness=1"},
    })
}
```

### Dirty Page Ratio

```go
if mem.DirtyRatio > 20 {
    recs = append(recs, Recommendation{
        Title:   "Reduce dirty page ratio for faster write-back",
        Commands: []string{
            "sysctl -w vm.dirty_ratio=5",
            "sysctl -w vm.dirty_background_ratio=2",
        },
    })
}
```

## Persistence

All `sysctl` changes are temporary — they reset on reboot. Each recommendation includes a `Persist` field showing how to make it permanent:

```bash
# Temporary (immediate effect):
sysctl -w net.ipv4.tcp_congestion_control=bbr

# Permanent (survives reboot):
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.d/99-melisai.conf
sysctl -p /etc/sysctl.d/99-melisai.conf
```

---

*Next: [Chapter 13 — AI Integration](13-ai-integration.md)*
