package executor

import (
	"os"
	"testing"
	"time"
)

func TestRegistryToolCount(t *testing.T) {
	count := len(Registry)
	// We expect ~66 tools total (20 original + 46 new)
	if count < 60 {
		t.Errorf("Registry has %d tools, expected at least 60", count)
	}
	t.Logf("Registry contains %d tools", count)
}

func TestRegistryAllToolsHaveValidFields(t *testing.T) {
	for name, spec := range Registry {
		if spec.Name == "" {
			t.Errorf("tool %q has empty Name", name)
		}
		if spec.Name != name {
			t.Errorf("tool %q has mismatched Name=%q", name, spec.Name)
		}
		if spec.Binary == "" {
			t.Errorf("tool %q has empty Binary", name)
		}
		if spec.Category == "" {
			t.Errorf("tool %q has empty Category", name)
		}
		if spec.Parser == nil {
			t.Errorf("tool %q has nil Parser", name)
		}
		if spec.BuildArgs == nil {
			t.Errorf("tool %q has nil BuildArgs", name)
		}
	}
}

func TestRegistryBuildArgs(t *testing.T) {
	dur := 30 * time.Second
	for name, spec := range Registry {
		args := spec.BuildArgs(dur)
		// Just ensure it doesn't panic; some tools (sockstat) may return nil
		_ = args
		t.Logf("tool %q args: %v", name, args)
	}
}

func TestRegistryCategories(t *testing.T) {
	validCategories := map[string]bool{
		"cpu": true, "disk": true, "memory": true,
		"network": true, "process": true, "stacktrace": true,
	}
	for name, spec := range Registry {
		if !validCategories[spec.Category] {
			t.Errorf("tool %q has invalid category %q", name, spec.Category)
		}
	}
}

// TestPIDFlagConsistency verifies that tools with PIDFlag set have it as "-p".
func TestPIDFlagConsistency(t *testing.T) {
	toolsWithPID := 0
	for name, spec := range Registry {
		if spec.PIDFlag != "" {
			toolsWithPID++
			if spec.PIDFlag != "-p" {
				t.Errorf("tool %q has PIDFlag=%q, expected '-p'", name, spec.PIDFlag)
			}
		}
	}
	// We expect at least 20 tools to support PID filtering
	if toolsWithPID < 20 {
		t.Errorf("only %d tools have PIDFlag set, expected at least 20", toolsWithPID)
	}
	t.Logf("%d/%d tools support -p PID filtering", toolsWithPID, len(Registry))
}

// TestPIDFlagToolsList verifies specific known tools have PIDFlag set.
func TestPIDFlagToolsList(t *testing.T) {
	expectedWithPID := []string{
		"runqlat", "cpudist", "profile", "offcputime",
		"opensnoop", "tcpconnlat", "tcpconnect", "tcpaccept",
		"ext4slower", "fileslower", "killsnoop", "capable",
		"runqslower", "funccount", "memleak", "syscount",
		"filelife", "biotop", "ext4dist", "xfsdist",
		"stackcount", "tcplife", "statsnoop",
	}
	for _, name := range expectedWithPID {
		spec, ok := Registry[name]
		if !ok {
			t.Errorf("tool %q not in registry", name)
			continue
		}
		if spec.PIDFlag == "" {
			t.Errorf("tool %q should have PIDFlag set but it's empty", name)
		}
	}
}

// TestPIDFlagNotOnEventTools verifies that event-only tools
// without PID support don't have PIDFlag set.
func TestPIDFlagNotOnEventTools(t *testing.T) {
	expectedWithoutPID := []string{
		"tcpretrans", "tcpdrop", "oomkill",
		"runqlen", "hardirqs", "softirqs",
		"cachestat", "biolatency",
	}
	for _, name := range expectedWithoutPID {
		spec, ok := Registry[name]
		if !ok {
			t.Errorf("tool %q not in registry", name)
			continue
		}
		if spec.PIDFlag != "" {
			t.Errorf("tool %q should NOT have PIDFlag but has %q", name, spec.PIDFlag)
		}
	}
}

// TestAllToolsParseFixtures verifies that every tool with a testdata fixture
// can parse it without error and produce a non-nil result.
func TestAllToolsParseFixtures(t *testing.T) {
	// Map tool name -> testdata file
	fixtures := map[string]string{
		// Original tools
		"runqlat":        "runqlat.txt",
		"biolatency":     "biolatency.txt",
		"biosnoop":       "biosnoop.txt",
		"ext4slower":     "ext4slower.txt",
		"tcpconnlat":     "tcpconnlat.txt",
		"tcpretrans":     "tcpretrans.txt",
		"tcprtt":         "tcprtt.txt",
		"gethostlatency": "gethostlatency.txt",
		"tcpdrop":        "tcpdrop.txt",
		"profile":        "profile_folded.txt",
		"offcputime":     "offcputime.txt",
		"cachestat":      "cachestat.txt",
		"execsnoop":      "execsnoop.txt",
		"hardirqs":       "hardirqs.txt",
		"softirqs":       "softirqs.txt",
		"mountsnoop":     "mountsnoop.txt",
		// New tools
		"opensnoop":   "opensnoop.txt",
		"killsnoop":   "killsnoop.txt",
		"capable":     "capable.txt",
		"exitsnoop":   "exitsnoop.txt",
		"filelife":    "filelife.txt",
		"runqslower":  "runqslower.txt",
		"tcpconnect":  "tcpconnect.txt",
		"tcpaccept":   "tcpaccept.txt",
		"tcplife":     "tcplife.txt",
		"oomkill":     "oomkill.txt",
		"drsnoop":     "drsnoop.txt",
		"llcstat":     "llcstat.txt",
		"ext4dist":    "ext4dist.txt",
		"biotop":      "biotop.txt",
		"slabratetop": "slabratetop.txt",
		"vfsstat":     "vfsstat.txt",
		"syscount":    "syscount.txt",
		"funccount":   "funccount.txt",
		"memleak":     "memleak.txt",
		"wakeuptime":  "wakeuptime.txt",
		"biostacks":   "biostacks.txt",
		"skbdrop":     "skbdrop.txt",
	}

	for toolName, fixture := range fixtures {
		t.Run(toolName, func(t *testing.T) {
			spec, ok := Registry[toolName]
			if !ok {
				t.Fatalf("tool %q not found in Registry", toolName)
			}

			path := testdataPath(fixture)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", fixture, err)
			}

			result, err := spec.Parser(string(data))
			if err != nil {
				t.Fatalf("parser error: %v", err)
			}
			if result == nil {
				t.Fatal("parser returned nil result")
			}
			if result.Collector != toolName {
				t.Errorf("collector = %q, want %q", result.Collector, toolName)
			}
			if result.Tier != 2 {
				t.Errorf("tier = %d, want 2", result.Tier)
			}

			// Verify the result has some data
			hasData := len(result.Events) > 0 || len(result.Histograms) > 0 || len(result.Stacks) > 0
			if !hasData {
				t.Errorf("tool %q parsed fixture but produced no events/histograms/stacks", toolName)
			}
		})
	}
}

// TestTabularToolsParseSample tests a representative sample of TABULAR tools.
func TestTabularToolsParseSample(t *testing.T) {
	tests := []struct {
		tool     string
		fixture  string
		minCount int
	}{
		{"opensnoop", "opensnoop.txt", 5},
		{"tcpconnect", "tcpconnect.txt", 4},
		{"tcpaccept", "tcpaccept.txt", 3},
		{"tcplife", "tcplife.txt", 3},
		{"oomkill", "oomkill.txt", 2},
		{"runqslower", "runqslower.txt", 3},
		{"drsnoop", "drsnoop.txt", 3},
		{"biotop", "biotop.txt", 3},
		{"syscount", "syscount.txt", 5},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			raw := readTestdata(t, tt.fixture)
			spec := Registry[tt.tool]
			result, err := spec.Parser(raw)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if len(result.Events) < tt.minCount {
				t.Errorf("expected at least %d events, got %d", tt.minCount, len(result.Events))
			}
		})
	}
}

// TestHistogramToolsParseSample tests the new HISTOGRAM tools.
func TestHistogramToolsParseSample(t *testing.T) {
	raw := readTestdata(t, "ext4dist.txt")
	spec := Registry["ext4dist"]
	result, err := spec.Parser(raw)
	if err != nil {
		t.Fatalf("ext4dist parse error: %v", err)
	}
	if len(result.Histograms) == 0 {
		t.Fatal("no histograms parsed")
	}
	hist := result.Histograms[0]
	if hist.Name != "ext4_latency" {
		t.Errorf("histogram name = %q, want ext4_latency", hist.Name)
	}
	if hist.TotalCount == 0 {
		t.Error("total count is 0")
	}
	if hist.P50 <= 0 {
		t.Error("P50 should be > 0")
	}
}

// TestFoldedToolsParseSample tests the new FOLDED stack tools.
func TestFoldedToolsParseSample(t *testing.T) {
	tests := []struct {
		tool      string
		fixture   string
		stackType string
	}{
		{"wakeuptime", "wakeuptime.txt", "wakeup"},
		{"biostacks", "biostacks.txt", "block-io"},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			raw := readTestdata(t, tt.fixture)
			spec := Registry[tt.tool]
			result, err := spec.Parser(raw)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if len(result.Stacks) == 0 {
				t.Fatal("no stacks parsed")
			}
			if result.Stacks[0].Type != tt.stackType {
				t.Errorf("stack type = %q, want %q", result.Stacks[0].Type, tt.stackType)
			}
		})
	}
}

// TestSkbdropParsesStacksAndEvents verifies skbdrop produces both events and stacks.
func TestSkbdropParsesStacksAndEvents(t *testing.T) {
	raw := readTestdata(t, "skbdrop.txt")
	spec := Registry["skbdrop"]
	result, err := spec.Parser(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(result.Events) == 0 {
		t.Error("expected events from skbdrop")
	}
	if len(result.Stacks) == 0 {
		t.Error("expected kernel stacks from skbdrop")
	}
}
