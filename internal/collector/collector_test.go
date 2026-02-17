package collector

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Duration != 30*time.Second {
		t.Errorf("Duration = %v, want 30s", cfg.Duration)
	}
	if cfg.SampleInterval != 1*time.Second {
		t.Errorf("SampleInterval = %v, want 1s", cfg.SampleInterval)
	}
	if cfg.Profile != "standard" {
		t.Errorf("Profile = %q, want standard", cfg.Profile)
	}
	if cfg.MaxEventsPerCollector != 1000 {
		t.Errorf("MaxEventsPerCollector = %d, want 1000", cfg.MaxEventsPerCollector)
	}
	if cfg.ProcRoot != "/proc" {
		t.Errorf("ProcRoot = %q, want /proc", cfg.ProcRoot)
	}
	if cfg.SysRoot != "/sys" {
		t.Errorf("SysRoot = %q, want /sys", cfg.SysRoot)
	}
}

func TestAvailability(t *testing.T) {
	tests := []struct {
		name string
		tier int
		want bool
	}{
		{"tier 1 available", 1, true},
		{"tier 2 available", 2, true},
		{"tier 3 available", 3, true},
		{"unavailable", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Availability{Tier: tt.tier, Reason: "test"}
			got := a.Tier > 0
			if got != tt.want {
				t.Errorf("Tier %d: available = %v, want %v", tt.tier, got, tt.want)
			}
		})
	}
}
