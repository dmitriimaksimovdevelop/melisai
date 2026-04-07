package model

import "testing"

// BenchmarkDetectAnomalies measures the cost of running all 37 threshold rules.
func BenchmarkDetectAnomalies(b *testing.B) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{
					Collector: "cpu",
					Category:  "cpu",
					Data: &CPUData{
						UserPct:               45,
						SystemPct:             15,
						IOWaitPct:             5,
						IdlePct:               35,
						NumCPUs:               8,
						LoadAvg1:              12,
						ContextSwitchesPerSec: 50000,
						PSISome10:             8,
					},
				},
			},
			"memory": {
				{
					Collector: "memory",
					Category:  "memory",
					Data: &MemoryData{
						TotalBytes:     16e9,
						AvailableBytes: 2e9,
						SwapTotalBytes: 4e9,
						SwapUsedBytes:  1e9,
						PSISome10:      12,
						Reclaim: &ReclaimStats{
							DirectReclaimRate: 50,
							CompactStallRate:  5,
							THPSplitRate:      10,
						},
						NUMANodes: []NUMANode{
							{Node: 0, MissRatio: 8.0},
						},
					},
				},
			},
			"disk": {
				{
					Collector: "disk",
					Category:  "disk",
					Data: &DiskData{
						Devices: []DiskDevice{
							{Name: "sda", AvgLatencyMs: 15, Rotational: true},
							{Name: "nvme0n1", AvgLatencyMs: 0.5, Rotational: false},
						},
						PSISome10: 20,
					},
				},
				{
					Collector:  "biolatency",
					Category:   "disk",
					Histograms: []Histogram{{Name: "block_io_latency_sda", Unit: "us", P99: 50000}},
				},
			},
			"network": {
				{
					Collector: "network",
					Category:  "network",
					Data: &NetworkData{
						TCP: &TCPStats{
							RetransRate:   20,
							TimeWaitCount: 8000,
							CloseWaitCount: 5,
						},
						Interfaces: []NetworkInterface{
							{Name: "eth0", ErrorsPerSec: 15, RxDiscards: 500},
						},
						Conntrack: &ConntrackStats{Max: 65536, UsagePct: 75},
						Softnet:   &SoftnetData{DropRate: 5, SqueezeRate: 3},
						TCPExt: &TCPExtendedStats{
							ListenOverflowRate:     2,
							TCPAbortMemRate:        0.5,
							TCPRcvQDropRate:        3,
							TCPZeroWindowDropRate:  1,
						},
						UDP: &UDPStats{RcvbufErrRate: 10},
					},
				},
			},
			"container": {
				{
					Collector: "container",
					Category:  "container",
					Data: &ContainerData{
						CPUThrottledPeriods: 200,
						MemoryLimit:         4e9,
						MemoryUsage:         3.5e9,
					},
				},
			},
		},
		Summary: Summary{
			Resources: map[string]USEMetric{},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectAnomalies(report)
	}
}

// BenchmarkDefaultThresholds measures the cost of accessing the threshold list.
func BenchmarkDefaultThresholds(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t := DefaultThresholds()
		_ = t
	}
}
