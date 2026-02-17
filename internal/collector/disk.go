// Disk collector (Tier 1): /proc/diskstats delta sampling,
// sysfs I/O scheduler, queue depth, rotational flag.
package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baikal/sysdiag/internal/model"
)

// DiskCollector gathers disk I/O metrics from procfs/sysfs (Tier 1).
type DiskCollector struct {
	procRoot string
	sysRoot  string
}

func NewDiskCollector(procRoot, sysRoot string) *DiskCollector {
	return &DiskCollector{procRoot: procRoot, sysRoot: sysRoot}
}

func (c *DiskCollector) Name() string     { return "disk_stats" }
func (c *DiskCollector) Category() string { return "disk" }
func (c *DiskCollector) Available() Availability {
	return Availability{Tier: 1}
}

// diskStatsRaw holds raw fields from /proc/diskstats.
type diskStatsRaw struct {
	name       string
	readOps    uint64
	readBytes  uint64 // sectors * 512
	writeOps   uint64
	writeBytes uint64
	ioInProg   uint64
	ioTimeMs   uint64
	wIOTimeMs  uint64
}

func (c *DiskCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()

	// Two-point sampling
	sample1 := c.readDiskStats()

	interval := cfg.SampleInterval
	if interval == 0 {
		interval = 1 * time.Second
	}
	select {
	case <-time.After(interval):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	sample2 := c.readDiskStats()

	// Compute deltas
	data := &model.DiskData{}
	for name, s2 := range sample2 {
		s1, ok := sample1[name]
		if !ok {
			continue
		}

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
		basePath := filepath.Join(c.sysRoot, "block", name)
		if _, err := os.Stat(basePath); err == nil {
			dev.Scheduler = c.readScheduler(basePath)
			dev.QueueDepth = c.readQueueDepth(basePath)
			dev.Rotational = c.readFile(filepath.Join(basePath, "queue", "rotational")) == "1"
			readAhead := c.readFile(filepath.Join(basePath, "queue", "read_ahead_kb"))
			dev.ReadAheadKB, _ = strconv.Atoi(readAhead)
		}

		data.Devices = append(data.Devices, dev)
		data.TotalOps += dev.ReadOps + dev.WriteOps
		data.ReadOps += dev.ReadOps
		data.WriteOps += dev.WriteOps
	}

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      data,
	}, nil
}

func (c *DiskCollector) readDiskStats() map[string]diskStatsRaw {
	f, err := os.Open(filepath.Join(c.procRoot, "diskstats"))
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]diskStatsRaw)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// /proc/diskstats has at least 14 fields
		if len(fields) < 14 {
			continue
		}
		name := fields[2]

		// Skip partitions (only process whole disks like sda, nvme0n1)
		// Simple heuristic: skip if name ends with digit AND contains 'p' partition
		// For now, include everything and let the UI filter
		readOps, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeOps, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		ioInProg, _ := strconv.ParseUint(fields[11], 10, 64)
		ioTimeMs, _ := strconv.ParseUint(fields[12], 10, 64)
		wIOTimeMs, _ := strconv.ParseUint(fields[13], 10, 64)

		result[name] = diskStatsRaw{
			name:       name,
			readOps:    readOps,
			readBytes:  readSectors * 512,
			writeOps:   writeOps,
			writeBytes: writeSectors * 512,
			ioInProg:   ioInProg,
			ioTimeMs:   ioTimeMs,
			wIOTimeMs:  wIOTimeMs,
		}
	}
	return result
}

func (c *DiskCollector) readScheduler(basePath string) string {
	data := c.readFile(filepath.Join(basePath, "queue", "scheduler"))
	// Format: "[mq-deadline] kyber bfq none" — active in brackets
	if idx := strings.Index(data, "["); idx >= 0 {
		end := strings.Index(data[idx:], "]")
		if end > 0 {
			return data[idx+1 : idx+end]
		}
	}
	return data
}

func (c *DiskCollector) readQueueDepth(basePath string) int {
	v, _ := strconv.Atoi(c.readFile(filepath.Join(basePath, "queue", "nr_requests")))
	return v
}

func (c *DiskCollector) readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
