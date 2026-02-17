# Chapter 4: Disk I/O Analysis — Deep Dive

## Overview

Disk I/O is often the bottleneck that masquerades as other problems. When a database is slow, it's usually waiting for disk. When an application has high latency, it might be a log file write blocking the event loop.

sysdiag's `DiskCollector` (`internal/collector/disk.go`) uses two-point sampling of `/proc/diskstats` and enriches the data with sysfs device properties.

## Source File: disk.go

- **Lines**: 174
- **Functions**: 6
- **Data Sources**: `/proc/diskstats`, `/sys/block/*/queue/`

## Function Walkthrough

### Collect() — Two-Point I/O Sampling

```go
func (c *DiskCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    sample1 := c.readDiskStats()
    // Wait 1 second
    sample2 := c.readDiskStats()
    // Compute deltas for each device
    for name, s2 := range sample2 {
        s1, ok := sample1[name]
        dev := model.DiskDevice{
            Name:         name,
            ReadOps:      int64(s2.readOps - s1.readOps),
            WriteOps:     int64(s2.writeOps - s1.writeOps),
            ReadBytes:    int64(s2.readBytes - s1.readBytes),
            WriteBytes:   int64(s2.writeBytes - s1.writeBytes),
            IOInProgress: int64(s2.ioInProg),
            IOTimeMs:     int64(s2.ioTimeMs - s1.ioTimeMs),
            WeightedIOMs: int64(s2.wIOTimeMs - s1.wIOTimeMs),
        }
        // Enrich with sysfs data
        dev.Scheduler = c.readScheduler(basePath)
        dev.QueueDepth = c.readQueueDepth(basePath)
        dev.Rotational = c.readFile(...) == "1"
        dev.ReadAheadKB, _ = strconv.Atoi(c.readFile(...))
    }
}
```

### readDiskStats() — Parsing /proc/diskstats

```go
func (c *DiskCollector) readDiskStats() map[string]diskStatsRaw {
    // /proc/diskstats has 14+ fields per line
    // Fields: major minor name reads_completed reads_merged sectors_read
    //         time_reading writes_completed writes_merged sectors_written
    //         time_writing ios_in_progress time_doing_io weighted_time_io
    readOps, _ := strconv.ParseUint(fields[3], 10, 64)
    readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
    // ...
    result[name] = diskStatsRaw{
        readBytes:  readSectors * 512,   // sectors are always 512 bytes
        // ...
    }
}
```

**The 512-byte sector convention**: Regardless of the actual disk sector size (512 or 4096 bytes), `/proc/diskstats` always counts in 512-byte sectors. This is a Linux convention.

### readScheduler() — I/O Scheduler Detection

```go
func (c *DiskCollector) readScheduler(basePath string) string {
    // /sys/block/sda/queue/scheduler
    // "[mq-deadline] kyber bfq none"  ← active scheduler in brackets
    if idx := strings.Index(data, "["); idx >= 0 {
        end := strings.Index(data[idx:], "]")
        return data[idx+1 : idx+end]  // "mq-deadline"
    }
}
```

**I/O Schedulers explained:**

| Scheduler | Best For | How It Works |
|-----------|----------|-------------|
| `mq-deadline` | General purpose, databases | Guarantees max latency per request |
| `bfq` | Desktops, interactive | Fair bandwidth sharing between processes |
| `kyber` | Fast SSDs | Minimal overhead, latency targets |
| `none` | NVMe, fast SSDs | No scheduling, direct dispatch |

**Tuning rule**: NVMe drives should use `none` or `kyber`. Rotational drives benefit from `mq-deadline`.

## Key Metrics

### IOTimeMs — Utilization

`IOTimeMs` counts milliseconds during which the device had I/O in progress. Over a 1-second sample:

```
Utilization% = IOTimeMs / 10
```

If IOTimeMs = 1000 (1 second), the disk was 100% busy.

### WeightedIOMs — Latency × Depth

`WeightedIOMs` = sum of (time spent × number of I/Os). It captures both latency and queue depth. High weighted IO time with low IO count = each operation is slow.

### IOInProgress — Queue Depth

The number of I/O operations currently in flight. This is a saturation indicator:

| Queue Depth | Assessment |
|-------------|-----------|
| 0-1 | Normal |
| 2-8 | Active but healthy |
| 8-32 | Getting saturated |
| > 32 | Heavily saturated |

## Diagnostic Examples

### Fast NVMe Drive
```json
{
  "name": "nvme0n1",
  "read_ops": 150,
  "write_ops": 3200,
  "io_time_ms": 45,
  "io_in_progress": 1,
  "scheduler": "none",
  "rotational": false,
  "queue_depth": 1023
}
```
4.5% utilization, 1 in-flight IO, `none` scheduler — perfect for NVMe.

### Struggling HDD
```json
{
  "name": "sda",
  "read_ops": 280,
  "write_ops": 50,
  "io_time_ms": 920,
  "io_in_progress": 12,
  "scheduler": "mq-deadline",
  "rotational": true,
  "read_ahead_kb": 128
}
```
92% utilization, 12 in-flight IOs — this HDD is near saturation. Consider moving to SSD.

---

*Next: [Chapter 5 — Network Analysis](05-network-analysis.md)*
