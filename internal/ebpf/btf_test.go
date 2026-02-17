package ebpf

import "testing"

func TestParseKernelVersion(t *testing.T) {
	tests := []struct {
		input     string
		wantMajor int
		wantMinor int
	}{
		{"6.1.0-generic", 6, 1},
		{"5.15.0-91-generic", 5, 15},
		{"5.8.0", 5, 8},
		{"4.15.0-213-generic", 4, 15},
		{"6.6.9+rpt-rpi-v8", 6, 6},
		{"", 0, 0},
		{"bad", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			major, minor := parseKernelVersion(tt.input)
			if major != tt.wantMajor || minor != tt.wantMinor {
				t.Errorf("parseKernelVersion(%q) = (%d, %d), want (%d, %d)",
					tt.input, major, minor, tt.wantMajor, tt.wantMinor)
			}
		})
	}
}

func TestDetectBTF(t *testing.T) {
	// This test runs on any OS — just verifies it doesn't panic
	info := DetectBTF()
	if info == nil {
		t.Fatal("DetectBTF returned nil")
	}

	// On macOS, BTF won't be available
	t.Logf("BTF available: %v, kernel: %s, CO-RE: %v",
		info.Available, info.KernelVersion, info.CORESupport)
}

func TestCapabilityLevel(t *testing.T) {
	tests := []struct {
		name string
		caps map[string]bool
		want int
	}{
		{
			"tier 3 - full",
			map[string]bool{
				"btf_vmlinux":           true,
				"bpf_syscall":           true,
				"config_bpf":            true,
				"config_bpf_syscall":    true,
				"config_debug_info_btf": true,
			},
			3,
		},
		{
			"tier 2 - bcc only",
			map[string]bool{
				"bpf_syscall": true,
				"config_bpf":  true,
			},
			2,
		},
		{
			"tier 1 - procfs only",
			map[string]bool{},
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := CapabilityLevel(tt.caps)
			if level != tt.want {
				t.Errorf("CapabilityLevel = %d, want %d", level, tt.want)
			}
		})
	}
}

func TestDecideTier(t *testing.T) {
	loader := NewLoader(false)

	// On macOS, should always fall back
	decision := DecideTier("biolatency", loader)
	if decision.UseTier3 {
		t.Log("Tier 3 available (Linux with BTF)")
	} else {
		if decision.Reason == "" {
			t.Error("expected non-empty fallback reason")
		}
	}
}

func TestNativePrograms(t *testing.T) {
	if len(NativePrograms) != 5 {
		t.Errorf("expected 5 native programs, got %d", len(NativePrograms))
	}

	// Verify all have names and categories
	for _, prog := range NativePrograms {
		if prog.Name == "" {
			t.Error("program missing name")
		}
		if prog.Category == "" {
			t.Errorf("program %s missing category", prog.Name)
		}
		if len(prog.MapNames) == 0 {
			t.Errorf("program %s has no maps", prog.Name)
		}
	}
}

func TestFormatCapabilities(t *testing.T) {
	caps := map[string]bool{
		"bpf_syscall": true,
		"config_bpf":  true,
		"kprobes":     false,
	}

	output := FormatCapabilities(caps)
	if output == "" {
		t.Error("empty capabilities output")
	}
	if !containsString(output, "Tier") {
		t.Error("missing tier level")
	}
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
