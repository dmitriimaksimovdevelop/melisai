# Chapter 10: Native eBPF

## Overview

Tier 3 is the future of Linux observability: loading eBPF programs directly from Go code without Python, without external binaries, without needing the bcc-tools package installed.

sysdiag's `internal/ebpf/` package provides the infrastructure for this.

## Source Files: ebpf/ (3 files)

| File | Lines | Purpose |
|------|-------|---------|
| `btf.go` | 59 | BTF (BPF Type Format) detection |
| `loader.go` | 131 | eBPF program loader |
| `capabilities.go` | ~50 | Kernel/eBPF capability assessment |

## BTF and CO-RE

### What is BTF?

BTF (BPF Type Format) is metadata that describes kernel data structures — their names, fields, sizes, and offsets. Without BTF, an eBPF program compiled on one kernel version might not work on another because struct layouts change.

### What is CO-RE?

CO-RE (Compile Once, Run Everywhere) uses BTF to **relocate** struct field accesses at load time:

```
┌─────────────────────────┐      ┌─────────────────────────┐
│  Compiled on kernel 5.4 │      │  Running on kernel 6.1  │
│                         │      │                         │
│  task_struct.comm       │      │  task_struct.comm       │
│  offset: 1240           │  ──► │  offset: 1312           │
│                         │ BTF  │  (field moved!)         │
│  eBPF reads offset 1240 │reloc │  eBPF reads offset 1312 │
└─────────────────────────┘      └─────────────────────────┘
```

Without CO-RE, you'd need to recompile the eBPF program for every kernel version.

### BTF Detection

```go
func HasBTF() bool {
    // Check for /sys/kernel/btf/vmlinux
    _, err := os.Stat("/sys/kernel/btf/vmlinux")
    return err == nil
}
```

BTF is available on kernels ≥ 5.4 when compiled with `CONFIG_DEBUG_INFO_BTF=y`. Most modern distros (Ubuntu 20.10+, Fedora 31+, RHEL 9+) have BTF enabled by default.

### Kernel Version Parsing

```go
func ParseKernelVersion(version string) (major, minor, patch int) {
    // "5.15.0-91-generic" → (5, 15, 0)
    parts := strings.SplitN(version, ".", 3)
    major, _ = strconv.Atoi(parts[0])
    minor, _ = strconv.Atoi(parts[1])
    // Patch may contain suffix like "-91-generic"
    patchStr := strings.SplitN(parts[2], "-", 2)[0]
    patch, _ = strconv.Atoi(patchStr)
}
```

### The Tier Decision

```go
func AssessCapabilities() Capabilities {
    caps := Capabilities{Tier: 1}  // Always at least Tier 1

    // Tier 2: BCC tools available?
    if BCCToolsAvailable() {
        caps.Tier = 2
        caps.BCCAvailable = true
    }

    // Tier 3: Native eBPF possible?
    if HasBTF() && KernelVersion >= (5, 8, 0) && os.Geteuid() == 0 {
        caps.Tier = 3
        caps.BTFAvailable = true
    }
}
```

## The Loader (Stub)

Currently, `loader.go` provides a framework for future cilium/ebpf programs:

```go
type EBPFLoader struct {
    specs    map[string]*ebpf.CollectionSpec
    programs map[string]*ebpf.Program
}

func (l *EBPFLoader) Load(programName string) error {
    // 1. Load pre-compiled eBPF ELF from embedded assets
    // 2. Apply CO-RE relocations using kernel BTF
    // 3. Attach to kprobes/tracepoints
    // 4. Create ring buffer for events
}
```

The actual eBPF programs (`.o` files compiled from C with clang) will be embedded using Go's `embed` package. This means the sysdiag binary is fully self-contained — no external files needed at runtime.

## Why Tier 3 Matters

| Feature | Tier 2 (BCC) | Tier 3 (Native) |
|---------|-------------|-----------------|
| **Dependencies** | Python, bcc-tools package | None (embedded in binary) |
| **Startup time** | 1-3 seconds per tool | < 100ms |
| **Memory** | 50-100MB per tool (Python) | < 5MB per program |
| **Kernel version** | Any kernel with BPF | ≥ 5.8 with BTF |
| **Security** | External binary verification | Embedded programs, no external trust |

---

*Next: [Chapter 11 — Anomaly Detection](11-anomaly-detection.md)*
