# Chapter 0: Introduction

## What is sysdiag?

**sysdiag** is a single Go binary that performs comprehensive Linux server performance analysis. It collects metrics from the kernel, analyzes them, and produces a structured JSON report that can be consumed by humans, AI/LLM, or monitoring systems.

Think of it as running 30+ diagnostic commands at once, but with structured output and automatic analysis.

## The Problem: "My Server is Slow"

Every system administrator has heard this. The server is slow. But what does "slow" mean?

- Is the CPU overloaded?
- Is the application waiting for disk I/O?
- Is there a memory leak causing swap usage?
- Is the network dropping packets?
- Is a container hitting its CPU quota?

Without a systematic approach, you end up running random commands and guessing. **sysdiag** provides that systematic approach.

## The USE Methodology

sysdiag is built around Brendan Gregg's **USE Methodology** — a framework for analyzing system performance. USE stands for:

| Letter | Meaning | Question to Ask |
|--------|---------|-----------------|
| **U** | Utilization | How busy is this resource? (0–100%) |
| **S** | Saturation | Is work queuing up? (runqueue, swap, IO queue) |
| **E** | Errors | Are there error events? (drops, retransmits, ECC) |

You apply these three questions to every system resource:

```
┌──────────┬──────────────────────┬──────────────────────┬────────────────────┐
│ Resource │ Utilization          │ Saturation           │ Errors             │
├──────────┼──────────────────────┼──────────────────────┼────────────────────┤
│ CPU      │ 100% - idle%         │ load_avg / num_cpus  │ —                  │
│ Memory   │ (total-available)/   │ swap_used / swap_    │ major page faults  │
│          │ total × 100          │ total × 100          │ (OOM events)       │
│ Disk     │ io_time / interval   │ io_in_progress       │ device errors      │
│ Network  │ bandwidth usage      │ drops, overflows     │ errors, retrans    │
└──────────┴──────────────────────┴──────────────────────┴────────────────────┘
```

### Why USE Works

Without USE, you might check CPU utilization and stop. But high CPU utilization without saturation is perfectly fine — it means you're using what you paid for. The problem starts when saturation appears (work queuing) or errors occur.

USE guarantees you check **all three dimensions** for **every resource**, so you never miss a bottleneck.

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                        sysdiag binary                            │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────────────┐ │
│  │   CLI       │    │ Orchestrator │    │    Output            │ │
│  │  (cobra)    │───▶│  (parallel)  │───▶│  JSON / FlameGraph   │ │
│  └─────────────┘    │  + profiles  │    │  + AI Prompt         │ │
│                     │  + signals   │    └──────────────────────┘ │
│                     └──────┬───────┘                             │
│                            │                                     │
│              ┌─────────────┼─────────────┐                       │
│              ▼             ▼             ▼                       │
│  ┌───────────────┐ ┌──────────────┐ ┌──────────────┐            │
│  │  Tier 1       │ │  Tier 2      │ │  Tier 3      │            │
│  │  Collectors   │ │  BCC         │ │  eBPF        │            │
│  │  (procfs/     │ │  Executor    │ │  (cilium/    │            │
│  │   sysfs)      │ │  + Security  │ │   ebpf)      │            │
│  │               │ │  + Parsers   │ │              │            │
│  │  7 collectors │ │  20 tools    │ │  BTF/CO-RE   │            │
│  └───────────────┘ └──────────────┘ └──────────────┘            │
│              │             │             │                       │
│              └─────────────┼─────────────┘                       │
│                            ▼                                     │
│              ┌──────────────────────────┐                        │
│              │     Model Layer          │                        │
│              │  • Report (JSON schema)  │                        │
│              │  • USE metrics           │                        │
│              │  • Anomaly detection     │                        │
│              │  • Health score (0-100)  │                        │
│              │  • Recommendations       │                        │
│              └──────────────────────────┘                        │
└──────────────────────────────────────────────────────────────────┘
```

## Tiered Collection

Not every Linux system has the same capabilities. A minimal container might only have `/proc`, while a bare-metal server with a modern kernel can use native eBPF. sysdiag handles this with three tiers:

### Tier 1 — procfs/sysfs (Always Available)

Reading virtual filesystems that the Linux kernel exposes to every process:

- `/proc/stat` — CPU utilization per core
- `/proc/meminfo` — memory breakdown
- `/proc/diskstats` — block device I/O counters
- `/proc/net/dev` — network interface statistics
- `/sys/block/*/queue/scheduler` — I/O scheduler parameters
- `/sys/fs/cgroup/` — container resource limits

**No root required.** Works on any Linux kernel. These are the 7 built-in collectors.

### Tier 2 — BCC Tools (Root + bcc-tools)

BCC (BPF Compiler Collection) tools trace kernel functions in real-time:

- `runqlat` — CPU scheduler latency histogram
- `biolatency` — block I/O latency per disk
- `tcpconnlat` — TCP connection establishment time
- `profile` — CPU flame graph via stack sampling

**Requires root** and the `bcc-tools` package. These tools give you latency distributions (histograms) that procfs cannot provide.

### Tier 3 — Native eBPF (Root + Kernel ≥ 5.8)

Using cilium/ebpf Go library to load BPF programs directly — no Python, no external dependencies:

- BTF (BPF Type Format) for CO-RE (Compile Once, Run Everywhere)
- Direct kernel tracing with zero external dependencies

**Requires root** and a modern kernel (≥ 5.8 with BTF support). This is the future of Linux observability.

### Automatic Fallback

```
Tier 3 available? ──yes──▶ Use native eBPF
       │no
       ▼
Tier 2 available? ──yes──▶ Use BCC tools
       │no
       ▼
Always: Use Tier 1 procfs collectors
```

## Collection Profiles

sysdiag supports three profiles that control how long collection runs and which tools are used:

| Profile | Duration | When to Use |
|---------|----------|-------------|
| `quick` | 10 seconds | Fast health check, CI/CD pipeline |
| `standard` | 30 seconds | Regular diagnostics, daily checks |
| `deep` | 60 seconds | Root cause analysis, includes stack traces |

## Report Structure

The output is a single JSON document designed for both human and machine consumption:

```json
{
  "metadata": {
    "tool": "sysdiag",
    "version": "0.2.0",
    "hostname": "web-server-01",
    "profile": "standard",
    "timestamp": "2024-01-15T10:30:00Z"
  },
  "system": {
    "os": "Ubuntu 22.04.3 LTS",
    "kernel": "5.15.0-91-generic",
    "uptime_seconds": 2592000
  },
  "categories": {
    "cpu": [ ... collector results ... ],
    "memory": [ ... ],
    "disk": [ ... ],
    "network": [ ... ],
    "process": [ ... ],
    "system": [ ... ]
  },
  "summary": {
    "health_score": 78,
    "anomalies": [
      { "severity": "warning", "message": "CPU utilization at 87.3%" }
    ],
    "resources": {
      "cpu": { "utilization_pct": 87.3, "saturation_pct": 2.1, "errors": 0 }
    },
    "recommendations": [
      { "title": "Enable TCP BBR", "commands": ["sysctl -w ..."] }
    ]
  }
}
```

## CLI Commands

```bash
# Primary command — collect system metrics
sudo sysdiag collect [flags]
  --profile string    Collection profile: quick|standard|deep (default "standard")
  --focus string      Focus areas (comma-separated): cpu,disk,network,stacks
  --output string     Output file path (default: stdout)
  --ai-prompt         Include AI analysis prompt in output

# Compare two reports
sysdiag diff <baseline.json> <current.json> [--json]

# Check system capabilities
sysdiag capabilities

# Install BPF tools
sudo sysdiag install
```

## Code Organization

```
cmd/sysdiag/main.go          ← CLI entry point, flag parsing
internal/
  collector/                  ← 7 Tier 1 data collectors
    collector.go              ← Collector interface + CollectConfig
    cpu.go                    ← /proc/stat sampling
    memory.go                 ← /proc/meminfo, vmstat, PSI, buddyinfo, NUMA
    disk.go                   ← /proc/diskstats, sysfs scheduler/queue
    network.go                ← /proc/net/dev, /proc/net/snmp, ss
    process.go                ← /proc/[pid]/stat, top-20 by CPU/memory
    container.go              ← cgroup v1/v2, Docker/K8s detection
    system.go                 ← OS info, filesystems, dmesg, sysctl readers
  executor/                   ← Tier 2 BCC tool runner
    executor.go               ← BCCExecutor with output capping
    security.go               ← Binary verification, env sanitization
    parsers.go                ← Histogram, tabular, folded stack parsers
    registry.go               ← 20 BCC tool specifications
    aggregate.go              ← Top-N event grouping
  ebpf/                       ← Tier 3 native eBPF
    btf.go                    ← BTF/CO-RE detection, capability assessment
    loader.go                 ← BPF program loader (stub with fallback)
  model/                      ← Data types + analysis
    types.go                  ← All struct definitions (Report, CPUData, etc.)
    report.go                 ← USE metric computation
    anomaly.go                ← 11 anomaly threshold rules
    health.go                 ← Weighted health score (0-100)
    recommendations.go        ← Sysctl tuning recommendations
  orchestrator/               ← Execution engine
    orchestrator.go           ← Parallel collection, signal handling
    profiles.go               ← quick/standard/deep profile definitions
  output/                     ← Report formatting
    json.go                   ← JSON writer (file or stdout)
    flamegraph.go             ← SVG flame graph generator
    ai_prompt.go              ← AI analysis prompt with anti-patterns
    progress.go               ← Collection progress reporter
  diff/                       ← Report comparison
    diff.go                   ← USE delta, histogram, regression detection
  installer/                  ← Dependency installer
    installer.go              ← Distro detection, package manager
```

## What's Next

The following chapters walk through each component in detail, explaining:

1. **What data** is collected
2. **Where** it comes from (which kernel files/interfaces)
3. **How** each function works (code walkthrough)
4. **Why** this data matters for performance analysis
5. **What to look for** in the results

---

*Next: [Chapter 1 — Linux Fundamentals for Performance Analysis](01-linux-fundamentals.md)*
