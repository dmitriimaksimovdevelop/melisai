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
		// Conntrack table usage
		{
			Metric: "conntrack_usage_pct", Category: "network",
			Warning: 70, Critical: 90,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.Conntrack != nil && net.Conntrack.Max > 0 {
								return net.Conntrack.UsagePct, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Conntrack table usage: %.1f%%", v)
			},
		},
		// Softnet drops rate (packets dropped by kernel network stack per second)
		{
			Metric: "softnet_dropped", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.SoftnetDropRate > 0 {
								return net.SoftnetDropRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Softnet packets dropped: %.1f/s (kernel can't keep up with NIC)", v)
			},
		},
		// Listen overflows rate (accept queue full per second)
		{
			Metric: "listen_overflows", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.ListenOverflowRate > 0 {
								return net.ListenOverflowRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Listen queue overflows: %.1f/s (backlog too small or missing reuseport)", v)
			},
		},
		// NIC RX discards (ring buffer overflow)
		{
			Metric: "nic_rx_discards", Category: "network",
			Warning: 100, Critical: 10000,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							var maxDiscards int64
							for _, iface := range net.Interfaces {
								if iface.RxDiscards > maxDiscards {
									maxDiscards = iface.RxDiscards
								}
							}
							if maxDiscards > 0 {
								return float64(maxDiscards), true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("NIC RX discards: %.0f (ring buffer overflow — increase with ethtool -G)", v)
			},
		},
		// CLOSE_WAIT sockets (application not closing connections)
		{
			Metric: "tcp_close_wait", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok && net.TCP != nil {
							if net.TCP.CloseWaitCount > 0 {
								return float64(net.TCP.CloseWaitCount), true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("CLOSE_WAIT sockets: %.0f (application not closing connections after remote FIN)", v)
			},
		},
		// Softnet time squeeze rate (NAPI budget exhausted per second)
		{
			Metric: "softnet_time_squeeze", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.SoftnetSqueezeRate > 0 {
								return net.SoftnetSqueezeRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Softnet time_squeeze: %.1f/s (NAPI budget exhausted — increase netdev_budget)", v)
			},
		},
		// TCPAbortOnMemory rate (connections aborted per second due to memory pressure)
		{
			Metric: "tcp_abort_on_memory", Category: "network",
			Warning: 0.1, Critical: 1,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCPAbortMemRate > 0 {
								return net.TCPAbortMemRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TCPAbortOnMemory: %.2f/s (connections killed by kernel due to TCP memory pressure)", v)
			},
		},
		// IRQ imbalance (uneven NET_RX distribution across CPUs)
		{
			Metric: "irq_imbalance", Category: "network",
			Warning: 5, Critical: 20,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok && len(net.IRQDistribution) > 1 {
							var maxDelta, minDelta int64
							minDelta = net.IRQDistribution[0].NetRxDelta
							for _, d := range net.IRQDistribution {
								if d.NetRxDelta > maxDelta {
									maxDelta = d.NetRxDelta
								}
								if d.NetRxDelta < minDelta {
									minDelta = d.NetRxDelta
								}
							}
							// Skip if traffic is too low to be meaningful
							if maxDelta < 100 {
								return 0, false
							}
							if minDelta > 0 {
								ratio := float64(maxDelta) / float64(minDelta)
								return ratio, true
							}
							// min is 0 but max >= 100 — extreme imbalance
							return float64(maxDelta), true
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("IRQ imbalance: max/min ratio %.1fx (enable RPS or configure IRQ affinity)", v)
			},
		},
		// UDP receive buffer error rate (drops per second)
		{
			Metric: "udp_rcvbuf_errors", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.UDPRcvbufErrRate > 0 {
								return net.UDPRcvbufErrRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("UDP receive buffer errors: %.1f/s (increase rmem_max and application SO_RCVBUF)", v)
			},
		},
		// TCP Recv-Q drops (application not reading ESTABLISHED sockets fast enough)
		{
			Metric: "tcp_rcvq_drop", Category: "network",
			Warning: 1, Critical: 100,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCPRcvQDropRate > 0 {
								return net.TCPRcvQDropRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TCP Recv-Q drops: %.1f/s (application not reading from sockets fast enough)", v)
			},
		},
		// TCP Zero Window drops
		{
			Metric: "tcp_zero_window_drop", Category: "network",
			Warning: 1, Critical: 50,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							if net.TCPZeroWindowDropRate > 0 {
								return net.TCPZeroWindowDropRate, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("TCP zero-window drops: %.1f/s (receiver advertised window=0, app not consuming data)", v)
			},
		},
		// Listen queue saturation (accept queue filling up)
		{
			Metric: "listen_queue_saturation", Category: "network",
			Warning: 70, Critical: 90,
			Evaluator: func(r *Report) (float64, bool) {
				if results, ok := r.Categories["network"]; ok {
					for _, res := range results {
						if net, ok := res.Data.(*NetworkData); ok {
							var maxFill float64
							for _, ls := range net.ListenSockets {
								if ls.FillPct > maxFill {
									maxFill = ls.FillPct
								}
							}
							if maxFill > 0 {
								return maxFill, true
							}
						}
					}
				}
				return 0, false
			},
			Message: func(v float64) string {
				return fmt.Sprintf("Listen queue fill: %.0f%% (accept queue near capacity — add SO_REUSEPORT or more worker threads)", v)
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
