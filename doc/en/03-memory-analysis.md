# Chapter 3: Memory Analysis — Deep Dive

## Overview

Memory is the most misunderstood resource. "90% memory used" doesn't mean the server needs more RAM — most of it is probably cache. Understanding what `MemAvailable` vs `MemFree` means is crucial.

melisai's `MemoryCollector` (`internal/collector/memory.go`) reads 6 data sources and provides a complete memory picture.

## Source File: memory.go

- **Lines**: 274
- **Functions**: 8
- **Data Sources**: `/proc/meminfo`, `/proc/vmstat`, `/proc/pressure/memory`, `/proc/buddyinfo`, `/sys/devices/system/node/`

## Function Walkthrough

### Collect() — Orchestrating Memory Data

```go
func (c *MemoryCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    data := &model.MemoryData{}
    c.parseMeminfo(data)           // Main memory breakdown
    c.parseVmstat(data)            // Page fault counters
    // Sysctl settings:
    data.Swappiness = readSysctlInt(c.procRoot, "sys/vm/swappiness")
    data.OvercommitMemory = readSysctlInt(c.procRoot, "sys/vm/overcommit_memory")
    data.OvercommitRatio = readSysctlInt(c.procRoot, "sys/vm/overcommit_ratio")
    data.DirtyRatio = readSysctlInt(c.procRoot, "sys/vm/dirty_ratio")
    data.DirtyBackgroundRatio = readSysctlInt(c.procRoot, "sys/vm/dirty_background_ratio")
    c.parsePSI(data)               // Pressure Stall Information
    data.BuddyInfo = c.parseBuddyinfo()  // Memory fragmentation
    data.NUMANodes = c.parseNUMAStats()   // Per-NUMA-node stats
    return &model.Result{...}
}
```

Unlike CPU collection, memory doesn't need delta sampling — most metrics are instant snapshots.

### parseMeminfo() — The Core Memory Breakdown

```go
func (c *MemoryCollector) parseMeminfo(data *model.MemoryData) {
    f, _ := os.Open(filepath.Join(c.procRoot, "meminfo"))
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        // "MemTotal:       16384000 kB"
        parts := strings.SplitN(line, ":", 2)
        key := strings.TrimSpace(parts[0])
        valStr := strings.TrimSuffix(strings.TrimSpace(parts[1]), " kB")
        val, _ := strconv.ParseInt(valStr, 10, 64)
        valBytes := val * 1024  // kB → bytes

        switch key {
        case "MemTotal":     data.TotalBytes = valBytes
        case "MemFree":      data.FreeBytes = valBytes
        case "MemAvailable": data.AvailableBytes = valBytes
        case "Cached":       data.CachedBytes = valBytes
        case "Buffers":      data.BuffersBytes = valBytes
        case "SwapTotal":    data.SwapTotalBytes = valBytes
        case "SwapFree":     data.SwapUsedBytes = data.SwapTotalBytes - valBytes
        case "Dirty":        data.DirtyBytes = valBytes
        case "HugePages_Total": data.HugePagesTotal = int(val)
        case "HugePages_Free":  data.HugePagesFree = int(val)
        }
    }
}
```

**Critical distinction — MemFree vs MemAvailable:**

```
┌─────────────────────────────────────────────────┐
│                  Total Memory                    │
├────────┬───────────┬───────────┬────────────────┤
│ Active │   Cache   │  Buffers  │    Free        │
│  Used  │(reclaimable)│         │                │
├────────┴───────────┴───────────┼────────────────┤
│←─────── MemAvailable ─────────→│                │
│                                │←── MemFree ───→│
└────────────────────────────────┴────────────────┘
```

- **MemFree**: Truly unused pages. On a healthy server, this is often near zero — and that's fine!
- **MemAvailable**: Free + reclaimable cache + reclaimable buffers. This is what your application can actually use.

**Rule**: Use `MemAvailable`, not `MemFree`, for capacity planning.

### parseVmstat() — Page Faults

```go
func (c *MemoryCollector) parseVmstat(data *model.MemoryData) {
    // Reads /proc/vmstat
    switch fields[0] {
    case "pgmajfault": data.MajorFaults = val  // Required disk I/O
    case "pgfault":    data.MinorFaults = val  // Resolved in memory
    }
}
```

- **Minor fault**: Page not in page table but IS in memory (e.g., shared library pages). Cost: ~1μs.
- **Major fault**: Page not in memory, must be read from disk. Cost: ~1-10ms (SSD) or ~5-20ms (HDD).

High major faults = the system is swapping or has insufficient cache.

### parsePSI() — Memory Pressure

```go
func (c *MemoryCollector) parsePSI(data *model.MemoryData) {
    // /proc/pressure/memory
    // "some avg10=0.00 avg60=0.15 avg300=0.08 total=142395"
    // "full avg10=0.00 avg60=0.00 avg300=0.00 total=0"
    switch {
    case prefix == "some" && parts[0] == "avg10": data.PSISome10 = val
    case prefix == "some" && parts[0] == "avg60": data.PSISome60 = val
    case prefix == "full" && parts[0] == "avg10": data.PSIFull10 = val
    case prefix == "full" && parts[0] == "avg60": data.PSIFull60 = val
    }
}
```

**Interpreting PSI:**
| Value | Meaning |
|-------|---------|
| some=0% | No memory pressure |
| some=5% | Some tasks waiting for memory 5% of the time |
| some=25% | Significant memory contention |
| full=5% | ALL tasks blocked 5% of the time — severe |
| full=25% | System is thrashing |

### parseBuddyinfo() — Fragmentation

```go
func (c *MemoryCollector) parseBuddyinfo() map[string][]int {
    // /proc/buddyinfo
    // "Node 0, zone   DMA    1    0    1    0    2    1    1    0    1    1    3"
    // Orders 0-10:    4KB  8KB  16KB 32KB 64KB 128KB 256KB 512KB 1MB  2MB  4MB
}
```

This data shows free blocks at each order (size). When large order counts are zero, the system can't allocate contiguous physical pages. This matters for:
- HugePages (2MB blocks)
- Network buffers (typically need 64KB+)
- DMA allocations

### parseNUMAStats() — NUMA Topology

```go
func (c *MemoryCollector) parseNUMAStats() []model.NUMANode {
    // Iterates /sys/devices/system/node/node*/
    // Reads meminfo (MemTotal, MemFree) and numastat (numa_hit, numa_miss, numa_foreign)
}
```

**NUMA ratios to watch:**
```
NUMA hit ratio = numa_hit / (numa_hit + numa_miss)
```

- **> 95%**: Good — most allocations are local
- **80-95%**: Acceptable — some remote access
- **< 80%**: Bad — process should be pinned to a NUMA node with `numactl`

## Sysctl Parameters Explained

| Parameter | Default | Meaning |
|-----------|---------|---------|
| `vm.swappiness` | 60 | How aggressively to swap (0=avoid, 100=aggressive) |
| `vm.overcommit_memory` | 0 | 0=heuristic, 1=always allow, 2=strict accounting |
| `vm.overcommit_ratio` | 50 | When overcommit_memory=2: commit limit = RAM × ratio% + swap |
| `vm.dirty_ratio` | 20 | % of RAM that can be dirty before synchronous write-back |
| `vm.dirty_background_ratio` | 10 | % of RAM dirty before background write-back starts |

**Common tuning:**
- **Database servers**: `swappiness=1` (avoid swap, let DB manage its cache)
- **Web servers**: `dirty_ratio=5`, `dirty_background_ratio=2` (faster write-back)
- **Elasticsearch**: `overcommit_memory=1` (avoid OOM on fork)

## Diagnostic Examples

### Healthy Server
```json
{
  "total_bytes": 68719476736,
  "available_bytes": 45097156608,
  "cached_bytes": 20000000000,
  "swap_used_bytes": 0,
  "major_faults": 12,
  "psi_some_10": 0.0
}
```
Available = 66% of total. Zero swap. No pressure.

### Memory Leak
```json
{
  "total_bytes": 68719476736,
  "available_bytes": 1073741824,
  "cached_bytes": 500000000,
  "swap_used_bytes": 8589934592,
  "major_faults": 125000,
  "psi_some_10": 32.5,
  "psi_full_10": 8.2
}
```
Available dropped to 1.5%, cache is tiny (evicted), 8GB swap used, 125K major faults, 32% pressure. This is a memory crisis — find the leaking process.

---

*Next: [Chapter 4 — Disk I/O Analysis](04-disk-analysis.md)*
