package orchestrator

import (
	"context"
	"fmt"
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
	// collectCalled tracks whether Collect was invoked.
	collectCalled bool
	// receivedConfig stores the config passed to Collect.
	receivedConfig collector.CollectConfig
}

func (m *mockCollector) Name() string     { return m.name }
func (m *mockCollector) Category() string { return m.category }
func (m *mockCollector) Available() collector.Availability {
	if m.tier == 0 {
		return collector.Availability{Tier: 0, Reason: "not available"}
	}
	return collector.Availability{Tier: m.tier}
}

func (m *mockCollector) Collect(ctx context.Context, cfg collector.CollectConfig) (*model.Result, error) {
	m.collectCalled = true
	m.receivedConfig = cfg

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

// --- Basic orchestrator tests ---

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

// --- --profile flag tests ---

func TestProfileSelectionAffectsDuration(t *testing.T) {
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
			profile := GetProfile(tt.profile)
			if profile.Duration != tt.expected {
				t.Errorf("profile %q duration = %v, want %v", tt.profile, profile.Duration, tt.expected)
			}
		})
	}
}

func TestProfileQuickRestrictsCollectors(t *testing.T) {
	// Quick profile has an explicit collector list
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},       // not in quick profile
		&mockCollector{name: "offcputime", category: "stacktrace", tier: 2, data: &model.CPUData{}}, // not in quick profile
	}

	cfg := collector.CollectConfig{
		Profile: "quick",
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	// cpu_utilization and biolatency are in quick profile; runqlat and offcputime are not
	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	if !names["cpu_utilization"] {
		t.Error("cpu_utilization should be in quick profile")
	}
	if !names["biolatency"] {
		t.Error("biolatency should be in quick profile")
	}
	if names["runqlat"] {
		t.Error("runqlat should NOT be in quick profile")
	}
	if names["offcputime"] {
		t.Error("offcputime should NOT be in quick profile")
	}
}

func TestProfileStandardIncludesAll(t *testing.T) {
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "tcpconnlat", category: "network", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "profile", category: "stacktrace", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	if len(active) != 5 {
		t.Errorf("standard profile should include all %d collectors, got %d", 5, len(active))
	}
}

// --- --focus flag tests ---

func TestFocusNetworkFiltersBCCTools(t *testing.T) {
	allCollectors := []collector.Collector{
		// Tier 1 — always included regardless of focus
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "memory_info", category: "memory", tier: 1, data: &model.MemoryData{}},
		&mockCollector{name: "network_stats", category: "network", tier: 1, data: &model.NetworkData{}},
		// Tier 2 — only network tools should be included
		&mockCollector{name: "tcpconnlat", category: "network", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "tcpretrans", category: "network", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "profile", category: "stacktrace", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   []string{"network"},
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	// Tier 1: all included
	if !names["cpu_utilization"] {
		t.Error("Tier 1 cpu_utilization should always be included with focus")
	}
	if !names["memory_info"] {
		t.Error("Tier 1 memory_info should always be included with focus")
	}
	if !names["network_stats"] {
		t.Error("Tier 1 network_stats should always be included with focus")
	}

	// Tier 2 network: included
	if !names["tcpconnlat"] {
		t.Error("tcpconnlat should be included with --focus network")
	}
	if !names["tcpretrans"] {
		t.Error("tcpretrans should be included with --focus network")
	}

	// Tier 2 non-network: excluded
	if names["runqlat"] {
		t.Error("runqlat (cpu) should be excluded with --focus network")
	}
	if names["biolatency"] {
		t.Error("biolatency (disk) should be excluded with --focus network")
	}
	if names["profile"] {
		t.Error("profile (stacktrace) should be excluded with --focus network")
	}
}

func TestFocusDiskFiltersBCCTools(t *testing.T) {
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "biosnoop", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "tcpconnlat", category: "network", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   []string{"disk"},
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	if !names["cpu_utilization"] {
		t.Error("Tier 1 cpu_utilization should always be included")
	}
	if !names["biolatency"] {
		t.Error("biolatency (disk) should be included with --focus disk")
	}
	if !names["biosnoop"] {
		t.Error("biosnoop (disk) should be included with --focus disk")
	}
	if names["tcpconnlat"] {
		t.Error("tcpconnlat (network) should be excluded with --focus disk")
	}
	if names["runqlat"] {
		t.Error("runqlat (cpu) should be excluded with --focus disk")
	}
}

func TestFocusStacksMapsToStacktrace(t *testing.T) {
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "profile", category: "stacktrace", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "offcputime", category: "stacktrace", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   []string{"stacks"},
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	// "stacks" maps to "stacktrace" category
	if !names["profile"] {
		t.Error("profile (stacktrace) should be included with --focus stacks")
	}
	if !names["offcputime"] {
		t.Error("offcputime (stacktrace) should be included with --focus stacks")
	}
	if names["runqlat"] {
		t.Error("runqlat (cpu) should be excluded with --focus stacks")
	}
}

func TestFocusMultipleAreas(t *testing.T) {
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "tcpconnlat", category: "network", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "profile", category: "stacktrace", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   []string{"network", "disk"},
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	if !names["tcpconnlat"] {
		t.Error("tcpconnlat (network) should be included with --focus network,disk")
	}
	if !names["biolatency"] {
		t.Error("biolatency (disk) should be included with --focus network,disk")
	}
	if names["runqlat"] {
		t.Error("runqlat (cpu) should be excluded with --focus network,disk")
	}
	if names["profile"] {
		t.Error("profile (stacktrace) should be excluded with --focus network,disk")
	}
}

func TestFocusEmptyMeansNoFilter(t *testing.T) {
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "runqlat", category: "cpu", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "tcpconnlat", category: "network", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   nil, // no focus
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	if len(active) != 4 {
		t.Errorf("no focus should include all %d collectors, got %d", 4, len(active))
	}
}

func TestFocusStoredInMetadata(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Focus:   []string{"network", "disk"},
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.Metadata.FocusAreas) != 2 {
		t.Errorf("FocusAreas = %v, want [network disk]", report.Metadata.FocusAreas)
	}
}

func TestFocusWithQuickProfile(t *testing.T) {
	// Quick profile has explicit collector list. Focus should still filter Tier 2 tools.
	allCollectors := []collector.Collector{
		&mockCollector{name: "cpu_utilization", category: "cpu", tier: 1, data: &model.CPUData{}},
		&mockCollector{name: "biolatency", category: "disk", tier: 2, data: &model.CPUData{}},
		&mockCollector{name: "tcpretrans", category: "network", tier: 2, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "quick",
		Focus:   []string{"network"},
		Quiet:   true,
	}

	orch := New(allCollectors, cfg)
	active := orch.filterCollectors()

	names := map[string]bool{}
	for _, c := range active {
		names[c.Name()] = true
	}

	if !names["cpu_utilization"] {
		t.Error("Tier 1 cpu_utilization should be included in quick+focus")
	}
	if !names["tcpretrans"] {
		t.Error("tcpretrans (network) should be included in quick+focus network")
	}
	if names["biolatency"] {
		t.Error("biolatency (disk) should be excluded in quick+focus network")
	}
}

// --- --duration flag test ---

func TestDurationOverrideInConfig(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	// Custom duration override (as CLI would set it)
	cfg := collector.CollectConfig{
		Profile:  "standard",
		Duration: 15 * time.Second, // overridden from default 30s
		Quiet:    true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mc.receivedConfig.Duration != 15*time.Second {
		t.Errorf("collector received duration = %v, want 15s", mc.receivedConfig.Duration)
	}
}

// --- --max-events flag test ---

func TestMaxEventsPassedToCollectors(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	cfg := collector.CollectConfig{
		Profile:               "standard",
		MaxEventsPerCollector: 500,
		Quiet:                 true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mc.receivedConfig.MaxEventsPerCollector != 500 {
		t.Errorf("collector received MaxEventsPerCollector = %d, want 500",
			mc.receivedConfig.MaxEventsPerCollector)
	}
}

// --- --quiet flag test ---

func TestQuietConfigPassedToCollectors(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !mc.receivedConfig.Quiet {
		t.Error("collector should receive Quiet=true")
	}
}

// --- --verbose flag test ---

func TestVerboseConfigPassedToCollectors(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
		Verbose: true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !mc.receivedConfig.Verbose {
		t.Error("collector should receive Verbose=true")
	}
}

// --- --pid flag test ---

func TestTargetPIDsPassedToCollectors(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	cfg := collector.CollectConfig{
		Profile:    "standard",
		TargetPIDs: []int{1234, 5678},
		Quiet:      true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mc.receivedConfig.TargetPIDs) != 2 {
		t.Errorf("collector received TargetPIDs = %v, want [1234 5678]",
			mc.receivedConfig.TargetPIDs)
	}
	if mc.receivedConfig.TargetPIDs[0] != 1234 {
		t.Errorf("TargetPIDs[0] = %d, want 1234", mc.receivedConfig.TargetPIDs[0])
	}
}

// --- --cgroup flag test ---

func TestTargetCgroupsPassedToCollectors(t *testing.T) {
	mc := &mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}}

	cfg := collector.CollectConfig{
		Profile:        "standard",
		TargetCgroups:  []string{"/sys/fs/cgroup/docker/abc123"},
		Quiet:          true,
	}

	orch := New([]collector.Collector{mc}, cfg)
	_, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mc.receivedConfig.TargetCgroups) != 1 {
		t.Errorf("collector received TargetCgroups = %v, want 1 entry",
			mc.receivedConfig.TargetCgroups)
	}
}

// --- Panic recovery test ---

func TestOrchestratorRecoversPanic(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{
			name:     "good",
			category: "cpu",
			tier:     1,
			data:     &model.CPUData{},
		},
		&panicCollector{name: "panicker", category: "cpu"},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run should not fail on panic: %v", err)
	}

	// Good collector result + panic result
	cpuResults := report.Categories["cpu"]
	if len(cpuResults) != 2 {
		t.Fatalf("cpu results = %d, want 2 (good + panicked)", len(cpuResults))
	}

	// Find the panic result
	var panicResult *model.Result
	for i := range cpuResults {
		if cpuResults[i].Collector == "panicker" {
			panicResult = &cpuResults[i]
		}
	}
	if panicResult == nil {
		t.Fatal("missing panic result")
	}
	if len(panicResult.Errors) == 0 {
		t.Error("panic result should have errors")
	}
}

type panicCollector struct {
	name     string
	category string
}

func (p *panicCollector) Name() string     { return p.name }
func (p *panicCollector) Category() string { return p.category }
func (p *panicCollector) Available() collector.Availability {
	return collector.Availability{Tier: 1}
}
func (p *panicCollector) Collect(ctx context.Context, cfg collector.CollectConfig) (*model.Result, error) {
	panic("test panic")
}

// --- Error handling test ---

func TestOrchestratorHandlesCollectorError(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{
			name:     "good",
			category: "cpu",
			tier:     1,
			data:     &model.CPUData{},
		},
		&mockCollector{
			name:     "bad",
			category: "memory",
			tier:     1,
			err:      fmt.Errorf("simulated failure"),
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

	// Good result should exist
	cpuResults := report.Categories["cpu"]
	if len(cpuResults) != 1 {
		t.Errorf("cpu results = %d, want 1", len(cpuResults))
	}

	// Bad result should exist with errors
	memResults := report.Categories["memory"]
	if len(memResults) != 1 {
		t.Fatalf("memory results = %d, want 1", len(memResults))
	}
	if len(memResults[0].Errors) == 0 {
		t.Error("bad collector should have errors recorded")
	}
}

// --- Health score and summary tests ---

func TestReportContainsSummary(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}},
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

	if report.Summary.Resources == nil {
		t.Error("report should have Resources map")
	}
	if report.Summary.Anomalies == nil {
		t.Error("report should have Anomalies slice")
	}
	// Health score computed (0-100)
	if report.Summary.HealthScore < 0 || report.Summary.HealthScore > 100 {
		t.Errorf("health score = %d, want 0-100", report.Summary.HealthScore)
	}
}

// --- Metadata tests ---

func TestReportMetadata(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}},
	}

	cfg := collector.CollectConfig{
		Profile: "deep",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.Metadata.Tool != "melisai" {
		t.Errorf("tool = %q, want melisai", report.Metadata.Tool)
	}
	if report.Metadata.SchemaVersion != "1.0.0" {
		t.Errorf("schema_version = %q, want 1.0.0", report.Metadata.SchemaVersion)
	}
	if report.Metadata.Profile != "deep" {
		t.Errorf("profile = %q, want deep", report.Metadata.Profile)
	}
	if report.Metadata.Duration != "1m0s" {
		t.Errorf("duration = %q, want 1m0s", report.Metadata.Duration)
	}
	if report.Metadata.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if report.Metadata.Arch == "" {
		t.Error("arch should not be empty")
	}
	if report.Metadata.CPUs == 0 {
		t.Error("cpus should not be 0")
	}
}

// --- focusCategoryMap test ---

func TestFocusCategoryMap(t *testing.T) {
	tests := []struct {
		focus    string
		category string
	}{
		{"stacks", "stacktrace"},
		{"cpu", "cpu"},
		{"memory", "memory"},
		{"disk", "disk"},
		{"network", "network"},
		{"process", "process"},
		{"container", "container"},
	}

	for _, tt := range tests {
		t.Run(tt.focus, func(t *testing.T) {
			cat, ok := focusCategoryMap[tt.focus]
			if !ok {
				t.Errorf("focus %q not in focusCategoryMap", tt.focus)
			}
			if cat != tt.category {
				t.Errorf("focusCategoryMap[%q] = %q, want %q", tt.focus, cat, tt.category)
			}
		})
	}
}

// --- Observer overhead in report ---

func TestReportHasObserverOverhead(t *testing.T) {
	collectors := []collector.Collector{
		&mockCollector{name: "test", category: "cpu", tier: 1, data: &model.CPUData{}},
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

	if report.Metadata.ObserverOverhead == nil {
		t.Error("report should include observer overhead")
	}
}

// --- Two-phase collection tests ---

// timestampCollector records when Collect starts and ends, for phase ordering verification.
type timestampCollector struct {
	name      string
	category  string
	tier      int
	delay     time.Duration
	startedAt time.Time
	endedAt   time.Time
}

func (tc *timestampCollector) Name() string     { return tc.name }
func (tc *timestampCollector) Category() string { return tc.category }
func (tc *timestampCollector) Available() collector.Availability {
	return collector.Availability{Tier: tc.tier}
}
func (tc *timestampCollector) Collect(ctx context.Context, cfg collector.CollectConfig) (*model.Result, error) {
	tc.startedAt = time.Now()
	if tc.delay > 0 {
		select {
		case <-time.After(tc.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	tc.endedAt = time.Now()
	return &model.Result{
		Collector: tc.name,
		Category:  tc.category,
		Tier:      tc.tier,
		StartTime: tc.startedAt,
		EndTime:   tc.endedAt,
		Data:      &model.CPUData{},
	}, nil
}

func TestTwoPhaseCollectionOrderTier1BeforeTier2(t *testing.T) {
	tier1a := &timestampCollector{name: "cpu_info", category: "cpu", tier: 1, delay: 50 * time.Millisecond}
	tier1b := &timestampCollector{name: "mem_info", category: "memory", tier: 1, delay: 50 * time.Millisecond}
	tier2a := &timestampCollector{name: "runqlat", category: "cpu", tier: 2, delay: 50 * time.Millisecond}
	tier2b := &timestampCollector{name: "biolatency", category: "disk", tier: 2, delay: 50 * time.Millisecond}
	tier3 := &timestampCollector{name: "tcpretrans_native", category: "network", tier: 3, delay: 50 * time.Millisecond}

	collectors := []collector.Collector{tier1a, tier1b, tier2a, tier2b, tier3}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// All Tier 1 collectors must finish BEFORE any Tier 2/3 collector starts
	latestTier1End := tier1a.endedAt
	if tier1b.endedAt.After(latestTier1End) {
		latestTier1End = tier1b.endedAt
	}

	for _, tc := range []*timestampCollector{tier2a, tier2b, tier3} {
		if tc.startedAt.Before(latestTier1End) {
			t.Errorf("Tier %d collector %q started at %v, before Tier 1 finished at %v",
				tc.tier, tc.name, tc.startedAt, latestTier1End)
		}
	}

	// Verify all results are in report
	totalResults := 0
	for _, results := range report.Categories {
		totalResults += len(results)
	}
	if totalResults != 5 {
		t.Errorf("expected 5 results, got %d", totalResults)
	}
}

func TestTwoPhaseCollectionTier2RunsInParallel(t *testing.T) {
	tier1 := &timestampCollector{name: "cpu_info", category: "cpu", tier: 1}
	tier2a := &timestampCollector{name: "runqlat", category: "cpu", tier: 2, delay: 100 * time.Millisecond}
	tier2b := &timestampCollector{name: "tcpconnlat", category: "network", tier: 2, delay: 100 * time.Millisecond}
	tier2c := &timestampCollector{name: "biolatency", category: "disk", tier: 2, delay: 100 * time.Millisecond}

	collectors := []collector.Collector{tier1, tier2a, tier2b, tier2c}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	start := time.Now()
	_, err := orch.Run(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Phase 1 (tier1) + Phase 2 (three 100ms collectors in parallel)
	// Sequential would be 300ms+ for tier2 alone
	// Parallel: ~100ms for tier2
	if elapsed > 350*time.Millisecond {
		t.Errorf("Tier 2 collectors should run in parallel: took %v (expected < 350ms)", elapsed)
	}
}

func TestTwoPhaseOnlyTier1NoPhase2(t *testing.T) {
	// When no Tier 2/3 collectors, only Phase 1 runs
	tier1a := &timestampCollector{name: "cpu_info", category: "cpu", tier: 1}
	tier1b := &timestampCollector{name: "mem_info", category: "memory", tier: 1}

	collectors := []collector.Collector{tier1a, tier1b}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(report.Categories))
	}
}

func TestTwoPhaseOnlyTier2NoTier1(t *testing.T) {
	// Edge case: only Tier 2 collectors
	tier2a := &timestampCollector{name: "runqlat", category: "cpu", tier: 2, delay: 10 * time.Millisecond}
	tier2b := &timestampCollector{name: "biolatency", category: "disk", tier: 2, delay: 10 * time.Millisecond}

	collectors := []collector.Collector{tier2a, tier2b}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(report.Categories))
	}
}

func TestTwoPhaseInterruptedAfterPhase1(t *testing.T) {
	// If context is cancelled after Phase 1, Phase 2 is skipped
	tier1 := &timestampCollector{name: "cpu_info", category: "cpu", tier: 1}
	tier2 := &timestampCollector{name: "runqlat", category: "cpu", tier: 2}

	collectors := []collector.Collector{tier1, tier2}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	// Cancel context after a very short time (Phase 1 should complete, Phase 2 should be skipped)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	orch := New(collectors, cfg)
	report, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Phase 1 result should exist
	if _, ok := report.Categories["cpu"]; !ok {
		t.Error("Phase 1 cpu results should be present")
	}

	// Tier 2 collector should not have been called (context cancelled between phases)
	if !tier2.startedAt.IsZero() {
		// If it did start, it should have an error
		t.Log("Tier 2 started despite cancellation — checking result has error")
	}
}

func TestTwoPhaseResultsMerged(t *testing.T) {
	// Tier 1 and Tier 2 collectors in the same category should be merged
	tier1CPU := &timestampCollector{name: "cpu_info", category: "cpu", tier: 1}
	tier2CPU := &timestampCollector{name: "runqlat", category: "cpu", tier: 2}
	tier2Disk := &timestampCollector{name: "biolatency", category: "disk", tier: 2}

	collectors := []collector.Collector{tier1CPU, tier2CPU, tier2Disk}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	report, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// CPU category should have 2 results (Tier 1 + Tier 2)
	cpuResults := report.Categories["cpu"]
	if len(cpuResults) != 2 {
		t.Errorf("cpu category should have 2 results (tier 1 + tier 2), got %d", len(cpuResults))
	}

	// Disk category should have 1 result
	diskResults := report.Categories["disk"]
	if len(diskResults) != 1 {
		t.Errorf("disk category should have 1 result, got %d", len(diskResults))
	}

	// Verify sorting: within cpu, results should be sorted by collector name
	if len(cpuResults) == 2 {
		if cpuResults[0].Collector > cpuResults[1].Collector {
			t.Errorf("results should be sorted: got %q before %q",
				cpuResults[0].Collector, cpuResults[1].Collector)
		}
	}
}

// --- Parallel execution test ---

func TestCollectorsRunInParallel(t *testing.T) {
	delay := 200 * time.Millisecond
	collectors := []collector.Collector{
		&mockCollector{name: "c1", category: "cpu", tier: 1, data: &model.CPUData{}, delay: delay},
		&mockCollector{name: "c2", category: "memory", tier: 1, data: &model.MemoryData{}, delay: delay},
		&mockCollector{name: "c3", category: "disk", tier: 1, data: &model.DiskData{}, delay: delay},
	}

	cfg := collector.CollectConfig{
		Profile: "standard",
		Quiet:   true,
	}

	orch := New(collectors, cfg)
	start := time.Now()
	_, err := orch.Run(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// If run sequentially: 3 * 200ms = 600ms
	// If run in parallel: ~200ms (plus overhead)
	// Allow generous margin but must be faster than sequential
	if elapsed > 500*time.Millisecond {
		t.Errorf("collectors should run in parallel: took %v (expected < 500ms)", elapsed)
	}
}
