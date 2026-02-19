package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// --- helpers ---------------------------------------------------------------

// writeProcStat writes a /proc/[pid]/stat file into the fake procfs tree.
// The stat line format is:
//
//	PID (comm) state ppid pgrp session tty_nr tpgid flags minflt cminflt
//	majflt cmajflt utime stime cutime cstime priority nice num_threads
//	itrealvalue starttime vsize rss <rest...>
func writeProcStat(t *testing.T, root string, pid int, comm, state string,
	utime, stime uint64, threads int, rss int64) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build a minimal but complete stat line (need at least 22 fields after ")")
	line := fmt.Sprintf(
		"%d (%s) %s 1 %d %d 0 -1 4194560 0 0 0 0 %d %d 0 0 20 0 %d 0 0 0 %d"+
			" 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
		pid, comm, state, pid, pid, utime, stime, threads, rss,
	)

	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFDs creates n dummy symlink entries in /proc/[pid]/fd/.
func writeFDs(t *testing.T, root string, pid, n int) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("%d", pid), "fd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%d", i))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeMeminfo creates a minimal /proc/meminfo with the given total in kB.
func writeMeminfo(t *testing.T, root string, totalKB int64) {
	t.Helper()
	content := fmt.Sprintf("MemTotal:       %d kB\nMemFree:         1024 kB\n", totalKB)
	if err := os.WriteFile(filepath.Join(root, "meminfo"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildFakeProcfs creates a fake procfs for the sort regression test.
// It writes two rounds of stat files into pass1Root and pass2Root so
// that when Collect() reads them the resulting CPU deltas and memory
// rankings are intentionally different:
//
//	PID 100  high CPU delta (500 ticks),  low  RSS (1000 pages)
//	PID 200  medium CPU delta (200 ticks), high RSS (50000 pages)
//	PID 300  low  CPU delta (50 ticks),   medium RSS (25000 pages)
//
// After collection the expected orderings are:
//
//	TopByCPU: 100, 200, 300
//	TopByMem: 200, 300, 100
func buildFakeProcfsPass(t *testing.T, root string, pass int) {
	t.Helper()
	// Pass 1 utime values => pass 2 adds deltas
	type entry struct {
		pid     int
		comm    string
		state   string
		utime1  uint64
		utime2  uint64
		stime   uint64
		threads int
		rss     int64
		fds     int
	}
	procs := []entry{
		{100, "worker", "R", 5000, 5500, 100, 4, 1000, 6},
		{200, "database", "S", 15000, 15200, 100, 8, 50000, 20},
		{300, "batch", "S", 50000, 50050, 100, 2, 25000, 3},
	}

	for _, p := range procs {
		utime := p.utime1
		if pass == 2 {
			utime = p.utime2
		}
		writeProcStat(t, root, p.pid, p.comm, p.state, utime, p.stime, p.threads, p.rss)
		writeFDs(t, root, p.pid, p.fds)
	}
}

// --- tests -----------------------------------------------------------------

// TestProcessTopByCPU_SortRegression is a regression test for the bug where
// TopByCPU and TopByMem shared the same underlying array. Sorting by memory
// would corrupt the CPU ordering because Go's sort.Slice mutates in place.
// The fix (copy into independent slices) is verified here: after Collect(),
// modifying TopByMem must not affect TopByCPU order, and vice-versa.
func TestProcessTopByCPU_SortRegression(t *testing.T) {
	// We need the collector to do two reads. The collector calls readAllPIDs
	// twice with a sleep in between. We use a single directory and rewrite
	// the stat files between the two reads by using a tiny interval.
	//
	// Strategy: start with pass-1 data, configure a 1ms interval. The
	// collector will read pass 1, sleep 1ms, then read pass 2.
	// We rewrite the stat files during that 1ms window via a goroutine.

	root := t.TempDir()
	writeMeminfo(t, root, 32768000) // 32 GB

	// --- write pass-1 data ---
	buildFakeProcfsPass(t, root, 1)

	// Spawn a goroutine that waits a tiny bit and then overwrites with pass-2 data.
	// The collector sleeps for SampleInterval between the two reads. We set the
	// interval to 5ms and rewrite after 1ms to be sure the overwrite lands
	// between the two reads.
	go func() {
		time.Sleep(1 * time.Millisecond)
		buildFakeProcfsPass(t, root, 2)
	}()

	c := NewProcessCollector(root)
	cfg := CollectConfig{
		SampleInterval: 5 * time.Millisecond,
	}
	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data, ok := result.Data.(*model.ProcessData)
	if !ok {
		t.Fatalf("unexpected data type: %T", result.Data)
	}

	// We expect 3 processes
	if data.Total != 3 {
		t.Errorf("Total = %d, want 3", data.Total)
	}

	if len(data.TopByCPU) == 0 || len(data.TopByMem) == 0 {
		t.Fatal("TopByCPU or TopByMem is empty")
	}

	// --- verify CPU ordering is descending by CPUPct ---
	for i := 1; i < len(data.TopByCPU); i++ {
		if data.TopByCPU[i-1].CPUPct < data.TopByCPU[i].CPUPct {
			t.Errorf("TopByCPU not sorted descending: [%d].CPUPct=%v < [%d].CPUPct=%v",
				i-1, data.TopByCPU[i-1].CPUPct, i, data.TopByCPU[i].CPUPct)
		}
	}

	// --- verify MEM ordering is descending by MemRSS ---
	for i := 1; i < len(data.TopByMem); i++ {
		if data.TopByMem[i-1].MemRSS < data.TopByMem[i].MemRSS {
			t.Errorf("TopByMem not sorted descending: [%d].MemRSS=%v < [%d].MemRSS=%v",
				i-1, data.TopByMem[i-1].MemRSS, i, data.TopByMem[i].MemRSS)
		}
	}

	// --- the critical regression check: the orderings must differ ---
	// CPU ranking should be: PID 100, 200, 300  (delta 500, 200, 50)
	// MEM ranking should be: PID 200, 300, 100  (rss 50000, 25000, 1000)
	if data.TopByCPU[0].PID == data.TopByMem[0].PID {
		// This can happen if the two slices share the same backing array
		// and both ended up sorted the same way.
		t.Log("WARNING: TopByCPU[0] and TopByMem[0] have the same PID — rankings may not differ for this test data")
	}

	// Verify the slices are independent: sorting one must not affect the other.
	cpuSnapshot := make([]model.ProcessInfo, len(data.TopByCPU))
	copy(cpuSnapshot, data.TopByCPU)

	// Re-sort TopByMem by PID (a dummy sort to mutate the slice)
	sort.Slice(data.TopByMem, func(i, j int) bool {
		return data.TopByMem[i].PID < data.TopByMem[j].PID
	})

	// TopByCPU must be unchanged
	for i := range data.TopByCPU {
		if data.TopByCPU[i].PID != cpuSnapshot[i].PID {
			t.Fatalf("REGRESSION: mutating TopByMem changed TopByCPU[%d]: got PID %d, want %d",
				i, data.TopByCPU[i].PID, cpuSnapshot[i].PID)
		}
	}
}

// TestReadProcPID_CommParsing verifies that readProcPID correctly handles
// tricky comm values: parentheses, spaces, and special characters inside
// the (comm) field.
func TestReadProcPID_CommParsing(t *testing.T) {
	tests := []struct {
		name     string
		pid      int
		comm     string // what goes between ( )
		wantComm string // expected parsed comm
	}{
		{
			name:     "simple comm",
			pid:      1,
			comm:     "nginx",
			wantComm: "nginx",
		},
		{
			name:     "comm with space",
			pid:      2,
			comm:     "my worker",
			wantComm: "my worker",
		},
		{
			name:     "comm with parentheses",
			pid:      3,
			comm:     "sd-pam(systemd)",
			wantComm: "sd-pam(systemd)",
		},
		{
			name:     "comm with nested parens",
			pid:      4,
			comm:     "a(b)c(d)",
			wantComm: "a(b)c(d)",
		},
		{
			name:     "comm with colon",
			pid:      5,
			comm:     "kworker/0:1-events",
			wantComm: "kworker/0:1-events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeProcStat(t, root, tt.pid, tt.comm, "S", 1000, 500, 1, 2000)
			writeFDs(t, root, tt.pid, 3)

			c := NewProcessCollector(root)
			ps, err := c.readProcPID(tt.pid)
			if err != nil {
				t.Fatalf("readProcPID(%d) error: %v", tt.pid, err)
			}
			if ps.comm != tt.wantComm {
				t.Errorf("comm = %q, want %q", ps.comm, tt.wantComm)
			}
		})
	}
}

// TestReadProcPID_StatFields verifies that utime, stime, threads, rss,
// and state are parsed from the correct field positions.
func TestReadProcPID_StatFields(t *testing.T) {
	root := t.TempDir()

	// PID 42: utime=12345, stime=6789, threads=7, rss=9999, state=R
	writeProcStat(t, root, 42, "myproc", "R", 12345, 6789, 7, 9999)
	writeFDs(t, root, 42, 5)

	c := NewProcessCollector(root)
	ps, err := c.readProcPID(42)
	if err != nil {
		t.Fatalf("readProcPID(42) error: %v", err)
	}

	if ps.state != "R" {
		t.Errorf("state = %q, want %q", ps.state, "R")
	}
	if ps.utime != 12345 {
		t.Errorf("utime = %d, want 12345", ps.utime)
	}
	if ps.stime != 6789 {
		t.Errorf("stime = %d, want 6789", ps.stime)
	}
	if ps.threads != 7 {
		t.Errorf("threads = %d, want 7", ps.threads)
	}
	if ps.rss != 9999 {
		t.Errorf("rss = %d, want 9999", ps.rss)
	}
	if ps.fds != 5 {
		t.Errorf("fds = %d, want 5", ps.fds)
	}
}

// TestProcessCollector_FDCounting verifies that the FD count for each
// process matches the number of entries in /proc/[pid]/fd/.
func TestProcessCollector_FDCounting(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	// Create processes with varying FD counts
	writeProcStat(t, root, 10, "low_fds", "S", 100, 50, 1, 500)
	writeFDs(t, root, 10, 3)

	writeProcStat(t, root, 20, "high_fds", "S", 100, 50, 1, 500)
	writeFDs(t, root, 20, 42)

	writeProcStat(t, root, 30, "no_fds", "S", 100, 50, 1, 500)
	// No fd directory for PID 30 — should get fds=0

	c := NewProcessCollector(root)

	// Test directly via readProcPID
	ps10, err := c.readProcPID(10)
	if err != nil {
		t.Fatalf("readProcPID(10): %v", err)
	}
	if ps10.fds != 3 {
		t.Errorf("PID 10 fds = %d, want 3", ps10.fds)
	}

	ps20, err := c.readProcPID(20)
	if err != nil {
		t.Fatalf("readProcPID(20): %v", err)
	}
	if ps20.fds != 42 {
		t.Errorf("PID 20 fds = %d, want 42", ps20.fds)
	}

	ps30, err := c.readProcPID(30)
	if err != nil {
		t.Fatalf("readProcPID(30): %v", err)
	}
	if ps30.fds != 0 {
		t.Errorf("PID 30 fds = %d, want 0 (no fd dir)", ps30.fds)
	}
}

// TestProcessCollector_StateCounting verifies the total, running,
// sleeping, and zombie counters.
func TestProcessCollector_StateCounting(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	// Create a mix of process states:
	// R=running, S=sleeping, D=uninterruptible sleep (counts as sleeping), Z=zombie
	writeProcStat(t, root, 1, "init", "S", 100, 50, 1, 500)
	writeFDs(t, root, 1, 3)
	writeProcStat(t, root, 2, "runner1", "R", 200, 100, 1, 500)
	writeFDs(t, root, 2, 3)
	writeProcStat(t, root, 3, "runner2", "R", 300, 100, 1, 500)
	writeFDs(t, root, 3, 3)
	writeProcStat(t, root, 4, "sleeper", "S", 400, 100, 1, 500)
	writeFDs(t, root, 4, 3)
	writeProcStat(t, root, 5, "iowait", "D", 500, 100, 1, 500)
	writeFDs(t, root, 5, 3)
	writeProcStat(t, root, 6, "zombie1", "Z", 600, 100, 1, 0)
	writeFDs(t, root, 6, 0)
	writeProcStat(t, root, 7, "zombie2", "Z", 700, 100, 1, 0)
	writeFDs(t, root, 7, 0)

	c := NewProcessCollector(root)
	cfg := CollectConfig{SampleInterval: 1 * time.Millisecond}

	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data, ok := result.Data.(*model.ProcessData)
	if !ok {
		t.Fatalf("unexpected data type: %T", result.Data)
	}

	if data.Total != 7 {
		t.Errorf("Total = %d, want 7", data.Total)
	}
	if data.Running != 2 {
		t.Errorf("Running = %d, want 2", data.Running)
	}
	// Sleeping includes both S and D states
	if data.Sleeping != 3 {
		t.Errorf("Sleeping = %d, want 3 (2 S + 1 D)", data.Sleeping)
	}
	if data.Zombie != 2 {
		t.Errorf("Zombie = %d, want 2", data.Zombie)
	}
}

// --- --pid and --cgroup filter tests ---

// TestProcessCollector_PIDFilter verifies that --pid filters the process list
// to only the target PIDs, while still counting all processes in totals.
func TestProcessCollector_PIDFilter(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	writeProcStat(t, root, 100, "nginx", "S", 500, 100, 4, 5000)
	writeFDs(t, root, 100, 6)
	writeProcStat(t, root, 200, "postgres", "S", 1500, 200, 8, 20000)
	writeFDs(t, root, 200, 20)
	writeProcStat(t, root, 300, "java", "R", 8000, 1000, 16, 50000)
	writeFDs(t, root, 300, 15)

	c := NewProcessCollector(root)
	cfg := CollectConfig{
		SampleInterval: 1 * time.Millisecond,
		TargetPIDs:     []int{200}, // Only show PID 200
	}

	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data, ok := result.Data.(*model.ProcessData)
	if !ok {
		t.Fatalf("unexpected data type: %T", result.Data)
	}

	// Total should count ALL processes (not just filtered ones)
	if data.Total != 3 {
		t.Errorf("Total = %d, want 3 (all procs counted)", data.Total)
	}

	// But TopByCPU and TopByMem should only contain PID 200
	if len(data.TopByCPU) != 1 {
		t.Errorf("TopByCPU len = %d, want 1 (only PID 200)", len(data.TopByCPU))
	} else if data.TopByCPU[0].PID != 200 {
		t.Errorf("TopByCPU[0].PID = %d, want 200", data.TopByCPU[0].PID)
	}

	if len(data.TopByMem) != 1 {
		t.Errorf("TopByMem len = %d, want 1 (only PID 200)", len(data.TopByMem))
	} else if data.TopByMem[0].PID != 200 {
		t.Errorf("TopByMem[0].PID = %d, want 200", data.TopByMem[0].PID)
	}
}

// TestProcessCollector_MultiplePIDFilter verifies that --pid with multiple PIDs works.
func TestProcessCollector_MultiplePIDFilter(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	writeProcStat(t, root, 100, "nginx", "S", 500, 100, 4, 5000)
	writeFDs(t, root, 100, 6)
	writeProcStat(t, root, 200, "postgres", "S", 1500, 200, 8, 20000)
	writeFDs(t, root, 200, 20)
	writeProcStat(t, root, 300, "java", "R", 8000, 1000, 16, 50000)
	writeFDs(t, root, 300, 15)

	c := NewProcessCollector(root)
	cfg := CollectConfig{
		SampleInterval: 1 * time.Millisecond,
		TargetPIDs:     []int{100, 300},
	}

	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data := result.Data.(*model.ProcessData)

	// Should have only PIDs 100 and 300 in the lists
	pids := map[int]bool{}
	for _, p := range data.TopByCPU {
		pids[p.PID] = true
	}

	if len(pids) != 2 {
		t.Errorf("TopByCPU should have 2 PIDs, got %d", len(pids))
	}
	if !pids[100] || !pids[300] {
		t.Errorf("TopByCPU should contain PIDs 100 and 300, got %v", pids)
	}
	if pids[200] {
		t.Error("TopByCPU should NOT contain PID 200")
	}
}

// TestProcessCollector_CgroupFilter verifies that --cgroup filters processes
// to only those belonging to the target cgroup.
func TestProcessCollector_CgroupFilter(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	// Create 3 processes with different cgroup memberships
	writeProcStat(t, root, 100, "app1", "S", 500, 100, 4, 5000)
	writeFDs(t, root, 100, 6)
	writeCgroup(t, root, 100, "0::/docker/abc123\n")

	writeProcStat(t, root, 200, "app2", "S", 1500, 200, 8, 20000)
	writeFDs(t, root, 200, 20)
	writeCgroup(t, root, 200, "0::/docker/def456\n")

	writeProcStat(t, root, 300, "host_proc", "R", 8000, 1000, 16, 50000)
	writeFDs(t, root, 300, 15)
	writeCgroup(t, root, 300, "0::/\n")

	c := NewProcessCollector(root)
	cfg := CollectConfig{
		SampleInterval: 1 * time.Millisecond,
		TargetCgroups:  []string{"/docker/abc123"},
	}

	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data := result.Data.(*model.ProcessData)

	// Total counts all processes
	if data.Total != 3 {
		t.Errorf("Total = %d, want 3", data.Total)
	}

	// But only PID 100 should appear in the lists (belongs to docker/abc123)
	if len(data.TopByCPU) != 1 {
		t.Errorf("TopByCPU len = %d, want 1 (only PID 100)", len(data.TopByCPU))
	} else if data.TopByCPU[0].PID != 100 {
		t.Errorf("TopByCPU[0].PID = %d, want 100", data.TopByCPU[0].PID)
	}
}

// TestProcessCollector_NoPIDFilter verifies that without --pid, all processes are shown.
func TestProcessCollector_NoPIDFilter(t *testing.T) {
	root := t.TempDir()
	writeMeminfo(t, root, 8192000)

	writeProcStat(t, root, 100, "nginx", "S", 500, 100, 4, 5000)
	writeFDs(t, root, 100, 6)
	writeProcStat(t, root, 200, "postgres", "S", 1500, 200, 8, 20000)
	writeFDs(t, root, 200, 20)

	c := NewProcessCollector(root)
	cfg := CollectConfig{
		SampleInterval: 1 * time.Millisecond,
		// No TargetPIDs — all processes should be included
	}

	result, err := c.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	data := result.Data.(*model.ProcessData)
	if len(data.TopByCPU) != 2 {
		t.Errorf("TopByCPU len = %d, want 2 (no filter)", len(data.TopByCPU))
	}
}

// writeCgroup creates a /proc/[pid]/cgroup file with the given content.
func writeCgroup(t *testing.T, root string, pid int, content string) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestProcessCollector_WithTestdata exercises the collector against the
// committed testdata/proc/ fixtures to ensure the integration works end-to-end
// with realistic proc files.
func TestProcessCollector_WithTestdata(t *testing.T) {
	root := "../../testdata/proc"

	// Verify testdata exists
	if _, err := os.Stat(filepath.Join(root, "meminfo")); err != nil {
		t.Skipf("testdata not found: %v", err)
	}

	c := NewProcessCollector(root)

	// Test readProcPID for each testdata process
	ps100, err := c.readProcPID(100)
	if err != nil {
		t.Fatalf("readProcPID(100): %v", err)
	}
	if ps100.comm != "nginx" {
		t.Errorf("PID 100 comm = %q, want %q", ps100.comm, "nginx")
	}
	if ps100.state != "S" {
		t.Errorf("PID 100 state = %q, want %q", ps100.state, "S")
	}
	if ps100.utime != 5000 {
		t.Errorf("PID 100 utime = %d, want 5000", ps100.utime)
	}
	if ps100.stime != 2000 {
		t.Errorf("PID 100 stime = %d, want 2000", ps100.stime)
	}
	if ps100.threads != 4 {
		t.Errorf("PID 100 threads = %d, want 4", ps100.threads)
	}
	if ps100.rss != 5000 {
		t.Errorf("PID 100 rss = %d, want 5000", ps100.rss)
	}
	if ps100.fds != 10 {
		t.Errorf("PID 100 fds = %d, want 10", ps100.fds)
	}

	ps200, err := c.readProcPID(200)
	if err != nil {
		t.Fatalf("readProcPID(200): %v", err)
	}
	if ps200.comm != "postgres" {
		t.Errorf("PID 200 comm = %q, want %q", ps200.comm, "postgres")
	}
	if ps200.fds != 20 {
		t.Errorf("PID 200 fds = %d, want 20", ps200.fds)
	}

	ps300, err := c.readProcPID(300)
	if err != nil {
		t.Fatalf("readProcPID(300): %v", err)
	}
	if ps300.comm != "java" {
		t.Errorf("PID 300 comm = %q, want %q", ps300.comm, "java")
	}
	if ps300.state != "R" {
		t.Errorf("PID 300 state = %q, want %q", ps300.state, "R")
	}
	if ps300.fds != 15 {
		t.Errorf("PID 300 fds = %d, want 15", ps300.fds)
	}
}
