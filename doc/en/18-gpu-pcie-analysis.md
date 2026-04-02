# Chapter 18: GPU & PCIe Topology Analysis

## Overview

A GPU computing job that should saturate 400 Gbps of InfiniBand is crawling at 60% throughput. The GPU is fine. The NIC is fine. The problem: the GPU sits on NUMA node 0 and the NIC on NUMA node 1. Every DMA transfer crosses the inter-socket link. 30-50% bandwidth penalty, invisible to application-level metrics.

melisai's `GPUCollector` (`internal/collector/gpu.go`) detects this automatically. It queries NVIDIA GPUs via `nvidia-smi`, maps PCI devices and NICs to NUMA nodes through sysfs, and flags every GPU-NIC pair that crosses a NUMA boundary.

## Source File: gpu.go

- **Lines**: 166
- **Functions**: 7
- **Tier**: 1 (no root needed, sysfs is world-readable)
- **Category**: `system`
- **Collector name**: `gpu_pcie`

## Why PCIe Topology Matters

Modern servers have multiple PCIe root complexes, one per CPU socket. Each root complex owns a set of PCIe slots. Devices in those slots have local access to the memory controller on that socket -- that is the device's NUMA node.

When a GPU on NUMA node 0 sends data via DMA to a NIC on NUMA node 1, the transfer crosses the inter-socket interconnect (UPI on Intel, Infinity Fabric on AMD):

| Scenario | Bandwidth Impact | Latency Impact |
|----------|-----------------|----------------|
| Same NUMA node | Baseline | Baseline |
| Cross-NUMA (2-socket) | 30-50% reduction | +40-80ns per access |
| Cross-NUMA (4-socket) | Up to 70% reduction | +100-200ns per hop |

The kernel does not warn you. `nvidia-smi` does not warn you. Applications see slow throughput and blame the network. melisai catches it.

## Data Structures

Three types in `internal/model/types.go`:

```go
type GPUDevice struct {
    Index       int    `json:"index"`
    Name        string `json:"name"`
    Driver      string `json:"driver,omitempty"`
    PCIBus      string `json:"pci_bus"`
    NUMANode    int    `json:"numa_node"`
    MemoryTotal int64  `json:"memory_total_mb,omitempty"`
    MemoryUsed  int64  `json:"memory_used_mb,omitempty"`
    UtilGPU     int    `json:"utilization_gpu_pct,omitempty"`
    UtilMemory  int    `json:"utilization_memory_pct,omitempty"`
    Temperature int    `json:"temperature_c,omitempty"`
    PowerWatts  int    `json:"power_watts,omitempty"`
}

type PCIeTopology struct {
    GPUs           []GPUDevice     `json:"gpus,omitempty"`
    NICNUMAMap     map[string]int  `json:"nic_numa_map,omitempty"`
    CrossNUMAPairs []CrossNUMAPair `json:"cross_numa_pairs,omitempty"`
}

type CrossNUMAPair struct {
    GPU     string `json:"gpu"`
    GPUNode int    `json:"gpu_numa_node"`
    NIC     string `json:"nic"`
    NICNode int    `json:"nic_numa_node"`
}
```

## How Detection Works

`Collect()` runs three steps:

```go
func (c *GPUCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    topo := &model.PCIeTopology{NICNUMAMap: make(map[string]int)}
    topo.GPUs = c.detectNvidiaGPUs(ctx)     // Step 1
    c.buildNICNUMAMap(topo)                  // Step 2
    c.findCrossNUMAPairs(topo)               // Step 3

    if len(topo.GPUs) == 0 && len(topo.NICNUMAMap) == 0 {
        return nil, nil   // graceful: nothing detected, no result
    }
    return &model.Result{Collector: c.Name(), Data: topo}, nil
}
```

The `nil, nil` return means "not applicable." The orchestrator omits this collector from the report. No noise.

### Step 1: detectNvidiaGPUs

Runs nvidia-smi with structured CSV output:

```
nvidia-smi --query-gpu=index,name,driver_version,pci.bus_id,memory.total,\
  memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw \
  --format=csv,noheader,nounits
```

- **5-second timeout** -- nvidia-smi can hang on a wedged driver. A dedicated `context.WithTimeout` prevents blocking the collection.
- **Graceful degradation** -- if nvidia-smi is missing or fails, returns nil.
- **NUMA lookup** -- for each GPU, reads `/sys/bus/pci/devices/<bus_id>/numa_node`. The PCI bus ID from nvidia-smi (e.g., `00000000:07:00.0`) maps directly to a sysfs path.

### Step 2: buildNICNUMAMap

Reads `/sys/class/net/*/device/numa_node` for each physical NIC. Filters out virtual interfaces: `lo`, `veth*`, `docker*`, `br-*`. A NUMA node value of `-1` (single-socket or virtual device) is skipped.

### Step 3: findCrossNUMAPairs

Cross product: for every GPU, check every NIC. If they are on different NUMA nodes and both have valid assignments (>= 0), record the pair:

```go
if gpu.NUMANode != nicNode && gpu.NUMANode >= 0 && nicNode >= 0 {
    topo.CrossNUMAPairs = append(topo.CrossNUMAPairs, model.CrossNUMAPair{
        GPU: gpu.Name, GPUNode: gpu.NUMANode,
        NIC: nic,      NICNode: nicNode,
    })
}
```

## Anomaly Detection

The `gpu_nic_cross_numa` rule in `internal/model/anomaly.go`:

```go
{
    Metric: "gpu_nic_cross_numa", Category: "system",
    Warning: 1, Critical: 1,
    Evaluator: func(r *Report) (float64, bool) {
        // Scans system category for PCIeTopology data
        // Returns count of cross-NUMA pairs
        return float64(len(topo.CrossNUMAPairs)), true
    },
    Message: func(v float64) string {
        return fmt.Sprintf(
            "GPU-NIC cross-NUMA: %.0f pair(s) on different NUMA nodes (PCIe DMA penalty)", v)
    },
},
```

Warning=1, Critical=1: cross-NUMA is binary. You either have it or you don't. One misplaced pair can cut throughput by 30-50%, so even a single pair is critical for GPU workloads.

## JSON Output Examples

### Healthy: GPUs and NICs on Same NUMA Node

```json
{
  "collector": "gpu_pcie",
  "category": "system",
  "tier": 1,
  "data": {
    "gpus": [
      {
        "index": 0,
        "name": "NVIDIA A100-SXM4-80GB",
        "driver": "535.129.03",
        "pci_bus": "00000000:07:00.0",
        "numa_node": 0,
        "memory_total_mb": 81920,
        "memory_used_mb": 42317,
        "utilization_gpu_pct": 87,
        "temperature_c": 62,
        "power_watts": 312
      }
    ],
    "nic_numa_map": {
      "eth0": 0,
      "ib0": 0
    }
  }
}
```

No `cross_numa_pairs` field -- omitted by `omitempty` because the slice is nil.

### Problem: Cross-NUMA GPU-NIC Pair

```json
{
  "collector": "gpu_pcie",
  "category": "system",
  "tier": 1,
  "data": {
    "gpus": [
      {"index": 0, "name": "NVIDIA A100-SXM4-80GB",
       "pci_bus": "00000000:07:00.0", "numa_node": 0},
      {"index": 1, "name": "NVIDIA A100-SXM4-80GB",
       "pci_bus": "00000000:8A:00.0", "numa_node": 1}
    ],
    "nic_numa_map": {"ib0": 1, "eth0": 0},
    "cross_numa_pairs": [
      {"gpu": "NVIDIA A100-SXM4-80GB", "gpu_numa_node": 0,
       "nic": "ib0", "nic_numa_node": 1},
      {"gpu": "NVIDIA A100-SXM4-80GB", "gpu_numa_node": 1,
       "nic": "eth0", "nic_numa_node": 0}
    ]
  }
}
```

Anomaly fires:

```json
{
  "metric": "gpu_nic_cross_numa",
  "value": 2,
  "threshold": 1,
  "severity": "critical",
  "message": "GPU-NIC cross-NUMA: 2 pair(s) on different NUMA nodes (PCIe DMA penalty)"
}
```

### No GPU Detected

`detectNvidiaGPUs()` returns nil. If NICs also lack NUMA affinity, `Collect()` returns `nil, nil`. The collector is absent from the report entirely.

## Diagnostic Commands

### nvidia-smi topo

```bash
$ nvidia-smi topo -m
        GPU0    GPU1    mlx5_0  mlx5_1  CPU Affinity    NUMA Affinity
GPU0     X      NV12    SYS     PHB     0-19            0
GPU1    NV12     X      PHB     SYS     20-39           1
mlx5_0  SYS     PHB      X      SYS    20-39           1
mlx5_1  PHB     SYS     SYS      X     0-19            0
```

- **PHB** = same PCIe Host Bridge (same NUMA node)
- **SYS** = crosses NUMA boundary (inter-socket link)
- **NV12** = NVLink (GPU-to-GPU)

GPU0-mlx5_0 is SYS (cross-NUMA) -- exactly what melisai detects.

### sysfs Direct Inspection

```bash
$ cat /sys/bus/pci/devices/0000:07:00.0/numa_node    # GPU
0
$ cat /sys/class/net/ib0/device/numa_node             # NIC
1
# Cross-NUMA confirmed
```

### numactl

```bash
$ numactl --hardware
available: 2 nodes (0-1)
node 0 cpus: 0-19
node 1 cpus: 20-39
node distances:
node   0   1
  0:  10  21
  1:  21  10
```

Distance 21 vs local 10 quantifies the penalty.

## Fixing Cross-NUMA Issues

### Option 1: Physical Slot Relocation

Move the GPU or NIC to a PCIe slot on the same socket. Only fix that eliminates the penalty entirely.

```bash
# Which slots are on which NUMA node
$ for dev in /sys/bus/pci/devices/*/numa_node; do
    echo "$(dirname $dev | xargs basename): $(cat $dev)"
  done | sort -t: -k2 -n
```

### Option 2: numactl Binding

Bind the application to the GPU's NUMA node. Does not fix NIC crossing, but keeps CPU and memory local to the GPU:

```bash
$ CUDA_VISIBLE_DEVICES=0 numactl --cpunodebind=0 --membind=0 ./train.py
```

### Option 3: Select the Right NIC

Route GPU traffic through the NIC on the same NUMA node:

```bash
$ cat /sys/class/net/ib0/device/numa_node   # 1
$ cat /sys/class/net/ib1/device/numa_node   # 0 -- use this for GPU0
$ ip route add 10.0.0.0/24 dev ib1
```

For NCCL multi-GPU training:

```bash
$ export NCCL_SOCKET_IFNAME=ib1
$ export NCCL_IB_HCA=mlx5_1   # HCA on same NUMA node as GPU
```

### Option 4: IRQ Affinity

Pin NIC interrupts to CPUs on the GPU's NUMA node:

```bash
$ cat /proc/interrupts | grep mlx5
$ echo 000fffff > /proc/irq/<irq_num>/smp_affinity
```

## GPUDirect RDMA

GPUDirect RDMA lets the NIC DMA directly to/from GPU memory, bypassing host memory. Extremely sensitive to PCIe topology:

1. **Same NUMA node** -- full bandwidth
2. **Cross-NUMA** -- works but reduced bandwidth (DMA still crosses inter-socket link)
3. **Behind PCIe switch** -- best case, peer-to-peer stays within the switch

```bash
$ lsmod | grep nv_peer_mem                              # loaded?
$ NCCL_DEBUG=INFO ./my_app 2>&1 | grep -i "gpu direct"  # active?
```

melisai's cross-NUMA detection is particularly valuable here: a misconfigured topology turns a zero-copy path into a two-hop DMA with worse performance than regular host-staged transfers.

## Design Decisions

**Why nvidia-smi instead of NVML?** Avoids CGO dependency on libnvidia-ml.so. Keeps the build static. Works even when NVML headers don't match the driver version.

**Why Tier 1?** sysfs is world-readable. nvidia-smi needs no root. Entire collector runs unprivileged.

**Why `nil, nil` return?** A server without GPUs should not have a GPU section with empty arrays. Nil means "not applicable."

**Why Warning=1 and Critical=1?** There is no "slightly cross-NUMA." Either your topology is correct or it is not.

## Quick Reference

| What | Where |
|------|-------|
| Collector source | `internal/collector/gpu.go` |
| Model types | `internal/model/types.go` (GPUDevice, PCIeTopology, CrossNUMAPair) |
| Anomaly rule | `internal/model/anomaly.go` (`gpu_nic_cross_numa`) |
| GPU NUMA sysfs | `/sys/bus/pci/devices/<bus_id>/numa_node` |
| NIC NUMA sysfs | `/sys/class/net/<iface>/device/numa_node` |
| nvidia-smi topology | `nvidia-smi topo -m` |
| NUMA hardware info | `numactl --hardware` |
| Visual topology | `lstopo` (from hwloc package) |

---

*Next: [Chapter 19 — Page Reclaim & THP](19-page-reclaim-thp.md)*
