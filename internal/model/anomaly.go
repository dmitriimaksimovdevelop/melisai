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
			Warning: 10, Critical: 50, // rate per second
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCP != nil {
								return net.TCP.RetransRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TCP retransmit rate: %.1f/sec", v)
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
			Metric: "cpu_throttling", Category: "container",
			Warning: 100, Critical: 1000,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["container"]; ok {
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
			Metric: "container_memory_usage", Category: "container",
			Warning: 80, Critical: 95,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["container"]; ok {
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
		// Tier 2 histogram-based thresholds (biolatency, runqlat, gethostlatency, cachestat)
		{
			Metric: "biolatency_p99_ssd", Category: "disk",
			Warning: 5, Critical: 25, // milliseconds
			Evaluator: histogramP99Evaluator("biolatency", false),
			Message: func(v float64) string {
				return fmt.Sprintf("Block I/O p99 latency (SSD): %.1fms", v)
			},
		},
		{
			Metric: "biolatency_p99_hdd", Category: "disk",
			Warning: 50, Critical: 200, // milliseconds
			Evaluator: histogramP99Evaluator("biolatency", true),
			Message: func(v float64) string {
				return fmt.Sprintf("Block I/O p99 latency (HDD): %.1fms", v)
			},
		},
		{
			Metric: "runqlat_p99", Category: "cpu",
			Warning: 10, Critical: 50, // milliseconds
			Evaluator: histogramP99Evaluator("runqlat", false),
			Message: func(v float64) string {
				return fmt.Sprintf("Run queue latency p99: %.1fms (scheduler delay)", v)
			},
		},
		{
			Metric: "dns_latency_p99", Category: "network",
			Warning: 50, Critical: 200, // milliseconds
			Evaluator: histogramP99Evaluator("gethostlatency", false),
			Message: func(v float64) string {
				return fmt.Sprintf("DNS lookup latency p99: %.1fms", v)
			},
		},
		{
			Metric: "cache_miss_ratio", Category: "memory",
			Warning: 5, Critical: 15, // percentage
			Evaluator: func(r *Report) (float64, bool) {
				for _, results := range r.Categories {
					for _, res := range results {
						if res.Collector == "cachestat" {
							for _, h := range res.Histograms {
								if h.Name == "cache_miss_ratio" {
									return h.Mean, true
								}
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Page cache miss ratio: %.1f%%", v)
			},
		},
		// PSI thresholds (CPU and I/O)
		{
			Metric: "cpu_psi_pressure", Category: "cpu",
			Warning: 5.0, Critical: 25.0,
			Evaluator: func(r *Report) (float64, bool) {
				// CPU PSI is read from /proc/pressure/cpu
				if results, ok := r.Categories["cpu"]; ok {
					for _, res := range results {
						if cpu, ok := res.Data.(*CPUData); ok {
							if cpu.PSISome10 > 0 {
								return cpu.PSISome10, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("CPU PSI pressure: %.1f%% (some tasks stalling on CPU)", v)
			},
		},
		{
			Metric: "io_psi_pressure", Category: "disk",
			Warning: 10.0, Critical: 50.0,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["disk"]; ok {
					for _, res := range results {
						if disk, ok := res.Data.(*DiskData); ok {
							if disk.PSISome10 > 0 {
								return disk.PSISome10, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("I/O PSI pressure: %.1f%% (tasks stalling on I/O)", v)
			},
		},
		// Disk average latency
		{
			Metric: "disk_avg_latency", Category: "disk",
			Warning: 5, Critical: 50, // ms; conservative for mixed SSD/HDD
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["disk"]; ok {
					for _, res := range results {
						if disk, ok := res.Data.(*DiskData); ok {
							var maxLat float64
							for _, dev := range disk.Devices {
								if dev.AvgLatencyMs > maxLat {
									maxLat = dev.AvgLatencyMs
								}
							}
							if maxLat > 0 {
								return maxLat, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Disk average I/O latency: %.1fms", v)
			},
		},
		// Network errors per second
		{
			Metric: "network_errors_per_sec", Category: "network",
			Warning: 10, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							var maxRate float64
							for _, iface := range net.Interfaces {
								if iface.ErrorsPerSec > maxRate {
									maxRate = iface.ErrorsPerSec
								}
							}
							if maxRate > 0 {
								return maxRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Network errors: %.1f/sec", v)
			},
		},
	}
}

// histogramP99Evaluator returns an evaluator that searches for histograms
// from a given collector and returns the max P99 value in milliseconds.
// If rotational is true, only matches histograms for rotational (HDD) devices;
// if false, only matches non-rotational (SSD/NVMe) devices.
// It cross-references with Tier 1 disk data to determine device type.
func histogramP99Evaluator(collectorName string, rotational bool) func(*Report) (float64, bool) {
	return func(r *Report) (float64, bool) {
		// Build set of rotational devices from Tier 1 disk data
		rotationalDevs := make(map[string]bool)
		if diskResults, ok := r.Categories["disk"]; ok {
			for _, res := range diskResults {
				if disk, ok := res.Data.(*DiskData); ok {
					for _, dev := range disk.Devices {
						rotationalDevs[dev.Name] = dev.Rotational
					}
				}
			}
		}

		var maxP99 float64
		found := false
		for _, results := range r.Categories {
			for _, res := range results {
				if res.Collector != collectorName {
					continue
				}
				for _, h := range res.Histograms {
					if h.P99 <= 0 {
						continue
					}

					// For biolatency, match device type from histogram name
					if collectorName == "biolatency" && len(rotationalDevs) > 0 {
						devName := extractDeviceName(h.Name)
						if devName != "" {
							isRotational, known := rotationalDevs[devName]
							if known && isRotational != rotational {
								continue // wrong device type
							}
						}
					}

					// Convert to milliseconds if histogram is in microseconds
					p99ms := h.P99
					if h.Unit == "us" {
						p99ms = h.P99 / 1000.0
					}
					if p99ms > maxP99 {
						maxP99 = p99ms
						found = true
					}
				}
			}
		}
		return maxP99, found
	}
}

// extractDeviceName extracts a device name from a histogram name like
// "block_io_latency_nvme0n1" → "nvme0n1".
func extractDeviceName(histName string) string {
	prefixes := []string{"block_io_latency_"}
	for _, prefix := range prefixes {
		if len(histName) > len(prefix) && histName[:len(prefix)] == prefix {
			return histName[len(prefix):]
		}
	}
	return ""
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
