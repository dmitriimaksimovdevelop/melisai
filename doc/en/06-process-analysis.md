# Chapter 6: Process Analysis — Deep Dive

## Overview

Sometimes the aggregate system metrics look fine, but a single process is causing all the pain. The `ProcessCollector` (`internal/collector/process.go`) identifies the top consumers of CPU and memory.

## Source File: process.go

- **Lines**: 228
- **Functions**: 5
- **Data Sources**: `/proc/[pid]/stat`, `/proc/[pid]/fd/`, `/proc/meminfo`

## Function Walkthrough

### Collect() — Top-N Process Discovery

```go
func (c *ProcessCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    totalMem := c.getTotalMemory()   // MemTotal for % calculation
    clkTck := 100.0                  // Clock ticks per second (standard Linux)

    pids1 := c.readAllPIDs()         // First CPU sample
    // Wait 1 second
    pids2 := c.readAllPIDs()         // Second CPU sample

    for pid, p2 := range pids2 {
        // Count process states
        switch p2.state {
        case "R": running++
        case "S", "D": sleeping++
        case "Z": zombie++
        }

        // CPU delta: (utime2+stime2 - utime1-stime1) / clkTck / interval × 100
        p1, ok := pids1[pid]
        if ok {
            totalTimeDelta := float64((p2.utime + p2.stime) - (p1.utime + p1.stime))
            cpuPct = totalTimeDelta / clkTck / interval.Seconds() * 100
        }

        // Memory: RSS pages × 4KB / total × 100
        memPct = float64(p2.rss*4096) / float64(totalMem) * 100
    }

    // Sort by CPU → top 20
    // Sort by Memory → top 20
}
```

### readProcPID() — Parsing /proc/[pid]/stat

```go
func (c *ProcessCollector) readProcPID(pid int) (procStat, error) {
    statData, _ := os.ReadFile(filepath.Join(pidPath, "stat"))
    // /proc/[pid]/stat format:
    // "1234 (process name) S 5678 1234 1234 0 -1 4194304 301 0 0 0 100 50 ..."
    //  PID  (comm)        state ...          utime stime ...

    // Tricky: comm can contain spaces and parens, e.g. "(Web Content)"
    commStart := strings.Index(statStr, "(")
    commEnd := strings.LastIndex(statStr, ")")
    comm := statStr[commStart+1 : commEnd]
    rest := strings.Fields(statStr[commEnd+2:])

    // rest[0]=state, rest[11]=utime, rest[12]=stime, rest[17]=threads, rest[21]=rss
    ps.state = rest[0]
    ps.utime, _ = strconv.ParseUint(rest[11], 10, 64)
    ps.stime, _ = strconv.ParseUint(rest[12], 10, 64)
    ps.threads, _ = strconv.Atoi(rest[17])
    ps.rss, _ = strconv.ParseInt(rest[21], 10, 64)

    // Count open file descriptors
    fdEntries, _ := os.ReadDir(filepath.Join(pidPath, "fd"))
    ps.fds = len(fdEntries)
}
```

**The parenthesis trick**: `/proc/[pid]/stat` embeds the command name in parentheses. Since command names can contain spaces, parentheses, and any character, you must find the LAST `)` to correctly parse the remaining fields. This is a classic gotcha in proc parsing.

## Process States

| State | Symbol | Meaning | In melisai |
|-------|--------|---------|-----------|
| Running | R | Currently executing or in run queue | `running` counter |
| Sleeping | S | Interruptible sleep — waiting for event | `sleeping` counter |
| Disk Sleep | D | Uninterruptible sleep — waiting for I/O | `sleeping` counter |
| Zombie | Z | Exited but parent hasn't called wait() | `zombie` counter |
| Stopped | T | Stopped by signal (SIGSTOP) | Not counted |

**Zombie processes**: A zombie is a process that has finished executing but its parent hasn't read its exit status (`wait()`). Each zombie holds a PID and small kernel struct. A few zombies are normal — hundreds indicate a parent that doesn't reap children.

## File Descriptors

Every open file, socket, pipe, and device uses a file descriptor. melisai counts FDs per process using `/proc/[pid]/fd/`:

| FD Count | Assessment |
|----------|-----------|
| < 100 | Normal for most processes |
| 100-1000 | Normal for servers (one FD per connection) |
| 1000-10000 | High — check if FDs are being leaked |
| > 10000 | Likely FD leak — check `ulimit -n` |

## What to Look For

### CPU Hog
```json
{
  "top_by_cpu": [
    {"pid": 1234, "comm": "java", "cpu_pct": 780.0, "threads": 200}
  ]
}
```
780% CPU = using ~8 cores. For a Java app with 200 threads, check GC, thread contention.

### Memory Leak
```json
{
  "top_by_mem": [
    {"pid": 5678, "comm": "node", "mem_rss": 15032385536, "mem_pct": 92.0, "fds": 12000}
  ]
}
```
14GB RSS (92% of total memory) + 12K file descriptors = both memory and FD leak.

---

*Next: [Chapter 7 — Container Analysis](07-container-analysis.md)*
