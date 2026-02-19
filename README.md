# melisai

**Comprehensive Linux system performance analyzer** -- single Go binary that collects metrics via BPF/eBPF tools, procfs/sysfs, and standard utilities. Produces structured JSON reports optimized for AI-driven diagnostics.

## Overview

melisai implements Brendan Gregg's **USE Method** (Utilization, Saturation, Errors) to systematically analyze Linux performance. It collects data at three tiers with automatic fallback:

| Tier | Source | Requirements |
|------|--------|-------------|
| **Tier 1** | `/proc`, `/sys`, `ss`, `dmesg` | Always works, no root |
| **Tier 2** | BCC tools (67 tools) | Root + bcc-tools |
| **Tier 3** | Native eBPF (cilium/ebpf) | Root + kernel >= 5.8 with BTF |

## Features

- **7 Tier 1 collectors** -- CPU, memory, disk, network, process, container, system
- **67 BCC tool parsers** -- histogram, tabular events, folded stacks, periodic
- **Security controls** -- binary verification (root-owned, not world-writable), environment sanitization
- **Per-application profiling** -- `--pid` targets 24 BCC tools to a specific process, `--cgroup` scopes to a container
- **Two-phase collection** -- Tier 1 baselines captured before BCC tools run (eliminates observer effect)
- **USE metrics** -- automatic computation for CPU, memory, disk, network, container resources
- **20 anomaly thresholds** -- warning/critical severity with Gregg's recommended values, delta-based metrics
- **Health score** -- weighted 0-100 score based on USE methodology
- **Sysctl recommendations** -- actionable commands with expected impact and source citations
- **AI prompt generation** -- dynamic, context-aware prompt with 27 known anti-patterns, PID/cgroup-aware
- **FlameGraph SVG** -- inline SVG generator from folded stacks
- **Report diff** -- regression/improvement detection with significance classification
- **Installer** -- auto-detects distro (Ubuntu/Debian/CentOS/Fedora/Arch) and installs BPF tools

## Quick Start

```bash
# Build (requires Go 1.23+)
GOOS=linux GOARCH=amd64 go build -o melisai ./cmd/melisai/

# Deploy to server
scp melisai root@server:/usr/local/bin/melisai

# Install BPF tools (run on the target Linux server)
sudo melisai install

# Check what's available
sudo melisai capabilities

# Quick health check (10s, core metrics + key BCC tools)
sudo melisai collect --profile quick -o report.json

# Standard analysis (30s, all 67 BCC tools)
sudo melisai collect --profile standard -o report.json

# Deep analysis (60s, all tools + extra profiling)
sudo melisai collect --profile deep --ai-prompt -o report.json

# Focus on specific subsystems
sudo melisai collect --profile standard --focus network,disk

# Profile a specific application by PID
sudo melisai collect --profile standard --pid 12345 --ai-prompt -o app-report.json

# Profile a container by cgroup path
sudo melisai collect --profile standard --cgroup /sys/fs/cgroup/system.slice/myservice.service -o svc-report.json

# Compare two reports
melisai diff baseline.json current.json -o diff.json

# Dry-run installation (show what would be installed)
sudo melisai install --dry-run
```

## Collection Profiles

| Profile | Duration | Collectors | Use Case |
|---------|----------|-----------|----------|
| `quick` | 10s | 10 (Tier 1 + biolatency, tcpretrans, opensnoop, oomkill) | Fast health check |
| `standard` | 30s | 66 (all Tier 1 + all 67 BCC tools) | Regular diagnostics |
| `deep` | 60s | 66 + extended profiling (memleak, biostacks, wakeuptime, biotop) | Root cause analysis |

## BCC Tools Coverage (~84% of Brendan Gregg's Observability Diagram)

### CPU (10 tools)
`runqlat`, `runqlen`, `cpudist`, `hardirqs`, `softirqs`, `runqslower`, `cpufreq`, `cpuunclaimed`, `llcstat`, `funccount`

### Disk (20 tools)
`biolatency`, `biosnoop`, `biotop`, `bitesize`, `ext4slower`, `ext4dist`, `fileslower`, `filelife`, `mountsnoop`, `btrfsslower`, `btrfsdist`, `xfsslower`, `xfsdist`, `nfsslower`, `nfsdist`, `zfsslower`, `zfsdist`, `mdflush`, `scsilatency`, `nvmelatency`, `vfsstat`

### Memory (7 tools)
`cachestat`, `oomkill`, `drsnoop`, `shmsnoop`, `numamove`, `memleak`, `slabratetop`

### Network (12 tools)
`tcpconnlat`, `tcpretrans`, `tcprtt`, `tcpdrop`, `tcpstates`, `tcpconnect`, `tcpaccept`, `tcplife`, `udpconnect`, `sofdsnoop`, `sockstat`, `skbdrop`, `tcpsynbl`, `gethostlatency`

### Process (10 tools)
`execsnoop`, `opensnoop`, `killsnoop`, `threadsnoop`, `syncsnoop`, `exitsnoop`, `statsnoop`, `capable`, `syscount`

### Stack Traces (5 tools)
`profile`, `offcputime`, `wakeuptime`, `offwaketime`, `biostacks`, `stackcount`

## Architecture

```
cmd/melisai/          CLI entry point (cobra)
internal/
  |-- collector/      Tier 1 procfs/sysfs collectors (7) + BCC adapter (PID injection)
  |-- executor/       BCC tool runner + security + parsers + registry (67 tools)
  |-- ebpf/           BTF detection, CO-RE loader, tier decision
  |-- model/          Data types, USE metrics, anomaly detection (20 rules), health score
  |-- observer/       PID tracker, observer-effect measurement
  |-- orchestrator/   Two-phase execution, signal handling, profiles
  |-- output/         JSON formatter, FlameGraph SVG, AI prompt generator
  |-- diff/           Report comparison engine
  |-- installer/      Distro detection, package installation
testdata/             Fixture files for parser tests
context/              Design documents (ARCHITECTURE.md, PLAN.md, etc.)
```

## Output Format

JSON schema designed for machine readability and AI analysis:

```json
{
  "metadata": {
    "tool": "melisai",
    "schema_version": "1.0.0",
    "hostname": "Ubuntu-2404-noble-amd64-base",
    "kernel_version": "6.8.0-90-generic",
    "arch": "amd64",
    "cpus": 20,
    "memory_gb": 62,
    "profile": "standard",
    "duration": "30s"
  },
  "categories": {
    "cpu": [
      {"collector": "cpu_utilization", "tier": 1, "data": {"user_pct": 12.5, ...}},
      {"collector": "runqlat", "tier": 2, "histograms": [{"name": "run_queue_latency", "p99": 64, ...}]}
    ],
    "disk": [...],
    "memory": [...],
    "network": [...],
    "process": [...],
    "stacktrace": [...],
    "system": [...],
    "container": [...]
  },
  "summary": {
    "health_score": 85,
    "anomalies": [{"severity": "critical", "message": "CPU utilization at 98.7%"}],
    "resources": {"cpu": {"utilization_pct": 45, "saturation_pct": 2, "errors": 0}},
    "recommendations": [{"title": "Enable BBR congestion control", "commands": ["sysctl -w net.ipv4.tcp_congestion_control=bbr"]}]
  },
  "ai_context": {"prompt": "You are a Linux systems performance expert..."}
}
```

## Example Report

See **[doc/example_report.md](doc/example_report.md)** for a realistic production example — a server with health score 32/100 where a message broker fsync storm on HDD causes cascading I/O saturation across all co-located containers. Includes the full BCC tool output (biolatency, ext4slower, runqlat, cachestat), root cause chain diagram, and prioritized remediation steps.

## AI Agent Usage Guide

melisai is designed to be operated by AI agents (Claude Code, etc.) for automated performance analysis. Here's the recommended workflow:

### Step 1: Deploy and Install

```bash
# Build on your dev machine
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/

# Deploy to target server
scp melisai root@<server>:/usr/local/bin/melisai

# Install BCC tools (first time only)
ssh root@<server> "melisai install"

# Verify
ssh root@<server> "melisai capabilities"
```

### Step 2: Collect Data

```bash
# For a quick health check
ssh root@<server> "melisai collect --profile quick --ai-prompt -o /tmp/report.json"

# For full analysis
ssh root@<server> "melisai collect --profile standard --ai-prompt -o /tmp/report.json"

# For per-application analysis (24 BCC tools trace only the target PID)
ssh root@<server> "melisai collect --profile standard --pid $(pgrep myapp) --ai-prompt -o /tmp/app-report.json"

# For container/service analysis
ssh root@<server> "melisai collect --profile standard --cgroup /sys/fs/cgroup/system.slice/nginx.service -o /tmp/nginx-report.json"

# Download report
scp root@<server>:/tmp/report.json ./report.json
```

### Step 3: Analyze the JSON

The report contains everything needed for analysis:

- `summary.health_score` -- quick 0-100 health indicator
- `summary.anomalies[]` -- detected issues with severity
- `summary.recommendations[]` -- actionable sysctl commands
- `categories.cpu[].data` -- CPU utilization, load average, PSI
- `categories.cpu[].histograms[]` -- scheduler latency distribution (P50/P90/P99)
- `categories.disk[].histograms[]` -- I/O latency distribution
- `categories.network[].events[]` -- TCP connection events, retransmissions
- `categories.stacktrace[].stacks[]` -- folded stack traces for flamegraphs
- `ai_context.prompt` -- ready-to-use prompt for AI analysis

### Step 4: Compare Over Time

```bash
# Take baseline
ssh root@<server> "melisai collect --profile standard -o /tmp/baseline.json"

# ... make changes ...

# Take current
ssh root@<server> "melisai collect --profile standard -o /tmp/current.json"

# Compare
ssh root@<server> "melisai diff /tmp/baseline.json /tmp/current.json -o /tmp/diff.json"
```

### Key Patterns for AI Agents

1. **Empty histogram data is normal** -- tools like `biolatency` return empty results when there's no I/O. This is not an error.
2. **BCC tools use `-bpfcc` suffix** on Ubuntu/Debian (e.g., `/usr/sbin/runqlat-bpfcc`). melisai handles this automatically.
3. **Tools that don't accept duration** (e.g., `oomkill`) are killed after `duration + 5s` via context cancellation.
4. **Tier 3 (native eBPF)** requires compiled `.o` files. Currently only `tcpretrans` has a Tier 3 implementation. If the `.o` file is missing, it falls back to BCC.
5. **The health score** is a weighted composite: CPU (1.5×), Memory (1.5×), Disk (1.0×), Network (1.0×), Container (1.2×). Score < 50 = critical, < 70 = warning.
6. **Two-phase collection** -- Tier 1 (procfs) runs first on a clean system, then Tier 2/3 (BCC/eBPF). This ensures CPU/memory/disk/network baselines are uncontaminated by BPF overhead.
7. **PID targeting** -- `--pid PID` injects `-p PID` into 24 BCC tools that support it. Tier 1 metrics remain system-wide for context. The AI prompt explains the scoping.
8. **Delta-based metrics** -- network errors and TCP retransmissions use per-second rates (not cumulative counters). Memory errors are not reported (cumulative faults since boot are not actionable).

### Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `tool "X" not found in allowed paths` | BCC tool not installed | Run `melisai install` |
| `no histogram buckets found` (in logs) | No events during collection | Normal -- tool returns empty result |
| `binary "X" is not owned by root` | File permissions issue | `chown root:root /usr/sbin/X-bpfcc` |
| Collection hangs | A BCC tool is stuck | Each tool has a `duration + 5s` timeout |
| `exit status 1` from BCC tool | Tool crashed or missing kernel support | Check `dmesg` for BPF errors |

## Development

```bash
# Run all tests
go test ./... -v -count=1

# Verify tool count
go test ./internal/executor/ -run TestRegistryToolCount -v

# Test all parsers against fixtures
go test ./internal/executor/ -run TestAllToolsParseFixtures -v

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/

# Vet
go vet ./...
```

## Requirements

- **Build**: Go 1.23+
- **Run (Tier 1)**: Linux (any kernel)
- **Run (Tier 2)**: bcc-tools (`apt install bpfcc-tools python3-bpfcc` on Ubuntu)
- **Run (Tier 3)**: Kernel >= 5.8 with BTF/CO-RE support

## Supported Distributions

| Distro | Package Manager | Tested |
|--------|----------------|--------|
| Ubuntu 22.04/24.04 | apt | Yes (24.04 verified) |
| Debian 11/12 | apt | Expected |
| CentOS/RHEL 8/9 | yum | Expected |
| Fedora 38+ | dnf | Expected |
| Arch Linux | pacman | Expected |

## License

[Apache License 2.0](LICENSE)
