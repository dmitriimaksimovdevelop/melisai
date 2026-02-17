# Chapter 7: Container Analysis — Deep Dive

## Overview

Containers (Docker, Kubernetes) use Linux cgroups to limit resources. The most insidious container problem is **CPU throttling** — your container silently pauses when it exceeds its CPU quota. The application just sees mysterious latency spikes.

sysdiag's `ContainerCollector` (`internal/collector/container.go`) detects the container runtime, version, and reads cgroup metrics that reveal throttling and memory pressure.

## Source File: container.go

- **Lines**: 246
- **Functions**: 10
- **Data Sources**: Filesystem probes, `/proc/1/cgroup`, `/sys/fs/cgroup/`

## Function Walkthrough

### detectRuntime() — Are We in a Container?

Detection order is important — check most specific first:

```go
func (c *ContainerCollector) detectRuntime() string {
    // 1. Kubernetes: service account directory exists
    if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err == nil {
        return "kubernetes"
    }
    // 2. Docker: /.dockerenv file exists
    if _, err := os.Stat("/.dockerenv"); err == nil {
        return "docker"
    }
    // 3. Cgroup-based detection: check /proc/1/cgroup for patterns
    if strings.Contains(content, "kubepods")   → "kubernetes"
    if strings.Contains(content, "docker")     → "docker"
    if strings.Contains(content, "containerd") → "docker"
    if strings.Contains(content, "lxc")        → "lxc"
    // 4. Not in a container
    return "none"
}
```

**Why check `/proc/1/cgroup` last?** The filesystem-based checks (`/var/run/secrets`, `/.dockerenv`) are faster and more reliable. Cgroup-based detection is a fallback for unusual container runtimes.

### detectCgroupVersion() — v1 or v2?

```go
func (c *ContainerCollector) detectCgroupVersion() int {
    // v2: unified hierarchy has cgroup.controllers
    if os.Stat("/sys/fs/cgroup/cgroup.controllers") == nil { return 2 }
    // v1: separate cpu controller directory
    if os.Stat("/sys/fs/cgroup/cpu") == nil { return 1 }
    return 0  // no cgroup support
}
```

### extractContainerID() — Getting the Container ID

```go
func (c *ContainerCollector) extractContainerID(cgroupPath string) string {
    // Parses: /docker/<64-hex-id>
    //         /kubepods/pod<uid>/<64-hex-id>
    //         docker-<64-hex-id>.scope
    // Searches from the end of the path for 64-character hex strings
    for i := len(parts) - 1; i >= 0; i-- {
        if len(part) == 64 && isHex(part) { return part }
        if strings.HasPrefix(part, "docker-") && strings.HasSuffix(part, ".scope") {
            // Extract ID from "docker-<id>.scope" format
        }
    }
}
```

### collectCgroupV2Metrics() — Modern CGroup Metrics

```go
// cpu.max → "100000 100000" (quota period) or "max 100000" (unlimited)
// cpu.stat → nr_throttled, throttled_usec
// memory.max → bytes or "max"
// memory.current → bytes
```

### collectCgroupV1Metrics() — Legacy CGroup Metrics

```go
// cpu/cpu.cfs_quota_us → microseconds, -1 = unlimited
// cpu/cpu.cfs_period_us → microseconds (default 100000)
// cpu/cpu.stat → nr_throttled, throttled_time (nanoseconds!)
// memory/memory.limit_in_bytes → bytes
// memory/memory.usage_in_bytes → bytes
```

**Critical difference**: v1 reports `throttled_time` in ***nanoseconds***, v2 in ***microseconds***. sysdiag normalizes v1 by dividing by 1000.

## Cgroup CPU Throttling — The Silent Killer

### How It Works (CFS Bandwidth Control)

```
┌─ Period = 100ms (default) ──────────────────────────────┐
│                                                          │
│  Quota = 50ms            Throttled zone                  │
│  ┌─────────────────────┐ ┌──────────────────────────────┐
│  │  Process runs       │ │  Process is FROZEN.           │
│  │  using CPU          │ │  No code executes.            │
│  │                     │ │  Just wait.                   │
│  └─────────────────────┘ └──────────────────────────────┘
│  0ms                50ms                              100ms
└──────────────────────────────────────────────────────────┘
         Next period starts: quota resets
```

With `cpu.max = "50000 100000"`:
- The container gets 50ms of CPU per 100ms period = 0.5 CPU cores
- If it tries to use more, the kernel **suspends all threads** until the period resets
- `nr_throttled` increments by 1
- `throttled_usec` accumulates the frozen time

### Interpreting Throttling Metrics

| nr_throttled | Assessment |
|-------------|-----------|
| 0 | No throttling — quota is sufficient |
| < 100 | Occasional bursts, usually acceptable |
| 100-1000 | Regular throttling — increase CPU limit |
| > 1000 | Severe throttling — application is CPU-starved |

## Container Memory Limits

```
memory.max = 2147483648        ← 2GB limit
memory.current = 1879048192   ← 1.75GB currently used (87.5%)

If current > max → OOM kill!
```

sysdiag reports this as a USE metric: `container_memory.utilization = current / limit × 100`

## Diagnostic Examples

### Healthy Container
```json
{
  "runtime": "kubernetes",
  "cgroup_version": 2,
  "pod_name": "api-server-abc12",
  "namespace": "production",
  "cpu_quota": 200000,
  "cpu_period": 100000,
  "cpu_throttled_periods": 0,
  "memory_limit": 8589934592,
  "memory_usage": 4294967296
}
```
2 CPU cores allocated, zero throttling, 50% memory used.

### CPU Throttled Container
```json
{
  "runtime": "kubernetes",
  "cpu_quota": 100000,
  "cpu_period": 100000,
  "cpu_throttled_periods": 5847,
  "cpu_throttled_time": 234000000,
  "memory_usage": 6442450944,
  "memory_limit": 8589934592
}
```
1 CPU core limit, 5847 throttle events, 234 seconds total throttled time. Increase CPU limit to at least 2 cores.

---

*Next: [Chapter 8 — System Information Collector](08-system-collector.md)*
