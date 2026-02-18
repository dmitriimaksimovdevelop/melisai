package model

import "testing"

// TestHealthScoreWithContainerResources verifies that container_cpu and
// container_memory resources contribute to the health score with weight 1.2.
func TestHealthScoreWithContainerResources(t *testing.T) {
	// Base case: only container resources, high utilization (>=95) triggers
	// a deduction of int(15 * 1.2) = 18 per resource.
	resources := map[string]USEMetric{
		"container_cpu":    {Utilization: 98, Saturation: 0, Errors: 0},
		"container_memory": {Utilization: 97, Saturation: 0, Errors: 0},
	}

	score := ComputeHealthScore(resources, nil)

	// Expected: 100 - 18 (container_cpu util>=95) - 18 (container_memory util>=95) = 64
	expected := 100 - int(15*1.2) - int(15*1.2)
	if score != expected {
		t.Errorf("score = %d, want %d (container resources with weight 1.2)", score, expected)
	}

	// Verify it differs from weight 1.0 (what disk/network would give).
	// With weight 1.0, deduction would be 15*1.0=15 per resource -> 100-15-15=70
	scoreIfWeight1 := 100 - int(15*1.0) - int(15*1.0)
	if score == scoreIfWeight1 {
		t.Error("container resources should use weight 1.2, not 1.0")
	}
}

// TestResourceWeight verifies the weight returned for each known resource type.
func TestResourceWeight(t *testing.T) {
	tests := []struct {
		resource string
		want     float64
	}{
		{"cpu", 1.5},
		{"memory", 1.5},
		{"disk", 1.0},
		{"network", 1.0},
		{"container_cpu", 1.2},
		{"container_memory", 1.2},
		{"unknown_resource", 0.5},
		{"", 0.5},
	}

	for _, tc := range tests {
		t.Run(tc.resource, func(t *testing.T) {
			// resourceWeight is unexported, but we can test it indirectly
			// by computing health score with a known USE metric configuration.
			// Utilization=98 (>=95) -> deduction = int(15 * weight).
			resources := map[string]USEMetric{
				tc.resource: {Utilization: 98, Saturation: 0, Errors: 0},
			}
			score := ComputeHealthScore(resources, nil)
			expectedDeduction := int(15 * tc.want)
			expectedScore := 100 - expectedDeduction

			if score != expectedScore {
				t.Errorf("resource %q: score = %d, want %d (weight=%.1f, deduction=%d)",
					tc.resource, score, expectedScore, tc.want, expectedDeduction)
			}
		})
	}
}

// TestHealthScoreMixedAnomalies verifies that 3 warnings + 1 critical produce
// the expected deduction: 100 - 3*5 - 1*10 = 75.
func TestHealthScoreMixedAnomalies(t *testing.T) {
	// No USE metric deductions: empty resources map.
	resources := map[string]USEMetric{}

	anomalies := []Anomaly{
		{Severity: "warning", Category: "cpu", Metric: "cpu_utilization"},
		{Severity: "warning", Category: "memory", Metric: "swap_usage"},
		{Severity: "warning", Category: "network", Metric: "tcp_retransmits"},
		{Severity: "critical", Category: "disk", Metric: "disk_avg_latency"},
	}

	score := ComputeHealthScore(resources, anomalies)

	// 3 warnings * 5 = 15, 1 critical * 10 = 10, total deduction = 25
	expected := 100 - 3*5 - 1*10
	if score != expected {
		t.Errorf("score = %d, want %d (3 warnings + 1 critical)", score, expected)
	}
}
