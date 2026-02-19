// CPU collector (Tier 1): /proc/stat delta sampling, load average,
// context switches, per-CPU breakdown, CFS scheduler params.
package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// CPUCollector gathers CPU metrics from procfs (Tier 1).
type CPUCollector struct {
	procRoot string
}

func NewCPUCollector(procRoot string) *CPUCollector {
	return &CPUCollector{procRoot: procRoot}
}

func (c *CPUCollector) Name() string     { return "cpu_utilization" }
func (c *CPUCollector) Category() string { return "cpu" }
func (c *CPUCollector) Available() Availability {
	return Availability{Tier: 1}
}

// cpuTimes holds jiffies for each CPU state.
type cpuTimes struct {
	user    uint64
	nice    uint64
	system  uint64
	idle    uint64
	iowait  uint64
	irq     uint64
	softirq uint64
	steal   uint64
}

func (t cpuTimes) total() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}

func (c *CPUCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	startTime := time.Now()

	// Two-point sampling for delta calculation
	sample1, perCPU1, ctxSw1 := c.readProcStat()

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

	sample2, perCPU2, ctxSw2 := c.readProcStat()

	// Compute overall CPU percentages from delta
	data := c.computeDelta(sample1, sample2)

	// Context switches per second
	ctxSwDelta := ctxSw2 - ctxSw1
	data.ContextSwitchesPerSec = int64(float64(ctxSwDelta) / interval.Seconds())

	// Load average
	data.LoadAvg1, data.LoadAvg5, data.LoadAvg15 = c.readLoadAvg()

	// Number of CPUs
	data.NumCPUs = runtime.NumCPU()

	// Per-CPU deltas
	data.PerCPU = c.computePerCPUDeltas(perCPU1, perCPU2)

	// CFS scheduler parameters
	data.SchedLatencyNS = readSysctlInt64(c.procRoot, "sys/kernel/sched_latency_ns")
	data.SchedMinGranularityNS = readSysctlInt64(c.procRoot, "sys/kernel/sched_min_granularity_ns")

	// CPU PSI pressure
	c.parseCPUPSI(data)

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: startTime,
		EndTime:   time.Now(),
		Data:      data,
	}, nil
}

// readProcStat parses /proc/stat and returns aggregate + per-CPU times + context switch count.
func (c *CPUCollector) readProcStat() (cpuTimes, map[int]cpuTimes, uint64) {
	f, err := os.Open(filepath.Join(c.procRoot, "stat"))
	if err != nil {
		return cpuTimes{}, nil, 0
	}
	defer f.Close()

	var aggregate cpuTimes
	perCPU := make(map[int]cpuTimes)
	var ctxSwitches uint64

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if fields[0] == "cpu" && len(fields) >= 9 {
			aggregate = parseCPULine(fields)
		} else if strings.HasPrefix(fields[0], "cpu") && len(fields) >= 9 {
			cpuNum, err := strconv.Atoi(strings.TrimPrefix(fields[0], "cpu"))
			if err == nil {
				perCPU[cpuNum] = parseCPULine(fields)
			}
		} else if fields[0] == "ctxt" {
			ctxSwitches, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}

	return aggregate, perCPU, ctxSwitches
}

func parseCPULine(fields []string) cpuTimes {
	parse := func(idx int) uint64 {
		if idx >= len(fields) {
			return 0
		}
		v, _ := strconv.ParseUint(fields[idx], 10, 64)
		return v
	}
	return cpuTimes{
		user:    parse(1),
		nice:    parse(2),
		system:  parse(3),
		idle:    parse(4),
		iowait:  parse(5),
		irq:     parse(6),
		softirq: parse(7),
		steal:   parse(8),
	}
}

func (c *CPUCollector) computeDelta(before, after cpuTimes) *model.CPUData {
	totalDelta := float64(after.total() - before.total())
	if totalDelta == 0 {
		return &model.CPUData{}
	}

	return &model.CPUData{
		UserPct:    float64(after.user-before.user+after.nice-before.nice) / totalDelta * 100,
		SystemPct:  float64(after.system-before.system) / totalDelta * 100,
		IOWaitPct:  float64(after.iowait-before.iowait) / totalDelta * 100,
		IdlePct:    float64(after.idle-before.idle) / totalDelta * 100,
		StealPct:   float64(after.steal-before.steal) / totalDelta * 100,
		IRQPct:     float64(after.irq-before.irq) / totalDelta * 100,
		SoftIRQPct: float64(after.softirq-before.softirq) / totalDelta * 100,
	}
}

func (c *CPUCollector) computePerCPUDeltas(before, after map[int]cpuTimes) []model.PerCPU {
	// Collect and sort CPU numbers for deterministic output order
	cpuNums := make([]int, 0, len(after))
	for cpuNum := range after {
		cpuNums = append(cpuNums, cpuNum)
	}
	sort.Ints(cpuNums)

	var result []model.PerCPU
	for _, cpuNum := range cpuNums {
		afterTimes := after[cpuNum]
		beforeTimes, ok := before[cpuNum]
		if !ok {
			continue
		}
		totalDelta := float64(afterTimes.total() - beforeTimes.total())
		if totalDelta == 0 {
			continue
		}
		result = append(result, model.PerCPU{
			CPU:       cpuNum,
			UserPct:   float64(afterTimes.user-beforeTimes.user+afterTimes.nice-beforeTimes.nice) / totalDelta * 100,
			SystemPct: float64(afterTimes.system-beforeTimes.system) / totalDelta * 100,
			IOWaitPct: float64(afterTimes.iowait-beforeTimes.iowait) / totalDelta * 100,
			IdlePct:   float64(afterTimes.idle-beforeTimes.idle) / totalDelta * 100,
		})
	}
	return result
}

func (c *CPUCollector) parseCPUPSI(data *model.CPUData) {
	f, err := os.Open(filepath.Join(c.procRoot, "pressure", "cpu"))
	if err != nil {
		return // PSI not available (kernel < 4.20)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				continue
			}
			val, _ := strconv.ParseFloat(parts[1], 64)
			switch parts[0] {
			case "avg10":
				data.PSISome10 = val
			case "avg60":
				data.PSISome60 = val
			}
		}
	}
}

func (c *CPUCollector) readLoadAvg() (float64, float64, float64) {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "loadavg"))
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	la1, _ := strconv.ParseFloat(fields[0], 64)
	la5, _ := strconv.ParseFloat(fields[1], 64)
	la15, _ := strconv.ParseFloat(fields[2], 64)
	return la1, la5, la15
}
