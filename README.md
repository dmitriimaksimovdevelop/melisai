# sysdiag

**Comprehensive Linux system performance analyzer** — single Go binary that collects metrics via BPF/eBPF tools, procfs/sysfs, and standard utilities. Produces structured JSON reports optimized for AI-driven diagnostics.

[![CI](https://github.com/dmitriimaksimovdevelop/sysdiag/actions/workflows/ci.yml/badge.svg)](https://github.com/dmitriimaksimovdevelop/sysdiag/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/dmitriimaksimovdevelop/sysdiag)](https://goreportcard.com/report/github.com/dmitriimaksimovdevelop/sysdiag)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Overview

sysdiag implements Brendan Gregg's **USE Method** (Utilization, Saturation, Errors) to systematically analyze Linux performance. It collects data at three tiers with automatic fallback:

| Tier | Source | Requirements |
|------|--------|-------------|
| **Tier 1** | `/proc`, `/sys`, `ss`, `dmesg` | Always works, no root |
| **Tier 2** | BCC tools — `runqlat`, `biolatency`, `tcpconnlat`, etc. | Root + bcc-tools |
| **Tier 3** | Native eBPF (cilium/ebpf) — zero deps | Root + kernel ≥ 5.8 with BTF |

## Features

- 🔍 **7 Tier 1 collectors** — CPU, memory, disk, network, process, container, system
- ⚡ **20 BCC tool parsers** — histogram, tabular events, folded stacks
- 🛡️ **Security controls** — binary verification (root-owned, not world-writable), environment sanitization
- 📊 **USE metrics** — automatic computation for CPU, memory, disk, network
- 🚨 **11 anomaly thresholds** — warning/critical severity with Gregg's recommended values
- 💯 **Health score** — weighted 0-100 score based on USE methodology
- 🔧 **Sysctl recommendations** — actionable commands with expected impact and source citations
- 🤖 **AI prompt generation** — dynamic, context-aware prompt with 21 known anti-patterns
- 🔥 **FlameGraph SVG** — inline SVG generator from folded stacks
- 📈 **Report diff** — regression/improvement detection with significance classification
- 📦 **Installer** — auto-detects distro and installs BPF tools

## Quick Start

```bash
# Build
make build-linux

# Run collection (requires Linux)
sudo ./bin/sysdiag collect --profile standard -o report.json

# With AI prompt
sudo ./bin/sysdiag collect --ai-prompt -o report.json

# Quick check (10s)
sudo ./bin/sysdiag collect --profile quick

# Deep analysis (60s, includes stack traces)
sudo ./bin/sysdiag collect --profile deep --focus stacks,disk

# Compare two reports
./bin/sysdiag diff baseline.json current.json

# Check system capabilities
./bin/sysdiag capabilities

# Install BPF tools
sudo ./bin/sysdiag install
```

## Architecture

```
cmd/sysdiag/          CLI entry point (cobra)
internal/
  ├── collector/      Tier 1 procfs/sysfs collectors (7)
  ├── executor/       BCC tool runner + security + parsers (12) + registry (20)
  ├── ebpf/           BTF detection, CO-RE loader stub, tier decision
  ├── model/          Data types, USE metrics, anomaly detection, health score
  ├── orchestrator/   Parallel execution, signal handling, profiles
  ├── output/         JSON formatter, FlameGraph SVG, AI prompt generator
  ├── diff/           Report comparison engine
  └── installer/      Distro detection, package installation
```

## Collection Profiles

| Profile | Duration | Collectors | Use Case |
|---------|----------|-----------|----------|
| `quick` | 10s | Core metrics | Fast health check |
| `standard` | 30s | All Tier 1 + key Tier 2 | Regular diagnostics |
| `deep` | 60s | All tiers + stacks | Root cause analysis |

## Output Format

JSON schema designed for machine readability and AI analysis:

```json
{
  "metadata": { "hostname": "...", "profile": "standard", ... },
  "system": { "kernel": "...", "uptime_seconds": 123456, ... },
  "categories": {
    "cpu": [{ "collector": "cpu_utilization", "tier": 1, "data": {...} }],
    "memory": [...],
    "disk": [...],
    "network": [...]
  },
  "summary": {
    "health_score": 85,
    "anomalies": [{ "severity": "warning", "message": "..." }],
    "resources": { "cpu": { "utilization_pct": 45, "saturation_pct": 0 } },
    "recommendations": [{ "title": "Enable BBR", "commands": [...] }]
  },
  "ai_context": { "prompt": "...", "known_patterns": [...] }
}
```

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-cover

# Build for all architectures
make build-all

# Lint
make lint

# Smoke test
make smoke
```

## Requirements

- **Build**: Go 1.21+
- **Run (Tier 1)**: Linux (any kernel)
- **Run (Tier 2)**: bcc-tools, bpftrace installed
- **Run (Tier 3)**: Kernel ≥ 5.8 with BTF/CO-RE support

## License

[Apache License 2.0](LICENSE)
