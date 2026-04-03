# Quick Start

From zero to diagnosis in 2 minutes.

## 1. Install

```bash
# One-liner (Linux amd64/arm64)
curl -sSL https://melisai.dev/install | sh

# Or build from source
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/
sudo mv melisai /usr/local/bin/
```

## 2. Run Your First Scan

```bash
# Quick health check (10 seconds, no BCC tools needed)
sudo melisai collect --profile quick -o report.json
```

You'll see:

```
melisai v0.4.1 | profile=quick | duration=10s

Tier 1 (procfs)  ████████████████████████████████████████ 8/8   2.1s
Tier 2 (BCC)     ████████████████████████████████████████ 4/4  10.3s

Health Score:  68 / 100  ⚠️
Anomalies:     cpu_utilization CRITICAL (98.7%)
               load_average WARNING (3.2x CPUs)
Recommendations: 2

Report saved to report.json
```

## 3. Read the Results

```bash
# Health score (0-100)
jq '.summary.health_score' report.json

# What's wrong?
jq '.summary.anomalies[]' report.json

# How to fix it
jq '.summary.recommendations[] | {type, title, commands}' report.json
```

## 4. Apply Fixes

Recommendations include copy-paste commands:

```bash
# Example: melisai recommends enabling BBR
sysctl -w net.core.default_qdisc=fq
sysctl -w net.ipv4.tcp_congestion_control=bbr
```

## 5. Verify the Fix

```bash
# Run again and compare
sudo melisai collect --profile quick -o after.json
melisai diff report.json after.json
```

The diff shows what improved, what regressed, and health score delta.

## What's Next?

| Goal | Command |
|------|---------|
| Full analysis with all 67 BCC tools | `sudo melisai collect --profile standard -o report.json` |
| Deep dive (stacks, memleak, biotop) | `sudo melisai collect --profile deep -o report.json` |
| Focus on network only | `sudo melisai collect --profile standard --focus network -o net.json` |
| Profile a specific process | `sudo melisai collect --profile standard --pid 12345 -o app.json` |
| Profile a container | `sudo melisai collect --profile standard --cgroup /sys/fs/cgroup/system.slice/nginx.service -o cg.json` |
| Install BCC tools (first time) | `sudo melisai install` |
| Use with Claude/Cursor (MCP) | `melisai mcp` → [MCP setup guide](13-ai-integration.md) |
| Check system capabilities | `melisai capabilities` |

## Collection Profiles

| Profile | Duration | What runs | Best for |
|---------|----------|-----------|----------|
| **quick** | 10s | Tier 1 + 4 essential BCC tools | Health checks, CI gates |
| **standard** | 30s | All Tier 1 + all 67 BCC tools | Regular diagnostics |
| **deep** | 60s | Everything + memleak, biostacks, wakeuptime | Root cause analysis |

## CLI Reference

```
melisai collect   Collect metrics and produce JSON report
  --profile       quick|standard|deep (default: standard)
  --focus         cpu,memory,disk,network,stacks (comma-separated)
  --pid           Filter to specific PID
  --cgroup        Filter to cgroup path
  --duration      Override profile duration (e.g., 15s, 1m)
  --ai-prompt     Include AI analysis prompt in output
  --output, -o    Output file (- for stdout)
  --quiet, -q     Suppress progress output
  --verbose, -v   Debug logging

melisai diff      Compare two reports
  <baseline.json> <current.json>
  --output, -o    Output diff file

melisai install   Install BCC tools and dependencies
  --dry-run       Show what would be installed

melisai mcp       Start MCP server (stdio JSON-RPC)

melisai capabilities  Show available tools and kernel features
```

---

*Next: [Introduction — How melisai Works](00-introduction.md)*
