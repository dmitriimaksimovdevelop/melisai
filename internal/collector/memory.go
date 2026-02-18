// Memory collector (Tier 1): /proc/meminfo, vmstat, buddyinfo, PSI,
// swap, NUMA stats, sysctl vm.* parameters.
package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// MemoryCollector gathers memory metrics from procfs (Tier 1).
type MemoryCollector struct {
	procRoot string
	sysRoot  string
}

func NewMemoryCollector(procRoot, sysRoot string) *MemoryCollector {
	return &MemoryCollector{procRoot: procRoot, sysRoot: sysRoot}
}

func (c *MemoryCollector) Name() string     { return "memory_info" }
func (c *MemoryCollector) Category() string { return "memory" }
func (c *MemoryCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *MemoryCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()
	data := &model.MemoryData{}

	// /proc/meminfo
	c.parseMeminfo(data)

	// /proc/vmstat — page faults
	c.parseVmstat(data)

	// vm.* sysctl settings
	data.Swappiness = readSysctlInt(c.procRoot, "sys/vm/swappiness")
	data.OvercommitMemory = readSysctlInt(c.procRoot, "sys/vm/overcommit_memory")
	data.OvercommitRatio = readSysctlInt(c.procRoot, "sys/vm/overcommit_ratio")
	data.DirtyRatio = readSysctlInt(c.procRoot, "sys/vm/dirty_ratio")
	data.DirtyBackgroundRatio = readSysctlInt(c.procRoot, "sys/vm/dirty_background_ratio")

	// min_free_kbytes
	data.MinFreeKbytes = readSysctlInt(c.procRoot, "sys/vm/min_free_kbytes")

	// Transparent Huge Pages
	data.THPEnabled = c.readTHPEnabled()

	// PSI memory pressure
	c.parsePSI(data)

	// /proc/buddyinfo
	data.BuddyInfo = c.parseBuddyinfo()

	// NUMA stats
	data.NUMANodes = c.parseNUMAStats()

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      data,
	}, nil
}

func (c *MemoryCollector) parseMeminfo(data *model.MemoryData) {
	f, err := os.Open(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, " kB")
		val, _ := strconv.ParseInt(strings.TrimSpace(valStr), 10, 64)
		valBytes := val * 1024 // kB to bytes

		switch key {
		case "MemTotal":
			data.TotalBytes = valBytes
		case "MemFree":
			data.FreeBytes = valBytes
		case "MemAvailable":
			data.AvailableBytes = valBytes
		case "Cached":
			data.CachedBytes = valBytes
		case "Buffers":
			data.BuffersBytes = valBytes
		case "SwapTotal":
			data.SwapTotalBytes = valBytes
		case "SwapFree":
			data.SwapUsedBytes = data.SwapTotalBytes - valBytes
		case "Dirty":
			data.DirtyBytes = valBytes
		case "HugePages_Total":
			data.HugePagesTotal = int(val) // no kB suffix
		case "HugePages_Free":
			data.HugePagesFree = int(val)
		}
	}
}

func (c *MemoryCollector) parseVmstat(data *model.MemoryData) {
	f, err := os.Open(filepath.Join(c.procRoot, "vmstat"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "pgmajfault":
			data.MajorFaults = val
		case "pgfault":
			data.MinorFaults = val
		}
	}
}

func (c *MemoryCollector) parsePSI(data *model.MemoryData) {
	f, err := os.Open(filepath.Join(c.procRoot, "pressure", "memory"))
	if err != nil {
		return // PSI not available (kernel < 4.20)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "some avg10=0.00 avg60=0.00 avg300=0.00 total=0"
		// Format: "full avg10=0.00 avg60=0.00 avg300=0.00 total=0"
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		prefix := fields[0]
		for _, field := range fields[1:] {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				continue
			}
			val, _ := strconv.ParseFloat(parts[1], 64)
			switch {
			case prefix == "some" && parts[0] == "avg10":
				data.PSISome10 = val
			case prefix == "some" && parts[0] == "avg60":
				data.PSISome60 = val
			case prefix == "full" && parts[0] == "avg10":
				data.PSIFull10 = val
			case prefix == "full" && parts[0] == "avg60":
				data.PSIFull60 = val
			}
		}
	}
}

func (c *MemoryCollector) readTHPEnabled() string {
	data, err := os.ReadFile(filepath.Join(c.sysRoot, "kernel", "mm", "transparent_hugepage", "enabled"))
	if err != nil {
		return ""
	}
	// Format: "always [madvise] never" — active in brackets
	content := string(data)
	if idx := strings.Index(content, "["); idx >= 0 {
		end := strings.Index(content[idx:], "]")
		if end > 0 {
			return content[idx+1 : idx+end]
		}
	}
	return strings.TrimSpace(content)
}

func (c *MemoryCollector) parseBuddyinfo() map[string][]int {
	f, err := os.Open(filepath.Join(c.procRoot, "buddyinfo"))
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string][]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// "Node 0, zone   DMA    1    0    1    ..."
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) < 2 {
			continue
		}
		// Parse the zone name and counts
		header := strings.TrimSpace(parts[0])
		countStr := strings.TrimSpace(parts[1])
		fields := strings.Fields(countStr)

		var counts []int
		for _, f := range fields {
			v, _ := strconv.Atoi(f)
			counts = append(counts, v)
		}
		if len(counts) > 0 {
			result[header] = counts
		}
	}
	return result
}

func (c *MemoryCollector) parseNUMAStats() []model.NUMANode {
	nodesDir := filepath.Join(c.sysRoot, "devices", "system", "node")
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil
	}

	var nodes []model.NUMANode
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "node") {
			continue
		}
		numStr := strings.TrimPrefix(entry.Name(), "node")
		nodeNum, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}

		nodePath := filepath.Join(nodesDir, entry.Name())
		node := model.NUMANode{Node: nodeNum}

		// Parse meminfo_extra
		if f, err := os.Open(filepath.Join(nodePath, "meminfo")); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				parts := strings.Fields(line)
				if len(parts) < 4 {
					continue
				}
				val, _ := strconv.ParseInt(parts[3], 10, 64)
				valBytes := val * 1024
				switch {
				case strings.Contains(line, "MemTotal"):
					node.MemTotalBytes = valBytes
				case strings.Contains(line, "MemFree"):
					node.MemFreeBytes = valBytes
				}
			}
			f.Close()
		}

		// Parse numastat
		if f, err := os.Open(filepath.Join(nodePath, "numastat")); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) != 2 {
					continue
				}
				val, _ := strconv.ParseInt(fields[1], 10, 64)
				switch fields[0] {
				case "numa_hit":
					node.NumaHit = val
				case "numa_miss":
					node.NumaMiss = val
				case "numa_foreign":
					node.NumaForeign = val
				}
			}
			f.Close()
		}

		nodes = append(nodes, node)
	}
	return nodes
}
