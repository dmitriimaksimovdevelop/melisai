package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/collector"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// mockCollector implements collector.Collector for testing.
type mockCollector struct {
	name     string
	category string
	tier     int
	data     interface{}
	err      error
	delay    time.Duration
}

func (m *mockCollector) Name() string     { return m.name }
func (m *mockCollector) Category() string { return m.category }
func (m *mockCollector) Available() collector.Availability {
	return collector.Availability{Tier: m.tier}
}

func (m *mockCollector) Collect(ctx context.Context, cfg collector.CollectConfig) (*model.Result, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &model.Result{
		Collector: m.name,
		Category:  m.category,
		Tier:      m.tier,
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Data:      m.data,
	}, nil
}

func TestOrchestratorRunBasic(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{
			name:     "test_cpu",
			category: "cpu",
			tier:     1,
			data:     &model.CPUData{UserPct: 50},
		},
		&mockCollector{
			name:     "test_mem",
			category: "memory",
			tier:     1,
			data:     &model.MemoryData{TotalBytes: 1024},
		},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	ctx := context.Background()

	report, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Metadata.Tool != "melisai" {
		t.Errorf("tool = %q, want melisai", report.Metadata.Tool)
	}

	if len(report.Categories) != 2 {
		t.Errorf("categories count = %d, want 2", len(report.Categories))
	}

	cpuResults, ok := report.Categories["cpu"]
	if !ok {
		t.Fatal("missing cpu category")
	}
	if len(cpuResults) != 1 {
		t.Errorf("cpu results count = %d, want 1", len(cpuResults))
	}
}

func TestOrchestratorSkipsUnavailable(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{
			name:     "available",
			category: "cpu",
			tier:     1,
			data:     &model.CPUData{},
		},
		&mockCollector{
			name:     "unavailable",
			category: "cpu",
			tier:     0, // unavailable
		},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Only the available collector should be in results
	cpuResults := report.Categories["cpu"]
	if len(cpuResults) != 1 {
		t.Errorf("cpu results = %d, want 1 (unavailable should be skipped)", len(cpuResults))
	}
}

func TestOrchestratorTimeout(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{
			name:     "slow",
			category: "cpu",
			tier:     1,
			delay:    5 * time.Second, // slower than timeout
		},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	// Set a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	orch := New(collectors, cfg)
	report, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have results with error
	if cpuResults, ok := report.Categories["cpu"]; ok {
		for _, r := range cpuResults {
			if len(r.Errors) == 0 {
				t.Error("expected errors for timed-out collector")
			}
		}
	}
}
