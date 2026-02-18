package model

import (
	"testing"
)

// TestTCPRetransmitRateThreshold verifies that tcp_retransmits evaluator uses
// RetransRate (rate/sec) and NOT absolute RetransSegs.
func TestTCPRetransmitRateThreshold(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{
					Collector: "network",
					Category:  "network",
					Data: &NetworkData{
						TCP: &TCPStats{
							RetransRate: 15.0,  // above warning=10, below critical=50
							RetransSegs: 50000, // high absolute value; must NOT be used
						},
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "tcp_retransmits" {
			found = true
			if a.Severity != "warning" {
				t.Errorf("tcp_retransmits severity = %q, want %q", a.Severity, "warning")
			}
			if a.Value != "15.00" {
				t.Errorf("tcp_retransmits value = %q, want %q (RetransRate, not RetransSegs)", a.Value, "15.00")
			}
		}
	}
	if !found {
		t.Error("expected tcp_retransmits anomaly with RetransRate=15.0 (threshold warning=10)")
	}
}

// TestContainerAnomalyCategory verifies that cpu_throttling and
// container_memory_usage evaluators look ONLY in r.Categories["container"],
// not iterate all categories. This is a regression test for bug #13.
func TestContainerAnomalyCategory(t *testing.T) {
	// Place ContainerData under "container" category -> should be found.
	reportWithContainer := &Report{
		Categories: map[string][]Result{
			"container": {
				{
					Collector: "container",
					Category:  "container",
					Data: &ContainerData{
						CPUThrottledPeriods: 500, // above warning=100
						MemoryLimit:         1e9, // 1 GB
						MemoryUsage:         9e8, // 900 MB = 90% -> above warning=80
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(reportWithContainer)

	hasThrottling := false
	hasMemUsage := false
	for _, a := range anomalies {
		if a.Metric == "cpu_throttling" {
			hasThrottling = true
		}
		if a.Metric == "container_memory_usage" {
			hasMemUsage = true
		}
	}
	if !hasThrottling {
		t.Error("expected cpu_throttling anomaly when ContainerData is in 'container' category")
	}
	if !hasMemUsage {
		t.Error("expected container_memory_usage anomaly when ContainerData is in 'container' category")
	}

	// Place ContainerData under "cpu" category (wrong) -> must NOT be found.
	reportWrongCategory := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{
					Collector: "container",
					Category:  "cpu",
					Data: &ContainerData{
						CPUThrottledPeriods: 500,
						MemoryLimit:         1e9,
						MemoryUsage:         9e8,
					},
				},
			},
		},
	}

	anomalies2 := DetectAnomalies(reportWrongCategory)
	for _, a := range anomalies2 {
		if a.Metric == "cpu_throttling" || a.Metric == "container_memory_usage" {
			t.Errorf("container metric %q should NOT be detected when data is in 'cpu' category (bug #13 regression)", a.Metric)
		}
	}
}

// TestDiskAvgLatencyThreshold verifies the disk_avg_latency evaluator triggers
// critical when any device has AvgLatencyMs above the critical threshold (50ms).
func TestDiskAvgLatencyThreshold(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"disk": {
				{
					Collector: "disk",
					Category:  "disk",
					Data: &DiskData{
						Devices: []DiskDevice{
							{Name: "sda", AvgLatencyMs: 2.0},  // healthy
							{Name: "sdb", AvgLatencyMs: 60.0}, // above critical=50
						},
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "disk_avg_latency" {
			found = true
			if a.Severity != "critical" {
				t.Errorf("disk_avg_latency severity = %q, want %q (value=60, critical=50)", a.Severity, "critical")
			}
			if a.Value != "60.00" {
				t.Errorf("disk_avg_latency value = %q, want %q", a.Value, "60.00")
			}
		}
	}
	if !found {
		t.Error("expected disk_avg_latency anomaly with AvgLatencyMs=60 (threshold critical=50)")
	}
}

// TestNetworkErrorsPerSecThreshold verifies network_errors_per_sec triggers a
// warning when any interface has ErrorsPerSec above the warning threshold (10).
func TestNetworkErrorsPerSecThreshold(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{
					Collector: "network",
					Category:  "network",
					Data: &NetworkData{
						Interfaces: []NetworkInterface{
							{Name: "eth0", ErrorsPerSec: 2.0},  // healthy
							{Name: "eth1", ErrorsPerSec: 15.0}, // above warning=10, below critical=100
						},
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "network_errors_per_sec" {
			found = true
			if a.Severity != "warning" {
				t.Errorf("network_errors_per_sec severity = %q, want %q", a.Severity, "warning")
			}
			if a.Value != "15.00" {
				t.Errorf("network_errors_per_sec value = %q, want %q", a.Value, "15.00")
			}
		}
	}
	if !found {
		t.Error("expected network_errors_per_sec anomaly with ErrorsPerSec=15 (threshold warning=10)")
	}
}

// TestHistogramP99Evaluator verifies that histogramP99Evaluator matches a
// histogram named "biolatency" and returns its P99 value. With P99=30 and
// SSD thresholds (warning=5, critical=25) it should trigger critical.
func TestHistogramP99Evaluator(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"disk": {
				{
					Collector: "biolatency",
					Category:  "disk",
					Histograms: []Histogram{
						{
							Name: "biolatency",
							Unit: "ms",
							P99:  30.0, // above critical=25 for SSD
						},
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "biolatency_p99_ssd" {
			found = true
			if a.Severity != "critical" {
				t.Errorf("biolatency_p99_ssd severity = %q, want %q (P99=30, critical=25)", a.Severity, "critical")
			}
			if a.Value != "30.00" {
				t.Errorf("biolatency_p99_ssd value = %q, want %q", a.Value, "30.00")
			}
		}
	}
	if !found {
		t.Error("expected biolatency_p99_ssd anomaly with P99=30 (threshold critical=25)")
	}
}

// TestCPUPSIThreshold verifies cpu_psi_pressure triggers critical when
// CPUData.PSISome10 exceeds the critical threshold (25).
func TestCPUPSIThreshold(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{
					Collector: "cpu",
					Category:  "cpu",
					Data: &CPUData{
						IdlePct:   80, // healthy utilization so cpu_utilization won't confuse
						IOWaitPct: 1,
						LoadAvg1:  0.5,
						NumCPUs:   4,
						PSISome10: 30.0, // above critical=25
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "cpu_psi_pressure" {
			found = true
			if a.Severity != "critical" {
				t.Errorf("cpu_psi_pressure severity = %q, want %q (PSISome10=30, critical=25)", a.Severity, "critical")
			}
			if a.Value != "30.00" {
				t.Errorf("cpu_psi_pressure value = %q, want %q", a.Value, "30.00")
			}
		}
	}
	if !found {
		t.Error("expected cpu_psi_pressure anomaly with PSISome10=30 (threshold critical=25)")
	}
}

// TestIOPSIThreshold verifies io_psi_pressure triggers warning when
// DiskData.PSISome10 exceeds the warning threshold (10) but stays below critical (50).
func TestIOPSIThreshold(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"disk": {
				{
					Collector: "disk",
					Category:  "disk",
					Data: &DiskData{
						Devices:   []DiskDevice{{Name: "sda"}},
						PSISome10: 15.0, // above warning=10, below critical=50
					},
				},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	found := false
	for _, a := range anomalies {
		if a.Metric == "io_psi_pressure" {
			found = true
			if a.Severity != "warning" {
				t.Errorf("io_psi_pressure severity = %q, want %q (PSISome10=15, warning=10)", a.Severity, "warning")
			}
			if a.Value != "15.00" {
				t.Errorf("io_psi_pressure value = %q, want %q", a.Value, "15.00")
			}
		}
	}
	if !found {
		t.Error("expected io_psi_pressure anomaly with PSISome10=15 (threshold warning=10)")
	}
}
