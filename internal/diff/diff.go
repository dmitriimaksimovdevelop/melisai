// Package diff compares two sysdiag reports and highlights regressions/improvements.
package diff

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/baikal/sysdiag/internal/model"
)

// DiffReport contains the comparison between two reports.
type DiffReport struct {
	Baseline     string         `json:"baseline"`
	Current      string         `json:"current"`
	TimeDelta    string         `json:"time_delta"`
	Changes      []MetricChange `json:"changes"`
	Regressions  int            `json:"regressions"`
	Improvements int            `json:"improvements"`
	HealthDelta  int            `json:"health_delta"` // positive = improved
}

// MetricChange represents a single metric difference between reports.
type MetricChange struct {
	Category     string  `json:"category"`
	Metric       string  `json:"metric"`
	OldValue     float64 `json:"old_value"`
	NewValue     float64 `json:"new_value"`
	Delta        float64 `json:"delta"`
	DeltaPct     float64 `json:"delta_pct"`
	Direction    string  `json:"direction"`    // "regression", "improvement", "unchanged"
	Significance string  `json:"significance"` // "high", "medium", "low"
}

// LoadReport reads and parses a JSON report file.
func LoadReport(path string) (*model.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var report model.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &report, nil
}

// Compare computes differences between two reports.
func Compare(baseline, current *model.Report) *DiffReport {
	diff := &DiffReport{
		Baseline:    baseline.Metadata.Timestamp,
		Current:     current.Metadata.Timestamp,
		HealthDelta: current.Summary.HealthScore - baseline.Summary.HealthScore,
	}

	// Compare USE metrics
	for resource, newMetric := range current.Summary.Resources {
		if oldMetric, ok := baseline.Summary.Resources[resource]; ok {
			addChange(diff, resource, "utilization", oldMetric.Utilization, newMetric.Utilization, true)
			addChange(diff, resource, "saturation", oldMetric.Saturation, newMetric.Saturation, true)
			addChange(diff, resource, "errors", float64(oldMetric.Errors), float64(newMetric.Errors), true)
		}
	}

	// Compare CPU-specific metrics
	oldCPU := extractCPU(baseline)
	newCPU := extractCPU(current)
	if oldCPU != nil && newCPU != nil {
		addChange(diff, "cpu", "user_pct", oldCPU.UserPct, newCPU.UserPct, true)
		addChange(diff, "cpu", "system_pct", oldCPU.SystemPct, newCPU.SystemPct, true)
		addChange(diff, "cpu", "iowait_pct", oldCPU.IOWaitPct, newCPU.IOWaitPct, true)
		addChange(diff, "cpu", "idle_pct", oldCPU.IdlePct, newCPU.IdlePct, false) // higher idle = better
		addChange(diff, "cpu", "load_avg_1", oldCPU.LoadAvg1, newCPU.LoadAvg1, true)
		addChange(diff, "cpu", "context_switches_per_sec",
			float64(oldCPU.ContextSwitchesPerSec), float64(newCPU.ContextSwitchesPerSec), true)
	}

	// Compare memory metrics
	oldMem := extractMemory(baseline)
	newMem := extractMemory(current)
	if oldMem != nil && newMem != nil {
		if oldMem.TotalBytes > 0 && newMem.TotalBytes > 0 {
			oldUtilPct := float64(oldMem.TotalBytes-oldMem.AvailableBytes) / float64(oldMem.TotalBytes) * 100
			newUtilPct := float64(newMem.TotalBytes-newMem.AvailableBytes) / float64(newMem.TotalBytes) * 100
			addChange(diff, "memory", "utilization_pct", oldUtilPct, newUtilPct, true)
		}
		addChange(diff, "memory", "major_faults", float64(oldMem.MajorFaults), float64(newMem.MajorFaults), true)
	}

	// Compare histogram p99 values
	compareHistograms(diff, baseline, current)

	// Tally regressions vs improvements
	for _, c := range diff.Changes {
		switch c.Direction {
		case "regression":
			diff.Regressions++
		case "improvement":
			diff.Improvements++
		}
	}

	return diff
}

func addChange(diff *DiffReport, category, metric string, oldVal, newVal float64, higherIsWorse bool) {
	delta := newVal - oldVal
	deltaPct := 0.0
	if oldVal != 0 {
		deltaPct = (delta / math.Abs(oldVal)) * 100
	}

	// Skip negligible changes
	if math.Abs(deltaPct) < 1.0 && math.Abs(delta) < 0.1 {
		return
	}

	direction := "unchanged"
	if higherIsWorse {
		if deltaPct > 5 {
			direction = "regression"
		} else if deltaPct < -5 {
			direction = "improvement"
		}
	} else {
		if deltaPct < -5 {
			direction = "regression"
		} else if deltaPct > 5 {
			direction = "improvement"
		}
	}

	significance := "low"
	absPct := math.Abs(deltaPct)
	if absPct >= 50 {
		significance = "high"
	} else if absPct >= 20 {
		significance = "medium"
	}

	diff.Changes = append(diff.Changes, MetricChange{
		Category:     category,
		Metric:       metric,
		OldValue:     oldVal,
		NewValue:     newVal,
		Delta:        delta,
		DeltaPct:     deltaPct,
		Direction:    direction,
		Significance: significance,
	})
}

func compareHistograms(diff *DiffReport, baseline, current *model.Report) {
	oldHists := collectHistograms(baseline)
	newHists := collectHistograms(current)

	for name, newH := range newHists {
		if oldH, ok := oldHists[name]; ok {
			addChange(diff, "histogram", name+"_p50", oldH.P50, newH.P50, true)
			addChange(diff, "histogram", name+"_p99", oldH.P99, newH.P99, true)
		}
	}
}

func collectHistograms(report *model.Report) map[string]model.Histogram {
	hists := make(map[string]model.Histogram)
	for _, results := range report.Categories {
		for _, r := range results {
			for _, h := range r.Histograms {
				hists[h.Name] = h
			}
		}
	}
	return hists
}

func extractCPU(report *model.Report) *model.CPUData {
	if results, ok := report.Categories["cpu"]; ok {
		for _, r := range results {
			if cpu, ok := r.Data.(*model.CPUData); ok {
				return cpu
			}
		}
	}
	return nil
}

func extractMemory(report *model.Report) *model.MemoryData {
	if results, ok := report.Categories["memory"]; ok {
		for _, r := range results {
			if mem, ok := r.Data.(*model.MemoryData); ok {
				return mem
			}
		}
	}
	return nil
}

// FormatDiff returns a human-readable diff summary.
func FormatDiff(d *DiffReport) string {
	var sb strings.Builder

	sb.WriteString("=== Report Diff ===\n")
	sb.WriteString(fmt.Sprintf("Baseline: %s\n", d.Baseline))
	sb.WriteString(fmt.Sprintf("Current:  %s\n\n", d.Current))

	symbol := "→"
	if d.HealthDelta > 0 {
		symbol = "↑"
	} else if d.HealthDelta < 0 {
		symbol = "↓"
	}
	sb.WriteString(fmt.Sprintf("Health Score: %+d %s\n", d.HealthDelta, symbol))
	sb.WriteString(fmt.Sprintf("Regressions: %d, Improvements: %d\n\n", d.Regressions, d.Improvements))

	// Show regressions first
	if d.Regressions > 0 {
		sb.WriteString("⚠ Regressions:\n")
		for _, c := range d.Changes {
			if c.Direction == "regression" {
				sb.WriteString(fmt.Sprintf("  [%s] %s/%s: %.2f → %.2f (%+.1f%%)\n",
					strings.ToUpper(c.Significance), c.Category, c.Metric,
					c.OldValue, c.NewValue, c.DeltaPct))
			}
		}
		sb.WriteString("\n")
	}

	if d.Improvements > 0 {
		sb.WriteString("✓ Improvements:\n")
		for _, c := range d.Changes {
			if c.Direction == "improvement" {
				sb.WriteString(fmt.Sprintf("  [%s] %s/%s: %.2f → %.2f (%+.1f%%)\n",
					strings.ToUpper(c.Significance), c.Category, c.Metric,
					c.OldValue, c.NewValue, c.DeltaPct))
			}
		}
	}

	return sb.String()
}
