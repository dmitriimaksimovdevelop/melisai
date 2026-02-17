package model

import (
	"encoding/json"
	"testing"
)

func TestReportJSON(t *testing.T) {
	report := &Report{
		Metadata: Metadata{
			Tool:          "sysdiag",
			Version:       "0.1.0",
			SchemaVersion: "1.0.0",
			Hostname:      "test-host",
			Timestamp:     "2024-01-01T00:00:00Z",
			Duration:      "30s",
			Profile:       "standard",
			FocusAreas:    []string{"disk", "network"},
			KernelVersion: "6.1.0",
			Arch:          "amd64",
			CPUs:          8,
			MemoryGB:      32,
			Capabilities:  []string{"procfs", "bcc"},
			ContainerEnv:  "kubernetes",
			CgroupVersion: 2,
		},
		Categories: map[string][]Result{
			"cpu": {
				{
					Collector: "cpu_utilization",
					Category:  "cpu",
					Tier:      1,
					Data: &CPUData{
						UserPct:   45.2,
						SystemPct: 12.1,
						IdlePct:   42.7,
						LoadAvg1:  3.14,
						NumCPUs:   8,
					},
				},
			},
		},
		Summary: Summary{
			HealthScore: 85,
			Anomalies: []Anomaly{
				{
					Severity:  "warning",
					Category:  "cpu",
					Metric:    "cpu_utilization",
					Message:   "High CPU utilization detected",
					Value:     "92.3%",
					Threshold: "90%",
				},
			},
			Resources: map[string]USEMetric{
				"cpu": {
					Utilization: 57.3,
					Saturation:  2.1,
					Errors:      0,
				},
			},
			Recommendations: []Recommendation{
				{
					Priority:       1,
					Category:       "network",
					Title:          "Enable BBR congestion control",
					Commands:       []string{"sysctl -w net.ipv4.tcp_congestion_control=bbr"},
					Persistent:     []string{"echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.d/99-sysdiag.conf"},
					ExpectedImpact: "2-3x throughput on high-BDP links",
					Evidence:       "tcp_congestion_control=cubic + retransmits=94/min",
					Source:         "Brendan Gregg, Systems Performance ch.10",
				},
			},
		},
	}

	// Verify serialization round-trip
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}

	if decoded.Metadata.SchemaVersion != "1.0.0" {
		t.Errorf("schema_version = %q, want 1.0.0", decoded.Metadata.SchemaVersion)
	}
	if decoded.Metadata.ContainerEnv != "kubernetes" {
		t.Errorf("container_env = %q, want kubernetes", decoded.Metadata.ContainerEnv)
	}
	if decoded.Summary.HealthScore != 85 {
		t.Errorf("health_score = %d, want 85", decoded.Summary.HealthScore)
	}
	if len(decoded.Summary.Anomalies) != 1 {
		t.Errorf("anomalies count = %d, want 1", len(decoded.Summary.Anomalies))
	}
	if len(decoded.Summary.Recommendations) != 1 {
		t.Errorf("recommendations count = %d, want 1", len(decoded.Summary.Recommendations))
	}
}

func TestHistogramJSON(t *testing.T) {
	h := Histogram{
		Name:       "block_io_latency",
		Unit:       "us",
		TotalCount: 10000,
		P50:        64,
		P90:        256,
		P99:        1024,
		P999:       4096,
		Max:        8192,
		Mean:       128.5,
		Buckets: []HistBucket{
			{Low: 0, High: 1, Count: 100},
			{Low: 2, High: 3, Count: 200},
			{Low: 4, High: 7, Count: 500},
			{Low: 8, High: 15, Count: 1500},
		},
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal histogram: %v", err)
	}

	var decoded Histogram
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal histogram: %v", err)
	}

	if decoded.P999 != 4096 {
		t.Errorf("p999 = %v, want 4096", decoded.P999)
	}
	if len(decoded.Buckets) != 4 {
		t.Errorf("buckets count = %d, want 4", len(decoded.Buckets))
	}
	// Verify bucket ordering is preserved
	if decoded.Buckets[0].Low != 0 || decoded.Buckets[0].High != 1 {
		t.Errorf("first bucket = {%d,%d}, want {0,1}", decoded.Buckets[0].Low, decoded.Buckets[0].High)
	}
}

func TestCPUDataFields(t *testing.T) {
	cpu := CPUData{
		UserPct:               45.2,
		SystemPct:             12.1,
		IOWaitPct:             3.5,
		IdlePct:               39.2,
		StealPct:              0.0,
		IRQPct:                0.0,
		SoftIRQPct:            0.0,
		ContextSwitchesPerSec: 15000,
		LoadAvg1:              3.14,
		LoadAvg5:              2.71,
		LoadAvg15:             1.99,
		NumCPUs:               8,
		SchedLatencyNS:        24000000,
		SchedMinGranularityNS: 3000000,
	}

	data, err := json.Marshal(cpu)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify field names in JSON
	jsonStr := string(data)
	for _, field := range []string{
		"user_pct", "system_pct", "iowait_pct", "idle_pct",
		"steal_pct", "irq_pct", "softirq_pct",
		"context_switches_per_sec", "load_avg_1", "load_avg_5", "load_avg_15",
		"num_cpus", "sched_latency_ns", "sched_min_granularity_ns",
	} {
		if !contains(jsonStr, field) {
			t.Errorf("JSON missing field: %s", field)
		}
	}
}

func TestMemoryDataFields(t *testing.T) {
	mem := MemoryData{
		TotalBytes:       34359738368,
		AvailableBytes:   17179869184,
		SwapUsedBytes:    1073741824,
		DirtyBytes:       5242880,
		MajorFaults:      42,
		MinorFaults:      1000000,
		Swappiness:       60,
		OvercommitMemory: 0,
		DirtyRatio:       20,
		PSISome10:        0.5,
		PSIFull10:        0.1,
	}

	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jsonStr := string(data)
	for _, field := range []string{
		"total_bytes", "available_bytes", "swap_used_bytes",
		"dirty_bytes", "major_faults", "minor_faults",
		"swappiness", "overcommit_memory", "dirty_ratio",
		"psi_some_10", "psi_full_10",
	} {
		if !contains(jsonStr, field) {
			t.Errorf("JSON missing field: %s", field)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && jsonContains(s, substr)
}

func jsonContains(s, field string) bool {
	return len(s) > 0 && len(field) > 0 && findField(s, field) >= 0
}

func findField(s, field string) int {
	target := `"` + field + `"`
	for i := 0; i+len(target) <= len(s); i++ {
		if s[i:i+len(target)] == target {
			return i
		}
	}
	return -1
}
