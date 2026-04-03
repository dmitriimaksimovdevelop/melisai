# All Chapters

Complete table of contents for the melisai documentation. 22 chapters covering Linux performance theory, melisai internals, and production tuning.

## Getting Started

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| — | [Quick Start](00-quickstart.md) | Install → run → read results → fix → verify. 2 minutes to first diagnosis |
| 0 | [Introduction](index.md) | What melisai is, USE methodology, three-tier architecture, report structure |
| 1 | [Linux Fundamentals](01-linux-fundamentals.md) | /proc, /sys, jiffies, CPU states, cgroups v1/v2, PSI, buddy allocator |

## Performance Analysis

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 2 | [CPU Analysis](02-cpu-analysis.md) | Delta sampling, per-CPU breakdown, load average, CFS scheduler tuning |
| 3 | [Memory Analysis](03-memory-analysis.md) | MemAvailable vs MemFree, vmstat, PSI, NUMA stats, swap, dirty pages |
| 4 | [Disk I/O Analysis](04-disk-analysis.md) | /proc/diskstats, 512-byte sectors, I/O schedulers, queue depth |
| 5 | [Network Analysis](05-network-analysis.md) | TCP stats, conntrack, softnet, IRQ distribution, NIC hardware, 30+ sysctls |
| 6 | [Process Analysis](06-process-analysis.md) | Top-N by CPU/memory, /proc/[pid]/stat, FD counting, state tracking |
| 7 | [Container Analysis](07-container-analysis.md) | K8s/Docker detection, cgroup v1/v2, CPU throttling, memory limits |

## Internals

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 8 | [System Collector](08-system-collector.md) | OS detection, uptime, filesystems, block devices, dmesg parsing |
| 9 | [BCC Tools](09-bcc-tools.md) | 67-tool registry, executor, security model, parsers, aggregation |
| 10 | [Native eBPF](10-ebpf-native.md) | BTF/CO-RE, cilium/ebpf loader, Tier 3 strategy |
| 15 | [Orchestrator](15-orchestrator.md) | Two-phase execution, parallel collectors, signal handling, profiles |
| 16 | [Output Formats](16-output-formats.md) | JSON schema, atomic writes, FlameGraph SVG, progress reporter |

## Intelligence

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 11 | [Anomaly Detection](11-anomaly-detection.md) | 37 threshold rules, rate-based detection, health score formula |
| 12 | [Recommendations](12-recommendations.md) | 35 sysctl fixes, "fix" vs "optimization" types, evidence-based |
| 13 | [AI Integration](13-ai-integration.md) | Dynamic prompt generation, 27 anti-patterns, MCP server setup |

## Advanced Topics

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 18 | [GPU & PCIe Topology](18-gpu-pcie-analysis.md) | NVIDIA detection, PCI→NUMA mapping, cross-NUMA GPU-NIC pairs, GPUDirect |
| 19 | [Page Reclaim & THP](19-page-reclaim-thp.md) | Watermarks, direct reclaim, compaction, THP defrag modes, tuning |
| 20 | [NUMA Optimization](20-numa-optimization.md) | Distance matrix, miss ratio, numactl, K8s topology manager |

## Operations

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 14 | [Report Diffing](14-report-diffing.md) | Before/after comparison, USE deltas, histogram changes |
| 21 | [Production Checklist](21-production-checklist.md) | All sysctls in one place, one-liner tuning script, anomaly→fix mapping |

## Reference

| # | Chapter | What You'll Learn |
|---|---------|-------------------|
| 17 | [Appendix](17-appendix.md) | Glossary, /proc reference, /sys hierarchy, sysctl table, CLI reference |
