# Chapter 5: Network Analysis — Deep Dive

## Overview

Network problems are notoriously hard to diagnose. Is it the application? The kernel? The network? Packet loss? Buffer exhaustion?

sysdiag's `NetworkCollector` (`internal/collector/network.go`) collects data from three sources: per-interface counters, TCP protocol statistics, and socket state summaries.

## Source File: network.go

- **Lines**: 195
- **Functions**: 5
- **Data Sources**: `/proc/net/dev`, `/proc/net/snmp`, `ss` command, `/proc/sys/net/`

## Function Walkthrough

### Collect() — Three Data Sources

```go
func (c *NetworkCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    data := &model.NetworkData{}
    data.Interfaces = c.parseNetDev()             // Interface stats
    data.TCP = c.parseSNMP()                      // TCP protocol stats
    c.parseSSConnections(ctx, data)                // Socket states
    // TCP sysctl parameters
    data.CongestionCtrl = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_congestion_control")
    data.TCPRmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_rmem")
    data.TCPWmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_wmem")
    data.SomaxConn = readSysctlInt(c.procRoot, "sys/net/core/somaxconn")
}
```

### parseNetDev() — Per-Interface Traffic

```go
func (c *NetworkCollector) parseNetDev() []model.NetworkInterface {
    // /proc/net/dev format:
    // Inter-|   Receive                                                |  Transmit
    //  face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets...
    //   eth0: 1234567  8901   0    0    0     0          0         0  9876543  6789...
    for scanner.Scan() {
        parts := strings.SplitN(line, ":", 2)
        name := strings.TrimSpace(parts[0])   // "eth0"
        fields := strings.Fields(parts[1])
        // RX fields (0-7): bytes, packets, errors, dropped, fifo, frame, compressed, multicast
        // TX fields (8-15): bytes, packets, errors, dropped, ...
        rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
        rxErrors, _ := strconv.ParseInt(fields[2], 10, 64)
        rxDropped, _ := strconv.ParseInt(fields[3], 10, 64)
        txBytes, _ := strconv.ParseInt(fields[8], 10, 64)
        txErrors, _ := strconv.ParseInt(fields[10], 10, 64)
        txDropped, _ := strconv.ParseInt(fields[11], 10, 64)
    }
}
```

**What errors and drops mean:**

| Counter | Cause |
|---------|-------|
| `rx_errors` | CRC errors, frame alignment errors — hardware/cable issue |
| `rx_dropped` | Kernel dropped packets — ring buffer full, no memory |
| `tx_errors` | Carrier errors, abort — cable/switch issue |
| `tx_dropped` | Queueing discipline dropped — traffic shaping or queue overflow |

**Rule**: Any non-zero error or drop counter warrants investigation.

### parseSNMP() — TCP Protocol Statistics

```go
func (c *NetworkCollector) parseSNMP() *model.TCPStats {
    // /proc/net/snmp has paired header/value lines:
    // Tcp: ... CurrEstab ActiveOpens PassiveOpens RetransSegs InErrs OutRsts ...
    // Tcp: ... 234       5678       9012        45          2      89      ...
    switch header {
    case "CurrEstab":   tcp.CurrEstab = v     // Currently open connections
    case "ActiveOpens":  tcp.ActiveOpens = v    // Client-initiated connections
    case "PassiveOpens": tcp.PassiveOpens = v   // Server-accepted connections
    case "RetransSegs":  tcp.RetransSegs = v    // Retransmissions
    case "InErrs":       tcp.InErrs = v         // Invalid segments received
    case "OutRsts":      tcp.OutRsts = v        // RSTs sent
    }
}
```

**Key TCP metrics:**

| Metric | Normal | Concern |
|--------|--------|---------|
| `RetransSegs` | < 0.1% of total segments | > 1% indicates packet loss or congestion |
| `InErrs` | 0 | Any value = corrupted in-flight packets |
| `OutRsts` | Low | High = connections being refused or reset |
| `ActiveOpens/PassiveOpens` ratio | Depends on role | Server should have more PassiveOpens |

### parseSSConnections() — Socket State Summary

```go
func (c *NetworkCollector) parseSSConnections(ctx context.Context, data *model.NetworkData) {
    // `ss -s` → summary with TIME_WAIT count
    // `ss -tn state close-wait` → CLOSE_WAIT connections (count lines - header)
    out, _ := exec.CommandContext(ctx, "ss", "-s").Output()
    // "TCP:   1234 (estab 56, closed 78, orphaned 0, timewait 90)"
    out2, _ := exec.CommandContext(ctx, "ss", "-tn", "state", "close-wait").Output()
    data.TCP.CloseWaitCount = len(lines) - 1  // subtract header
}
```

**TCP State Problems:**

```
                  Normal flow
    ┌──────────┐          ┌──────────┐
    │ESTABLISHED├──close──►│FIN_WAIT_1│
    └──────────┘          └────┬─────┘
                               │
                          ┌────▼─────┐
                          │TIME_WAIT │  ← stays 60 seconds
                          └──────────┘

                  Problem: leaked connection
    ┌──────────┐          ┌──────────┐
    │ESTABLISHED├──peer───►│CLOSE_WAIT│  ← application never closes!
    └──────────┘  closes  └──────────┘
```

| State | Count | Meaning |
|-------|-------|---------|
| TIME_WAIT < 1000 | Normal | Connections cooling down |
| TIME_WAIT > 10000 | High churn | Many short-lived connections — consider keep-alive |
| TIME_WAIT > 50000 | Port exhaustion risk | Ephemeral ports may run out |
| CLOSE_WAIT > 0 | Bug! | Application receives FIN but never closes the socket |
| CLOSE_WAIT > 100 | Critical bug | Connection leak — application must be fixed |

## Sysctl Parameters

| Parameter | Typical | Purpose |
|-----------|---------|---------|
| `tcp_congestion_control` | `cubic` | Congestion algorithm (bbr is better for lossy/WAN) |
| `tcp_rmem` | `4096 131072 6291456` | TCP receive buffer (min/default/max) |
| `tcp_wmem` | `4096 16384 4194304` | TCP send buffer (min/default/max) |
| `somaxconn` | 4096 | Maximum listen backlog (increase for high-connection servers) |

**Common tuning:**
- **High-throughput**: Increase `tcp_rmem`/`tcp_wmem` max to 16MB+
- **WAN optimization**: Switch to `bbr` congestion control
- **Web servers**: `somaxconn=65535` to avoid connection drops under load

## Diagnostic Examples

### Healthy Web Server
```json
{
  "interfaces": [
    {"name": "eth0", "rx_bytes": 5000000, "tx_bytes": 50000000, "rx_errors": 0, "tx_dropped": 0}
  ],
  "tcp": {
    "curr_estab": 500, "retrans_segs": 2, "time_wait_count": 200, "close_wait_count": 0
  },
  "congestion_ctrl": "bbr"
}
```
No errors, no drops, 200 TIME_WAIT (normal), zero CLOSE_WAIT, BBR enabled.

### Connection Leak
```json
{
  "tcp": {
    "curr_estab": 12000,
    "close_wait_count": 8500,
    "retrans_segs": 0
  }
}
```
8500 CLOSE_WAIT = massive connection leak. The application is not closing sockets after the remote side disconnects.

### Network Congestion
```json
{
  "interfaces": [
    {"name": "eth0", "rx_dropped": 45000, "rx_errors": 0}
  ],
  "tcp": {
    "retrans_segs": 12000,
    "time_wait_count": 55000
  },
  "congestion_ctrl": "cubic"
}
```
45K rx_dropped + 12K retransmits = ring buffer overflow under load. Increase ring buffer size and switch to BBR.

---

*Next: [Chapter 6 — Process Analysis](06-process-analysis.md)*
