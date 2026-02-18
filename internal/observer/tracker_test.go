package observer

import (
	"os"
	"sort"
	"sync"
	"testing"
)

func TestNewPIDTracker(t *testing.T) {
	tracker := NewPIDTracker()

	if tracker.SelfPID() != os.Getpid() {
		t.Errorf("SelfPID() = %d, want %d", tracker.SelfPID(), os.Getpid())
	}
	if tracker.ChildCount() != 0 {
		t.Errorf("ChildCount() = %d, want 0", tracker.ChildCount())
	}
}

func TestPIDTracker_AddRemove(t *testing.T) {
	tracker := NewPIDTracker()

	tracker.Add(1000, "runqlat")
	tracker.Add(1001, "biolatency")

	if tracker.ChildCount() != 2 {
		t.Errorf("ChildCount() = %d, want 2", tracker.ChildCount())
	}

	if !tracker.IsOwnPID(1000) {
		t.Error("IsOwnPID(1000) = false, want true")
	}
	if !tracker.IsOwnPID(1001) {
		t.Error("IsOwnPID(1001) = false, want true")
	}

	tracker.Remove(1000)
	if tracker.IsOwnPID(1000) {
		t.Error("IsOwnPID(1000) = true after Remove, want false")
	}
	if tracker.ChildCount() != 1 {
		t.Errorf("ChildCount() = %d after Remove, want 1", tracker.ChildCount())
	}
}

func TestPIDTracker_IsOwnPID(t *testing.T) {
	tracker := NewPIDTracker()
	tracker.Add(2000, "execsnoop")

	// Self PID is always own
	if !tracker.IsOwnPID(tracker.SelfPID()) {
		t.Error("self PID should be own")
	}

	// Child is own
	if !tracker.IsOwnPID(2000) {
		t.Error("child PID should be own")
	}

	// Unknown PID is not own
	if tracker.IsOwnPID(99999) {
		t.Error("unknown PID should not be own")
	}
}

func TestPIDTracker_AllPIDs(t *testing.T) {
	tracker := NewPIDTracker()
	tracker.Add(3000, "profile")
	tracker.Add(3001, "offcputime")

	pids := tracker.AllPIDs()

	if len(pids) != 3 {
		t.Fatalf("AllPIDs() returned %d PIDs, want 3", len(pids))
	}

	sort.Ints(pids)
	selfPID := tracker.SelfPID()

	found := false
	for _, pid := range pids {
		if pid == selfPID {
			found = true
			break
		}
	}
	if !found {
		t.Error("AllPIDs() should include self PID")
	}
}

func TestPIDTracker_Concurrent(t *testing.T) {
	tracker := NewPIDTracker()
	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			tracker.Add(pid, "tool")
			tracker.IsOwnPID(pid)
		}(5000 + i)
	}
	wg.Wait()

	if tracker.ChildCount() != 100 {
		t.Errorf("ChildCount() = %d after concurrent adds, want 100", tracker.ChildCount())
	}

	// Concurrent removes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			tracker.Remove(pid)
		}(5000 + i)
	}
	wg.Wait()

	if tracker.ChildCount() != 0 {
		t.Errorf("ChildCount() = %d after concurrent removes, want 0", tracker.ChildCount())
	}
}
