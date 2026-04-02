# Chapter 21: Production Tuning Checklist

## Purpose

This is a quick-reference card for every sysctl and kernel parameter that melisai collects, analyzes, and recommends. Each item includes the parameter, its kernel default, the recommended production value, when to change it, and which melisai metric triggers the recommendation.

All `sysctl` changes are **temporary** by default. To persist across reboot, write them to `/etc/sysctl.d/99-melisai.conf` and run `sysctl -p /etc/sysctl.d/99-melisai.conf`.

---

## CPU Scheduler

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `kernel.sched_latency_ns` | 24000000 (24ms) | **6000000** (6ms) | Interactive/latency-sensitive workloads; when `runqlat_p99` is high | `runqlat_p99` |
| `kernel.sched_min_granularity_ns` | 3000000 (3ms) | **750000** (0.75ms) | Same as above; smaller granularity = fairer scheduling on busy CPUs | `runqlat_p99` |
| `kernel.sched_numa_balancing` | 0 | **1** | Multi-NUMA systems where processes migrate across nodes; reduces remote memory access | `cpu_utilization`, load imbalance across NUMA nodes |

**Notes:**
- `sched_latency_ns` controls how long the scheduler waits before preempting a task. Lower values improve responsiveness but increase context-switch overhead.
- `sched_min_granularity_ns` sets the minimum time slice. Setting it too low on throughput workloads wastes CPU on scheduling.
- `sched_numa_balancing` enables automatic page migration to the NUMA node where the accessing thread runs. Leave at 0 on single-socket machines.

---

## Memory Management

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `vm.swappiness` | 60 | **10** (databases), **30** (general prod) | When swap is being used and latency matters; databases should almost never swap | `swap_usage` |
| `vm.dirty_ratio` | 20 | **10** | When write stalls are observed; high dirty ratio means large writeback bursts | `memory_utilization`, dirty page analysis |
| `vm.dirty_background_ratio` | 10 | **5** | Ensures background writeback starts sooner, preventing dirty page accumulation | `memory_utilization` |
| `vm.overcommit_memory` | 0 (heuristic) | **2** (strict) | Production databases (PostgreSQL, Redis) where OOM kills are unacceptable | `memory_utilization` |
| `vm.min_free_kbytes` | varies (~67MB) | **131072** (128MB) | Systems with >16GB RAM; prevents direct reclaim stalls under burst allocation | `memory_utilization` |
| `vm.watermark_scale_factor` | 10 | **200** | When direct reclaim events are detected; widens the gap between free page watermarks | `memory_psi_pressure` |
| `vm.dirty_expire_centisecs` | 3000 (30s) | **1500** (15s) | Reduce how long dirty pages linger before being eligible for writeback | `memory_utilization` |
| `vm.dirty_writeback_centisecs` | 500 (5s) | **300** (3s) | How often the flusher thread wakes up; shorter = smoother I/O, slightly more overhead | `memory_utilization` |
| `vm.zone_reclaim_mode` | 0 | **0** (default) or **1** (NUMA-local) | Set to 1 only if your workload strictly requires NUMA-local allocation and can tolerate reclaim stalls; 0 is correct for most workloads | `memory_psi_pressure` |

### Transparent Huge Pages (THP)

| Setting | Default | Recommended | When to Change | melisai Metric |
|---------|---------|-------------|----------------|----------------|
| `/sys/kernel/mm/transparent_hugepage/enabled` | `always` | **`madvise`** | Latency-sensitive workloads (databases, JVMs); `always` causes compaction stalls | `memory_utilization`, THP status in report |
| `/sys/kernel/mm/transparent_hugepage/defrag` | `always` | **`defer+madvise`** | Same as above; `always` triggers synchronous compaction on page faults | `memory_psi_pressure` |

**Notes:**
- `overcommit_memory=2` requires `overcommit_ratio` to be set (typically 80-95%). Total commit limit = swap + RAM * ratio / 100.
- `min_free_kbytes` too high wastes memory; too low causes allocation stalls. Scale with total RAM.
- THP `madvise` mode lets applications opt in with `madvise(MADV_HUGEPAGE)` while avoiding surprise latency spikes.

---

## Network -- TCP Stack

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.ipv4.tcp_congestion_control` | cubic | **bbr** | Always on kernels >= 4.9; BBR handles bufferbloat and lossy networks better | `tcp_retransmits` |
| `net.core.default_qdisc` | pfifo_fast | **fq** | Required for BBR; Fair Queue provides pacing support | `tcp_retransmits` |
| `net.core.somaxconn` | 4096 | **65535** | High-traffic servers with listen queue overflows | `listen_overflows` |
| `net.ipv4.tcp_max_syn_backlog` | 1024 | **8192** | SYN flood protection or high connection rate | `listen_overflows` |
| `net.ipv4.tcp_rmem` | 4096 131072 6291456 | **4096 131072 16777216** | High-bandwidth or high-latency links; max buffer must cover BDP | `tcp_retransmits` |
| `net.ipv4.tcp_wmem` | 4096 16384 4194304 | **4096 131072 16777216** | Same as tcp_rmem; send buffers need to match BDP | `tcp_retransmits` |
| `net.core.rmem_max` | 212992 | **16777216** (16MB) | Caps the maximum receive buffer; must be >= tcp_rmem max | `tcp_retransmits` |
| `net.core.wmem_max` | 212992 | **16777216** (16MB) | Caps the maximum send buffer; must be >= tcp_wmem max | `tcp_retransmits` |
| `net.ipv4.ip_local_port_range` | 32768 60999 | **1024 65535** | High connection rate causes ephemeral port exhaustion | `tcp_timewait` |
| `net.ipv4.tcp_tw_reuse` | 2 | **1** | Enables reuse of TIME_WAIT sockets for new outgoing connections | `tcp_timewait` |
| `net.ipv4.tcp_fin_timeout` | 60 | **15** | Reduces how long sockets stay in FIN_WAIT_2; frees resources faster | `tcp_timewait` |
| `net.ipv4.tcp_slow_start_after_idle` | 1 | **0** | Persistent connections (HTTP/2, gRPC) should not reset cwnd after idle | `tcp_retransmits` |
| `net.ipv4.tcp_fastopen` | 1 | **3** | Enables TFO for both client (1) and server (2); saves 1 RTT on reconnects | `tcp_retransmits` |
| `net.ipv4.tcp_syncookies` | 1 | **1** (keep enabled) | SYN flood protection; should always be on in production | `listen_overflows` |
| `net.ipv4.tcp_notsent_lowat` | -1 (unlimited) | **131072** (128KB) | Reduces memory for apps with many idle connections (HTTP/2, websockets) | `memory_utilization` |
| `net.ipv4.tcp_mtu_probing` | 0 | **1** | Enables Path MTU discovery; avoids black-hole routers that drop ICMP | `tcp_retransmits` |

### TCP Keepalive

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.ipv4.tcp_keepalive_time` | 7200 (2h) | **300** (5min) | Detect dead connections faster; important behind load balancers | `tcp_close_wait` |
| `net.ipv4.tcp_keepalive_intvl` | 75 | **15** | Interval between keepalive probes after initial timeout | `tcp_close_wait` |
| `net.ipv4.tcp_keepalive_probes` | 9 | **5** | Number of unacknowledged probes before dropping connection | `tcp_close_wait` |

### TCP Memory

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.ipv4.tcp_mem` | auto-calculated | **Increase 2-4x** if PruneCalled > 0 | When the kernel starts pruning TCP receive queues due to memory pressure | `tcp_abort_on_memory` |

**Notes:**
- `tcp_mem` is in pages (not bytes). Three values: low / pressure / max. When usage exceeds "pressure", kernel starts pruning. Example: `1048576 2097152 4194304` (4-8-16 GB).
- Buffer tuning formula: `required_buffer = bandwidth_bps * RTT_seconds`. A 1 Gbps link with 100ms RTT needs 12.5 MB buffers.
- `tcp_tw_reuse=1` is safe for outgoing connections. Never use the removed `tcp_tw_recycle`.
- `tcp_fastopen=3` requires application support (`TCP_FASTOPEN` socket option).

---

## Network -- Packet Processing

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.core.netdev_budget` | 300 | **600** or higher | When `softnet_time_squeeze` > 0; NAPI poll budget exhausted | `softnet_time_squeeze` |
| `net.core.netdev_budget_usecs` | 2000 | **8000** | Same as above; allows more time per softirq cycle for packet processing | `softnet_time_squeeze` |
| `net.core.netdev_max_backlog` | 1000 | **10000** | When `softnet_dropped` > 0; per-CPU backlog queue overflow | `softnet_dropped` |

### Conntrack

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.netfilter.nf_conntrack_max` | 65536 | **Double current** if usage > 70% | Connection-tracking table nearing capacity; causes new connections to be dropped | `conntrack_usage_pct` |

### Neighbor Table (ARP/NDP)

| Parameter | Default | Recommended | When to Change | melisai Metric |
|-----------|---------|-------------|----------------|----------------|
| `net.ipv4.neigh.default.gc_thresh1` | 128 | **2048** | Kubernetes/large-subnet environments with many pods; prevents "neighbour table overflow" | `network_errors_per_sec` |
| `net.ipv4.neigh.default.gc_thresh2` | 512 | **4096** | Same; soft limit before garbage collection triggers | `network_errors_per_sec` |
| `net.ipv4.neigh.default.gc_thresh3` | 1024 | **8192** | Same; hard limit — entries above this are immediately rejected | `network_errors_per_sec` |

### NIC Hardware Tuning

| Setting | Default | Recommended | When to Change | melisai Metric |
|---------|---------|-------------|----------------|----------------|
| `ethtool -G <iface> rx <max>` | vendor default | **Maximum ring size** | When `rx_discards` or ring buffer drops are detected | `nic_rx_discards` |
| `ethtool -K <iface> tso on gro on` | usually on | **Ensure enabled** | TCP Segmentation Offload and Generic Receive Offload reduce CPU overhead | `cpu_utilization`, `softnet_time_squeeze` |

**Notes:**
- `netdev_budget` controls how many packets a CPU can process in one NAPI poll cycle. Increasing it trades latency fairness for throughput.
- Conntrack is loaded automatically when iptables/nftables NAT rules exist. On Kubernetes nodes, conntrack table can fill up fast. Monitor with `conntrack -C`.
- Neighbor table overflows produce `neighbour table overflow` in dmesg and cause intermittent connectivity failures.
- Verify ring buffer max with `ethtool -g <iface>`. Current values shown in melisai report under `nic_details`.

---

## Disk I/O

| Setting | Default | Recommended | When to Change | melisai Metric |
|---------|---------|-------------|----------------|----------------|
| I/O scheduler (SSD) | varies | **mq-deadline** | SSDs benefit from simple deadline scheduling over complex algorithms | `disk_avg_latency`, `biolatency_p99_ssd` |
| I/O scheduler (HDD) | varies | **bfq** | Spinning disks benefit from BFQ's fairness and latency guarantees | `disk_avg_latency`, `biolatency_p99_hdd` |
| `read_ahead_kb` | 128 | **Workload-dependent** | Increase for sequential reads (databases doing scans); decrease for random I/O | `disk_utilization` |

**Changing the I/O scheduler:**
```bash
# Check current scheduler (brackets show active):
cat /sys/block/sda/queue/scheduler
# [mq-deadline] none

# Change at runtime:
echo mq-deadline > /sys/block/sda/queue/scheduler

# Persist via udev rule:
# /etc/udev/rules.d/60-scheduler.rules
# ACTION=="add|change", KERNEL=="sd*", ATTR{queue/rotational}=="0", ATTR{queue/scheduler}="mq-deadline"
# ACTION=="add|change", KERNEL=="sd*", ATTR{queue/rotational}=="1", ATTR{queue/scheduler}="bfq"
```

**Changing read-ahead:**
```bash
# Check current:
cat /sys/block/sda/queue/read_ahead_kb

# Set to 256KB for sequential workload:
echo 256 > /sys/block/sda/queue/read_ahead_kb
```

---

## Validation After Tuning

After applying changes, run melisai to verify the impact:

```bash
# Before tuning — capture baseline:
sudo melisai --duration 30s -o before.json

# Apply tuning (see one-liner below)

# After tuning — capture comparison:
sudo melisai --duration 30s -o after.json

# Compare reports:
melisai diff before.json after.json
```

Key things to check in the diff:
- Health score should increase
- Previously triggered anomalies should disappear
- No new anomalies introduced by the changes

---

## One-Liner Tuning Script

The following script applies all recommended production settings. Review and adjust values to your workload before running.

```bash
#!/usr/bin/env bash
# melisai production tuning — apply with: sudo bash tune.sh
# Generated for melisai v0.4.1
set -euo pipefail

SYSCTL_CONF="/etc/sysctl.d/99-melisai.conf"

cat > "$SYSCTL_CONF" << 'SYSCTL'
# === CPU Scheduler ===
kernel.sched_latency_ns = 6000000
kernel.sched_min_granularity_ns = 750000
kernel.sched_numa_balancing = 1

# === Memory ===
vm.swappiness = 10
vm.dirty_ratio = 10
vm.dirty_background_ratio = 5
vm.overcommit_memory = 2
vm.min_free_kbytes = 131072
vm.watermark_scale_factor = 200
vm.dirty_expire_centisecs = 1500
vm.dirty_writeback_centisecs = 300
vm.zone_reclaim_mode = 0

# === Network: TCP Stack ===
net.ipv4.tcp_congestion_control = bbr
net.core.default_qdisc = fq
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.tcp_rmem = 4096 131072 16777216
net.ipv4.tcp_wmem = 4096 131072 16777216
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_notsent_lowat = 131072
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_intvl = 15
net.ipv4.tcp_keepalive_probes = 5

# === Network: Packet Processing ===
net.core.netdev_budget = 600
net.core.netdev_budget_usecs = 8000
net.core.netdev_max_backlog = 10000

# === Network: Neighbor Table (K8s / large subnets) ===
net.ipv4.neigh.default.gc_thresh1 = 2048
net.ipv4.neigh.default.gc_thresh2 = 4096
net.ipv4.neigh.default.gc_thresh3 = 8192
SYSCTL

echo "[1/5] Wrote sysctl config to $SYSCTL_CONF"

# Apply sysctl settings
sysctl -p "$SYSCTL_CONF"
echo "[2/5] Applied sysctl settings"

# THP: madvise mode
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
echo "defer+madvise" > /sys/kernel/mm/transparent_hugepage/defrag
echo "[3/5] Set THP to madvise, defrag to defer+madvise"

# NIC tuning: maximize ring buffers and enable offloads on all physical interfaces
for iface in /sys/class/net/*/device; do
    iface_name=$(basename "$(dirname "$iface")")
    rx_max=$(ethtool -g "$iface_name" 2>/dev/null | awk '/Pre-set maximums/,/Current/ { if (/RX:/) print $2 }' | head -1)
    if [[ -n "$rx_max" && "$rx_max" != "n/a" ]]; then
        ethtool -G "$iface_name" rx "$rx_max" 2>/dev/null || true
    fi
    ethtool -K "$iface_name" tso on gro on 2>/dev/null || true
done
echo "[4/5] Tuned NIC ring buffers and offloads"

# I/O scheduler: mq-deadline for SSD, bfq for HDD
for dev in /sys/block/sd* /sys/block/nvme*; do
    [[ -f "$dev/queue/rotational" ]] || continue
    rot=$(cat "$dev/queue/rotational")
    if [[ "$rot" == "0" ]]; then
        echo mq-deadline > "$dev/queue/scheduler" 2>/dev/null || true
    else
        echo bfq > "$dev/queue/scheduler" 2>/dev/null || true
    fi
done
echo "[5/5] Set I/O schedulers (mq-deadline for SSD, bfq for HDD)"

echo ""
echo "Tuning complete. Run 'sudo melisai --duration 30s' to verify."
echo "NOTE: vm.overcommit_memory=2 is set. Ensure overcommit_ratio is appropriate."
echo "NOTE: Review conntrack_max and tcp_mem separately — they depend on your workload."
```

### What the Script Does NOT Set

The following parameters require workload-specific values and are intentionally omitted:

| Parameter | Why Omitted |
|-----------|-------------|
| `net.netfilter.nf_conntrack_max` | Depends on current usage; melisai recommends doubling when >70% |
| `net.ipv4.tcp_mem` | Auto-calculated by kernel based on RAM; only increase if `PruneCalled` > 0 |
| `vm.overcommit_ratio` | Required when `overcommit_memory=2`; set to 80-95% based on workload |
| `read_ahead_kb` | Depends on workload pattern (sequential vs. random) |

---

## Quick Reference: melisai Anomaly to Sysctl Mapping

When melisai flags an anomaly, this table tells you which parameter to tune first:

| melisai Anomaly | First Parameter to Check |
|-----------------|--------------------------|
| `runqlat_p99` high | `kernel.sched_latency_ns`, `kernel.sched_min_granularity_ns` |
| `swap_usage` warning | `vm.swappiness`, check for memory leak |
| `memory_psi_pressure` | `vm.min_free_kbytes`, `vm.watermark_scale_factor`, THP settings |
| `tcp_retransmits` high | `net.ipv4.tcp_congestion_control=bbr`, buffer sizes |
| `tcp_timewait` high | `net.ipv4.tcp_tw_reuse`, `ip_local_port_range`, `tcp_fin_timeout` |
| `listen_overflows` | `net.core.somaxconn`, `tcp_max_syn_backlog` |
| `conntrack_usage_pct` high | `net.netfilter.nf_conntrack_max` |
| `softnet_dropped` | `net.core.netdev_max_backlog` |
| `softnet_time_squeeze` | `net.core.netdev_budget`, `netdev_budget_usecs` |
| `nic_rx_discards` | `ethtool -G <iface> rx <max>` |
| `tcp_close_wait` | `tcp_keepalive_time/intvl/probes` (and fix the application) |
| `tcp_abort_on_memory` | `net.ipv4.tcp_mem` |
| `irq_imbalance` | `irqbalance` service or manual `smp_affinity` |
| `udp_rcvbuf_errors` | `net.core.rmem_max`, application `SO_RCVBUF` |
| `disk_avg_latency` high | I/O scheduler, `read_ahead_kb` |
| `cpu_utilization` critical | Profile with `perf`/`flamegraph`; scheduler tuning is secondary |

---

## Persistence Checklist

After applying your tuning, verify these items survive a reboot:

- [ ] `/etc/sysctl.d/99-melisai.conf` exists and contains your settings
- [ ] `sysctl -p /etc/sysctl.d/99-melisai.conf` runs without errors
- [ ] THP settings in `/etc/rc.local` or a systemd unit (sysfs is not covered by sysctl)
- [ ] NIC ring buffer in `/etc/udev/rules.d/` or a networkd/ifup script
- [ ] I/O scheduler in `/etc/udev/rules.d/60-scheduler.rules`
- [ ] `irqbalance` is running: `systemctl enable --now irqbalance`

---

*This checklist covers all parameters that melisai v0.4.1 collects and analyzes.*
