# Chapter 1: Linux Fundamentals for Performance Analysis

Before diving into sysdiag's code, you need to understand **where** performance data comes from. Linux doesn't have a "performance API" — instead, the kernel exposes data through virtual filesystems and special files.

## The /proc Filesystem (procfs)

`/proc` is a **virtual filesystem** — it doesn't live on disk. Every time you read a file in `/proc`, the kernel generates the content on-the-fly from internal data structures. No disk I/O occurs.

### Key Files Used by sysdiag

| File | Content | Used by |
|------|---------|---------|
| `/proc/stat` | CPU time counters (per-state, per-core) | CPUCollector |
| `/proc/loadavg` | 1/5/15 min load averages | CPUCollector |
| `/proc/meminfo` | Memory breakdown (total, free, available, cache...) | MemoryCollector |
| `/proc/vmstat` | VM event counters (page faults, paging...) | MemoryCollector |
| `/proc/buddyinfo` | Memory fragmentation per zone | MemoryCollector |
| `/proc/pressure/memory` | PSI (memory stall information) | MemoryCollector |
| `/proc/diskstats` | Per-device I/O counters | DiskCollector |
| `/proc/net/dev` | Per-interface traffic counters | NetworkCollector |
| `/proc/net/snmp` | TCP/UDP/IP protocol statistics | NetworkCollector |
| `/proc/[pid]/stat` | Per-process CPU time and state | ProcessCollector |
| `/proc/[pid]/fd/` | Open file descriptor count | ProcessCollector |
| `/proc/1/cgroup` | Cgroup membership (container detection) | ContainerCollector |
| `/proc/sys/*` | Kernel tunables (sysctl values) | Various |

### How to Read /proc Files

```bash
# Example: reading CPU counters
$ cat /proc/stat
cpu  10132153 290696 3084719 46828483 16683 0 25195 0 0 0
cpu0 1393280  32966  572056  13343292 6130  0 17875 0 0 0
...

# The numbers are "jiffies" (time units, typically 1/100th of a second)
# Fields: user nice system idle iowait irq softirq steal guest guest_nice
```

### Why Delta Sampling

All `/proc` counters are **monotonically increasing** — they only go up. To get the current rate, you need **two readings** separated by a time interval:

```
Reading 1 at T=0:  cpu user=10000 system=5000 idle=85000
Reading 2 at T=1s: cpu user=10100 system=5020 idle=85880
                                                          
Delta:            user=100  system=20  idle=880
Total delta:      100 + 20 + 880 = 1000 jiffies

user%  = 100 / 1000 × 100 = 10.0%
system% = 20 / 1000 × 100 =  2.0%
idle%  = 880 / 1000 × 100 = 88.0%
```

This is exactly what sysdiag's `computeDelta()` function does.

## The /sys Filesystem (sysfs)

`/sys` represents the kernel's device model. Unlike `/proc` (which is mostly flat), `/sys` is a tree that mirrors the hardware/driver hierarchy.

### Key Paths Used by sysdiag

```
/sys/block/sda/queue/scheduler     ← Active I/O scheduler
/sys/block/sda/queue/nr_requests   ← I/O queue depth
/sys/block/sda/queue/rotational    ← 1=HDD, 0=SSD
/sys/block/sda/queue/read_ahead_kb ← Prefetch size

/sys/fs/cgroup/                    ← Control group hierarchy
/sys/fs/cgroup/cpu.max             ← CPU quota (cgroup v2)
/sys/fs/cgroup/memory.max          ← Memory limit (cgroup v2)
/sys/fs/cgroup/memory.current      ← Current memory usage (cgroup v2)

/sys/devices/system/node/node0/meminfo   ← Per-NUMA-node memory
/sys/devices/system/node/node0/numastat  ← NUMA hit/miss counters
```

## Jiffies and Time Accounting

Linux measures CPU time in **jiffies** — discrete time units. The rate is defined by the kernel configuration parameter `HZ`:

| HZ Value | 1 Jiffy | Common On |
|----------|---------|-----------|
| 100 | 10 ms | Most servers |
| 250 | 4 ms | Common desktop |
| 1000 | 1 ms | Low-latency kernels |

When sysdiag reads `/proc/stat`, it reads jiffie counts accumulated since boot. The `CLK_TCK` (clock ticks per second) is typically 100 on Linux, accessible via `sysconf(_SC_CLK_TCK)`.

### CPU States Explained

Each jiffy, every CPU core is in exactly one state:

```
┌─────────┬──────────────────────────────────────────────────────┐
│ State   │ Description                                          │
├─────────┼──────────────────────────────────────────────────────┤
│ user    │ Running user-space code                              │
│ nice    │ Running user-space code at lower priority            │
│ system  │ Running kernel code (syscalls, drivers)              │
│ idle    │ Doing nothing, waiting for work                      │
│ iowait  │ Idle, but waiting for I/O to complete                │
│ irq     │ Handling hardware interrupts                         │
│ softirq │ Handling software interrupts (network, timers)       │
│ steal   │ Time stolen by hypervisor (VMs only)                 │
└─────────┴──────────────────────────────────────────────────────┘
```

> **Common misconception about iowait**: IOWait does NOT mean "CPU is busy with I/O". It means "CPU is idle AND has pending I/O". If the system has other tasks to run, those jiffies show up as user/system instead, even with the same I/O load. IOWait going to zero doesn't mean I/O is gone — it may just mean the CPU found other work.

## Control Groups (cgroups)

Cgroups limit and account for resource usage. They're the foundation of containers (Docker, Kubernetes).

### cgroup v1 vs cgroup v2

| Feature | cgroup v1 | cgroup v2 |
|---------|-----------|-----------|
| **Hierarchy** | Multiple trees (cpu, memory, io) | Single unified tree |
| **Interface** | `cpu.cfs_quota_us`, `memory.limit_in_bytes` | `cpu.max`, `memory.max` |
| **Detection** | `/sys/fs/cgroup/cpu/` exists | `/sys/fs/cgroup/cgroup.controllers` exists |
| **Adoption** | Older systems, Docker default until recently | Newer kernels, systemd default |

### CPU Throttling

When a container exceeds its CPU quota, the kernel "throttles" it — the process is paused until the next period:

```
cpu.max = "100000 100000"
         ^quota   ^period (both in microseconds)

This means: 100ms of CPU per 100ms period = 1 full CPU core

cpu.stat:
  nr_throttled = 1247    ← throttle events count
  throttled_usec = 58432 ← total time spent throttled (μs)
```

Throttling is invisible to the application — it just appears as latency. sysdiag's ContainerCollector detects this.

## Pressure Stall Information (PSI)

Available since Linux 4.20, PSI directly answers: "Are tasks stalling because a resource is scarce?"

```bash
$ cat /proc/pressure/memory
some avg10=0.00 avg60=0.15 avg300=0.08 total=142395
full avg10=0.00 avg60=0.00 avg300=0.00 total=0
```

- **some**: Percentage of time *at least one* task was stalled
- **full**: Percentage of time *all* tasks were stalled (nothing productive running)
- **avg10/60/300**: Running averages over 10s, 60s, 300s windows

PSI is far more reliable than checking memory free/available, because it measures actual impact on workloads rather than just capacity.

## NUMA (Non-Uniform Memory Access)

On multi-socket servers, each CPU has "local" memory (fast access) and "remote" memory (through the interconnect, 2-3× slower):

```
┌───────────────────┐      ┌───────────────────┐
│   CPU Socket 0    │      │   CPU Socket 1    │
│  ┌─────────────┐  │      │  ┌─────────────┐  │
│  │ Core 0-7    │  │◄────►│  │ Core 8-15   │  │
│  └─────────────┘  │ QPI/ │  └─────────────┘  │
│  ┌─────────────┐  │ UPI  │  ┌─────────────┐  │
│  │ Local RAM   │  │      │  │ Local RAM   │  │
│  │ (Node 0)    │  │      │  │ (Node 1)    │  │
│  └─────────────┘  │      │  └─────────────┘  │
└───────────────────┘      └───────────────────┘
```

sysdiag tracks `numa_hit` (memory allocated locally) vs `numa_miss` (allocated remotely). A high `numa_miss` ratio indicates your application process is accessing memory on the wrong socket, adding latency.

## The Buddy Allocator

The kernel manages physical memory in "pages" (typically 4KB). When larger contiguous blocks are needed, the **buddy allocator** pairs pages into increasingly larger blocks:

```
Order:  0    1     2     3     4     5    ... 10
Size:   4KB  8KB   16KB  32KB  64KB  128KB   4MB
```

`/proc/buddyinfo` shows how many free blocks of each order exist per zone:

```
Node 0, zone   DMA      1    1    1    0    2    1    1    0    1    1    3
Node 0, zone  DMA32   917  572  249   103   52   23   8    3    1    1    0
Node 0, zone Normal  4861 1430  541   171   43   16   5    2    0    0    0
```

When higher-order entries (right columns) are all zero, the memory is **fragmented** — the kernel can't allocate contiguous blocks larger than a certain size. This can cause allocation failures even when there's plenty of total free memory.

## Summary of Data Sources

```
┌──────────────────────────────────────────────────────┐
│                    Linux Kernel                       │
│                                                      │
│  ┌─────────────────┐      ┌─────────────────┐       │
│  │   /proc (procfs) │      │   /sys (sysfs)   │       │
│  │   Process/system │      │   Device model   │       │
│  │   counters       │      │   parameters     │       │
│  └────────┬────────┘      └────────┬────────┘       │
│           │                        │                 │
│           ▼                        ▼                 │
│  ┌─────────────────────────────────────────────────┐ │
│  │              sysdiag collectors                  │ │
│  │  read files → parse text → delta compute →      │ │
│  │  → structured Go types → JSON output            │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

---

*Next: [Chapter 2 — CPU Analysis](02-cpu-analysis.md)*
