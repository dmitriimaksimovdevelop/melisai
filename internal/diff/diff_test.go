package diff

import (
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

func TestCompareReports(t *testing.T) {
	baseline := &model.Report{
		Metadata: model.Metadata{Timestamp: "2024-01-01T00:00:00Z"},
		Summary: model.Summary{
			HealthScore: 80,
			Resources: map[string]model.USEMetric{
				"cpu":    {Utilization: 40, Saturation: 0},
				"memory": {Utilization: 50, Saturation: 5},
			},
		},
		Categories: map[string][]model.Result{},
	}

	current := &model.Report{
		Metadata: model.Metadata{Timestamp: "2024-01-02T00:00:00Z"},
		Summary: model.Summary{
			HealthScore: 60,
			Resources: map[string]model.USEMetric{
				"cpu":    {Utilization: 90, Saturation: 20},
				"memory": {Utilization: 55, Saturation: 5},
			},
		},
		Categories: map[string][]model.Result{},
	}

	diff := Compare(baseline, current)

	if diff.HealthDelta != -20 {
		t.Errorf("health delta = %d, want -20", diff.HealthDelta)
	}
	if diff.Regressions == 0 {
		t.Error("expected regressions for CPU increase")
	}
	if len(diff.Changes) == 0 {
		t.Fatal("no changes detected")
	}

	// CPU utilization should be a regression
	found := false
	for _, c := range diff.Changes {
		if c.Category == "cpu" && c.Metric == "utilization" {
			found = true
			if c.Direction != "regression" {
				t.Errorf("cpu util direction = %q, want regression", c.Direction)
			}
			if c.Significance != "high" {
				t.Errorf("cpu util significance = %q, want high (125%% change)", c.Significance)
			}
		}
	}
	if !found {
		t.Error("missing CPU utilization change")
	}
}

func TestCompareIdentical(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{Timestamp: "2024-01-01T00:00:00Z"},
		Summary: model.Summary{
			HealthScore: 90,
			Resources: map[string]model.USEMetric{
				"cpu": {Utilization: 50, Saturation: 0},
			},
		},
		Categories: map[string][]model.Result{},
	}

	diff := Compare(report, report)
	if diff.HealthDelta != 0 {
		t.Errorf("health delta = %d, want 0", diff.HealthDelta)
	}
	if diff.Regressions != 0 {
		t.Errorf("regressions = %d, want 0 for identical reports", diff.Regressions)
	}
}

func TestCompareImprovement(t *testing.T) {
	baseline := &model.Report{
		Metadata: model.Metadata{Timestamp: "before"},
		Summary: model.Summary{
			HealthScore: 50,
			Resources: map[string]model.USEMetric{
				"cpu": {Utilization: 90, Saturation: 30, Errors: 100},
			},
		},
		Categories: map[string][]model.Result{},
	}

	current := &model.Report{
		Metadata: model.Metadata{Timestamp: "after"},
		Summary: model.Summary{
			HealthScore: 95,
			Resources: map[string]model.USEMetric{
				"cpu": {Utilization: 30, Saturation: 0, Errors: 0},
			},
		},
		Categories: map[string][]model.Result{},
	}

	diff := Compare(baseline, current)
	if diff.HealthDelta <= 0 {
		t.Errorf("health delta = %d, want positive", diff.HealthDelta)
	}
	if diff.Improvements == 0 {
		t.Error("expected improvements")
	}
}

func TestFormatDiff(t *testing.T) {
	diff := &DiffReport{
		Baseline:     "2024-01-01",
		Current:      "2024-01-02",
		HealthDelta:  -15,
		Regressions:  2,
		Improvements: 1,
		Changes: []MetricChange{
			{Category: "cpu", Metric: "utilization", OldValue: 40, NewValue: 90, DeltaPct: 125, Direction: "regression", Significance: "high"},
			{Category: "memory", Metric: "saturation", OldValue: 5, NewValue: 30, DeltaPct: 500, Direction: "regression", Significance: "high"},
			{Category: "disk", Metric: "utilization", OldValue: 80, NewValue: 40, DeltaPct: -50, Direction: "improvement", Significance: "high"},
		},
	}

	output := FormatDiff(diff)
	if output == "" {
		t.Fatal("empty diff output")
	}
	if len(output) < 50 {
		t.Error("diff output too short")
	}
}
