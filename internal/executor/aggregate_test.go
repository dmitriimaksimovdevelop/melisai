package executor

import (
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

func TestAggregateByField(t *testing.T) {
	events := []model.Event{
		{Details: map[string]interface{}{"raddr": "10.0.0.1"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.2"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.1"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.1"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.3"}},
	}

	result := AggregateByField(events, "raddr", 2)

	if result.TotalCount != 5 {
		t.Errorf("total = %d, want 5", result.TotalCount)
	}
	if len(result.TopByCount) != 2 {
		t.Errorf("top-N count = %d, want 2", len(result.TopByCount))
	}
	if result.TopByCount[0].Key != "10.0.0.1" {
		t.Errorf("top key = %q, want 10.0.0.1", result.TopByCount[0].Key)
	}
	if result.TopByCount[0].Count != 3 {
		t.Errorf("top count = %d, want 3", result.TopByCount[0].Count)
	}
}

func TestAggregateRetransmits(t *testing.T) {
	events := []model.Event{
		{Details: map[string]interface{}{"raddr": "10.0.0.1"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.1"}},
		{Details: map[string]interface{}{"raddr": "10.0.0.2"}},
	}

	result := AggregateRetransmits(events)
	if result.TotalCount != 3 {
		t.Errorf("total = %d, want 3", result.TotalCount)
	}
}

func TestAggregateEmptyEvents(t *testing.T) {
	result := AggregateByField(nil, "raddr", 10)
	if result.TotalCount != 0 {
		t.Errorf("total = %d, want 0", result.TotalCount)
	}
	if len(result.TopByCount) != 0 {
		t.Errorf("entries = %d, want 0", len(result.TopByCount))
	}
}

func TestAggregateConnections(t *testing.T) {
	events := []model.Event{
		{Details: map[string]interface{}{"daddr": "10.0.0.1", "lat(ms)": 10.0}},
		{Details: map[string]interface{}{"daddr": "10.0.0.1", "lat(ms)": 20.0}},
		{Details: map[string]interface{}{"daddr": "10.0.0.2", "lat(ms)": 5.0}},
	}

	result := AggregateConnections(events)
	if result.TotalCount != 3 {
		t.Errorf("total = %d, want 3", result.TotalCount)
	}
	if len(result.TopByCount) == 0 {
		t.Fatal("no entries")
	}
	// Top should be 10.0.0.1 with 2 connections
	if result.TopByCount[0].Key != "10.0.0.1" {
		t.Errorf("top = %q, want 10.0.0.1", result.TopByCount[0].Key)
	}
}
