# Chapter 9: BCC Tools — Deep Kernel Tracing

## Overview

Tier 1 collectors read counters from `/proc` — they tell you **what** is happening (CPU is 90% busy). But they can't tell you **why** (which process, which function, what latency distribution).

That's where BCC (BPF Compiler Collection) tools come in. They attach eBPF programs to kernel functions and trace events in real-time, giving you histograms, per-event details, and stack traces.

sysdiag's executor package (`internal/executor/`) manages running these tools securely and parsing their output.

## Source Files: executor/ (5 files)

| File | Lines | Purpose |
|------|-------|---------|
| `executor.go` | 133 | `BCCExecutor` — runs external binaries with security checks |
| `security.go` | 133 | `SecurityChecker` — verifies binary integrity |
| `registry.go` | 181 | `Registry` — catalog of 20 BCC tools |
| `parsers.go` | 463 | Output parsers for histograms, tables, stacks |
| `aggregate.go` | 149 | Event aggregation (top-N, connections) |

## The Executor: How BCC Tools Are Run

### BCCExecutor.Run()

```go
func (e *BCCExecutor) Run(ctx context.Context, toolName string, duration time.Duration) (*model.Result, error) {
    spec, ok := Registry[toolName]          // Look up tool specification
    binary := e.security.ResolveBinary(spec.Binary)  // Find and verify path
    args := spec.BuildArgs(duration)         // Build CLI arguments
    env := e.security.SanitizeEnv()          // Clean environment

    cmd := exec.CommandContext(ctx, binary, args...)
    cmd.Env = env
    cmd.Stdout = NewLimitedWriter(50 * 1024 * 1024)  // Cap output at 50MB

    cmd.Start()
    // Wait for completion or context cancellation
    cmd.Wait()

    // Parse output using tool-specific parser
    result := spec.Parser(output)
}
```

### LimitedWriter — Output Protection

```go
type LimitedWriter struct {
    buf   bytes.Buffer
    limit int
}

func (w *LimitedWriter) Write(p []byte) (int, error) {
    if w.buf.Len() + len(p) > w.limit {
        return 0, ErrOutputLimitExceeded  // Stop accepting data
    }
    return w.buf.Write(p)
}
```

**Why limit output?** Some BCC tools (like `execsnoop`, `tcpdrop`) produce per-event output. On a busy server, this can generate gigabytes. The 50MB cap prevents memory exhaustion.

## Security Model

### Why Security Matters

BCC tools run as root and can trace ANY kernel function. Compromised binaries could:
- Read arbitrary kernel memory
- Modify system calls
- Log sensitive data (passwords, crypto keys)

### SecurityChecker

```go
func (s *SecurityChecker) ResolveBinary(name string) string {
    for _, dir := range AllowedBinaryPaths {
        path := filepath.Join(dir, name)
        if s.VerifyBinary(path) { return path }
    }
    return ""  // Binary not found in allowed paths
}

func (s *SecurityChecker) VerifyBinary(path string) bool {
    info, _ := os.Stat(path)
    stat := info.Sys().(*syscall.Stat_t)
    // Must be owned by root (UID 0)
    if stat.Uid != 0 { return false }
    // Must not be world-writable
    if info.Mode()&0002 != 0 { return false }
    return true
}
```

### Allowed Binary Paths

```go
var AllowedBinaryPaths = []string{
    "/usr/share/bcc/tools",      // Ubuntu/Debian bcc-tools
    "/usr/sbin",                  // System binaries
    "/usr/bin",                   // Standard PATH
    "/sbin",                      // Legacy system binaries
    "/snap/bpftrace/current/bin", // Snap package
}
```

### Environment Sanitization

```go
func (s *SecurityChecker) SanitizeEnv() []string {
    return []string{
        "PATH=/usr/sbin:/usr/bin:/sbin:/bin",
        "HOME=/root",
        "LANG=C",
    }
    // Removes LD_PRELOAD, LD_LIBRARY_PATH, and all other
    // potentially dangerous environment variables
}
```

## Tool Registry — The Complete Catalog

### CPU Tools

| Tool | What It Traces | Output Type |
|------|---------------|-------------|
| **runqlat** | Time spent in CPU run queue | Histogram (μs) |
| **runqlen** | Run queue length | Histogram (count) |
| **cpudist** | On-CPU time per process | Histogram (μs) |
| **hardirqs** | Hardware interrupt time | Histogram (μs) |
| **softirqs** | Software interrupt time | Histogram (μs) |
| **profile** | CPU stack sampling (flame graph) | Folded stacks |
| **offcputime** | Off-CPU stack traces (why blocked?) | Folded stacks |

### Disk Tools

| Tool | What It Traces | Output Type |
|------|---------------|-------------|
| **biolatency** | Block I/O latency per device | Histogram (μs) |
| **biosnoop** | Every block I/O operation | Table (per-event) |
| **ext4slower** | Slow ext4 filesystem operations | Table (per-event) |
| **fileslower** | Slow file reads/writes (>10ms) | Table (per-event) |
| **bitesize** | I/O size distribution | Histogram (bytes) |

### Network Tools

| Tool | What It Traces | Output Type |
|------|---------------|-------------|
| **tcpconnlat** | TCP connection establishment time | Table (per-event) |
| **tcpretrans** | TCP retransmissions | Table (per-event) |
| **tcprtt** | TCP round-trip time | Histogram (μs) |
| **gethostlatency** | DNS resolution time | Table (per-event) |
| **tcpdrop** | Dropped TCP packets (with reason) | Table (per-event) |
| **tcpstates** | TCP state transitions | Table (per-event) |

### Other Tools

| Tool | What It Traces | Output Type |
|------|---------------|-------------|
| **cachestat** | Page cache hit/miss ratio | Table (per-interval) |
| **execsnoop** | New process execution | Table (per-event) |

## Output Parsers

### ParseHistogram() — Power-of-2 Distributions

BCC histogram output looks like:

```
     usecs               : count    distribution
         0 -> 1          : 0        |                                        |
         2 -> 3          : 0        |                                        |
         4 -> 7          : 15       |****                                    |
         8 -> 15         : 107      |*****************************           |
        16 -> 31         : 145      |****************************************|
        32 -> 63         : 83       |***********************                 |
        64 -> 127        : 12       |***                                     |
       128 -> 255        : 3        |*                                       |
```

The parser converts this to:

```go
type Histogram struct {
    Name    string         // e.g. "runqlat"
    Unit    string         // e.g. "usecs"
    Buckets []HistBucket   // [{Low:0, High:1, Count:0}, {Low:2, High:3, Count:0}, ...]
    P50     float64        // 50th percentile (median)
    P90     float64        // 90th percentile
    P99     float64        // 99th percentile
    P999    float64        // 99.9th percentile
}
```

**Percentile calculation**: The parser iterates through buckets, accumulating counts until reaching the target percentage. The actual percentile value is the midpoint of the target bucket.

### ParseTabularEvents() — Per-Event Data

```
PID    COMM         LAT(ms) RADDR            RPORT
5234   curl         1.52    203.0.113.50     443
8901   python       23.41   10.0.0.5         5432
```

Parsed into:

```go
type Event struct {
    Fields map[string]string  // {"PID":"5234", "COMM":"curl", "LAT":"1.52", ...}
}
```

### ParseFoldedStacks() — Flame Graph Data

```
main;handleRequest;db.Query;net.Write 42
main;handleRequest;json.Marshal 15
main;handleRequest;log.Printf 3
```

Each line: semicolon-separated stack (bottom→top) + sample count.

## Aggregation

### AggregateByField() — Top-N Analysis

```go
func AggregateByField(events []model.Event, field string, topN int) []AggregatedEntry {
    // Groups events by field value, computes count, average, and total
    // Example: group tcpconnlat events by RADDR → top 10 slowest destinations
}
```

### AggregateConnections() — Network Summary

```go
func AggregateConnections(events []model.Event) []ConnectionSummary {
    // Groups by source→destination, computes connection count and avg latency
}
```

## Interpreting BCC Results

### runqlat — How long do processes wait for CPU?

| Percentile | Good | Warning | Critical |
|-----------|------|---------|----------|
| p50 | < 10μs | 10-100μs | > 100μs |
| p99 | < 100μs | 100-1000μs | > 1ms |

### biolatency — How long do disk operations take?

| Percentile | SSD | HDD | Critical |
|-----------|-----|-----|----------|
| p50 | < 100μs | < 5ms | — |
| p99 | < 1ms | < 20ms | > 50ms |

### tcpconnlat — Connection time

| Latency | Meaning |
|---------|---------|
| < 1ms | Same datacenter |
| 1-5ms | Same region |
| 5-50ms | Cross-region |
| > 100ms | Cross-continent or network issue |
| > 1000ms | DNS or routing problem |

---

*Next: [Chapter 10 — Native eBPF](10-ebpf-native.md)*
