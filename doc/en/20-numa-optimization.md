# Chapter 20: NUMA Optimization

## What is NUMA?

Modern multi-socket servers do not have a single, flat memory bus. Each CPU socket owns a chunk of physical RAM. This architecture is called **Non-Uniform Memory Access** (NUMA). A CPU accessing its local memory is fast. A CPU accessing memory owned by another socket pays a latency tax.

```
        Socket 0                                  Socket 1
  ┌──────────────────┐    QPI / UPI Link    ┌──────────────────┐
  │  CPU cores 0-19  │◄──────────────────►  │  CPU cores 20-39 │
  │  L1/L2/L3 cache  │  (30-50% penalty)    │  L1/L2/L3 cache  │
  └────────┬─────────┘                      └────────┬─────────┘
  ┌────────┴─────────┐                      ┌────────┴─────────┐
  │  Local DRAM      │                      │  Local DRAM      │
  │  Node 0: 64 GiB  │                      │  Node 1: 64 GiB  │
  │  distance: 10    │   remote: 21         │  distance: 10    │
  └──────────────────┘                      └──────────────────┘
```

Key terms:

- **Node**: A NUMA domain -- one socket plus its attached memory
- **Local access**: CPU reads memory from its own node (distance = 10)
- **Remote access**: CPU reads memory from another node (distance = 21+)
- **numa_hit**: Allocation satisfied from the requested node
- **numa_miss**: Allocation went to a different node
- **numa_foreign**: Another node allocated from this node's memory

## Why It Matters

| Interconnect | Latency penalty | Found in |
|-------------|----------------|----------|
| Intel QPI   | ~30-40%        | Xeon E5/E7 v1-v4 |
| Intel UPI   | ~30-50%        | Xeon Scalable (Skylake+) |
| AMD Infinity Fabric | ~20-40% | EPYC (Rome, Milan, Genoa) |
| 4-socket QPI | ~60-100% (2 hops) | 4S/8S enterprise servers |

A database doing 10M lookups/sec with 40% remote access penalty -- that is the difference between meeting SLA and missing it. NUMA misplacement is one of the top performance killers people overlook because "the server has enough RAM."

## Distance Matrix Explained

Every node exposes `/sys/devices/system/node/nodeN/distance`:

```
# cat /sys/devices/system/node/node0/distance
10 21 31 31
```

- `10` = local (self), always 10. This is the baseline.
- `21` = one hop. Ratio: 21/10 = 2.1x local latency.
- `31` = two hops (4-socket or multi-die EPYC). 3.1x penalty.

## How melisai Detects Problems

`MemoryCollector.parseNUMAStats()` reads four sources per node:

| Source | Path | Provides |
|--------|------|----------|
| meminfo | `/sys/devices/system/node/nodeN/meminfo` | `MemTotalBytes`, `MemFreeBytes` |
| numastat | `/sys/devices/system/node/nodeN/numastat` | `numa_hit`, `numa_miss`, `numa_foreign` |
| distance | `/sys/devices/system/node/nodeN/distance` | Hop costs between nodes |
| cpulist | `/sys/devices/system/node/nodeN/cpulist` | CPUs belonging to this node |

Two sysctls are also collected: `vm.zone_reclaim_mode` and `kernel.sched_numa_balancing`.

### MissRatio Calculation

```go
total := node.NumaHit + node.NumaMiss
if total > 0 {
    node.MissRatio = float64(node.NumaMiss) / float64(total) * 100
}
```

Computed per node. The anomaly engine takes the **maximum** across all nodes:

| Severity | Threshold | Meaning |
|----------|-----------|---------|
| Warning  | > 5%      | Cross-node allocations -- investigate |
| Critical | > 20%     | Heavy cross-node traffic -- fix immediately |

## JSON Output Example

```json
{
  "zone_reclaim_mode": 0,
  "sched_numa_balancing": 1,
  "numa_nodes": [
    {
      "node": 0, "mem_total_bytes": 68719476736, "mem_free_bytes": 12884901888,
      "numa_hit": 48291035, "numa_miss": 127, "numa_foreign": 18442,
      "miss_ratio": 0.0003, "distance": [10, 21], "cpus": "0-19"
    },
    {
      "node": 1, "mem_total_bytes": 68719476736, "mem_free_bytes": 34359738368,
      "numa_hit": 31204871, "numa_miss": 18442, "numa_foreign": 127,
      "miss_ratio": 0.059, "distance": [21, 10], "cpus": "20-39"
    }
  ]
}
```

Node 1 has 18,442 misses -- something on node 1 pulls memory from node 0. Confirmed by `numa_foreign=18442` on node 0.

## Diagnostic Examples

### Single-Node Server -- No Issues

```json
{ "numa_nodes": [
    { "node": 0, "numa_hit": 92456123, "numa_miss": 0, "miss_ratio": 0,
      "distance": [10], "cpus": "0-7" }
]}
```

One NUMA node, distance `[10]`, zero misses. Nothing to fix.

### Dual-Socket with High Miss Ratio

```json
{ "numa_nodes": [
    { "node": 0, "mem_free_bytes": 4294967296,
      "numa_hit": 112847291, "numa_miss": 891204, "miss_ratio": 0.78,
      "distance": [10, 21], "cpus": "0-23" },
    { "node": 1, "mem_free_bytes": 102005473280,
      "numa_hit": 18294102, "numa_miss": 24017893, "miss_ratio": 56.8,
      "distance": [21, 10], "cpus": "24-47" }
]}
```

Node 1: **56.8% miss ratio**. A large application runs on CPUs 24-47 but its memory was allocated on node 0. Every access crosses the UPI link. Node 0 has 4 GiB free, node 1 has 102 GiB free -- the app consumed all of node 0's RAM despite running on node 1.

melisai fires: `severity: critical, metric: numa_miss_ratio, value: 56.8`.

### Imbalanced Memory, Low Miss Ratio

```json
{ "numa_nodes": [
    { "node": 0, "mem_free_bytes": 1073741824,
      "numa_hit": 98102344, "numa_miss": 2104, "cpus": "0-23" },
    { "node": 1, "mem_free_bytes": 120259084288,
      "numa_hit": 5291033, "numa_miss": 41, "cpus": "24-47" }
]}
```

Miss ratios are low but node 0 has 1 GiB free while node 1 has 112 GiB free. All workloads are pinned to node 0. When it runs out of free memory, the kernel starts either reclaiming (kswapd) or allocating remotely. Both are bad. This typically means all services started by default on CPU 0's core group.

## Fixing NUMA Issues

### 1. numactl -- Pin Processes

```bash
# Run PostgreSQL on node 0 CPUs and node 0 memory
numactl --cpunodebind=0 --membind=0 /usr/lib/postgresql/16/bin/postgres

# Verify placement
numastat -p <pid>
```

For systemd services:

```ini
# /etc/systemd/system/postgresql.service.d/numa.conf
[Service]
CPUAffinity=0-23
NUMAPolicy=bind
NUMAMask=0
```

### 2. sched_numa_balancing

Kernel auto-migrates pages to the node where they are most accessed. Works by marking pages inaccessible, trapping faults, and migrating toward the accessing CPU.

```bash
sysctl -w kernel.sched_numa_balancing=1
echo 'kernel.sched_numa_balancing=1' >> /etc/sysctl.d/99-numa.conf
```

Use for general-purpose servers with many small services. Avoid for real-time workloads where migration overhead (page faults, TLB flushes) is unacceptable.

### 3. zone_reclaim_mode

```bash
# 0 = allocate remote before reclaiming local cache (default, recommended)
# 1 = reclaim local cache before going remote
sysctl -w vm.zone_reclaim_mode=0
```

Keep at 0. Reclaiming page cache (2x penalty) is better than re-reading from disk (1000x+ penalty). Only set to 1 for HPC workloads where memory locality dominates I/O.

### 4. Interleave for Databases

```bash
numactl --interleave=all /usr/lib/postgresql/16/bin/postgres
```

Shared buffers accessed by backends on all nodes benefit from interleaving: every thread gets ~50% local access, predictable latency. Binding gives local threads great latency and remote threads terrible latency.

### Decision Matrix

| Workload | Policy | Why |
|----------|--------|-----|
| Large DB (PostgreSQL, MySQL) | `--interleave=all` | All backends hit the buffer pool |
| Redis (single-threaded) | `--cpunodebind=N --membind=N` | One thread, one node |
| JVM application | `--cpunodebind=N --membind=N` | Heap is private to the process |
| Multi-service host | `sched_numa_balancing=1` | Too many processes to pin |
| HPC / MPI | Explicit binding per rank | Each rank owns its data |
| GPU workload | Bind to GPU's NUMA node | See below |

## NUMA and Containers

Kubernetes manages NUMA topology via the **Topology Manager**:

| Policy | Behavior |
|--------|----------|
| `none` | No NUMA awareness (default) |
| `best-effort` | Try to align, schedule anyway if impossible |
| `restricted` | Reject pod if NUMA alignment fails |
| `single-numa-node` | All resources from one NUMA node |

```yaml
topologyManagerPolicy: "single-numa-node"
topologyManagerScope: "pod"
```

For databases on NUMA hardware in K8s, `single-numa-node` + guaranteed QoS class is the minimum. Without it, the kubelet may put a pod's CPUs on node 0 and its hugepages on node 1.

melisai's system-level NUMA stats combined with container metrics (Chapter 7) let you correlate high miss ratios with container CPU assignments.

## NUMA and GPUs

GPUs attach to a specific PCIe root complex on a specific NUMA node. melisai's `GPUDevice` struct captures this:

```go
type GPUDevice struct {
    NUMANode    int    `json:"numa_node"`
    // ...
}
```

Always pin GPU workloads to the GPU's NUMA node:

```bash
cat /sys/bus/pci/devices/0000:41:00.0/numa_node   # => 1
numactl --cpunodebind=1 --membind=1 python train.py
```

Cross-NUMA GPU access penalizes `cudaMemcpy`, PCIe peer-to-peer transfers, and CPU-side preprocessing. See [Chapter 18 -- GPU Monitoring](18-gpu-monitoring.md) for GPU metric collection.

## Key Takeaways

1. **Miss ratio > 5% = warning.** Something is allocating on the wrong node.
2. **Miss ratio > 20% = critical.** 30-50% latency penalty on a large fraction of accesses.
3. **Distance matrix tells cost.** 10=local, 21=2.1x, 31=3.1x.
4. **numactl for dedicated workloads.** Pin CPUs and memory to the same node.
5. **Interleave for shared data.** `--interleave=all` for database buffer pools.
6. **zone_reclaim_mode=0** is correct for almost everyone.
7. **GPUs have NUMA affinity.** Pin to the GPU's node.
8. **K8s needs Topology Manager.** Default is `none` -- no NUMA awareness.

---

*Next: [Chapter 21 -- Production Tuning Checklist](21-production-checklist.md)*
