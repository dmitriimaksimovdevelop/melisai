package orchestrator

import (
	"testing"
	"time"
)

func TestGetProfile(t *testing.T) {
	tests := []struct {
		name     string
		expected time.Duration
	}{
		{"quick", 10 * time.Second},
		{"standard", 30 * time.Second},
		{"deep", 60 * time.Second},
		{"unknown", 30 * time.Second}, // falls back to standard
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := GetProfile(tt.name)
			if p.Duration != tt.expected {
				t.Errorf("Profile %q duration = %v, want %v", tt.name, p.Duration, tt.expected)
			}
		})
	}
}

func TestProfileCollectors(t *testing.T) {
	quick := GetProfile("quick")
	if len(quick.Collectors) < 5 {
		t.Errorf("quick profile should have at least 5 collectors, got %d", len(quick.Collectors))
	}

	standard := GetProfile("standard")
	if standard.Collectors[0] != "all" {
		t.Errorf("standard profile collectors should be [all], got %v", standard.Collectors)
	}

	deep := GetProfile("deep")
	if len(deep.Extra) == 0 {
		t.Error("deep profile should have extra collectors")
	}
}

func TestProfileFocusDuration(t *testing.T) {
	standard := GetProfile("standard")

	stacksDuration := standard.GetDuration("stacks")
	if stacksDuration != 15*time.Second {
		t.Errorf("stacks focus duration = %v, want 15s", stacksDuration)
	}

	// Unknown focus area falls back to profile duration
	unknownDuration := standard.GetDuration("unknown")
	if unknownDuration != 30*time.Second {
		t.Errorf("unknown focus duration = %v, want 30s (profile default)", unknownDuration)
	}
}

func TestProfileNames(t *testing.T) {
	names := ProfileNames()
	if len(names) != 3 {
		t.Errorf("ProfileNames count = %d, want 3", len(names))
	}

	expected := map[string]bool{"quick": true, "standard": true, "deep": true}
	for _, name := range names {
		if !expected[name] {
			t.Errorf("unexpected profile name: %s", name)
		}
	}
}
