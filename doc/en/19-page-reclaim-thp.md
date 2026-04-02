# Chapter 19: Page Reclaim, Compaction & Transparent Huge Pages

## The Page Reclaim Problem

Linux doesn't keep memory idle. Every free page becomes file cache, slab cache, or
anonymous memory. Unused RAM is wasted RAM. But when an application requests memory
and there are no free pages, the kernel must *reclaim* existing pages before the
allocation can succeed.

Modern NVMe storage makes this worse. A single NVMe drive pushes 7 GB/s and millions
of IOPS. Applications allocate at rates that overwhelm background reclaim. The
allocating thread stalls while the kernel scrambles to free pages synchronously.
This is **direct reclaim** — the single biggest source of unexplained latency
spikes on memory-pressured systems.

Add Transparent Huge Pages and it gets worse. THP needs 2 MB of *contiguous*
physical memory. When memory is fragmented, the kernel compacts pages to create
contiguous blocks. Compaction is expensive, non-deterministic, and happens in
the allocation path. A 4 KB allocation at 100 ns can become a 2 MB THP allocation
at 10 ms — a 100,000x latency increase.

melisai measures all three subsystems — reclaim, compaction, THP — using two-point
sampling of `/proc/vmstat` counters.

## Memory Watermarks

The kernel uses three watermark levels per zone to decide when to reclaim:

```
   Total zone memory
   ┌──────────────────────────────────────────────────┐
   │            Used memory (anon + cache)             │
   ├──────────────────────────────────────────────────┤ ← high watermark
   │        Free pages — kswapd stops here            │
   ├──────────────────────────────────────────────────┤ ← low watermark
   │        Free pages — kswapd starts here           │
   ├──────────────────────────────────────────────────┤ ← min watermark
   │     Reserved — direct reclaim territory          │
   │     (allocations BLOCK here)                     │
   └──────────────────────────────────────────────────┘
```

1. Free pages above `high` — allocations succeed instantly.
2. Free pages below `low` — **kswapd** wakes up, reclaims in the background.
3. Free pages below `min` — **direct reclaim**. The allocating thread scans and frees pages itself.

Two sysctls control the gaps:

| Sysctl | Default | Effect |
|--------|---------|--------|
| `vm.min_free_kbytes` | ~67 MB on 64 GB | Sets the `min` watermark across all zones |
| `vm.watermark_scale_factor` | 10 (0.1%) | Gap between min/low/high as % of zone size |

On a 256 GB server with `watermark_scale_factor=10`, the gap between `low` and
`min` is only ~256 MB. A burst of allocations blows through that in milliseconds,
triggering direct reclaim before kswapd can react.

## Direct Reclaim vs kswapd

**kswapd (background)** — kernel thread, one per NUMA node. Wakes at `low`
watermark, scans LRU lists, frees pages. No application latency.
Counters: `pgscan_kswapd`, `pgsteal_kswapd`.

**Direct reclaim (synchronous)** — runs in the allocating thread's context.
Triggered at `min` watermark. Application blocks until pages are freed.
Latency: 100 us to 100+ ms.
Counters: `pgscan_direct`, `pgsteal_direct`, `allocstall_*`.

Key ratios:

```
reclaim_efficiency = pgsteal / pgscan    (higher = better, 1.0 = perfect)
direct_ratio      = pgscan_direct / (pgscan_direct + pgscan_kswapd)
```

- `direct_ratio = 0` — all reclaim is background. Healthy.
- `direct_ratio < 0.1` — occasional direct reclaim. Acceptable.
- `direct_ratio > 0.3` — kswapd cannot keep up. Latency impact.
- `direct_ratio > 0.7` — severe. Applications are stalling.

`allocstall_normal` is the most direct indicator — each increment means one
thread entered the slow path.

## How melisai Measures It

melisai reads `/proc/vmstat` twice — start and end of collection — and computes
per-second rates from the delta:

```go
// internal/collector/memory.go
func (c *MemoryCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    vmstat1 := c.parseVmstatRaw()          // First sample
    time.Sleep(cfg.Duration)                // Default: 10s
    vmstat2 := c.parseVmstatFull(data)      // Second sample + populate ReclaimStats
    c.computeReclaimRates(data, vmstat1, vmstat2, interval.Seconds())
}

func (c *MemoryCollector) computeReclaimRates(data *model.MemoryData,
    v1, v2 map[string]int64, secs float64) {
    if d := v2["pgscan_direct"] - v1["pgscan_direct"]; d > 0 {
        data.Reclaim.DirectReclaimRate = float64(d) / secs
    }
    if d := v2["compact_stall"] - v1["compact_stall"]; d > 0 {
        data.Reclaim.CompactStallRate = float64(d) / secs
    }
    if d := v2["thp_split_page"] - v1["thp_split_page"]; d > 0 {
        data.Reclaim.THPSplitRate = float64(d) / secs
    }
}
```

Two-point sampling captures *what happened during the collection window*, not
lifetime averages. A 10-second window catches bursts that cumulative counters dilute.

### Anomaly Thresholds

| Metric | Warning | Critical | Meaning |
|--------|---------|----------|---------|
| `direct_reclaim_rate` | 10 pages/s | 1000 pages/s | Applications blocking on page reclaim |
| `compaction_stall_rate` | 1/s | 100/s | Allocations blocked on defragmentation |
| `thp_split_rate` | 1/s | 100/s | Huge pages breaking apart, TLB thrashing |

## Compaction

Memory compaction is the kernel's defragmentation. It moves pages to create
contiguous free blocks — needed for huge pages, high-order kernel allocations,
and CMA regions.

```
Fragmented:   [used][free][used][free][used][free][used][free]
               Migration scanner →                ← Free scanner
Compacted:    [used][used][used][used][free][free][free][free]
```

Three counters:

| Counter | Meaning |
|---------|---------|
| `compact_stall` | Allocations that waited for compaction |
| `compact_success` | Compaction runs that created the requested order |
| `compact_fail` | Compaction runs that failed — too fragmented |

Success rate = `compact_success / (compact_success + compact_fail)`. Below 0.5
means compaction is burning CPU but failing. The kernel falls back to smaller
allocations or triggers direct reclaim — producing multi-millisecond stalls.

## THP: Friend or Foe?

Transparent Huge Pages map 2 MB virtual to a single 2 MB physical page instead
of 512 x 4 KB pages. The benefit: 512x fewer TLB entries, 5-15% performance
gain for large-memory workloads.

The cost is in three operations:

**1. Fault-time allocation (`thp_fault_alloc`)** — On a page fault, the kernel
tries to allocate 2 MB contiguously. If fragmented, this triggers compaction
in the fault path, on the application's thread.

**2. Collapse (`thp_collapse_alloc`)** — `khugepaged` scans existing 4 KB pages
and merges 512 contiguous ones into a huge page. Background, but burns CPU.

**3. Split (`thp_split_page`)** — When the kernel must reclaim part of a huge
page, it splits it back into 512 small pages. This requires TLB shootdown IPIs
to every CPU with the page mapped. On a 128-core machine, one split = 127 IPIs.
At 100 splits/s, that is 12,700 IPIs/s of pure overhead.

## THP Defrag Modes

| Mode | Behavior | Latency Impact |
|------|----------|----------------|
| `always` | Synchronous compaction on every THP fault | **Worst** — unbounded stalls |
| `defer` | Try, queue compaction if fail, fall back to 4 KB | Low |
| `defer+madvise` | `defer` for most, sync for `MADV_HUGEPAGE` regions | Low for most |
| `madvise` | Only THP for `MADV_HUGEPAGE` regions | **None** for non-opted-in |
| `never` | No THP defrag | None |

**Production recommendation**: `defer+madvise`. Applications that want huge pages
(PostgreSQL, JVM, Redis) opt in via `madvise(MADV_HUGEPAGE)`; everything else is
protected from compaction stalls.

melisai reads from:
```
/sys/kernel/mm/transparent_hugepage/enabled   → always/madvise/never
/sys/kernel/mm/transparent_hugepage/defrag    → always/defer/defer+madvise/madvise/never
```

## JSON Output

```json
{
  "memory": {
    "thp_enabled": "always",
    "thp_defrag": "always",
    "min_free_kbytes": 67584,
    "watermark_scale_factor": 10,
    "dirty_expire_centisecs": 3000,
    "dirty_writeback_centisecs": 500,
    "reclaim": {
      "pgscan_direct": 48210,
      "pgscan_kswapd": 3841920,
      "pgsteal_direct": 41002,
      "pgsteal_kswapd": 3740100,
      "allocstall_normal": 312,
      "allocstall_movable": 18,
      "compact_stall": 89,
      "compact_success": 62,
      "compact_fail": 27,
      "thp_fault_alloc": 14320,
      "thp_collapse_alloc": 8410,
      "thp_split_page": 1205,
      "direct_reclaim_rate": 482.1,
      "compact_stall_rate": 8.9,
      "thp_split_rate": 12.5
    }
  }
}
```

Reading this: `direct_reclaim_rate=482.1` is above warning (10), below critical
(1000). `compact_stall_rate=8.9` means fragmentation. `thp_enabled=always` with
`thp_defrag=always` is the worst combination for latency-sensitive workloads.
Compaction success rate `62/(62+27) = 70%` — 30% wasted effort.

## Diagnostic Examples

### Healthy: No Reclaim Pressure

```json
{
  "reclaim": {
    "pgscan_direct": 0, "pgscan_kswapd": 120400,
    "pgsteal_kswapd": 118200, "allocstall_normal": 0,
    "compact_stall": 0, "thp_split_page": 3,
    "direct_reclaim_rate": 0, "compact_stall_rate": 0, "thp_split_rate": 0.3
  },
  "thp_enabled": "madvise", "thp_defrag": "defer+madvise",
  "watermark_scale_factor": 150
}
```

All reclaim through kswapd. Zero direct reclaim, zero compaction stalls. kswapd
efficiency: `118200/120400 = 98.2%`. THP on madvise — only opted-in apps get huge pages.

### Critical: THP Storm Under Memory Pressure

```json
{
  "reclaim": {
    "pgscan_direct": 2841000, "pgscan_kswapd": 1420000,
    "pgsteal_direct": 890000, "pgsteal_kswapd": 1210000,
    "allocstall_normal": 14200,
    "compact_stall": 4200, "compact_success": 800, "compact_fail": 3400,
    "thp_fault_alloc": 420, "thp_split_page": 8900,
    "direct_reclaim_rate": 28410, "compact_stall_rate": 420, "thp_split_rate": 890
  },
  "thp_enabled": "always", "thp_defrag": "always",
  "watermark_scale_factor": 10
}
```

Everything is wrong: `direct_reclaim_rate=28410` (critical), direct reclaim
exceeds kswapd, reclaim efficiency `890K/2841K = 31%`, compaction failure
rate 81%, and `thp_fault_alloc=420` vs `thp_split_page=8900` means THP is
net-negative. melisai generates three recommendations:

```
1. Direct reclaim active — increase watermark reserves
     sysctl -w vm.watermark_scale_factor=200
     sysctl -w vm.min_free_kbytes=131072

2. THP splits detected with THP=always — switch to madvise
     echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
     echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag

3. Compaction stalls detected — memory fragmented
     echo 1 > /proc/sys/vm/compact_memory
     sysctl -w vm.extfrag_threshold=500
```

### Warning: Dirty Writeback Too Slow

```json
{
  "reclaim": {
    "pgscan_direct": 8400, "pgscan_kswapd": 620000,
    "direct_reclaim_rate": 84, "compact_stall_rate": 0, "thp_split_rate": 0
  },
  "dirty_expire_centisecs": 3000, "dirty_writeback_centisecs": 500
}
```

Moderate direct reclaim (84/s), no compaction or THP issues. The problem: dirty
pages sit in memory for 30s (`dirty_expire_centisecs=3000`). Under pressure,
the kernel must write them back synchronously before reclaiming.

## Tuning Guide

### Step 1: Increase Watermark Gaps

```bash
# Default: 10 (0.1%). Recommended: 150-300 (1.5-3%)
sysctl -w vm.watermark_scale_factor=200
# Or set directly (e.g., 256 MB on 64 GB server)
sysctl -w vm.min_free_kbytes=262144
```

Trade-off: higher watermarks reserve more memory. On 256 GB,
`watermark_scale_factor=200` reserves ~5 GB. Worth it to eliminate stalls.

### Step 2: THP Policy

```bash
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag
```

### Step 3: Dirty Page Writeback

```bash
sysctl -w vm.dirty_expire_centisecs=1000     # 10s (default: 30s)
sysctl -w vm.dirty_writeback_centisecs=100    # 1s  (default: 5s)
sysctl -w vm.dirty_background_ratio=5         # start flushing at 5%
sysctl -w vm.dirty_ratio=10                   # block writers at 10%
```

### Step 4: Proactive Compaction (kernel 5.9+)

```bash
sysctl -w vm.compaction_proactiveness=20      # background defrag
echo 1 > /proc/sys/vm/compact_memory          # one-shot manual
```

### Step 5: Make It Persistent

```bash
cat >> /etc/sysctl.d/99-melisai-reclaim.conf << 'EOF'
vm.watermark_scale_factor=200
vm.min_free_kbytes=262144
vm.dirty_expire_centisecs=1000
vm.dirty_writeback_centisecs=100
EOF

# THP needs a systemd unit:
cat > /etc/systemd/system/thp-madvise.service << 'EOF'
[Unit]
Description=Set THP to madvise
After=sysinit.target local-fs.target
[Service]
Type=oneshot
ExecStart=/bin/sh -c 'echo madvise > /sys/kernel/mm/transparent_hugepage/enabled'
ExecStart=/bin/sh -c 'echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag'
[Install]
WantedBy=basic.target
EOF
systemctl enable thp-madvise.service
```

## When to Use Static Huge Pages

THP is convenient but unpredictable. For guaranteed huge page allocation without
compaction stalls, use static (pre-allocated) huge pages:

- **Databases**: PostgreSQL `huge_pages=on`, Oracle SGA
- **DPDK**: requires pre-allocated huge pages
- **JVM**: `-XX:+UseLargePages` on latency-critical paths

```bash
# Reserve 4096 huge pages (8 GB)
sysctl -w vm.nr_hugepages=4096
# Or at boot: hugepages=4096 on kernel command line
```

Static huge pages are reserved at boot and cannot be used for anything else.
melisai reports both:

```json
{ "huge_pages_total": 4096, "huge_pages_free": 1024, "thp_enabled": "madvise" }
```

If `huge_pages_free == huge_pages_total`, you have reserved pages nothing uses.

## Quick Reference

| Symptom | Counter | Threshold | Fix |
|---------|---------|-----------|-----|
| Latency spikes | `direct_reclaim_rate > 10` | W=10, C=1000 | Increase `watermark_scale_factor` |
| Allocation stalls | `allocstall_normal > 0` | Any increment | Increase `min_free_kbytes` |
| THP splitting | `thp_split_rate > 1` | W=1, C=100 | THP to `madvise` |
| Compaction failures | `compact_fail > compact_success` | Ratio | `compact_memory`, `extfrag_threshold` |
| Dirty page pressure | High `pgscan_direct`, no compaction | Context | Lower `dirty_expire_centisecs` |

---

*Next: [Chapter 20 — NUMA Optimization](20-numa-optimization.md)*
