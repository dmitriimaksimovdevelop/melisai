# AI Prompt: Migrate BCC Tool to Native eBPF

Use this prompt when asking AI to port a specific BCC tool to native eBPF in the melisai project.

---

## Prompt Template

```
You are working on the melisai project — a Linux system performance analyzer written in Go.
Repository: /Users/baikal/work/BPF_analaze

## Task
Port the BCC tool `<TOOL_NAME>` from Tier 2 (BCC Python) to Tier 3 (native eBPF via cilium/ebpf).

## Context
- Read `context/NATIVE_EBPF_MIGRATION.md` for the full migration plan and patterns.
- Read `internal/collector/ebpf_tcpretrans.go` as the reference implementation (already working Tier 3 collector).
- Read `internal/ebpf/loader.go` for ProgramSpec and loading mechanism.
- Read `internal/ebpf/c/tcpretrans.bpf.c` for the BPF C code pattern.
- Read `internal/executor/registry.go` to find the current BCC ToolSpec for `<TOOL_NAME>` (parser, args, category).
- Read `internal/collector/bcc_adapter.go` to understand the BCC fallback pattern.

## Reference Material
- BCC libbpf-tools source: https://github.com/iovisor/bcc/tree/master/libbpf-tools
- Look at `libbpf-tools/<TOOL_NAME>.bpf.c` for the reference eBPF C implementation.
- Look at `libbpf-tools/<TOOL_NAME>.h` for the shared event struct definition.

## What to Create

### 1. BPF C Program: `internal/ebpf/c/<TOOL_NAME>.bpf.c`
- Include `vmlinux.h`, `bpf_helpers.h`, `bpf_core_read.h`, `bpf_tracing.h`
- Use CO-RE macros for kernel struct access (`BPF_CORE_READ`)
- Define event struct matching what Go will parse
- For histograms: use BPF_MAP_TYPE_ARRAY with log2 buckets (computed in-kernel)
- For events: use BPF_MAP_TYPE_PERF_EVENT_ARRAY
- Use appropriate attach point (kprobe, tracepoint, or raw_tracepoint)
- Prefer tracepoints over kprobes where available (more stable ABI)

### 2. Go Collector: `internal/collector/ebpf_<TOOL_NAME>.go`
- Follow the exact pattern from `ebpf_tcpretrans.go`
- Implement the `Collector` interface (Name, Category, Available, Collect)
- Available() checks: loader.CanLoad() + .o file exists → Tier 3
- Collect() flow:
  1. Find ProgramSpec from ebpf.NativePrograms
  2. loader.TryLoad(ctx, spec)
  3. For events: perf.NewReader → read loop → manual binary parse
  4. For histograms: read BPF map directly → convert to model.Histogram
  5. Return model.Result with Tier 3
- Use manual `binary.LittleEndian` parsing (NOT binary.Read)
- Pre-allocate event slices with capacity hints

### 3. ProgramSpec Registration: `internal/ebpf/loader.go`
- Add entry to NativePrograms slice with Name, Category, ObjectFile, AttachTo, Section, MapNames

### 4. Orchestrator Registration: `internal/orchestrator/orchestrator.go`
- In RegisterCollectors(), add Tier 3 check before BCC fallback (same pattern as tcpretrans)

### 5. Makefile Target
- Add clang compilation command for the new .bpf.c file

### 6. Tests: `internal/collector/ebpf_<TOOL_NAME>_test.go`
- Test binary parsing of event struct with known byte sequence
- Test Available() returns correct Tier when .o file exists/missing

## Output Type
<HISTOGRAM|EVENTS|FOLDED_STACKS>

## Current BCC Attach Points
<Describe the kernel functions the BCC tool attaches to>

## Constraints
- MUST work with cilium/ebpf v0.12.3 (already in go.mod)
- MUST use CO-RE (no runtime compilation)
- MUST compile with: clang -g -O2 -target bpf -D__TARGET_ARCH_x86
- MUST NOT break existing BCC fallback — BCC tool stays as Tier 2
- MUST include vmlinux.h (already in internal/ebpf/c/)
- The .o file is compiled on Linux, not on macOS dev machine

## Validation
After implementation, I will validate on a Linux SSH server:
1. `make generate` — compile BPF
2. `go build -o melisai ./cmd/melisai/`
3. `sudo ./melisai collect --profile quick -o native.json -v`
4. Compare histogram/event output with BCC baseline
5. Run `go test ./internal/collector/ -run <Tool>`
```

---

## Example: Filling in the Template for `runqlat`

```
Tool: runqlat
Output Type: HISTOGRAM
Category: cpu
Current BCC attach points:
  - Tracepoint: sched:sched_wakeup (records wakeup timestamp)
  - Tracepoint: sched:sched_wakeup_new (records wakeup timestamp for new tasks)
  - Tracepoint: sched:sched_switch (computes delta = now - wakeup_ts)
  - Histogram: log2 buckets of delta in microseconds
libbpf-tools reference: https://github.com/iovisor/bcc/blob/master/libbpf-tools/runqlat.bpf.c
```

## Example: Filling in the Template for `biolatency`

```
Tool: biolatency
Output Type: HISTOGRAM (per-disk with -D flag)
Category: disk
Current BCC attach points:
  - Tracepoint: block:block_rq_issue (records request start timestamp)
  - Tracepoint: block:block_rq_complete (computes delta, increments histogram)
  - Per-disk histograms keyed by dev_t
libbpf-tools reference: https://github.com/iovisor/bcc/blob/master/libbpf-tools/biolatency.bpf.c
```

## Example: Filling in the Template for `profile`

```
Tool: profile
Output Type: FOLDED_STACKS
Category: stacktrace
Current BCC attach points:
  - perf_event (CPU cycle sampling at configurable frequency, default 49Hz)
  - BPF_STACK_TRACE map for kernel + user stacks
  - Aggregates: stack_id → count
libbpf-tools reference: https://github.com/iovisor/bcc/blob/master/libbpf-tools/profile.bpf.c
Note: This is the most complex tool — requires stack unwinding and symbol resolution
```
