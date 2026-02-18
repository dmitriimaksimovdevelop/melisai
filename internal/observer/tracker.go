// Package observer provides observer-effect mitigation for sysdiag.
// It tracks sysdiag's own PID and all spawned BCC tool PIDs so that
// collectors can exclude self-generated noise from metrics.
package observer

import (
	"os"
	"sync"
)

// PIDTracker is a thread-safe registry of sysdiag's own PID and
// all child BCC tool PIDs. Collectors use it to filter self-noise
// from process lists, event streams, and stack traces.
type PIDTracker struct {
	mu       sync.RWMutex
	selfPID  int
	children map[int]string   // pid → tool name
	before   *beforeSnapshot  // set by SnapshotBefore()
}

// NewPIDTracker creates a PIDTracker seeded with the current process PID.
func NewPIDTracker() *PIDTracker {
	return &PIDTracker{
		selfPID:  os.Getpid(),
		children: make(map[int]string),
	}
}

// SelfPID returns sysdiag's own process ID.
func (t *PIDTracker) SelfPID() int {
	return t.selfPID
}

// Add registers a child process PID with its tool name.
func (t *PIDTracker) Add(pid int, tool string) {
	t.mu.Lock()
	t.children[pid] = tool
	t.mu.Unlock()
}

// Remove unregisters a child process PID.
func (t *PIDTracker) Remove(pid int) {
	t.mu.Lock()
	delete(t.children, pid)
	t.mu.Unlock()
}

// IsOwnPID returns true if pid is sysdiag itself or any tracked child.
func (t *PIDTracker) IsOwnPID(pid int) bool {
	if pid == t.selfPID {
		return true
	}
	t.mu.RLock()
	_, ok := t.children[pid]
	t.mu.RUnlock()
	return ok
}

// AllPIDs returns sysdiag's PID plus all currently tracked child PIDs.
func (t *PIDTracker) AllPIDs() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	pids := make([]int, 0, 1+len(t.children))
	pids = append(pids, t.selfPID)
	for pid := range t.children {
		pids = append(pids, pid)
	}
	return pids
}

// ChildCount returns the number of currently tracked child PIDs.
func (t *PIDTracker) ChildCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.children)
}
