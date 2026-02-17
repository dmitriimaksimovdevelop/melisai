# Chapter 17: Appendix

## Glossary

| Term | Definition |
|------|-----------|
| **BCC** | BPF Compiler Collection — Python-based eBPF tools |
| **BPF/eBPF** | Extended Berkeley Packet Filter — in-kernel virtual machine for safe tracing |
| **BTF** | BPF Type Format — kernel struct metadata for CO-RE |
| **CO-RE** | Compile Once, Run Everywhere — portable eBPF programs |
| **CFS** | Completely Fair Scheduler — Linux default CPU scheduler |
| **cgroup** | Control Group — kernel resource limiting mechanism |
| **HZ** | Kernel tick frequency (typically 100 on servers) |
| **jiffy** | One tick of the kernel clock (1/HZ seconds) |
| **IRQ** | Interrupt Request — hardware notification to CPU |
| **NUMA** | Non-Uniform Memory Access — multi-socket memory topology |
| **OOM** | Out of Memory — kernel kills process to free memory |
| **procfs** | Process filesystem (/proc) — kernel data as virtual files |
| **PSI** | Pressure Stall Information — resource contention metric |
| **RSS** | Resident Set Size — physical memory used by a process |
| **sysfs** | System filesystem (/sys) — device model as virtual files |
| **TLB** | Translation Lookaside Buffer — page table cache |
| **USE** | Utilization, Saturation, Errors — Gregg's analysis methodology |

## Key Files Reference

### /proc hierarchy

| Path | Content | Used By |
|------|---------|---------|
| `/proc/stat` | CPU time counters per core | CPUCollector |
| `/proc/loadavg` | 1/5/15 min load averages | CPUCollector |
| `/proc/meminfo` | Memory breakdown (30+ fields) | MemoryCollector |
| `/proc/vmstat` | VM event counters | MemoryCollector |
| `/proc/buddyinfo` | Memory fragmentation per zone | MemoryCollector |
| `/proc/pressure/memory` | Memory PSI (some/full) | MemoryCollector |
| `/proc/diskstats` | Per-device I/O counters | DiskCollector |
| `/proc/net/dev` | Per-interface traffic | NetworkCollector |
| `/proc/net/snmp` | TCP/UDP/IP protocol stats | NetworkCollector |
| `/proc/[pid]/stat` | Per-process CPU/state | ProcessCollector |
| `/proc/[pid]/fd/` | Open file descriptors | ProcessCollector |
| `/proc/1/cgroup` | Cgroup membership | ContainerCollector |
| `/proc/uptime` | System uptime | SystemCollector |
| `/proc/version` | Kernel version string | SystemCollector |
| `/proc/cmdline` | Boot parameters | SystemCollector |
| `/proc/sys/*` | Kernel tunables | Various |

### /sys hierarchy

| Path | Content | Used By |
|------|---------|---------|
| `/sys/block/*/queue/scheduler` | I/O scheduler | DiskCollector |
| `/sys/block/*/queue/nr_requests` | I/O queue depth | DiskCollector |
| `/sys/block/*/queue/rotational` | HDD vs SSD | DiskCollector, SystemCollector |
| `/sys/block/*/size` | Device size in sectors | SystemCollector |
| `/sys/fs/cgroup/` | Cgroup hierarchy | ContainerCollector |
| `/sys/kernel/btf/vmlinux` | BTF presence | eBPF loader |
| `/sys/devices/system/node/` | NUMA topology | MemoryCollector |

### Sysctl parameters used

| Parameter | Default | Meaning |
|-----------|---------|---------|
| `vm.swappiness` | 60 | Swap aggressiveness |
| `vm.overcommit_memory` | 0 | Memory overcommit policy |
| `vm.dirty_ratio` | 20 | Sync write-back threshold |
| `vm.dirty_background_ratio` | 10 | Async write-back threshold |
| `kernel.sched_latency_ns` | 6000000 | CFS scheduling round time |
| `kernel.sched_min_granularity_ns` | 750000 | Minimum preemption time |
| `net.ipv4.tcp_congestion_control` | cubic | TCP congestion algorithm |
| `net.ipv4.tcp_rmem` | 4K/128K/6M | TCP receive buffer |
| `net.ipv4.tcp_wmem` | 4K/16K/4M | TCP send buffer |
| `net.core.somaxconn` | 4096 | Listen backlog max |

## CLI Reference

```
sysdiag — Linux server performance diagnostic tool

Usage:
  sysdiag [command]

Commands:
  collect       Collect system metrics and generate report
  diff          Compare two reports for regressions/improvements
  capabilities  Show system capabilities (BCC, eBPF, kernel features)
  install       Install BPF tools (bcc-tools, bpftrace)
  version       Print version information

Flags for 'collect':
  --profile string        Collection profile: quick|standard|deep (default "standard")
  --focus string          Comma-separated focus areas: cpu,disk,network,stacks,all
  --output string         Output file path (default: stdout)
  --ai-prompt             Include AI analysis prompt in output
  --pids ints             Target specific PIDs
  --cgroups strings       Target specific cgroup paths
  --max-events int        Max events per collector (default: 1000)
  --quiet                 Suppress progress output
  --json                  JSON output (default)

Flags for 'diff':
  --json                  Structured JSON output (default: human-readable)
  --threshold float       Significance threshold % (default: 5.0)
```

## References

1. Gregg, Brendan. **"Systems Performance: Enterprise and the Cloud"**, 2nd Edition. Addison-Wesley, 2020.
2. Gregg, Brendan. **"BPF Performance Tools"**. Addison-Wesley, 2019.
3. Linux kernel documentation: https://www.kernel.org/doc/Documentation/
4. BCC tools repository: https://github.com/iovisor/bcc
5. cilium/ebpf library: https://github.com/cilium/ebpf
6. Brendan Gregg's blog: https://www.brendangregg.com/
7. USE method: https://www.brendangregg.com/usemethod.html
8. Linux insides: https://0xax.gitbooks.io/linux-insides/
