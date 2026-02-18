package collector

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// testdataProc returns the absolute path to the shared testdata/proc directory.
func testdataProc(t *testing.T) string {
	t.Helper()
	// Tests run from the package directory; testdata is at ../../testdata/proc.
	abs, err := filepath.Abs("../../testdata/proc")
	if err != nil {
		t.Fatalf("resolving testdata path: %v", err)
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		t.Fatalf("testdata directory does not exist: %s", abs)
	}
	return abs
}

// floatEq checks whether two float64 values are equal within a tolerance.
func floatEq(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// --- Test readProcStat ---

func TestReadProcStat(t *testing.T) {
	procRoot := testdataProc(t)
	c := NewCPUCollector(procRoot)

	agg, perCPU, ctxSw := c.readProcStat()

	// Aggregate line: cpu  100000 2000 30000 800000 5000 1000 500 0 0 0
	if agg.user != 100000 {
		t.Errorf("aggregate user = %d, want 100000", agg.user)
	}
	if agg.nice != 2000 {
		t.Errorf("aggregate nice = %d, want 2000", agg.nice)
	}
	if agg.system != 30000 {
		t.Errorf("aggregate system = %d, want 30000", agg.system)
	}
	if agg.idle != 800000 {
		t.Errorf("aggregate idle = %d, want 800000", agg.idle)
	}
	if agg.iowait != 5000 {
		t.Errorf("aggregate iowait = %d, want 5000", agg.iowait)
	}
	if agg.irq != 1000 {
		t.Errorf("aggregate irq = %d, want 1000", agg.irq)
	}
	if agg.softirq != 500 {
		t.Errorf("aggregate softirq = %d, want 500", agg.softirq)
	}
	if agg.steal != 0 {
		t.Errorf("aggregate steal = %d, want 0", agg.steal)
	}

	// total = 100000+2000+30000+800000+5000+1000+500+0 = 938500
	wantTotal := uint64(938500)
	if agg.total() != wantTotal {
		t.Errorf("aggregate total() = %d, want %d", agg.total(), wantTotal)
	}

	// Per-CPU: 4 CPUs (cpu0..cpu3)
	if len(perCPU) != 4 {
		t.Fatalf("per-CPU count = %d, want 4", len(perCPU))
	}

	// Each cpuN has: 25000 500 7500 200000 1250 250 125 0
	for i := 0; i < 4; i++ {
		cpu, ok := perCPU[i]
		if !ok {
			t.Errorf("per-CPU[%d] not found", i)
			continue
		}
		if cpu.user != 25000 {
			t.Errorf("cpu%d user = %d, want 25000", i, cpu.user)
		}
		if cpu.system != 7500 {
			t.Errorf("cpu%d system = %d, want 7500", i, cpu.system)
		}
		if cpu.idle != 200000 {
			t.Errorf("cpu%d idle = %d, want 200000", i, cpu.idle)
		}
		// total per CPU = 25000+500+7500+200000+1250+250+125+0 = 234625
		if cpu.total() != 234625 {
			t.Errorf("cpu%d total() = %d, want 234625", i, cpu.total())
		}
	}

	// Context switches: ctxt 98765432
	if ctxSw != 98765432 {
		t.Errorf("context switches = %d, want 98765432", ctxSw)
	}
}

func TestReadProcStat_MissingFile(t *testing.T) {
	c := NewCPUCollector("/nonexistent/path")

	agg, perCPU, ctxSw := c.readProcStat()

	if agg.total() != 0 {
		t.Errorf("aggregate total should be 0 for missing file, got %d", agg.total())
	}
	if perCPU != nil {
		t.Errorf("perCPU should be nil for missing file, got %v", perCPU)
	}
	if ctxSw != 0 {
		t.Errorf("context switches should be 0 for missing file, got %d", ctxSw)
	}
}

// --- Test computeDelta ---

func TestComputeDelta(t *testing.T) {
	c := NewCPUCollector("")

	before := cpuTimes{
		user: 100, nice: 10, system: 50, idle: 800,
		iowait: 20, irq: 5, softirq: 5, steal: 10,
	}
	// total before = 1000

	after := cpuTimes{
		user: 200, nice: 20, system: 100, idle: 1600,
		iowait: 40, irq: 15, softirq: 10, steal: 15,
	}
	// total after = 2000
	// delta total = 1000

	data := c.computeDelta(before, after)

	// UserPct = (user_delta + nice_delta) / total_delta * 100
	// = ((200-100) + (20-10)) / 1000 * 100 = 110/1000 * 100 = 11.0
	if !floatEq(data.UserPct, 11.0, 0.01) {
		t.Errorf("UserPct = %.4f, want 11.0", data.UserPct)
	}

	// SystemPct = (100-50) / 1000 * 100 = 5.0
	if !floatEq(data.SystemPct, 5.0, 0.01) {
		t.Errorf("SystemPct = %.4f, want 5.0", data.SystemPct)
	}

	// IOWaitPct = (40-20) / 1000 * 100 = 2.0
	if !floatEq(data.IOWaitPct, 2.0, 0.01) {
		t.Errorf("IOWaitPct = %.4f, want 2.0", data.IOWaitPct)
	}

	// IdlePct = (1600-800) / 1000 * 100 = 80.0
	if !floatEq(data.IdlePct, 80.0, 0.01) {
		t.Errorf("IdlePct = %.4f, want 80.0", data.IdlePct)
	}

	// StealPct = (15-10) / 1000 * 100 = 0.5
	if !floatEq(data.StealPct, 0.5, 0.01) {
		t.Errorf("StealPct = %.4f, want 0.5", data.StealPct)
	}

	// IRQPct = (15-5) / 1000 * 100 = 1.0
	if !floatEq(data.IRQPct, 1.0, 0.01) {
		t.Errorf("IRQPct = %.4f, want 1.0", data.IRQPct)
	}

	// SoftIRQPct = (10-5) / 1000 * 100 = 0.5
	if !floatEq(data.SoftIRQPct, 0.5, 0.01) {
		t.Errorf("SoftIRQPct = %.4f, want 0.5", data.SoftIRQPct)
	}

	// Sum should be 100%
	sum := data.UserPct + data.SystemPct + data.IOWaitPct + data.IdlePct +
		data.StealPct + data.IRQPct + data.SoftIRQPct
	if !floatEq(sum, 100.0, 0.01) {
		t.Errorf("sum of all pcts = %.4f, want 100.0", sum)
	}
}

func TestComputeDelta_ZeroInterval(t *testing.T) {
	c := NewCPUCollector("")

	same := cpuTimes{
		user: 100, nice: 10, system: 50, idle: 800,
		iowait: 20, irq: 5, softirq: 5, steal: 10,
	}

	data := c.computeDelta(same, same)

	// When total delta is zero, all fields should be zero (safe division).
	if data.UserPct != 0 || data.SystemPct != 0 || data.IdlePct != 0 {
		t.Errorf("zero-interval delta should return all zeros; got User=%.2f System=%.2f Idle=%.2f",
			data.UserPct, data.SystemPct, data.IdlePct)
	}
}

func TestComputeDelta_FullCPU(t *testing.T) {
	c := NewCPUCollector("")

	before := cpuTimes{user: 0, system: 0, idle: 1000}
	after := cpuTimes{user: 500, system: 500, idle: 1000}
	// delta total = 1000, all from user+system

	data := c.computeDelta(before, after)

	if !floatEq(data.UserPct, 50.0, 0.01) {
		t.Errorf("UserPct = %.4f, want 50.0", data.UserPct)
	}
	if !floatEq(data.SystemPct, 50.0, 0.01) {
		t.Errorf("SystemPct = %.4f, want 50.0", data.SystemPct)
	}
	if !floatEq(data.IdlePct, 0.0, 0.01) {
		t.Errorf("IdlePct = %.4f, want 0.0", data.IdlePct)
	}
}

// --- Test computePerCPUDeltas (sort order regression) ---

func TestComputePerCPUDeltas_DeterministicOrder(t *testing.T) {
	c := NewCPUCollector("")

	// Build before/after maps with non-sequential CPU numbers to test sort.
	before := map[int]cpuTimes{
		3: {user: 100, idle: 900},
		0: {user: 50, idle: 950},
		7: {user: 200, idle: 800},
		1: {user: 75, idle: 925},
	}
	after := map[int]cpuTimes{
		3: {user: 200, idle: 1800},
		0: {user: 150, idle: 1850},
		7: {user: 400, idle: 1600},
		1: {user: 175, idle: 1825},
	}

	result := c.computePerCPUDeltas(before, after)

	if len(result) != 4 {
		t.Fatalf("per-CPU result count = %d, want 4", len(result))
	}

	// Must be sorted by CPU number: 0, 1, 3, 7
	expectedOrder := []int{0, 1, 3, 7}
	for i, want := range expectedOrder {
		if result[i].CPU != want {
			t.Errorf("result[%d].CPU = %d, want %d", i, result[i].CPU, want)
		}
	}

	// Verify stability: run it multiple times, order must never change.
	for run := 0; run < 50; run++ {
		r := c.computePerCPUDeltas(before, after)
		for i, want := range expectedOrder {
			if r[i].CPU != want {
				t.Fatalf("run %d: result[%d].CPU = %d, want %d (non-deterministic order!)",
					run, i, r[i].CPU, want)
			}
		}
	}
}

func TestComputePerCPUDeltas_MissingCPUInBefore(t *testing.T) {
	c := NewCPUCollector("")

	before := map[int]cpuTimes{
		0: {user: 50, idle: 950},
		// CPU 1 missing from before
	}
	after := map[int]cpuTimes{
		0: {user: 150, idle: 1850},
		1: {user: 175, idle: 1825}, // not in before
	}

	result := c.computePerCPUDeltas(before, after)

	// Only CPU 0 should appear (CPU 1 has no before sample).
	if len(result) != 1 {
		t.Fatalf("result count = %d, want 1", len(result))
	}
	if result[0].CPU != 0 {
		t.Errorf("result[0].CPU = %d, want 0", result[0].CPU)
	}
}

func TestComputePerCPUDeltas_ZeroDelta(t *testing.T) {
	c := NewCPUCollector("")

	same := map[int]cpuTimes{
		0: {user: 100, idle: 900},
		1: {user: 200, idle: 800},
	}

	result := c.computePerCPUDeltas(same, same)

	// Zero total delta CPUs are skipped entirely.
	if len(result) != 0 {
		t.Errorf("zero-delta per-CPU should produce empty result, got %d entries", len(result))
	}
}

func TestComputePerCPUDeltas_Percentages(t *testing.T) {
	c := NewCPUCollector("")

	before := map[int]cpuTimes{
		0: {user: 0, nice: 0, system: 0, idle: 1000, iowait: 0},
	}
	after := map[int]cpuTimes{
		0: {user: 300, nice: 0, system: 100, idle: 1400, iowait: 200},
	}
	// delta total per CPU 0 = (300+100+400+200) = 1000

	result := c.computePerCPUDeltas(before, after)

	if len(result) != 1 {
		t.Fatalf("result count = %d, want 1", len(result))
	}

	r := result[0]
	if !floatEq(r.UserPct, 30.0, 0.01) {
		t.Errorf("CPU0 UserPct = %.4f, want 30.0", r.UserPct)
	}
	if !floatEq(r.SystemPct, 10.0, 0.01) {
		t.Errorf("CPU0 SystemPct = %.4f, want 10.0", r.SystemPct)
	}
	if !floatEq(r.IdlePct, 40.0, 0.01) {
		t.Errorf("CPU0 IdlePct = %.4f, want 40.0", r.IdlePct)
	}
	if !floatEq(r.IOWaitPct, 20.0, 0.01) {
		t.Errorf("CPU0 IOWaitPct = %.4f, want 20.0", r.IOWaitPct)
	}
}

// --- Test readLoadAvg ---

func TestReadLoadAvg(t *testing.T) {
	procRoot := testdataProc(t)
	c := NewCPUCollector(procRoot)

	la1, la5, la15 := c.readLoadAvg()

	// testdata/proc/loadavg: 3.14 2.71 1.99 3/456 12345
	if !floatEq(la1, 3.14, 0.001) {
		t.Errorf("LoadAvg1 = %f, want 3.14", la1)
	}
	if !floatEq(la5, 2.71, 0.001) {
		t.Errorf("LoadAvg5 = %f, want 2.71", la5)
	}
	if !floatEq(la15, 1.99, 0.001) {
		t.Errorf("LoadAvg15 = %f, want 1.99", la15)
	}
}

func TestReadLoadAvg_MissingFile(t *testing.T) {
	c := NewCPUCollector("/nonexistent/path")

	la1, la5, la15 := c.readLoadAvg()

	if la1 != 0 || la5 != 0 || la15 != 0 {
		t.Errorf("missing loadavg should return zeros, got %f %f %f", la1, la5, la15)
	}
}

func TestReadLoadAvg_MalformedContent(t *testing.T) {
	tmp := t.TempDir()

	// Write a loadavg file with fewer than 3 fields.
	if err := os.WriteFile(filepath.Join(tmp, "loadavg"), []byte("1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCPUCollector(tmp)
	la1, la5, la15 := c.readLoadAvg()

	if la1 != 0 || la5 != 0 || la15 != 0 {
		t.Errorf("malformed loadavg with <3 fields should return zeros, got %f %f %f", la1, la5, la15)
	}
}

// --- Test parseCPUPSI ---

func TestParseCPUPSI(t *testing.T) {
	procRoot := testdataProc(t)
	c := NewCPUCollector(procRoot)

	// Use computeDelta to get a real *model.CPUData, then call parseCPUPSI.
	data := c.computeDelta(cpuTimes{idle: 1000}, cpuTimes{idle: 2000})
	c.parseCPUPSI(data)

	if !floatEq(data.PSISome10, 2.50, 0.001) {
		t.Errorf("PSISome10 = %f, want 2.50", data.PSISome10)
	}
	if !floatEq(data.PSISome60, 1.80, 0.001) {
		t.Errorf("PSISome60 = %f, want 1.80", data.PSISome60)
	}
}

func TestParseCPUPSI_MissingFile(t *testing.T) {
	c := NewCPUCollector("/nonexistent/path")

	data := c.computeDelta(cpuTimes{idle: 1000}, cpuTimes{idle: 2000})
	c.parseCPUPSI(data)

	// PSI not available should leave zeros.
	if data.PSISome10 != 0 || data.PSISome60 != 0 {
		t.Errorf("missing PSI should leave zeros, got PSI10=%f PSI60=%f",
			data.PSISome10, data.PSISome60)
	}
}

func TestParseCPUPSI_FullLine(t *testing.T) {
	tmp := t.TempDir()

	// Create pressure directory and CPU PSI file with "full" line (CPU only has "some").
	if err := os.MkdirAll(filepath.Join(tmp, "pressure"), 0755); err != nil {
		t.Fatal(err)
	}
	content := "some avg10=5.00 avg60=3.00 avg300=1.00 total=9000000\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"
	if err := os.WriteFile(filepath.Join(tmp, "pressure", "cpu"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCPUCollector(tmp)
	data := c.computeDelta(cpuTimes{idle: 1000}, cpuTimes{idle: 2000})
	c.parseCPUPSI(data)

	if !floatEq(data.PSISome10, 5.00, 0.001) {
		t.Errorf("PSISome10 = %f, want 5.00", data.PSISome10)
	}
	if !floatEq(data.PSISome60, 3.00, 0.001) {
		t.Errorf("PSISome60 = %f, want 3.00", data.PSISome60)
	}
}

// --- Test edge cases: overflow simulation ---

func TestComputeDelta_LargeValues(t *testing.T) {
	c := NewCPUCollector("")

	// Simulate large counter values close to uint64 range.
	before := cpuTimes{
		user: 1<<62 - 1000, idle: 1<<62 - 5000,
	}
	after := cpuTimes{
		user: 1<<62 - 500, idle: 1<<62 - 2000,
	}
	// delta user = 500, delta idle = 3000, delta total = 3500
	// UserPct = 500/3500*100 = ~14.29
	// IdlePct = 3000/3500*100 = ~85.71

	data := c.computeDelta(before, after)

	if !floatEq(data.UserPct, 500.0/3500.0*100, 0.01) {
		t.Errorf("UserPct = %.4f, want %.4f", data.UserPct, 500.0/3500.0*100)
	}
	if !floatEq(data.IdlePct, 3000.0/3500.0*100, 0.01) {
		t.Errorf("IdlePct = %.4f, want %.4f", data.IdlePct, 3000.0/3500.0*100)
	}
}

// --- Test readProcStat with custom /proc/stat content ---

func TestReadProcStat_MinimalFile(t *testing.T) {
	tmp := t.TempDir()

	// A minimal /proc/stat with just an aggregate line and no per-CPU lines.
	content := "cpu  5000 100 2000 90000 300 50 20 10 0 0\nctxt 12345\n"
	if err := os.WriteFile(filepath.Join(tmp, "stat"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCPUCollector(tmp)
	agg, perCPU, ctxSw := c.readProcStat()

	if agg.user != 5000 {
		t.Errorf("user = %d, want 5000", agg.user)
	}
	if agg.steal != 10 {
		t.Errorf("steal = %d, want 10", agg.steal)
	}
	if len(perCPU) != 0 {
		t.Errorf("perCPU should be empty for no cpuN lines, got %d", len(perCPU))
	}
	if ctxSw != 12345 {
		t.Errorf("ctxSw = %d, want 12345", ctxSw)
	}
}

func TestReadProcStat_ShortFields(t *testing.T) {
	tmp := t.TempDir()

	// A /proc/stat where the cpu line has fewer than 9 fields (should be skipped).
	content := "cpu  5000 100\ncpu0 1000\nctxt 99\n"
	if err := os.WriteFile(filepath.Join(tmp, "stat"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCPUCollector(tmp)
	agg, perCPU, ctxSw := c.readProcStat()

	// Both cpu lines have <9 fields, so aggregate should be zero.
	if agg.total() != 0 {
		t.Errorf("aggregate total should be 0 for short fields, got %d", agg.total())
	}
	if len(perCPU) != 0 {
		t.Errorf("perCPU should be empty for short fields, got %d", len(perCPU))
	}
	if ctxSw != 99 {
		t.Errorf("ctxSw = %d, want 99", ctxSw)
	}
}

// --- Test constructor and interface ---

func TestNewCPUCollector(t *testing.T) {
	c := NewCPUCollector("/test/proc")

	if c.procRoot != "/test/proc" {
		t.Errorf("procRoot = %q, want /test/proc", c.procRoot)
	}
	if c.Name() != "cpu_utilization" {
		t.Errorf("Name() = %q, want cpu_utilization", c.Name())
	}
	if c.Category() != "cpu" {
		t.Errorf("Category() = %q, want cpu", c.Category())
	}
	if c.Available().Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", c.Available().Tier)
	}
}
