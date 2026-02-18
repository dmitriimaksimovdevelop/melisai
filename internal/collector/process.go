// Process collector (Tier 1): /proc/[pid]/stat, /proc/[pid]/status,
// /proc/[pid]/fd, top processes by CPU and memory.
package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/baikal/sysdiag/internal/model"
)

// ProcessCollector gathers per-process metrics for top consumers (Tier 1).
type ProcessCollector struct {
	procRoot string
}

func NewProcessCollector(procRoot string) *ProcessCollector {
	return &ProcessCollector{procRoot: procRoot}
}

func (c *ProcessCollector) Name() string     { return "process_info" }
func (c *ProcessCollector) Category() string { return "process" }
func (c *ProcessCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *ProcessCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()

	// Get total memory for percentage calculation
	totalMem := c.getTotalMemory()

	// Get clock ticks per second (typically 100)
	clkTck := 100.0

	// Read all PIDs first pass for CPU baseline
	pids1 := c.readAllPIDs()

	// Wait for sample interval
	interval := cfg.SampleInterval
	if interval == 0 {
		interval = 1 * time.Second
	}
	select {
	case <-time.After(interval):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Second pass
	pids2 := c.readAllPIDs()

	// Calculate CPU deltas
	var processes []model.ProcessInfo
	var totalProcs, running, sleeping, zombie int
	var excludedPIDs []int

	for pid, p2 := range pids2 {
		totalProcs++
		switch p2.state {
		case "R":
			running++
		case "S", "D":
			sleeping++
		case "Z":
			zombie++
		}

		p1, ok := pids1[pid]
		cpuPct := 0.0
		if ok {
			totalTimeDelta := float64((p2.utime + p2.stime) - (p1.utime + p1.stime))
			cpuPct = totalTimeDelta / clkTck / interval.Seconds() * 100
		}

		memPct := 0.0
		if totalMem > 0 {
			memPct = float64(p2.rss*4096) / float64(totalMem) * 100
		}

		pi := model.ProcessInfo{
			PID:     pid,
			Comm:    p2.comm,
			CPUPct:  cpuPct,
			MemRSS:  p2.rss * 4096, // pages to bytes
			MemPct:  memPct,
			Threads: p2.threads,
			FDs:     p2.fds,
			State:   p2.state,
		}

		// Exclude sysdiag's own processes from top lists (but keep in totals)
		if cfg.PIDTracker != nil && cfg.PIDTracker.IsOwnPID(pid) {
			excludedPIDs = append(excludedPIDs, pid)
			continue
		}

		processes = append(processes, pi)
	}

	// Top 20 by CPU
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CPUPct > processes[j].CPUPct
	})
	n := min(20, len(processes))
	topCPU := make([]model.ProcessInfo, n)
	copy(topCPU, processes[:n])

	// Top 20 by memory
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].MemRSS > processes[j].MemRSS
	})
	n = min(20, len(processes))
	topMem := make([]model.ProcessInfo, n)
	copy(topMem, processes[:n])

	data := &model.ProcessData{
		TopByCPU:     topCPU,
		TopByMem:     topMem,
		Total:        totalProcs,
		Running:      running,
		Sleeping:     sleeping,
		Zombie:       zombie,
		ExcludedPIDs: excludedPIDs,
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

type procStat struct {
	comm    string
	state   string
	utime   uint64
	stime   uint64
	rss     int64
	threads int
	fds     int
}

func (c *ProcessCollector) readAllPIDs() map[int]procStat {
	entries, err := os.ReadDir(c.procRoot)
	if err != nil {
		return nil
	}

	result := make(map[int]procStat)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		ps, err := c.readProcPID(pid)
		if err != nil {
			continue
		}
		result[pid] = ps
	}
	return result
}

func (c *ProcessCollector) readProcPID(pid int) (procStat, error) {
	pidPath := filepath.Join(c.procRoot, strconv.Itoa(pid))

	// Read /proc/[pid]/stat
	statData, err := os.ReadFile(filepath.Join(pidPath, "stat"))
	if err != nil {
		return procStat{}, err
	}

	// Parse stat — tricky because comm can contain spaces and parens
	statStr := string(statData)
	commStart := strings.Index(statStr, "(")
	commEnd := strings.LastIndex(statStr, ")")
	if commStart < 0 || commEnd < 0 {
		return procStat{}, fmt.Errorf("malformed stat")
	}

	comm := statStr[commStart+1 : commEnd]
	rest := strings.Fields(statStr[commEnd+2:])
	// rest[0]=state, rest[11]=utime, rest[12]=stime, rest[17]=threads, rest[21]=rss

	ps := procStat{comm: comm}
	if len(rest) > 0 {
		ps.state = rest[0]
	}
	if len(rest) > 12 {
		ps.utime, _ = strconv.ParseUint(rest[11], 10, 64)
		ps.stime, _ = strconv.ParseUint(rest[12], 10, 64)
	}
	if len(rest) > 17 {
		ps.threads, _ = strconv.Atoi(rest[17])
	}
	if len(rest) > 21 {
		ps.rss, _ = strconv.ParseInt(rest[21], 10, 64)
	}

	// Count FDs
	fdEntries, err := os.ReadDir(filepath.Join(pidPath, "fd"))
	if err == nil {
		ps.fds = len(fdEntries)
	}

	return ps, nil
}

func (c *ProcessCollector) getTotalMemory() int64 {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				v, _ := strconv.ParseInt(fields[1], 10, 64)
				return v * 1024 // kB to bytes
			}
		}
	}
	return 0
}
