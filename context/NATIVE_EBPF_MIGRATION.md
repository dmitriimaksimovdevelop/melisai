# Native eBPF Migration Plan: BCC Tools → cilium/ebpf

## Context

melisai currently runs **67 BCC tools** as external Python processes. Each spawns a Python interpreter (~50MB RAM) + LLVM compiler (~200ms). With 67 tools in parallel this means **67 Python processes, ~3GB peak RAM, and significant observer effect** on the system being diagnosed.

The goal is to gradually replace BCC tools with **native eBPF programs** loaded directly from Go via cilium/ebpf. This eliminates Python/LLVM overhead, reduces observer effect, and improves startup latency by ~40x per tool.

**BCC remains as Tier 2 fallback** for systems without BTF support (kernel < 5.8).

---

## Architecture

### Current (Tier 2 — BCC)
```
Go → exec.Command("runqlat-bpfcc") → Python → LLVM → eBPF kernel
                                    → stdout text → regex parse → model.Result
```

### Target (Tier 3 — Native eBPF)
```
Go → cilium/ebpf.LoadCollection("runqlat.o") → eBPF kernel
                                              → perf buffer → binary parse → model.Result
```

### Fallback Chain (preserved)
```
Tier 3 (native eBPF, CO-RE)  →  available? use it
Tier 2 (BCC Python tools)    →  available? use it
Tier 1 (procfs/sysfs)        →  always works
```

---

## Implementation Pattern

Each native eBPF tool follows this pattern (template: `ebpf_tcpretrans.go`):

### Step 1: Write BPF C program
```
internal/ebpf/c/<tool>.bpf.c
```
- Use CO-RE macros (`BPF_CORE_READ`, `SEC`, `BPF_KPROBE`)
- Include `vmlinux.h` (not kernel headers)
- Define event struct shared with Go
- Use perf event array or ring buffer for output
- Reference: `libbpf-tools/<tool>.bpf.c` from BCC repo

### Step 2: Compile to ELF object
```bash
clang -g -O2 -target bpf -D__TARGET_ARCH_x86 \
    -I internal/ebpf/c \
    -c internal/ebpf/c/<tool>.bpf.c \
    -o internal/ebpf/bpf/<tool>.o
```

### Step 3: Register ProgramSpec
```go
// internal/ebpf/loader.go — add to NativePrograms slice
{
    Name:       "<tool>",
    Category:   "<category>",
    ObjectFile: "internal/ebpf/bpf/<tool>.o",
    AttachTo:   "<kernel_function>",
    Section:    "<section_name>",
    MapNames:   []string{"events"},
}
```

### Step 4: Create Go collector
```go
// internal/collector/ebpf_<tool>.go
type Native<Tool>Collector struct {
    loader *ebpf.Loader
}

func (c *Native<Tool>Collector) Name() string     { return "<tool>" }
func (c *Native<Tool>Collector) Category() string  { return "<category>" }
func (c *Native<Tool>Collector) Available() Availability {
    // Check BTF + .o file exists → Tier 3
}
func (c *Native<Tool>Collector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    // 1. loader.TryLoad(ctx, spec)
    // 2. Open perf buffer from "events" map
    // 3. Read loop with manual binary.LittleEndian parsing
    // 4. Convert to model.Event / model.Histogram
    // 5. Return model.Result with Tier 3
}
```

### Step 5: Register in orchestrator
```go
// internal/orchestrator/orchestrator.go — RegisterCollectors()
if tool == "<tool>" {
    native := collector.NewNative<Tool>Collector(ebpfLoader)
    if native.Available().Tier > 0 {
        collectors = append(collectors, native)
        return // skip BCC fallback
    }
}
```

### Step 6: Validate against BCC output
```bash
# Collect with BCC (force Tier 2)
melisai collect --profile quick -o bcc.json

# Collect with native eBPF (Tier 3 auto-selected)
melisai collect --profile quick -o native.json

# Compare
melisai diff bcc.json native.json
```

---

## Migration Phases

### Phase 1: High-Impact Histogram Tools (5 tools)
**Priority**: These generate the most overhead and produce the most useful data.

| Tool | Attach Point | Output Type | libbpf-tools ref | Complexity |
|------|-------------|-------------|-------------------|------------|
| `runqlat` | `tp/sched_switch` + `tp/sched_wakeup` | Histogram (BPF map) | `runqlat.bpf.c` | Medium |
| `biolatency` | `tp/block_rq_issue` + `tp/block_rq_complete` | Histogram (BPF map) | `biolatency.bpf.c` | Medium |
| `tcpconnlat` | `kprobe/tcp_v4_connect` + `kretprobe` | Events (perf buffer) | `tcpconnlat.bpf.c` | Medium |
| `cpudist` | `tp/sched_switch` | Histogram (BPF map) | `cpudist.bpf.c` | Medium |
| `tcprtt` | `kprobe/tcp_rcv_established` | Histogram (BPF map) | `tcprtt.bpf.c` | Easy |

**Expected savings**: ~5s startup, ~1.5GB RAM, significantly lower CPU noise.

### Phase 2: Stack Trace Tools (3 tools)
**Priority**: Heaviest tools, `profile` alone uses ~100MB RAM in BCC.

| Tool | Attach Point | Output Type | libbpf-tools ref | Complexity |
|------|-------------|-------------|-------------------|------------|
| `profile` | `perf_event` (CPU sampling) | Folded stacks (BPF stack map) | `profile.bpf.c` | Hard |
| `offcputime` | `tp/sched_switch` | Folded stacks (BPF stack map) | `offcputime.bpf.c` | Hard |
| `wakeuptime` | `tp/sched_wakeup` | Folded stacks (BPF stack map) | — | Hard |

**Expected savings**: ~3s startup, ~1GB RAM, much cleaner stack data.

### Phase 3: Event-Based Network Tools (6 tools)
| Tool | Attach Point | Output Type | libbpf-tools ref |
|------|-------------|-------------|-------------------|
| `tcpretrans` | `kprobe/tcp_retransmit_skb` | Events | **Already done** |
| `tcpdrop` | `kprobe/tcp_drop` | Events + stacks | `tcpdrop.bpf.c` |
| `tcpstates` | `tp/sock/inet_sock_set_state` | Events | `tcpstates.bpf.c` |
| `tcpconnect` | `kprobe/tcp_v4_connect` | Events | `tcpconnect.bpf.c` |
| `tcplife` | `tp/sock/inet_sock_set_state` | Events | `tcplife.bpf.c` |
| `tcpaccept` | `kretprobe/inet_csk_accept` | Events | — |

### Phase 4: Event-Based Process/Disk Tools (6 tools)
| Tool | Attach Point | Output Type | libbpf-tools ref |
|------|-------------|-------------|-------------------|
| `execsnoop` | `tp/syscalls/sys_enter_execve` | Events | `execsnoop.bpf.c` |
| `opensnoop` | `tp/syscalls/sys_enter_open*` | Events | `opensnoop.bpf.c` |
| `biosnoop` | `tp/block_rq_*` | Events | `biosnoop.bpf.c` |
| `ext4slower` | `kprobe/ext4_file_*` | Events | — |
| `killsnoop` | `tp/syscalls/sys_enter_kill` | Events | `killsnoop.bpf.c` |
| `oomkill` | `tp/oom/mark_victim` | Events | — |

### Phase 5: Remaining Tools (low priority)
- Filesystem-specific (`btrfsdist`, `xfsdist`, `nfsdist`, etc.)
- Memory tools (`cachestat`, `shmsnoop`, `drsnoop`)
- Rare event tools (`mountsnoop`, `mdflush`, `syncsnoop`)
- These can remain BCC-only until needed

---

## Validation Strategy

### Per-Tool Validation (SSH to Linux server)
```bash
# 1. Compile BPF program
make generate

# 2. Run with native eBPF
sudo ./melisai collect --profile quick -o native.json --verbose

# 3. Run with BCC only (disable Tier 3)
sudo ./melisai collect --profile quick -o bcc.json --verbose

# 4. Compare results
./melisai diff bcc.json native.json

# 5. Check specific tool output
jq '.categories.cpu[] | select(.collector=="runqlat")' native.json
jq '.categories.cpu[] | select(.collector=="runqlat")' bcc.json
```

### Automated CI Validation
```yaml
# .github/workflows/ebpf-test.yml
name: eBPF Integration
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-24.04  # Kernel 6.8+, BTF available
    steps:
      - uses: actions/checkout@v4
      - name: Install deps
        run: sudo apt-get install -y clang llvm libbpf-dev linux-tools-common
      - name: Compile eBPF
        run: make generate
      - name: Build
        run: go build -o melisai ./cmd/melisai/
      - name: Test native eBPF collection
        run: sudo ./melisai collect --profile quick -o report.json
      - name: Validate report
        run: python3 tests/validation/check_detection.py report.json
```

### Kernel Compatibility Matrix
| Kernel | BTF | CO-RE | Expected Tier |
|--------|-----|-------|---------------|
| 4.x | No | No | Tier 2 (BCC) or Tier 1 |
| 5.4 | Partial | No | Tier 2 (BCC) |
| 5.8+ | Yes | Yes | Tier 3 (native eBPF) |
| 6.1+ | Yes | Yes | Tier 3 (full support) |

---

## Key Technical Notes

### CO-RE Macros Cheat Sheet
```c
// Read kernel struct field (portable across versions)
u32 pid = BPF_CORE_READ(task, tgid);

// Read nested field
u32 ppid = BPF_CORE_READ(task, real_parent, tgid);

// Check field existence at runtime
if (bpf_core_field_exists(task->jobctl))
    ...

// Kprobe with typed arguments
SEC("kprobe/tcp_retransmit_skb")
int BPF_KPROBE(func_name, struct sock *sk, struct sk_buff *skb) { ... }

// Tracepoint with typed arguments
SEC("tp/sched/sched_switch")
int handle_switch(struct trace_event_raw_sched_switch *ctx) { ... }
```

### BCC → Native eBPF Conversion Rules
| BCC Python | Native eBPF (C + Go) |
|------------|---------------------|
| `BPF_HISTOGRAM(dist)` | `struct { __uint(type, BPF_MAP_TYPE_HASH); } dist SEC(".maps");` |
| `dist.increment(bpf_log2l(delta))` | `bpf_map_update_elem(&dist, &key, &val, BPF_ANY)` |
| `b.attach_kprobe(...)` | `link.Kprobe("func", prog, nil)` in Go |
| `b.trace_fields()` | `perf.NewReader(map, bufSize)` in Go |
| `b["events"].open_perf_buffer(cb)` | `perf.NewReader(eventsMap, 4096)` in Go |
| Text output parsing (regex) | Binary struct parsing (`binary.LittleEndian`) |

### Histogram in Native eBPF
BCC histograms use Python-side aggregation. Native eBPF does it in-kernel:

```c
// BPF side: increment histogram bucket in-kernel
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 64);  // 64 log2 buckets
    __type(key, u32);
    __type(value, u64);
} hist SEC(".maps");

static __always_inline void hist_increment(u64 value) {
    u32 slot = log2l(value);
    if (slot >= 64) slot = 63;
    u64 *count = bpf_map_lookup_elem(&hist, &slot);
    if (count) __sync_fetch_and_add(count, 1);
}
```

```go
// Go side: read histogram map after collection
var hist [64]uint64
for i := 0; i < 64; i++ {
    key := uint32(i)
    var val uint64
    histMap.Lookup(&key, &val)
    hist[i] = val
}
// Convert to model.Histogram with buckets, percentiles, etc.
```

---

## Dependencies

### Build-time (Linux only)
- `clang` >= 11 (BPF target compilation)
- `llvm-strip` (strip debug info from .o)
- `libbpf-dev` (headers: `bpf/bpf_helpers.h`, `bpf/bpf_core_read.h`)
- `vmlinux.h` (generated from kernel BTF or shipped in repo)

### Runtime
- Kernel >= 5.8 with BTF enabled (`CONFIG_DEBUG_INFO_BTF=y`)
- `/sys/kernel/btf/vmlinux` must exist
- CAP_BPF or root

### Go
- `github.com/cilium/ebpf` v0.12+ (already in go.mod)

---

## Files to Create/Modify Per Tool

```
internal/ebpf/c/<tool>.bpf.c          # NEW: BPF C program
internal/ebpf/bpf/<tool>.o            # GENERATED: compiled ELF
internal/collector/ebpf_<tool>.go      # NEW: Go collector
internal/collector/ebpf_<tool>_test.go # NEW: unit tests
internal/ebpf/loader.go               # MODIFY: add ProgramSpec
internal/orchestrator/orchestrator.go  # MODIFY: register native collector
Makefile                               # MODIFY: add compilation target
```

---

## Estimated Timeline

| Phase | Tools | Effort per tool | Total |
|-------|-------|-----------------|-------|
| Phase 1 | 5 histogram tools | 2-3 days | 2-3 weeks |
| Phase 2 | 3 stack trace tools | 3-5 days | 2-3 weeks |
| Phase 3 | 6 network tools | 1-2 days | 2 weeks |
| Phase 4 | 6 process/disk tools | 1-2 days | 2 weeks |
| Phase 5 | ~47 remaining | as needed | ongoing |

**Phase 1 alone covers ~80% of the performance benefit.**
