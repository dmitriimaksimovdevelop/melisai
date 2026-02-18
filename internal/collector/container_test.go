package collector

import (
	"strings"
	"testing"
)

// ---------- Category (regression for bug #2) ----------

// TestContainerCategory verifies that ContainerCollector.Category() returns
// "container". This is a regression test for bug #2 where the category was
// incorrectly returning a different value.
func TestContainerCategory(t *testing.T) {
	c := NewContainerCollector("/proc", "/sys")
	got := c.Category()
	if got != "container" {
		t.Errorf("Category() = %q, want %q (regression: bug #2)", got, "container")
	}
}

func TestContainerName(t *testing.T) {
	c := NewContainerCollector("/proc", "/sys")
	if got := c.Name(); got != "container_info" {
		t.Errorf("Name() = %q, want %q", got, "container_info")
	}
}

func TestContainerAvailability(t *testing.T) {
	c := NewContainerCollector("/proc", "/sys")
	avail := c.Available()
	if avail.Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", avail.Tier)
	}
}

// ---------- detectRuntime ----------

// TestDetectRuntimeNone verifies that in a non-container environment
// (no /.dockerenv, no kubernetes service account, no container patterns
// in /proc/1/cgroup) the runtime is reported as "none".
func TestDetectRuntimeNone(t *testing.T) {
	// testdata/proc/1/cgroup contains "0::/" which has no container patterns.
	c := NewContainerCollector("../../testdata/proc", "../../testdata/sys")
	got := c.detectRuntime()
	if got != "none" {
		t.Errorf("detectRuntime() = %q, want %q", got, "none")
	}
}

func TestDetectRuntimeMissingCgroup(t *testing.T) {
	// Point at a non-existent procRoot so /proc/1/cgroup cannot be read.
	c := NewContainerCollector("/nonexistent/proc", "/nonexistent/sys")
	got := c.detectRuntime()
	// Without /.dockerenv or kubernetes service account on the test host,
	// and with an unreadable cgroup file, expect "none".
	if got != "none" {
		t.Errorf("detectRuntime() with missing files = %q, want %q", got, "none")
	}
}

// ---------- detectCgroupVersion ----------

// TestDetectCgroupVersionNone verifies that when neither cgroup v1 nor v2
// directories exist, the version is 0.
func TestDetectCgroupVersionNone(t *testing.T) {
	// Point sysRoot at a directory with no fs/cgroup hierarchy.
	c := NewContainerCollector("../../testdata/proc", "/nonexistent/sys")
	got := c.detectCgroupVersion()
	if got != 0 {
		t.Errorf("detectCgroupVersion() = %d, want 0", got)
	}
}

// ---------- readCgroupPath ----------

// TestReadCgroupPath reads testdata/proc/1/cgroup which contains "0::/"
// and expects "/" as the cgroup path (the third colon-separated field).
func TestReadCgroupPath(t *testing.T) {
	c := NewContainerCollector("../../testdata/proc", "../../testdata/sys")
	got := c.readCgroupPath()
	if got != "/" {
		t.Errorf("readCgroupPath() = %q, want %q", got, "/")
	}
}

func TestReadCgroupPathMissing(t *testing.T) {
	c := NewContainerCollector("/nonexistent/proc", "/nonexistent/sys")
	got := c.readCgroupPath()
	if got != "" {
		t.Errorf("readCgroupPath() with missing file = %q, want empty", got)
	}
}

// ---------- extractContainerID ----------

func TestExtractContainerID(t *testing.T) {
	// A valid 64-char hex ID used across test cases.
	hexID := strings.Repeat("a1b2c3d4", 8) // 64 chars

	tests := []struct {
		name       string
		cgroupPath string
		want       string
	}{
		{
			name:       "docker_direct",
			cgroupPath: "/docker/" + hexID,
			want:       hexID,
		},
		{
			name:       "docker_scope_format",
			cgroupPath: "/system.slice/docker-" + hexID + ".scope",
			want:       hexID,
		},
		{
			name:       "kubernetes_pod",
			cgroupPath: "/kubepods/poduid/" + hexID,
			want:       hexID,
		},
		{
			name:       "root_cgroup",
			cgroupPath: "/",
			want:       "",
		},
		{
			name:       "empty_path",
			cgroupPath: "",
			want:       "",
		},
		{
			name:       "non_hex_64chars",
			cgroupPath: "/docker/" + strings.Repeat("z", 64),
			want:       "",
		},
		{
			name:       "short_hex_id",
			cgroupPath: "/docker/abc123",
			want:       "",
		},
	}

	c := NewContainerCollector("../../testdata/proc", "../../testdata/sys")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.extractContainerID(tt.cgroupPath)
			if got != tt.want {
				t.Errorf("extractContainerID(%q) = %q, want %q", tt.cgroupPath, got, tt.want)
			}
		})
	}
}

// ---------- isHex helper ----------

func TestIsHex(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0123456789abcdef", true},
		{"ABCDEF", true},
		{"aAbBcCdDeEfF", true},
		{"", true}, // vacuously true, but not a valid container ID (length != 64)
		{"xyz", false},
		{"0123g", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isHex(tt.input)
			if got != tt.want {
				t.Errorf("isHex(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
