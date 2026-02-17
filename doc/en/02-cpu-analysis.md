# Chapter 2: CPU Analysis — Deep Dive

## Overview

The CPU is the most fundamental resource. When it's overloaded, everything slows down. But "CPU is high" is not a diagnosis — you need to know **why** it's high and **what kind** of load it is.

sysdiag's `CPUCollector` (`internal/collector/cpu.go`) answers these questions by reading `/proc/stat` twice and computing the delta.

## Source File: cpu.go

- **Lines**: 207
- **Functions**: 10
- **Tier**: 1 (always available, no root needed)

## The Collector Interface

Every collector in sysdiag implements this interface:

```go
type Collector interface {
    Name() string                                              // e.g. "cpu_utilization"
    Category() string                                          // e.g. "cpu"
    Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error)
    Available() Availability                                   // tier + reason
}
```

For CPUCollector:
- `Name()` → `"cpu_utilization"`
- `Category()` → `"cpu"`
- `Available()` → `Availability{Tier: 1}` (always available)

## Data Structures

### cpuTimes — Raw Kernel Counters

```go
type cpuTimes struct {
    user    uint64  // Running user-space code
    nice    uint64  // Running user-space code at lower priority
    system  uint64  // Running kernel code (syscalls)
    idle    uint64  // Doing nothing
    iowait  uint64  // Idle, waiting for I/O
    irq     uint64  // Handling hardware interrupts
    softirq uint64  // Handling software interrupts
    steal   uint64  // Time stolen by hypervisor (VMs)
}
```

These are raw jiffie counts from `/proc/stat`. Each jiffy represents a tick of the system clock (typically 10ms on a 100 HZ kernel).

The `total()` method sums all states:

```go
func (t cpuTimes) total() uint64 {
    return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}
```

### CPUData — The Output

```go
type CPUData struct {
    UserPct    float64  // user + nice as percentage
    SystemPct  float64  // kernel time percentage
    IOWaitPct  float64  // I/O wait percentage
    IdlePct    float64  // idle percentage
    StealPct   float64  // hypervisor steal
    IRQPct     float64  // hardware interrupt handling
    SoftIRQPct float64  // software interrupt handling

    LoadAvg1   float64  // 1-minute load average
    LoadAvg5   float64  // 5-minute load average
    LoadAvg15  float64  // 15-minute load average
    NumCPUs    int      // logical CPU count

    ContextSwitchesPerSec int64  // context switches per second
    PerCPU []PerCPU              // per-core breakdown

    SchedLatencyNS        int64  // CFS sched_latency_ns
    SchedMinGranularityNS int64  // CFS sched_min_granularity_ns
}
```

## Function-by-Function Walkthrough

### Collect() — The Entry Point

```go
func (c *CPUCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    startTime := time.Now()

    // First sample
    sample1, perCPU1, ctxSw1 := c.readProcStat()

    // Wait for interval (default 1 second)
    interval := cfg.SampleInterval
    if interval == 0 {
        interval = 1 * time.Second
    }
    select {
    case <-time.After(interval):
    case <-ctx.Done():
        return nil, ctx.Err()  // cancelled or timed out
    }

    // Second sample
    sample2, perCPU2, ctxSw2 := c.readProcStat()

    // Compute deltas
    data := c.computeDelta(sample1, sample2)
    // ... context switches, load avg, per-CPU, scheduler params ...
}
```

**Key design decisions:**

1. **Two-point sampling**: We read `/proc/stat` twice, 1 second apart. The delta tells us what happened during that interval.

2. **Context cancellation**: The `select` statement ensures that if the orchestrator cancels (SIGINT, timeout), the collector returns immediately instead of blocking.

3. **Configurable interval**: The `CollectConfig.SampleInterval` defaults to 1 second, but can be extended for more accurate measurements.

### readProcStat() — Parsing /proc/stat

```go
func (c *CPUCollector) readProcStat() (cpuTimes, map[int]cpuTimes, uint64) {
    f, err := os.Open(filepath.Join(c.procRoot, "stat"))
    // ...
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        fields := strings.Fields(line)
        if fields[0] == "cpu" && len(fields) >= 9 {
            aggregate = parseCPULine(fields)        // Total across all CPUs
        } else if strings.HasPrefix(fields[0], "cpu") {
            cpuNum, _ := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
            perCPU[cpuNum] = parseCPULine(fields)   // Per-CPU breakdown
        } else if fields[0] == "ctxt" {
            ctxSwitches, _ = strconv.ParseUint(fields[1], 10, 64)
        }
    }
    return aggregate, perCPU, ctxSwitches
}
```

The function returns three values:
1. **Aggregate** CPU times (across all cores)
2. **Per-CPU** times (map: CPU number → cpuTimes)
3. **Context switch count** (total since boot)

### parseCPULine() — Extracting Fields

```go
func parseCPULine(fields []string) cpuTimes {
    parse := func(idx int) uint64 {
        if idx >= len(fields) { return 0 }
        v, _ := strconv.ParseUint(fields[idx], 10, 64)
        return v
    }
    return cpuTimes{
        user:    parse(1),   // field[1] = user jiffies
        nice:    parse(2),   // field[2] = nice jiffies
        system:  parse(3),   // field[3] = system jiffies
        idle:    parse(4),   // field[4] = idle jiffies
        iowait:  parse(5),   // field[5] = iowait jiffies
        irq:     parse(6),   // field[6] = hardware interrupt jiffies
        softirq: parse(7),   // field[7] = software interrupt jiffies
        steal:   parse(8),   // field[8] = steal jiffies (VMs)
    }
}
```

**Why field indices start at 1**: Field 0 is the label (`cpu`, `cpu0`, etc.), so the actual counters start at field 1.

### computeDelta() — Percentage Calculation

```go
func (c *CPUCollector) computeDelta(before, after cpuTimes) *model.CPUData {
    totalDelta := float64(after.total() - before.total())
    if totalDelta == 0 {
        return &model.CPUData{}  // no CPU time passed (impossible in practice)
    }
    return &model.CPUData{
        UserPct:    float64(after.user - before.user + after.nice - before.nice) / totalDelta * 100,
        SystemPct:  float64(after.system - before.system) / totalDelta * 100,
        IOWaitPct:  float64(after.iowait - before.iowait) / totalDelta * 100,
        IdlePct:    float64(after.idle - before.idle) / totalDelta * 100,
        StealPct:   float64(after.steal - before.steal) / totalDelta * 100,
        IRQPct:     float64(after.irq - before.irq) / totalDelta * 100,
        SoftIRQPct: float64(after.softirq - before.softirq) / totalDelta * 100,
    }
}
```

**Key formula**:
```
percentage = (after_state - before_state) / (after_total - before_total) × 100
```

This works because all CPU states are mutually exclusive and exhaustive — they always sum to 100%.

**Note**: `user` and `nice` are combined into `UserPct`. Both represent user-space code, just at different scheduling priorities.

### computePerCPUDeltas() — Per-Core Analysis

```go
func (c *CPUCollector) computePerCPUDeltas(before, after map[int]cpuTimes) []model.PerCPU {
    var result []model.PerCPU
    for cpuNum, afterTimes := range after {
        beforeTimes, ok := before[cpuNum]
        if !ok { continue }

        totalDelta := float64(afterTimes.total() - beforeTimes.total())
        if totalDelta == 0 { continue }

        result = append(result, model.PerCPU{
            CPU:       cpuNum,
            UserPct:   float64(afterTimes.user - beforeTimes.user + ...) / totalDelta * 100,
            SystemPct: ...,
            IOWaitPct: ...,
            IdlePct:   ...,
        })
    }
    return result
}
```

**Why per-CPU matters**: On a 32-core server, aggregate CPU might show 50% utilization, but a single-threaded bottleneck will show 1 core at 100% and 31 cores idle. Per-CPU data reveals this.

### readLoadAvg() — Saturation Indicator

```go
func (c *CPUCollector) readLoadAvg() (float64, float64, float64) {
    data, _ := os.ReadFile(filepath.Join(c.procRoot, "loadavg"))
    // "1.23 0.98 0.76 2/1234 56789"
    //  ^1m   ^5m  ^15m  ^running/total  ^last_pid
    fields := strings.Fields(string(data))
    la1, _ := strconv.ParseFloat(fields[0], 64)
    la5, _ := strconv.ParseFloat(fields[1], 64)
    la15, _ := strconv.ParseFloat(fields[2], 64)
    return la1, la5, la15
}
```

**Load average** is the average number of processes in the run queue OR in uninterruptible sleep (D state). It includes processes waiting for I/O.

**Interpreting load average:**
| Load / # CPUs | Meaning |
|--------------|---------|
| < 1.0 | Under capacity — room for more work |
| = 1.0 | At capacity — no spare cycles |
| > 1.0 | Over capacity — work is queuing |
| > 2.0 | Significant queuing — noticeable latency |
| > 4.0 | Heavily overloaded |

sysdiag normalizes load average by dividing by the number of CPUs:
```go
ratio := cpu.LoadAvg1 / float64(cpu.NumCPUs)
```
A ratio > 1.0 means saturation.

## CFS Scheduler Parameters

The Completely Fair Scheduler (CFS) uses two parameters that affect latency:

- **`sched_latency_ns`** (default: 6ms): The target time for one complete scheduling round. All runnable tasks should get to run within this time window.

- **`sched_min_granularity_ns`** (default: 0.75ms): Minimum time a task runs before being preempted. Prevents excessive context switching.

sysdiag reads these from:
```
/proc/sys/kernel/sched_latency_ns
/proc/sys/kernel/sched_min_granularity_ns
```

If `sched_latency_ns` is very high (e.g., 24ms on some cloud providers), scheduling latency will be poor — but throughput is better. This is a classic latency-throughput tradeoff.

## Context Switches

Every time the CPU switches from one process to another, it performs a **context switch**. The cost is typically 1-10 microseconds, but at very high rates (>100K/sec), the overhead becomes significant.

sysdiag computes context switches **per second**:
```go
ctxSwDelta := ctxSw2 - ctxSw1
data.ContextSwitchesPerSec = int64(float64(ctxSwDelta) / interval.Seconds())
```

**Interpreting context switch rates:**
| Rate | Assessment |
|------|-----------|
| < 10K/s | Normal for most workloads |
| 10K-50K/s | Active system, check if voluntary |
| 50K-200K/s | High. Either many threads or locking contention |
| > 200K/s | Very high. Likely indicates thread/lock issues |

## What to Look For in CPU Results

### Healthy System
```json
{
  "user_pct": 25.0,
  "system_pct": 5.0,
  "iowait_pct": 0.1,
  "idle_pct": 69.9,
  "load_avg_1": 2.5,
  "num_cpus": 8
}
```
Load per CPU = 2.5/8 = 0.31 — well under capacity.

### CPU-Bound Application
```json
{
  "user_pct": 92.0,
  "system_pct": 3.0,
  "idle_pct": 5.0,
  "per_cpu": [
    {"cpu": 0, "user_pct": 99.0, "idle_pct": 1.0},
    {"cpu": 1, "user_pct": 2.0,  "idle_pct": 98.0},
    ...
  ]
}
```
One core at 99% user = single-threaded bottleneck. The application needs parallelization.

### I/O Wait Problem
```json
{
  "user_pct": 5.0,
  "system_pct": 3.0,
  "iowait_pct": 45.0,
  "idle_pct": 47.0
}
```
High IOWait = processes are blocked waiting for disk. Check disk latency next.

### VM Steal Issue
```json
{
  "user_pct": 30.0,
  "steal_pct": 25.0,
  "idle_pct": 40.0
}
```
25% steal = the hypervisor is giving 25% of your CPU time to other VMs. You need a bigger instance or dedicated CPU.

### Interrupt Storm
```json
{
  "softirq_pct": 35.0,
  "context_switches_per_sec": 180000
}
```
High softirq = heavy network processing (especially on the core handling interrupts). Check for IRQ affinity and consider RSS (Receive Side Scaling).

---

*Next: [Chapter 3 — Memory Analysis](03-memory-analysis.md)*
