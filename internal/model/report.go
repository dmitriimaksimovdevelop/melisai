package model

// ComputeUSEMetrics calculates USE methodology metrics from collected data.
// Maps to: CPU, Memory, Disk, Network × {Utilization, Saturation, Errors}
func ComputeUSEMetrics(report *Report) map[string]USEMetric {
	resources := make(map[string]USEMetric)

	// CPU
	if cpuResults, ok := report.Categories["cpu"]; ok {
		for _, r := range cpuResults {
			if cpu, ok := r.Data.(*CPUData); ok {
				resources["cpu"] = USEMetric{
					Utilization: 100 - cpu.IdlePct,
					Saturation:  computeCPUSaturation(cpu),
					Errors:      0, // CPU errors are rare
				}
				break
			}
		}
	}

	// Memory
	if memResults, ok := report.Categories["memory"]; ok {
		for _, r := range memResults {
			if mem, ok := r.Data.(*MemoryData); ok {
				utilization := 0.0
				if mem.TotalBytes > 0 {
					utilization = float64(mem.TotalBytes-mem.AvailableBytes) / float64(mem.TotalBytes) * 100
				}
				resources["memory"] = USEMetric{
					Utilization: utilization,
					Saturation:  computeMemSaturation(mem),
					Errors:      int(mem.MajorFaults), // OOM-related
				}
				break
			}
		}
	}

	// Disk
	if diskResults, ok := report.Categories["disk"]; ok {
		for _, r := range diskResults {
			if disk, ok := r.Data.(*DiskData); ok {
				resources["disk"] = USEMetric{
					Utilization: computeDiskUtilization(disk),
					Saturation:  computeDiskSaturation(disk),
					Errors:      0,
				}
				break
			}
		}
	}

	// Network
	if netResults, ok := report.Categories["network"]; ok {
		for _, r := range netResults {
			if net, ok := r.Data.(*NetworkData); ok {
				resources["network"] = USEMetric{
					Utilization: 0, // Requires link speed, which we may not have
					Saturation:  computeNetSaturation(net),
					Errors:      computeNetErrors(net),
				}
				break
			}
		}
	}

	// Software resources (container)
	if containerResults, ok := report.Categories["container"]; ok {
		for _, r := range containerResults {
			if container, ok := r.Data.(*ContainerData); ok && container.Runtime != "none" {
				if container.CPUQuota > 0 && container.CPUPeriod > 0 {
					// Cgroup CPU utilization: throttled time as % of total allowed CPU time.
					// CPUThrottledTime is in microseconds, CPUQuota/CPUPeriod define the allowed ratio.
					allowedCPURatio := float64(container.CPUQuota) / float64(container.CPUPeriod)
					if allowedCPURatio > 0 && container.CPUThrottledTime > 0 {
						// Utilization = percentage of throttled time vs total CPU budget
						throttledSec := float64(container.CPUThrottledTime) / 1e6
						resources["container_cpu"] = USEMetric{
							Utilization: throttledSec / allowedCPURatio * 100,
							Saturation:  float64(container.CPUThrottledPeriods), // queue depth: how often throttling occurred
						}
					} else {
						resources["container_cpu"] = USEMetric{
							Utilization: 0,
							Saturation:  float64(container.CPUThrottledPeriods),
						}
					}
				}
				if container.MemoryLimit > 0 {
					resources["container_memory"] = USEMetric{
						Utilization: float64(container.MemoryUsage) / float64(container.MemoryLimit) * 100,
					}
				}
			}
		}
	}

	return resources
}

func computeCPUSaturation(cpu *CPUData) float64 {
	// Run queue saturation: load avg / num CPUs
	if cpu.NumCPUs > 0 {
		ratio := cpu.LoadAvg1 / float64(cpu.NumCPUs)
		if ratio > 1.0 {
			return (ratio - 1.0) * 100 // percentage over capacity
		}
	}
	return 0
}

func computeMemSaturation(mem *MemoryData) float64 {
	// Swap usage as saturation indicator
	if mem.SwapTotalBytes > 0 {
		return float64(mem.SwapUsedBytes) / float64(mem.SwapTotalBytes) * 100
	}
	return 0
}

func computeDiskUtilization(disk *DiskData) float64 {
	// Maximum IOTimeMs across devices
	var maxUtil float64
	for _, dev := range disk.Devices {
		// IOTimeMs is in ms per second of sampling; 1000 = 100% utilization
		util := float64(dev.IOTimeMs) / 10.0 // rough percentage
		if util > maxUtil {
			maxUtil = util
		}
	}
	if maxUtil > 100 {
		maxUtil = 100
	}
	return maxUtil
}

func computeDiskSaturation(disk *DiskData) float64 {
	var maxQueue float64
	for _, dev := range disk.Devices {
		if float64(dev.IOInProgress) > maxQueue {
			maxQueue = float64(dev.IOInProgress)
		}
	}
	return maxQueue
}

func computeNetSaturation(net *NetworkData) float64 {
	if net.TCP != nil {
		return float64(net.TCP.TimeWaitCount + net.TCP.CloseWaitCount)
	}
	return 0
}

func computeNetErrors(net *NetworkData) int {
	total := 0
	for _, iface := range net.Interfaces {
		total += int(iface.RxErrors + iface.TxErrors + iface.RxDropped + iface.TxDropped)
	}
	if net.TCP != nil {
		total += net.TCP.RetransSegs + net.TCP.InErrs
	}
	return total
}
