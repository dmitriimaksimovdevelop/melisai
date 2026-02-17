# Chapter 8: System Information Collector

## Overview

The `SystemCollector` (`internal/collector/system.go`) gathers environmental context: what OS is running, what kernel version, what filesystems are mounted, what block devices exist, and recent kernel errors from dmesg.

This data isn't about performance metrics — it's about **context**. An anomaly detection rule might fire differently on Ubuntu 20.04 with kernel 5.4 than on RHEL 9 with kernel 6.1.

## Source File: system.go

- **Lines**: 237
- **Functions**: 9
- **Tier**: 1 (always available)

## Function Walkthrough

### readOSRelease() — OS Identification

```go
func (c *SystemCollector) readOSRelease() string {
    data, _ := os.ReadFile("/etc/os-release")
    for _, line := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(line, "PRETTY_NAME=") {
            return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
        }
    }
    return runtime.GOOS  // fallback: "linux"
}
```

Example: returns `"Ubuntu 22.04.3 LTS"` or `"Debian GNU/Linux 12 (bookworm)"`.

### Uptime Parsing

```go
// /proc/uptime: "1234567.89 2345678.12"
//                ^uptime_sec  ^idle_time_all_cpus
parts := strings.Fields(raw)
uptime, _ := strconv.ParseFloat(parts[0], 64)
sysInfo.UptimeSeconds = int64(uptime)
```

**Why uptime matters**: A freshly rebooted server (uptime < 1 hour) may not have warmed its caches yet. Performance baselines should be taken after the system is warm.

### collectFilesystems() — Disk Usage

```go
func (c *SystemCollector) collectFilesystems(ctx context.Context) []model.FilesystemInfo {
    out, _ := exec.CommandContext(ctx, "df", "-P", "-T").Output()
    // -P: POSIX output format (no line wrapping)
    // -T: include filesystem type
    // Fields: Filesystem Type 1024-blocks Used Available Capacity Mounted
    fss = append(fss, model.FilesystemInfo{
        Mount:   fields[6],           // e.g. "/data"
        Device:  fields[0],           // e.g. "/dev/sda1"
        Type:    fields[1],           // e.g. "ext4"
        SizeGB:  sizeKB / 1024 / 1024, // KB → GB
        UsedPct: usedKB / sizeKB * 100,
    })
}
```

### collectBlockDevices() — Hardware Detection

```go
func (c *SystemCollector) collectBlockDevices() []model.BlockDevice {
    entries, _ := os.ReadDir("/sys/block/")
    for _, entry := range entries {
        name := entry.Name()
        // Size from sectors (512 bytes each)
        sizeStr := c.readFile(filepath.Join(basePath, "size"))
        sectors, _ := strconv.ParseInt(sizeStr, 10, 64)
        sizeGB := float64(sectors*512) / (1024 * 1024 * 1024)

        // SSD vs HDD
        rotStr := c.readFile(filepath.Join(basePath, "queue", "rotational"))
        devType := "ssd"
        if rotStr == "1" { devType = "hdd" }

        // Model name (if available)
        modelStr := c.readFile(filepath.Join(basePath, "device", "model"))
    }
}
```

### collectDmesg() — Kernel Errors

```go
func (c *SystemCollector) collectDmesg(ctx context.Context) []model.LogEntry {
    out, _ := exec.CommandContext(ctx, "dmesg", "--level=err,warn", "-T", "--nopager").Output()
    lines := strings.Split(string(out), "\n")
    // Take last 50 entries max
    // Classify each entry as "err" or "warn" based on content heuristics
}
```

**What to look for in dmesg:**
| Pattern | Meaning |
|---------|---------|
| `EXT4-fs error` | Filesystem corruption |
| `EDAC ... CE` | Correctable memory errors (ECC) |
| `EDAC ... UE` | Uncorrectable memory errors — REPLACE RAM |
| `NMI: ... stuck` | Hardware watchdog timeout |
| `Out of memory` | OOM killer invoked |
| `CPU: ... microcode updated` | CPU firmware update applied |
| `link ... down` | Network link failure |

### readSysctl*() — Kernel Parameters

Three helper functions read kernel tunables:

```go
func readSysctlInt(procRoot, path string) int       { ... }
func readSysctlInt64(procRoot, path string) int64    { ... }
func readSysctlString(procRoot, path string) string  { ... }
```

These read files under `/proc/sys/` — e.g., `readSysctlInt(procRoot, "sys/vm/swappiness")` reads `/proc/sys/vm/swappiness`.

These are shared by all collectors (they're package-level functions in `system.go`), used by Memory, Network, and CPU collectors.

---

*Next: [Chapter 9 — BCC Tools](09-bcc-tools.md)*
