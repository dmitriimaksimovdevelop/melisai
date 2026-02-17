package output

import (
	"strings"
	"testing"

	"github.com/baikal/sysdiag/internal/model"
)

func TestGenerateAIPrompt(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{
			Hostname:      "prod-web-01",
			KernelVersion: "5.15.0",
			CPUs:          8,
			MemoryGB:      32,
			Profile:       "standard",
			Duration:      "30s",
		},
		Summary: model.Summary{
			HealthScore: 65,
			Anomalies: []model.Anomaly{
				{Severity: "critical", Category: "cpu", Message: "CPU at 97%"},
			},
			Resources: map[string]model.USEMetric{
				"cpu": {Utilization: 97, Saturation: 20},
			},
		},
		Categories: map[string][]model.Result{},
	}

	ctx := GenerateAIPrompt(report)

	if ctx == nil {
		t.Fatal("nil AI context")
	}
	if ctx.Prompt == "" {
		t.Error("empty prompt")
	}
	if !strings.Contains(ctx.Prompt, "prod-web-01") {
		t.Error("missing hostname")
	}
	if !strings.Contains(ctx.Prompt, "65/100") {
		t.Error("missing health score")
	}
	if !strings.Contains(ctx.Prompt, "CRITICAL") {
		t.Error("missing anomaly severity")
	}
	if ctx.Methodology == "" {
		t.Error("missing methodology")
	}
	if len(ctx.KnownPatterns) == 0 {
		t.Error("missing known patterns")
	}
}

func TestAIPromptWithStacks(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{Hostname: "test"},
		Categories: map[string][]model.Result{
			"stacktrace": {
				{Stacks: []model.StackTrace{{Stack: "main;work", Count: 100}}},
			},
		},
		Summary: model.Summary{Resources: map[string]model.USEMetric{}},
	}

	ctx := GenerateAIPrompt(report)
	if !strings.Contains(ctx.Prompt, "Stack traces are available") {
		t.Error("missing stack trace hint")
	}
}

func TestAIPromptWithHistograms(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{Hostname: "test"},
		Categories: map[string][]model.Result{
			"disk": {
				{Histograms: []model.Histogram{{Name: "biolatency"}}},
			},
		},
		Summary: model.Summary{Resources: map[string]model.USEMetric{}},
	}

	ctx := GenerateAIPrompt(report)
	if !strings.Contains(ctx.Prompt, "Latency histograms are available") {
		t.Error("missing histogram hint")
	}
}
