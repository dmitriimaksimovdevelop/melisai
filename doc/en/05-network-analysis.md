# Chapter 5: Network Analysis — Deep Dive

## Overview

Network problems are notoriously hard to diagnose. Is it the application? The kernel? The network? Packet loss? Buffer exhaustion?

melisai's `NetworkCollector` (`internal/collector/network.go`) collects data from multiple sources: per-interface counters, TCP protocol statistics, socket state summaries, conntrack table stats, softnet per-CPU counters, IRQ distribution, NIC hardware details, and TCP extended stats.

## Source File: network.go

- **Lines**: ~520
- **Functions**: 12
- **Data Sources**: `/proc/net/dev`, `/proc/net/snmp`, `/proc/net/netstat`, `/proc/net/softnet_stat`, `/proc/softirqs`, `/proc/sys/net/`, `/sys/class/net/`, `ss`, `ethtool`

## Function Walkthrough

### Collect() — Two-Point Sampling + Deep Diagnostics

The collector uses two-point sampling: it reads counters before and after a configurable interval to compute rates (errors/sec, retransmits/sec, IRQ deltas).

```go
func (c *NetworkCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    // Phase 1: first sample
    ifaces1 := c.parseNetDev()        // /proc/net/dev
    snmp1 := c.parseSNMP()            // /proc/net/snmp
    irqSample1 := c.readNetRxSoftirqs() // /proc/softirqs

    // Wait for interval (default 1s)
    time.After(interval)

    // Phase 2: second sample + derived rates
    data.Interfaces = c.parseNetDev()
    data.TCP = c.parseSNMP()
    c.parseSSConnections(ctx, data)    // ss command

    // Deep diagnostics (all Tier 1 — no root needed for procfs)
    data.Conntrack = c.parseConntrack()
    data.SoftnetStats = c.parseSoftnetStat()
    data.IRQDistribution = c.computeIRQDistribution(irqSample1)
    c.parseNetstat(data)               // /proc/net/netstat
    c.enrichNICDetails(ctx, data)      // sysfs + ethtool
}
```

### parseNetDev() — Per-Interface Traffic

```go
// /proc/net/dev format:
// Inter-|   Receive                                                |  Transmit
//  face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets...
//   eth0: 1234567  8901   0    0    0     0          0         0  9876543  6789...
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
// /proc/net/snmp has paired header/value lines:
// Tcp: ... CurrEstab ActiveOpens PassiveOpens RetransSegs InErrs OutRsts ...
// Tcp: ... 234       5678       9012        45          2      89      ...
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
// `ss -s` → summary with TIME_WAIT count
// `ss -tn state close-wait` → CLOSE_WAIT connections (count lines - header)
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

## Deep Network Diagnostics

### parseConntrack() — Connection Tracking Table

Reads conntrack table usage from `/proc/sys/net/netfilter/`:

```go
type ConntrackStats struct {
    Count        int64   // current entries
    Max          int64   // nf_conntrack_max
    UsagePct     float64 // count/max * 100
    Drops        int64   // dropped due to full table
    InsertFailed int64   // failed to insert new entry
    EarlyDrop    int64   // entries dropped early to make room
}
```

| Metric | Warning | Critical | Meaning |
|--------|---------|----------|---------|
| UsagePct > 70% | Yes | > 90% | Table approaching capacity — new connections will be dropped |
| Drops > 0 | Yes | Yes | Connections already being dropped |

**Fix**: `sysctl -w net.netfilter.nf_conntrack_max=<current*2>`

### parseSoftnetStat() — Per-CPU Packet Processing

Reads `/proc/net/softnet_stat` — hex columns per CPU line:

```
00beef02 00000002 00000005 ...   ← CPU 0
0000abcd 00000000 00000003 ...   ← CPU 1
```

| Column | Name | Meaning |
|--------|------|---------|
| 0 | processed | Total packets processed by this CPU |
| 1 | dropped | Packets dropped (softirq couldn't keep up) |
| 2 | time_squeeze | Times softirq budget ran out |

**Any non-zero `dropped` = kernel is losing packets.** Causes:
- Single CPU handling all NIC interrupts (no RPS/RSS)
- `net.core.netdev_budget` too low (default 300)
- IRQ affinity pinning all interrupts to one core

### computeIRQDistribution() — NET_RX Softirq Delta

Two-point sampling of `/proc/softirqs` NET_RX line to show per-CPU interrupt processing rate:

```go
type IRQDistribution struct {
    CPU        int   // CPU number
    NetRxDelta int64 // NET_RX interrupts processed during sample interval
}
```

**What to look for**: If one CPU has 10x the delta of others, that CPU is the NIC interrupt bottleneck. Fix with IRQ affinity or enable RPS.

### parseNetstat() — TCP Extended Counters

Reads `/proc/net/netstat` `TcpExt:` section for production-critical counters:

| Counter | Meaning | Action |
|---------|---------|--------|
| `ListenOverflows` | Accept queue full — SYN dropped | Increase somaxconn, add SO_REUSEPORT |
| `ListenDrops` | Same as overflows but includes other causes | Check application accept() rate |
| `TCPAbortOnMemory` | Connection aborted due to memory pressure | Increase tcp_mem |
| `PruneCalled` | Kernel pruned TCP receive buffers | Increase tcp_mem limits |
| `TCPOFOQueue` | Out-of-order packets queued | Network reordering or congestion |

### enrichNICDetails() — Hardware-Level Info

Uses sysfs and `ethtool` to gather NIC hardware details per interface:

| Source | Field | What it tells you |
|--------|-------|-------------------|
| `/sys/class/net/<iface>/speed` | Speed | Link speed (1000Mbps, 10000Mbps) |
| `/sys/class/net/<iface>/queues/` | RxQueues, TxQueues | Number of hardware queues |
| `/sys/class/net/<iface>/queues/rx-0/rps_cpus` | RPSEnabled | Whether RPS distributes packets across CPUs |
| `/sys/class/net/<iface>/master` | BondSlave | Whether this NIC is part of a bond |
| `ethtool -i` | Driver | NIC driver name (e.g., ixgbe, mlx5_core) |
| `ethtool -g` | RingRxCur, RingRxMax | Current/max ring buffer size |
| `ethtool -S` | RxDiscards, RxBufErrors | NIC-level packet drops |

**Ring buffer overflow** (`RxDiscards > 0` with `RingRxCur < RingRxMax`):
```bash
# Increase ring buffer to max
ethtool -G eth0 rx 4096
```

## Sysctl Parameters

| Parameter | Typical | Purpose |
|-----------|---------|---------|
| `tcp_congestion_control` | `cubic` | Congestion algorithm (bbr is better for lossy/WAN) |
| `tcp_rmem` | `4096 131072 6291456` | TCP receive buffer (min/default/max) |
| `tcp_wmem` | `4096 16384 4194304` | TCP send buffer (min/default/max) |
| `somaxconn` | 4096 | Maximum listen backlog (increase for high-connection servers) |
| `tcp_mem` | `pages pages pages` | Global TCP memory limits (low/pressure/high) |
| `tcp_max_tw_buckets` | 65536 | Max TIME_WAIT sockets |
| `tcp_keepalive_time` | 7200 | Seconds before keepalive probes |
| `netdev_budget` | 300 | Max packets processed per softirq cycle |

**Common tuning:**
- **High-throughput**: Increase `tcp_rmem`/`tcp_wmem` max to 16MB+
- **WAN optimization**: Switch to `bbr` congestion control
- **Web servers**: `somaxconn=65535` to avoid connection drops under load
- **High PPS**: Increase `netdev_budget` to 4096+, enable RPS

## Anomaly Detection Rules (Network)

| Rule | Warning | Critical | Source |
|------|---------|----------|--------|
| tcp_retransmits | 10/s | 50/s | /proc/net/snmp |
| tcp_timewait | 5000 | 20000 | ss |
| network_errors_per_sec | 1/s | 100/s | /proc/net/dev |
| conntrack_usage_pct | 70% | 90% | /proc/sys/net/netfilter/ |
| softnet_dropped | 1 | 10 | /proc/net/softnet_stat |
| listen_overflows | 1 | 100 | /proc/net/netstat |
| nic_rx_discards | 100 | 10000 | ethtool -S |

## Diagnostic Examples

### Healthy Web Server
```json
{
  "interfaces": [
    {"name": "eth0", "rx_bytes": 5000000, "tx_bytes": 50000000,
     "rx_errors": 0, "tx_dropped": 0, "driver": "virtio_net",
     "rx_queues": 4, "ring_rx_current": 256, "ring_rx_max": 256}
  ],
  "tcp": {
    "curr_estab": 500, "retrans_segs": 2, "time_wait_count": 200, "close_wait_count": 0
  },
  "congestion_ctrl": "bbr",
  "conntrack": {"count": 500, "max": 65536, "usage_pct": 0.76},
  "listen_overflows": 0,
  "softnet_stats": [
    {"cpu": 0, "processed": 50000, "dropped": 0, "time_squeeze": 0}
  ]
}
```
No errors, no drops, low conntrack usage, zero ListenOverflows, no softnet drops.

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

### NIC Ring Buffer Overflow
```json
{
  "interfaces": [
    {"name": "eth0", "rx_dropped": 45000, "driver": "ixgbe",
     "ring_rx_current": 256, "ring_rx_max": 4096, "rx_discards": 12000}
  ],
  "softnet_stats": [
    {"cpu": 0, "processed": 5000000, "dropped": 200, "time_squeeze": 50}
  ]
}
```
NIC drops 12K packets at hardware level (ring buffer at 256/4096). Fix: `ethtool -G eth0 rx 4096`

### Conntrack Table Full
```json
{
  "conntrack": {"count": 61000, "max": 65536, "usage_pct": 93.1, "drops": 150}
}
```
Table at 93% with active drops. Fix: `sysctl -w net.netfilter.nf_conntrack_max=131072`

---

*Next: [Chapter 6 — Process Analysis](06-process-analysis.md)*
