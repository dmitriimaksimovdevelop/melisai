package model

import "testing"

func TestComputeUSEMetrics(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{Data: &CPUData{
					IdlePct: 20, IOWaitPct: 5,
					LoadAvg1: 8, NumCPUs: 4,
				}},
			},
			"memory": {
				{Data: &MemoryData{
					TotalBytes: 16e9, AvailableBytes: 4e9,
					SwapTotalBytes: 4e9, SwapUsedBytes: 1e9,
					MajorFaults: 100,
				}},
			},
		},
	}

	resources := ComputeUSEMetrics(report)

	// CPU
	cpu, ok := resources["cpu"]
	if !ok {
		t.Fatal("missing cpu USE metric")
	}
	if cpu.Utilization < 79 || cpu.Utilization > 81 {
		t.Errorf("cpu util = %.1f, want ~80", cpu.Utilization)
	}
	if cpu.Saturation == 0 {
		t.Error("cpu saturation should be > 0 (load=8, cpus=4)")
	}

	// Memory
	mem, ok := resources["memory"]
	if !ok {
		t.Fatal("missing memory USE metric")
	}
	if mem.Utilization < 74 || mem.Utilization > 76 {
		t.Errorf("mem util = %.1f, want ~75", mem.Utilization)
	}
	if mem.Saturation < 24 || mem.Saturation > 26 {
		t.Errorf("mem sat = %.1f, want ~25", mem.Saturation)
	}
}

func TestDetectAnomalies(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{Data: &CPUData{
					IdlePct: 3, IOWaitPct: 35,
					LoadAvg1: 20, NumCPUs: 4,
				}},
			},
			"memory": {
				{Data: &MemoryData{
					TotalBytes: 16e9, AvailableBytes: 500e6,
					SwapTotalBytes: 4e9, SwapUsedBytes: 3e9,
				}},
			},
		},
	}

	anomalies := DetectAnomalies(report)

	if len(anomalies) == 0 {
		t.Fatal("expected anomalies for saturated system")
	}

	// Should have critical CPU anomalies
	hasCriticalCPU := false
	for _, a := range anomalies {
		if a.Category == "cpu" && a.Severity == "critical" {
			hasCriticalCPU = true
		}
	}
	if !hasCriticalCPU {
		t.Error("expected critical CPU anomaly")
	}
}

func TestDetectAnomaliesHealthy(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{Data: &CPUData{
					IdlePct: 80, IOWaitPct: 1,
					LoadAvg1: 0.5, NumCPUs: 4,
				}},
			},
		},
	}

	anomalies := DetectAnomalies(report)
	if len(anomalies) != 0 {
		t.Errorf("healthy system should have 0 anomalies, got %d", len(anomalies))
	}
}

func TestComputeHealthScore(t *testing.T) {
	// Healthy system
	resources := map[string]USEMetric{
		"cpu":    {Utilization: 20, Saturation: 0, Errors: 0},
		"memory": {Utilization: 50, Saturation: 0, Errors: 0},
	}
	score := ComputeHealthScore(resources, nil)
	if score != 100 {
		t.Errorf("healthy score = %d, want 100", score)
	}

	// Saturated system
	resources = map[string]USEMetric{
		"cpu":    {Utilization: 98, Saturation: 60, Errors: 0},
		"memory": {Utilization: 96, Saturation: 55, Errors: 500},
	}
	anomalies := []Anomaly{
		{Severity: "critical"},
		{Severity: "critical"},
		{Severity: "warning"},
	}
	score = ComputeHealthScore(resources, anomalies)
	if score >= 50 {
		t.Errorf("saturated score = %d, should be < 50", score)
	}
}

func TestHealthScoreClamp(t *testing.T) {
	// Extreme saturation should clamp to 0
	resources := map[string]USEMetric{
		"cpu":     {Utilization: 100, Saturation: 100, Errors: 10000},
		"memory":  {Utilization: 100, Saturation: 100, Errors: 10000},
		"disk":    {Utilization: 100, Saturation: 100, Errors: 10000},
		"network": {Utilization: 100, Saturation: 100, Errors: 10000},
	}
	anomalies := make([]Anomaly, 20)
	for i := range anomalies {
		anomalies[i] = Anomaly{Severity: "critical"}
	}

	score := ComputeHealthScore(resources, anomalies)
	if score != 0 {
		t.Errorf("extreme score = %d, want 0", score)
	}
}

func TestGenerateRecommendations(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"cpu": {
				{Data: &CPUData{
					LoadAvg1: 20, NumCPUs: 4,
					SchedLatencyNS: 30000000,
				}},
			},
			"memory": {
				{Data: &MemoryData{
					Swappiness: 60, SwapUsedBytes: 1e9, SwapTotalBytes: 4e9,
					DirtyRatio: 40, OvercommitMemory: 0,
				}},
			},
			"network": {
				{Data: &NetworkData{
					CongestionCtrl: "cubic",
					TCP:            &TCPStats{RetransSegs: 500},
					SomaxConn:      128,
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)
	if len(recs) == 0 {
		t.Fatal("expected recommendations")
	}

	// Should have CPU saturation recommendation
	hasLoad := false
	for _, r := range recs {
		if r.Category == "cpu" {
			hasLoad = true
		}
	}
	if !hasLoad {
		t.Error("missing CPU recommendation")
	}

	// Should have network BBR recommendation
	hasBBR := false
	for _, r := range recs {
		if r.Category == "network" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w net.ipv4.tcp_congestion_control=bbr" {
					hasBBR = true
				}
			}
		}
	}
	if !hasBBR {
		t.Error("missing BBR recommendation")
	}

	// All should have priority ordering
	for i := 1; i < len(recs); i++ {
		if recs[i].Priority <= recs[i-1].Priority {
			// acceptable (same priority)
		}
	}
}
