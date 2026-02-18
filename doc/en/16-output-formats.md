# Chapter 16: Output Formats

## Overview

melisai produces three output types: structured JSON, SVG flame graphs, and AI prompts. The output package (`internal/output/`) handles formatting and writing.

## Source Files: output/ (5 files)

| File | Lines | Purpose |
|------|-------|---------|
| `json.go` | ~106 | JSON serialization to file or stdout |
| `flamegraph.go` | ~106 | SVG flame graph from folded stacks |
| `ai_prompt.go` | ~103 | AI/LLM analysis prompt (see Chapter 13) |
| `progress.go` | ~53 | Collection progress display |

## JSON Output

### WriteJSON()

```go
func WriteJSON(report *model.Report, path string) error {
    data, err := json.MarshalIndent(report, "", "  ")

    if path == "" || path == "-" {
        os.Stdout.Write(data)
    } else {
        // Atomic write: write to temp file, then rename
        tmpFile := path + ".tmp"
        os.WriteFile(tmpFile, data, 0644)
        os.Rename(tmpFile, path)
    }
}
```

**Atomic write pattern**: Writing to a temporary file and renaming prevents partial writes if melisai is killed mid-write. `os.Rename` is atomic on the same filesystem.

### JSON Schema

The complete JSON output follows this structure:
```json
{
  "metadata": {
    "tool": "melisai",
    "version": "0.2.0",
    "hostname": "...",
    "timestamp": "...",
    "profile": "standard",
    "duration_seconds": 30
  },
  "system": {
    "os": "Ubuntu 22.04",
    "kernel": "5.15.0-91-generic",
    "uptime_seconds": 1234567,
    "boot_params": "...",
    "filesystems": [...],
    "block_devices": [...],
    "dmesg_errors": [...]
  },
  "categories": {
    "cpu": [{"collector": "cpu_utilization", "tier": 1, "data": {...}}],
    "memory": [...],
    "disk": [...],
    "network": [...],
    "process": [...],
    "system": [...]
  },
  "summary": {
    "health_score": 78,
    "resources": {
      "cpu": {"utilization": 45.2, "saturation": 12.3, "errors": 0},
      "memory": {"utilization": 67.8, "saturation": 5.0, "errors": 123}
    },
    "anomalies": [...],
    "recommendations": [...]
  }
}
```

## Flame Graph Generator

### WriteFlamegraph()

```go
func WriteFlamegraph(stacks []model.StackTrace, outputPath string) error {
    // 1. Convert stacks to folded format
    //    "main;handleRequest;db.Query 42"
    //    "main;handleRequest;json.Marshal 15"
    // 2. Generate SVG using flame graph algorithm
    // 3. Write to output file
}
```

Flame graphs visualize CPU profiling data:

```
┌──────────────────────────────────────────────────────────┐
│ root                                                      │
├──────────┬───────────────────────────┬───────────────────┤
│ goroutine│ goroutine 2               │ goroutine 3       │
│ 1        │                           │                   │
│          ├───────────┬───────────────┤                   │
│          │handleReq  │other          │                   │
│          ├─────┬─────┤               │                   │
│          │json │ db  │               │                   │
│          │     │Query│               │                   │
└── ── ── ─┴─────┴─────┴── ── ── ── ──┴── ── ── ── ── ──┘
Width = proportion of CPU time
```

- Wide bars = hot functions consuming lots of CPU
- Vertical stacking = call chain
- Read bottom-to-top: caller → callee

## Progress Reporter

```go
type ProgressReporter struct {
    quiet bool
}

func (p *ProgressReporter) Start(name string) {
    if !p.quiet {
        fmt.Fprintf(os.Stderr, "  %-30s ", name)
    }
}

func (p *ProgressReporter) Done(duration time.Duration) {
    if !p.quiet {
        fmt.Fprintf(os.Stderr, "✓ %s\n", duration.Round(time.Millisecond))
    }
}
```

Output during collection:
```
melisai v0.2.0 — collecting with standard profile (30s)
  cpu_utilization               ✓ 1.003s
  memory_info                   ✓ 12ms
  disk_io                       ✓ 1.001s
  network_stats                 ✓ 45ms
  process_top                   ✓ 1.005s
  container_info                ✓ 3ms
  system_info                   ✓ 218ms
```

---

*Next: [Chapter 17 — Appendix](17-appendix.md)*
