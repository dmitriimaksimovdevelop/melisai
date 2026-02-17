package model

import "fmt"

// Threshold defines an anomaly detection rule.
type Threshold struct {
	Metric    string
	Category  string
	Warning   float64
	Critical  float64
	Evaluator func(report *Report) (float64, bool)
	Message   func(value float64) string
}

// DefaultThresholds returns the built-in anomaly thresholds.
// Based on Brendan Gregg's recommended thresholds.
func DefaultThresholds() []Threshold {
	return []Threshold{
		// CPU
		{
			Metric: "cpu_utilization", Category: "cpu",
			Warning: 80, Critical: 95,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["cpu"]; ok {
					for _, res := range results {
						if cpu, ok := res.Data.(*CPUData); ok {
							return 100 - cpu.IdlePct, true
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("CPU utilization at %.1f%%", v)
			},
		},
		{
			Metric: "cpu_iowait", Category: "cpu",
			Warning: 10, Critical: 30,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["cpu"]; ok {
					for _, res := range results {
						if cpu, ok := res.Data.(*CPUData); ok {
							return cpu.IOWaitPct, true
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("High CPU iowait: %.1f%% (CPUs blocked on I/O)", v)
			},
		},
		{
			Metric: "load_average", Category: "cpu",
			Warning: 2.0, Critical: 4.0, // multiplied by NumCPU
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["cpu"]; ok {
					for _, res := range results {
						if cpu, ok := res.Data.(*CPUData); ok {
							if cpu.NumCPUs > 0 {
								return cpu.LoadAvg1 / float64(cpu.NumCPUs), true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Load average per CPU: %.2f (saturation threshold: 1.0)", v)
			},
		},
		// Memory
		{
			Metric: "memory_utilization", Category: "memory",
			Warning: 85, Critical: 95,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["memory"]; ok {
					for _, res := range results {
						if mem, ok := res.Data.(*MemoryData); ok {
							if mem.TotalBytes > 0 {
								return float64(mem.TotalBytes-mem.AvailableBytes) / float64(mem.TotalBytes) * 100, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Memory utilization at %.1f%%", v)
			},
		},
		{
			Metric: "swap_usage", Category: "memory",
			Warning: 10, Critical: 50,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["memory"]; ok {
					for _, res := range results {
						if mem, ok := res.Data.(*MemoryData); ok {
							if mem.SwapTotalBytes > 0 {
								return float64(mem.SwapUsedBytes) / float64(mem.SwapTotalBytes) * 100, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Swap usage at %.1f%%", v)
			},
		},
		{
			Metric: "memory_psi_pressure", Category: "memory",
			Warning: 5.0, Critical: 25.0,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["memory"]; ok {
					for _, res := range results {
						if mem, ok := res.Data.(*MemoryData); ok {
							if mem.PSISome10 > 0 {
								return mem.PSISome10, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Memory PSI pressure: %.1f%% (some tasks stalling)", v)
			},
		},
		// Network
		{
			Metric: "tcp_retransmits", Category: "network",
			Warning: 50, Critical: 200,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCP != nil {
								return float64(net.TCP.RetransSegs), true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TCP retransmits: %.0f segments", v)
			},
		},
		{
			Metric: "tcp_timewait", Category: "network",
			Warning: 5000, Critical: 20000,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCP != nil {
								return float64(net.TCP.TimeWaitCount), true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TIME_WAIT connections: %.0f", v)
			},
		},
		// Disk
		{
			Metric: "disk_utilization", Category: "disk",
			Warning: 70, Critical: 90,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["disk"]; ok {
					for _, res := range results {
						if disk, ok := res.Data.(*DiskData); ok {
							return computeDiskUtilization(disk), true
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Disk utilization: %.1f%%", v)
			},
		},
		// Container-specific
		{
			Metric: "cpu_throttling", Category: "system",
			Warning: 100, Critical: 1000,
			Evaluator: func(r *Report) (float64, bool) {
				for _, results := range r.Categories {
					for _, res := range results {
						if c, ok := res.Data.(*ContainerData); ok {
							return float64(c.CPUThrottledPeriods), true
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("CPU throttled periods: %.0f (container CPU limit hit)", v)
			},
		},
		{
			Metric: "container_memory_usage", Category: "system",
			Warning: 80, Critical: 95,
			Evaluator: func(r *Report) (float64, bool) {
				for _, results := range r.Categories {
					for _, res := range results {
						if c, ok := res.Data.(*ContainerData); ok {
							if c.MemoryLimit > 0 {
								return float64(c.MemoryUsage) / float64(c.MemoryLimit) * 100, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Container memory: %.1f%% of limit (OOM risk)", v)
			},
		},
	}
}

// DetectAnomalies runs all threshold checks against the report.
func DetectAnomalies(report *Report) []Anomaly {
	var anomalies []Anomaly

	for _, threshold := range DefaultThresholds() {
		value, found := threshold.Evaluator(report)
		if !found {
			continue
		}

		var severity string
		switch {
		case value >= threshold.Critical:
			severity = "critical"
		case value >= threshold.Warning:
			severity = "warning"
		default:
			continue // below all thresholds
		}

		anomalies = append(anomalies, Anomaly{
			Severity:  severity,
			Category:  threshold.Category,
			Metric:    threshold.Metric,
			Message:   threshold.Message(value),
			Value:     fmt.Sprintf("%.2f", value),
			Threshold: fmt.Sprintf("warning=%.0f, critical=%.0f", threshold.Warning, threshold.Critical),
		})
	}

	return anomalies
}
