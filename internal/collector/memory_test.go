package collector

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/baikal/sysdiag/internal/model"
)

const (
	testProcRoot = "../../testdata/proc"
	testSysRoot  = "../../testdata/sys"
)

// newTestMemoryCollector creates a MemoryCollector pointed at testdata fixtures.
func newTestMemoryCollector() *MemoryCollector {
	return NewMemoryCollector(testProcRoot, testSysRoot)
}

func TestParseMeminfo(t *testing.T) {
	mc := newTestMemoryCollector()
	data := &model.MemoryData{}
	mc.parseMeminfo(data)

	tests := []struct {
		name string
		got  int64
		want int64
	}{
		{"TotalBytes", data.TotalBytes, 32768000 * 1024},
		{"FreeBytes", data.FreeBytes, 2048000 * 1024},
		{"AvailableBytes", data.AvailableBytes, 16384000 * 1024},
		{"CachedBytes", data.CachedBytes, 8192000 * 1024},
		{"BuffersBytes", data.BuffersBytes, 1024000 * 1024},
		{"SwapTotalBytes", data.SwapTotalBytes, 4096000 * 1024},
		// SwapUsed = SwapTotal - SwapFree = 4096000 - 3072000 = 1024000 kB
		{"SwapUsedBytes", data.SwapUsedBytes, 1024000 * 1024},
		{"DirtyBytes", data.DirtyBytes, 5120 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}

	// HugePages fields are not multiplied by 1024 (no " kB" suffix in procfs)
	if data.HugePagesTotal != 64 {
		t.Errorf("HugePagesTotal = %d, want 64", data.HugePagesTotal)
	}
	if data.HugePagesFree != 32 {
		t.Errorf("HugePagesFree = %d, want 32", data.HugePagesFree)
	}
}

func TestParseMeminfoMissingFile(t *testing.T) {
	mc := NewMemoryCollector("/nonexistent/proc", testSysRoot)
	data := &model.MemoryData{}
	mc.parseMeminfo(data)

	// All fields should remain zero when file is missing.
	if data.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0 for missing file", data.TotalBytes)
	}
	if data.FreeBytes != 0 {
		t.Errorf("FreeBytes = %d, want 0 for missing file", data.FreeBytes)
	}
}

func TestParseVmstat(t *testing.T) {
	mc := newTestMemoryCollector()
	data := &model.MemoryData{}
	mc.parseVmstat(data)

	if data.MinorFaults != 1000000 {
		t.Errorf("MinorFaults = %d, want 1000000", data.MinorFaults)
	}
	if data.MajorFaults != 42 {
		t.Errorf("MajorFaults = %d, want 42", data.MajorFaults)
	}
}

func TestParseVmstatMissingFile(t *testing.T) {
	mc := NewMemoryCollector("/nonexistent/proc", testSysRoot)
	data := &model.MemoryData{}
	mc.parseVmstat(data)

	if data.MinorFaults != 0 {
		t.Errorf("MinorFaults = %d, want 0 for missing file", data.MinorFaults)
	}
	if data.MajorFaults != 0 {
		t.Errorf("MajorFaults = %d, want 0 for missing file", data.MajorFaults)
	}
}

func TestParsePSI(t *testing.T) {
	mc := newTestMemoryCollector()
	data := &model.MemoryData{}
	mc.parsePSI(data)

	const epsilon = 0.001

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"PSISome10", data.PSISome10, 0.50},
		{"PSISome60", data.PSISome60, 0.30},
		{"PSIFull10", data.PSIFull10, 0.10},
		{"PSIFull60", data.PSIFull60, 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if math.Abs(tt.got-tt.want) > epsilon {
				t.Errorf("%s = %f, want %f", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestParsePSIMissingFile(t *testing.T) {
	mc := NewMemoryCollector("/nonexistent/proc", testSysRoot)
	data := &model.MemoryData{}
	mc.parsePSI(data)

	// All PSI fields should be zero when pressure file is missing (kernel < 4.20).
	if data.PSISome10 != 0 || data.PSISome60 != 0 {
		t.Errorf("PSI some should be 0 when file missing, got some10=%f some60=%f",
			data.PSISome10, data.PSISome60)
	}
	if data.PSIFull10 != 0 || data.PSIFull60 != 0 {
		t.Errorf("PSI full should be 0 when file missing, got full10=%f full60=%f",
			data.PSIFull10, data.PSIFull60)
	}
}

func TestReadTHPEnabled(t *testing.T) {
	mc := newTestMemoryCollector()
	got := mc.readTHPEnabled()

	// testdata contains "always [madvise] never" — active setting is in brackets.
	if got != "madvise" {
		t.Errorf("readTHPEnabled() = %q, want %q", got, "madvise")
	}
}

func TestReadTHPEnabledMissingFile(t *testing.T) {
	mc := NewMemoryCollector(testProcRoot, "/nonexistent/sys")
	got := mc.readTHPEnabled()

	if got != "" {
		t.Errorf("readTHPEnabled() = %q, want empty string for missing file", got)
	}
}

func TestReadTHPEnabledNoBrackets(t *testing.T) {
	// Create a temp sysfs tree with a THP file that has no brackets.
	tmpDir := t.TempDir()
	thpDir := filepath.Join(tmpDir, "kernel", "mm", "transparent_hugepage")
	if err := os.MkdirAll(thpDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thpDir, "enabled"), []byte("always\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mc := NewMemoryCollector(testProcRoot, tmpDir)
	got := mc.readTHPEnabled()

	if got != "always" {
		t.Errorf("readTHPEnabled() = %q, want %q for no-bracket input", got, "always")
	}
}

func TestSysctlValues(t *testing.T) {
	mc := newTestMemoryCollector()
	data := &model.MemoryData{}

	// Collect calls the sysctl reading; exercise them directly the same way Collect does.
	data.Swappiness = readSysctlInt(mc.procRoot, "sys/vm/swappiness")
	data.OvercommitMemory = readSysctlInt(mc.procRoot, "sys/vm/overcommit_memory")
	data.OvercommitRatio = readSysctlInt(mc.procRoot, "sys/vm/overcommit_ratio")
	data.DirtyRatio = readSysctlInt(mc.procRoot, "sys/vm/dirty_ratio")
	data.DirtyBackgroundRatio = readSysctlInt(mc.procRoot, "sys/vm/dirty_background_ratio")
	data.MinFreeKbytes = readSysctlInt(mc.procRoot, "sys/vm/min_free_kbytes")

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"Swappiness", data.Swappiness, 60},
		{"OvercommitMemory", data.OvercommitMemory, 0},
		{"OvercommitRatio", data.OvercommitRatio, 50},
		{"DirtyRatio", data.DirtyRatio, 20},
		{"DirtyBackgroundRatio", data.DirtyBackgroundRatio, 10},
		{"MinFreeKbytes", data.MinFreeKbytes, 67584},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestSysctlMissingFile(t *testing.T) {
	got := readSysctlInt("/nonexistent/proc", "sys/vm/swappiness")
	if got != 0 {
		t.Errorf("readSysctlInt for missing file = %d, want 0", got)
	}
}

func TestCollectIntegration(t *testing.T) {
	mc := newTestMemoryCollector()
	cfg := CollectConfig{
		Profile:  "quick",
		ProcRoot: testProcRoot,
		SysRoot:  testSysRoot,
	}

	result, err := mc.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Check Result metadata.
	if result.Collector != "memory_info" {
		t.Errorf("Collector = %q, want %q", result.Collector, "memory_info")
	}
	if result.Category != "memory" {
		t.Errorf("Category = %q, want %q", result.Category, "memory")
	}
	if result.Tier != 1 {
		t.Errorf("Tier = %d, want 1", result.Tier)
	}
	if result.StartTime.IsZero() || result.EndTime.IsZero() {
		t.Error("StartTime or EndTime is zero")
	}
	if result.EndTime.Before(result.StartTime) {
		t.Errorf("EndTime %v is before StartTime %v", result.EndTime, result.StartTime)
	}

	// Verify Data is MemoryData with expected values.
	data, ok := result.Data.(*model.MemoryData)
	if !ok {
		t.Fatalf("Data type = %T, want *model.MemoryData", result.Data)
	}

	// Spot-check a representative subset of fields from each parser.
	if data.TotalBytes != 32768000*1024 {
		t.Errorf("TotalBytes = %d, want %d", data.TotalBytes, 32768000*1024)
	}
	if data.MinorFaults != 1000000 {
		t.Errorf("MinorFaults = %d, want 1000000", data.MinorFaults)
	}
	if data.MajorFaults != 42 {
		t.Errorf("MajorFaults = %d, want 42", data.MajorFaults)
	}
	if data.Swappiness != 60 {
		t.Errorf("Swappiness = %d, want 60", data.Swappiness)
	}
	if data.MinFreeKbytes != 67584 {
		t.Errorf("MinFreeKbytes = %d, want 67584", data.MinFreeKbytes)
	}
	if data.THPEnabled != "madvise" {
		t.Errorf("THPEnabled = %q, want %q", data.THPEnabled, "madvise")
	}

	const epsilon = 0.001
	if math.Abs(data.PSISome10-0.50) > epsilon {
		t.Errorf("PSISome10 = %f, want 0.50", data.PSISome10)
	}
	if math.Abs(data.PSIFull10-0.10) > epsilon {
		t.Errorf("PSIFull10 = %f, want 0.10", data.PSIFull10)
	}

	// SwapUsed should be computed correctly.
	wantSwapUsed := int64(1024000 * 1024)
	if data.SwapUsedBytes != wantSwapUsed {
		t.Errorf("SwapUsedBytes = %d, want %d", data.SwapUsedBytes, wantSwapUsed)
	}

	// HugePages should carry through.
	if data.HugePagesTotal != 64 {
		t.Errorf("HugePagesTotal = %d, want 64", data.HugePagesTotal)
	}
	if data.HugePagesFree != 32 {
		t.Errorf("HugePagesFree = %d, want 32", data.HugePagesFree)
	}
}

func TestMemoryCollectorMetadata(t *testing.T) {
	mc := newTestMemoryCollector()

	if mc.Name() != "memory_info" {
		t.Errorf("Name() = %q, want %q", mc.Name(), "memory_info")
	}
	if mc.Category() != "memory" {
		t.Errorf("Category() = %q, want %q", mc.Category(), "memory")
	}

	avail := mc.Available()
	if avail.Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", avail.Tier)
	}
}
