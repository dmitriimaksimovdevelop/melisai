package observer

import (
	"testing"
)

func TestParseProcStat(t *testing.T) {
	// Realistic /proc/self/stat content
	content := "12345 (sysdiag) S 1 12345 12345 0 -1 4194560 1000 0 0 0 500 200 0 0 20 0 27 0 0 0 8192" +
		" 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0"

	snap := parseProcStat(content)

	if snap.utime != 500 {
		t.Errorf("utime = %d, want 500", snap.utime)
	}
	if snap.stime != 200 {
		t.Errorf("stime = %d, want 200", snap.stime)
	}
	if snap.rss != 8192 {
		t.Errorf("rss = %d, want 8192", snap.rss)
	}
}

func TestParseProcStat_CommWithParens(t *testing.T) {
	content := "42 (sd-pam(systemd)) S 1 42 42 0 -1 0 0 0 0 0 100 50 0 0 20 0 1 0 0 0 4096" +
		" 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0"

	snap := parseProcStat(content)

	if snap.utime != 100 {
		t.Errorf("utime = %d, want 100", snap.utime)
	}
	if snap.stime != 50 {
		t.Errorf("stime = %d, want 50", snap.stime)
	}
}

func TestParseProcStat_Malformed(t *testing.T) {
	snap := parseProcStat("garbage data")
	if snap.utime != 0 || snap.stime != 0 || snap.rss != 0 {
		t.Errorf("malformed stat should return zeros, got %+v", snap)
	}
}

func TestParseProcIO(t *testing.T) {
	content := `rchar: 12345678
wchar: 87654321
syscr: 1000
syscw: 2000
read_bytes: 4096000
write_bytes: 2048000
cancelled_write_bytes: 0
`
	r, w := parseProcIO(content)

	if r != 4096000 {
		t.Errorf("read_bytes = %d, want 4096000", r)
	}
	if w != 2048000 {
		t.Errorf("write_bytes = %d, want 2048000", w)
	}
}

func TestParseProcIO_Empty(t *testing.T) {
	r, w := parseProcIO("")
	if r != 0 || w != 0 {
		t.Errorf("empty io should return zeros, got read=%d write=%d", r, w)
	}
}

func TestParseProcStatus(t *testing.T) {
	content := `Name:	sysdiag
Umask:	0022
State:	S (sleeping)
Tgid:	12345
Pid:	12345
Threads:	27
voluntary_ctxt_switches:	500
nonvoluntary_ctxt_switches:	100
`
	vol, nonvol := parseProcStatus(content)

	if vol != 500 {
		t.Errorf("voluntary = %d, want 500", vol)
	}
	if nonvol != 100 {
		t.Errorf("nonvoluntary = %d, want 100", nonvol)
	}
}

func TestParseProcStatus_Missing(t *testing.T) {
	vol, nonvol := parseProcStatus("Name:\ttest\nState:\tR\n")
	if vol != 0 || nonvol != 0 {
		t.Errorf("missing fields should return zeros, got vol=%d nonvol=%d", vol, nonvol)
	}
}

func TestTicksToMs(t *testing.T) {
	// 100 ticks at 100 Hz = 1000 ms
	if got := ticksToMs(100); got != 1000 {
		t.Errorf("ticksToMs(100) = %d, want 1000", got)
	}
	if got := ticksToMs(0); got != 0 {
		t.Errorf("ticksToMs(0) = %d, want 0", got)
	}
	if got := ticksToMs(1); got != 10 {
		t.Errorf("ticksToMs(1) = %d, want 10", got)
	}
}

func TestSnapshotBeforeAfter_NilSafe(t *testing.T) {
	tracker := NewPIDTracker()

	// SnapshotAfter without SnapshotBefore should return safe defaults
	summary := tracker.SnapshotAfter()

	if summary.SelfPID != tracker.SelfPID() {
		t.Errorf("SelfPID = %d, want %d", summary.SelfPID, tracker.SelfPID())
	}
	// All values should be zero since no snapshot was taken
	if summary.CPUUserMs != 0 || summary.CPUSystemMs != 0 {
		t.Errorf("expected zero CPU values without SnapshotBefore, got user=%d sys=%d",
			summary.CPUUserMs, summary.CPUSystemMs)
	}
}
