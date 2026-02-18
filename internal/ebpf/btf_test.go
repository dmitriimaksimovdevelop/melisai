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

func TestNativePrograms(t *testing.T) {
	if len(NativePrograms) < 1 {
		t.Errorf("expected at least 1 native program, got %d", len(NativePrograms))
	}

	// Verify all have names and categories
	for _, prog := range NativePrograms {
		if prog.Name == "" {
			t.Error("program missing name")
		}
		if prog.Category == "" {
			t.Errorf("program %s missing category", prog.Name)
		}
		if prog.ObjectFile == "" {
			t.Errorf("program %s missing object file", prog.Name)
		}
	}
}
