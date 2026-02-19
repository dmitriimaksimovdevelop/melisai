# melisai

**Linux performance diagnostics for AI agents.** Single Go binary. Collects 67 BCC/eBPF tools + procfs metrics. Outputs structured JSON with health score, anomalies, and recommendations. Ships with an MCP server for interactive use from Claude Desktop, Cursor, or any MCP-compatible client.

[![Go 1.23+](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![BCC Coverage](https://img.shields.io/badge/BCC_tools-67%2F80-green)](https://github.com/iovisor/bcc)

```
$ sudo melisai collect --profile quick -o report.json

  melisai v0.1.1 | profile=quick | duration=10s

  Tier 1 (procfs)  ████████████████████████████████████████ 7/7   2.1s
  Tier 2 (BCC)     ████████████████████████████████████████ 4/4  10.3s

  Health Score:  68 / 100  ⚠️
  Anomalies:     cpu_utilization CRITICAL (98.7%)
                 load_average WARNING (3.2x CPUs)
  Recommendations: 2

  Report saved to report.json
```

---

## Why melisai?

Most performance tools give you raw numbers. melisai gives you **a diagnosis**.

- Runs Brendan Gregg's [USE Method](https://www.brendangregg.com/usemethod.html) automatically
- Flags anomalies with severity (warning/critical) using field-tested thresholds
- Computes a single **health score** (0-100) so an AI agent can decide what to do next
- Generates a context-aware **AI prompt** with 23 known anti-patterns
- Works over **MCP** (Model Context Protocol) so Claude/Cursor can diagnose a server interactively

---

## Quick Start

```bash
# 1. Build (requires Go 1.23+, cross-compile from macOS/Linux)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/

# 2. Deploy
scp melisai root@server:/usr/local/bin/

# 3. Install BCC tools on the server (first time only)
ssh root@server "melisai install"

# 4. Run
ssh root@server "melisai collect --profile quick -o /tmp/report.json"
```

---

## MCP Server

melisai includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) server. AI agents connect over stdio and interactively diagnose system performance -- no file juggling required.

```bash
melisai mcp   # starts stdio JSON-RPC server
```

**Claude Desktop / Cursor config** (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "melisai": {
      "command": "ssh",
      "args": ["root@your-server", "/usr/local/bin/melisai", "mcp"]
    }
  }
}
```

### Tools

| Tool | What it does | Time |
|------|-------------|------|
| `get_health` | Quick 0-100 score + anomalies. Tier 1 only, no root needed | ~1s |
| `collect_metrics` | Full profile with all BCC/eBPF tools. Args: `profile`, `focus`, `pid` | 10s-60s |
| `explain_anomaly` | Root causes + recommendations for a specific anomaly ID | instant |
| `list_anomalies` | All 23 detectable anomaly metric IDs with descriptions | instant |

### Typical workflow

```
Agent                              melisai
  │                                   │
  ├── get_health ──────────────────►  │  "score: 68, cpu_utilization CRITICAL"
  │                                   │
  ├── explain_anomaly ─────────────►  │  "High CPU: root causes, what to check..."
  │   anomaly_id: cpu_utilization     │
  │                                   │
  ├── collect_metrics ─────────────►  │  Full JSON report with 67 BCC tools,
  │   profile: standard               │  histograms, events, stack traces,
  │   focus: stacks                   │  AI prompt included
  │                                   │
  └── (agent analyzes & recommends)   │
```

---

## How It Works

melisai collects metrics at three tiers with automatic fallback:

```
Tier 1: /proc, /sys, ss, dmesg          ← always works, no root
Tier 2: 67 BCC tools (runqlat, bio...)   ← root + bcc-tools
Tier 3: native eBPF (cilium/ebpf)       ← root + kernel ≥ 5.8 + BTF
```

Collection runs in **two phases** to eliminate observer effect:
1. **Phase 1** -- Tier 1 collectors capture clean baselines (CPU, memory, disk, network)
2. **Phase 2** -- BCC/eBPF tools run without contaminating the baselines

The report includes:

| Section | Content |
|---------|---------|
| `summary.health_score` | Weighted 0-100 score (CPU 1.5x, Memory 1.5x, Disk 1.0x, Network 1.0x) |
| `summary.anomalies[]` | Detected issues with severity, metric, value, threshold |
| `summary.resources` | USE metrics per resource (utilization, saturation, errors) |
| `summary.recommendations[]` | Copy-paste sysctl commands with citations |
| `categories.*` | Raw data: histograms, events, stack traces per subsystem |
| `ai_context.prompt` | Dynamic prompt with system context and 23 anti-patterns |

---

## Collection Profiles

| Profile | Duration | What runs | Best for |
|---------|----------|-----------|----------|
| **quick** | 10s | Tier 1 + biolatency, tcpretrans, opensnoop, oomkill | Health checks, CI gates |
| **standard** | 30s | All Tier 1 + all 67 BCC tools | Regular diagnostics |
| **deep** | 60s | Everything + memleak, biostacks, wakeuptime, biotop | Root cause analysis |

```bash
# Quick health check
sudo melisai collect --profile quick -o report.json

# Full analysis
sudo melisai collect --profile standard --ai-prompt -o report.json

# Deep dive focused on disk
sudo melisai collect --profile deep --focus disk -o report.json

# Profile a specific process (24 BCC tools filter to this PID)
sudo melisai collect --profile standard --pid 12345 -o app.json

# Profile a container
sudo melisai collect --profile standard --cgroup /sys/fs/cgroup/system.slice/nginx.service -o nginx.json

# Compare before/after
melisai diff baseline.json current.json -o diff.json
```

---

## Output Example

See [doc/example_report.md](doc/example_report.md) for a full production example -- a server scoring 32/100 where a message broker fsync storm on HDD cascades into I/O starvation across all containers.

Abbreviated JSON:

```json
{
  "metadata": {
    "tool": "melisai", "schema_version": "1.0.0",
    "hostname": "prod-web-01", "kernel_version": "6.8.0-90-generic",
    "cpus": 20, "memory_gb": 62, "profile": "standard", "duration": "30s"
  },
  "categories": {
    "cpu": [
      {"collector": "cpu_utilization", "tier": 1, "data": {"user_pct": 12.5, "iowait_pct": 0.3, "idle_pct": 85.2}},
      {"collector": "runqlat", "tier": 2, "histograms": [{"name": "runqlat", "unit": "us", "p50": 4, "p99": 64}]}
    ],
    "disk": [ ... ], "memory": [ ... ], "network": [ ... ],
    "process": [ ... ], "stacktrace": [ ... ], "container": [ ... ]
  },
  "summary": {
    "health_score": 85,
    "anomalies": [{"severity": "warning", "metric": "cpu_psi_pressure", "message": "CPU PSI: 12.5%"}],
    "resources": {"cpu": {"utilization_pct": 14.8, "saturation_pct": 0.4, "errors": 0}},
    "recommendations": [{"title": "Enable BBR", "commands": ["sysctl -w net.ipv4.tcp_congestion_control=bbr"]}]
  },
  "ai_context": {"prompt": "You are a Linux performance engineer. Analyze this report..."}
}
```

---

## BCC Tools (67)

~84% coverage of Brendan Gregg's [BPF observability diagram](https://www.brendangregg.com/BPF/bcc-tracing-tools.png).

| Subsystem | Tools |
|-----------|-------|
| **CPU** (10) | runqlat, runqlen, cpudist, hardirqs, softirqs, runqslower, cpufreq, cpuunclaimed, llcstat, funccount |
| **Disk** (20) | biolatency, biosnoop, biotop, bitesize, ext4slower, ext4dist, fileslower, filelife, mountsnoop, btrfsslower, btrfsdist, xfsslower, xfsdist, nfsslower, nfsdist, zfsslower, zfsdist, mdflush, scsilatency, nvmelatency, vfsstat |
| **Memory** (7) | cachestat, oomkill, drsnoop, shmsnoop, numamove, memleak, slabratetop |
| **Network** (14) | tcpconnlat, tcpretrans, tcprtt, tcpdrop, tcpstates, tcpconnect, tcpaccept, tcplife, udpconnect, sofdsnoop, sockstat, skbdrop, tcpsynbl, gethostlatency |
| **Process** (9) | execsnoop, opensnoop, killsnoop, threadsnoop, syncsnoop, exitsnoop, statsnoop, capable, syscount |
| **Stacks** (6) | profile, offcputime, wakeuptime, offwaketime, biostacks, stackcount |

24 of these tools support `--pid` filtering for per-process analysis.

---

## Anomaly Detection

20 threshold rules based on Gregg's recommended values:

| Metric | Warning | Critical | Source |
|--------|---------|----------|--------|
| cpu_utilization | 80% | 95% | /proc/stat |
| cpu_iowait | 10% | 30% | /proc/stat |
| load_average | 2x CPUs | 4x CPUs | /proc/loadavg |
| memory_utilization | 85% | 95% | /proc/meminfo |
| swap_usage | 10% | 50% | /proc/meminfo |
| disk_utilization | 70% | 90% | /proc/diskstats |
| disk_avg_latency | 5ms | 50ms | /proc/diskstats |
| tcp_retransmits | 10/s | 50/s | /proc/net/snmp |
| tcp_timewait | 5k | 20k | ss |
| runqlat_p99 | 10ms | 50ms | BCC histogram |
| biolatency_p99_ssd | 5ms | 25ms | BCC histogram |
| biolatency_p99_hdd | 50ms | 200ms | BCC histogram |
| cpu_throttling | 100 | 1000 periods | cgroup cpu.stat |
| ... and 7 more (PSI, cache miss, DNS, container memory, network errors) | | | |

---

## Architecture

```
cmd/melisai/           CLI (cobra) + MCP subcommand
internal/
  ├── collector/       7 Tier 1 collectors + BCC adapter
  ├── executor/        BCC runner, security, 67 parsers, registry
  ├── ebpf/            Native eBPF loader (cilium/ebpf, CO-RE)
  ├── mcp/             MCP server (4 tools, stdio JSON-RPC)
  ├── model/           Types, USE metrics, anomalies, health score
  ├── observer/        PID tracker, overhead measurement
  ├── orchestrator/    Two-phase execution, signal handling, profiles
  ├── output/          JSON, FlameGraph SVG, AI prompt generator
  ├── diff/            Report comparison engine
  └── installer/       Distro detection, package installation
```

**Security**: all BCC binaries are verified (root-owned, not world-writable, in allowed paths). No shell execution. Environment sanitized. Output capped at 50MB per tool.

---

## Requirements

| | Minimum | Notes |
|---|---|---|
| **Build** | Go 1.23+ | Cross-compile: `GOOS=linux GOARCH=amd64` |
| **Tier 1** | Any Linux kernel | No root needed |
| **Tier 2** | bcc-tools installed | `sudo melisai install` handles this |
| **Tier 3** | Kernel ≥ 5.8 with BTF | Falls back to Tier 2 automatically |

### Tested distros

| Distro | Verified |
|--------|----------|
| Ubuntu 24.04 | Full validation (20 CPUs, 62 GiB, 8 workload tests) |
| Ubuntu 22.04 | Docker integration test |
| Debian 12 | Docker integration test |
| Fedora 39 | Docker integration test |
| CentOS Stream 9 | Docker integration test |

---

## Development

```bash
# Run all 258 tests
go test ./... -v

# With race detector
go test ./... -race

# Lint
make lint

# Cross-compile
make build    # or: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/

# Validation tests (Linux + root + stress-ng)
make test-validation
```

---

## Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| `tool "X" not found in allowed paths` | BCC tool not installed | `sudo melisai install` |
| `binary "X" is not owned by root` | Permissions | `chown root:root /usr/sbin/X-bpfcc` |
| Empty histogram data | No events during collection window | Normal -- not an error |
| `exit status 1` from BCC tool | Missing kernel support | Check `dmesg` for BPF errors |

---

## License

[Apache License 2.0](LICENSE)
