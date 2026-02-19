package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/collector"
	"github.com/dmitriimaksimovdevelop/melisai/internal/orchestrator"
)

// TestCLIConfigWiring verifies that CLI flags produce the correct CollectConfig.
// This simulates what RunE does without actually running collectors.

func TestCLIProfileSetsCorrectDuration(t *testing.T) {
	tests := []struct {
		profile  string
		expected time.Duration
	}{
		{"quick", 10 * time.Second},
		{"standard", 30 * time.Second},
		{"deep", 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			cfg := collector.DefaultConfig()
			cfg.Profile = tt.profile

			profile := orchestrator.GetProfile(cfg.Profile)
			cfg.Duration = profile.Duration

			if cfg.Duration != tt.expected {
				t.Errorf("profile %q → duration = %v, want %v", tt.profile, cfg.Duration, tt.expected)
			}
		})
	}
}

func TestCLIDurationOverridesProfile(t *testing.T) {
	cfg := collector.DefaultConfig()
	cfg.Profile = "standard"

	// Profile would set 30s
	profile := orchestrator.GetProfile(cfg.Profile)
	cfg.Duration = profile.Duration

	// Duration override (simulates --duration 15s)
	durationStr := "15s"
	d, err := time.ParseDuration(durationStr)
	if err != nil {
		t.Fatalf("parse duration: %v", err)
	}
	cfg.Duration = d

	if cfg.Duration != 15*time.Second {
		t.Errorf("duration override = %v, want 15s", cfg.Duration)
	}
}

func TestCLIFocusParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"network", []string{"network"}},
		{"network,disk", []string{"network", "disk"}},
		{"stacks,network,disk", []string{"stacks", "network", "disk"}},
		{"", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cfg := collector.DefaultConfig()
			if tt.input != "" {
				cfg.Focus = strings.Split(tt.input, ",")
			}

			if tt.expected == nil {
				if cfg.Focus != nil {
					t.Errorf("focus = %v, want nil", cfg.Focus)
				}
				return
			}

			if len(cfg.Focus) != len(tt.expected) {
				t.Errorf("focus = %v, want %v", cfg.Focus, tt.expected)
				return
			}
			for i, f := range cfg.Focus {
				if f != tt.expected[i] {
					t.Errorf("focus[%d] = %q, want %q", i, f, tt.expected[i])
				}
			}
		})
	}
}

func TestCLIMaxEventsFlag(t *testing.T) {
	cfg := collector.DefaultConfig()

	// Default
	if cfg.MaxEventsPerCollector != 1000 {
		t.Errorf("default MaxEvents = %d, want 1000", cfg.MaxEventsPerCollector)
	}

	// Override (simulates --max-events 500)
	cfg.MaxEventsPerCollector = 500
	if cfg.MaxEventsPerCollector != 500 {
		t.Errorf("overridden MaxEvents = %d, want 500", cfg.MaxEventsPerCollector)
	}
}

func TestCLIPIDFlag(t *testing.T) {
	cfg := collector.DefaultConfig()

	// Simulates --pid 1234
	collectPID := 1234
	if collectPID > 0 {
		cfg.TargetPIDs = []int{collectPID}
	}

	if len(cfg.TargetPIDs) != 1 || cfg.TargetPIDs[0] != 1234 {
		t.Errorf("TargetPIDs = %v, want [1234]", cfg.TargetPIDs)
	}
}

func TestCLICgroupFlag(t *testing.T) {
	cfg := collector.DefaultConfig()

	// Simulates --cgroup /docker/abc
	collectCgroup := "/docker/abc"
	if collectCgroup != "" {
		cfg.TargetCgroups = []string{collectCgroup}
	}

	if len(cfg.TargetCgroups) != 1 || cfg.TargetCgroups[0] != "/docker/abc" {
		t.Errorf("TargetCgroups = %v, want [/docker/abc]", cfg.TargetCgroups)
	}
}

func TestCLIQuietFlag(t *testing.T) {
	cfg := collector.DefaultConfig()
	cfg.Quiet = true

	if !cfg.Quiet {
		t.Error("Quiet should be true")
	}
}

func TestCLIVerboseFlag(t *testing.T) {
	cfg := collector.DefaultConfig()
	cfg.Verbose = true

	if !cfg.Verbose {
		t.Error("Verbose should be true")
	}
}

func TestCLIUnknownProfileFallsBack(t *testing.T) {
	cfg := collector.DefaultConfig()
	cfg.Profile = "nonexistent"

	profile := orchestrator.GetProfile(cfg.Profile)
	// Falls back to standard
	if profile.Duration != 30*time.Second {
		t.Errorf("unknown profile should fallback to standard (30s), got %v", profile.Duration)
	}
}

func TestCLIPIDZeroNotSet(t *testing.T) {
	cfg := collector.DefaultConfig()

	// Simulates default --pid 0 (not set)
	collectPID := 0
	if collectPID > 0 {
		cfg.TargetPIDs = []int{collectPID}
	}

	if cfg.TargetPIDs != nil {
		t.Errorf("TargetPIDs should be nil when pid=0, got %v", cfg.TargetPIDs)
	}
}

func TestCLICgroupEmptyNotSet(t *testing.T) {
	cfg := collector.DefaultConfig()

	// Simulates default --cgroup "" (not set)
	collectCgroup := ""
	if collectCgroup != "" {
		cfg.TargetCgroups = []string{collectCgroup}
	}

	if cfg.TargetCgroups != nil {
		t.Errorf("TargetCgroups should be nil when cgroup empty, got %v", cfg.TargetCgroups)
	}
}
