package observer

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// OverheadSummary captures melisai's own resource consumption during collection.
type OverheadSummary struct {
	SelfPID         int   `json:"self_pid"`
	ChildPIDs       []int `json:"child_pids"`
	CPUUserMs       int64 `json:"cpu_user_ms"`
	CPUSystemMs     int64 `json:"cpu_system_ms"`
	MemoryRSSBytes  int64 `json:"memory_rss_bytes"`
	DiskReadBytes   int64 `json:"disk_read_bytes"`
	DiskWriteBytes  int64 `json:"disk_write_bytes"`
	ContextSwitches int64 `json:"context_switches"`
}

// procSnapshot holds raw values from /proc/[pid]/stat and /proc/[pid]/io.
type procSnapshot struct {
	utime          uint64 // in clock ticks
	stime          uint64
	rss            int64 // in pages
	voluntaryCtxSw int64
	nonvolCtxSw    int64
	readBytes      int64
	writeBytes     int64
}

// beforeSnapshot stores the initial readings for delta calculation.
type beforeSnapshot struct {
	self     procSnapshot
	children map[int]procSnapshot
}

// SnapshotBefore records the current resource usage of melisai and its children.
// Call this before starting collectors.
func (t *PIDTracker) SnapshotBefore() {
	t.mu.Lock()
	defer t.mu.Unlock()

	snap := &beforeSnapshot{
		self:     readProcSnapshot(t.selfPID),
		children: make(map[int]procSnapshot),
	}
	for pid := range t.children {
		snap.children[pid] = readProcSnapshot(pid)
	}
	t.before = snap
}

// SnapshotAfter reads current resource usage and computes the delta
// since SnapshotBefore. Returns a summary of melisai's overhead.
func (t *PIDTracker) SnapshotAfter() OverheadSummary {
	t.mu.RLock()
	before := t.before
	childPIDs := make([]int, 0, len(t.children))
	for pid := range t.children {
		childPIDs = append(childPIDs, pid)
	}
	t.mu.RUnlock()

	summary := OverheadSummary{
		SelfPID:   t.selfPID,
		ChildPIDs: childPIDs,
	}

	if before == nil {
		return summary
	}

	// Self process delta
	selfNow := readProcSnapshot(t.selfPID)
	summary.CPUUserMs = ticksToMs(selfNow.utime - before.self.utime)
	summary.CPUSystemMs = ticksToMs(selfNow.stime - before.self.stime)
	summary.MemoryRSSBytes = selfNow.rss * 4096
	summary.ContextSwitches = (selfNow.voluntaryCtxSw - before.self.voluntaryCtxSw) +
		(selfNow.nonvolCtxSw - before.self.nonvolCtxSw)
	summary.DiskReadBytes = selfNow.readBytes - before.self.readBytes
	summary.DiskWriteBytes = selfNow.writeBytes - before.self.writeBytes

	// Add children overhead
	for _, pid := range childPIDs {
		childNow := readProcSnapshot(pid)
		beforeChild, ok := before.children[pid]
		if !ok {
			// Child started after SnapshotBefore — use its full values
			beforeChild = procSnapshot{}
		}
		summary.CPUUserMs += ticksToMs(childNow.utime - beforeChild.utime)
		summary.CPUSystemMs += ticksToMs(childNow.stime - beforeChild.stime)
		summary.MemoryRSSBytes += childNow.rss * 4096
		summary.ContextSwitches += (childNow.voluntaryCtxSw - beforeChild.voluntaryCtxSw) +
			(childNow.nonvolCtxSw - beforeChild.nonvolCtxSw)
		summary.DiskReadBytes += childNow.readBytes - beforeChild.readBytes
		summary.DiskWriteBytes += childNow.writeBytes - beforeChild.writeBytes
	}

	return summary
}

// ticksToMs converts clock ticks (typically 100 Hz) to milliseconds.
func ticksToMs(ticks uint64) int64 {
	// SC_CLK_TCK is 100 on virtually all Linux systems
	return int64(ticks) * 10
}

// readProcSnapshot reads /proc/[pid]/stat and /proc/[pid]/io for the given PID.
// Returns zero values if the process no longer exists (race-safe).
func readProcSnapshot(pid int) procSnapshot {
	var snap procSnapshot

	// Read /proc/[pid]/stat
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return snap
	}
	snap = parseProcStat(string(statData))

	// Read /proc/[pid]/io (may require same-user or root)
	ioData, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
	if err != nil {
		return snap // stat data is still useful
	}
	snap.readBytes, snap.writeBytes = parseProcIO(string(ioData))

	// Read /proc/[pid]/status for context switches
	statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return snap
	}
	snap.voluntaryCtxSw, snap.nonvolCtxSw = parseProcStatus(string(statusData))

	return snap
}

// parseProcStat extracts utime, stime, rss from /proc/[pid]/stat content.
func parseProcStat(content string) procSnapshot {
	var snap procSnapshot

	// Find end of comm field: last ")" in the line
	commEnd := strings.LastIndex(content, ")")
	if commEnd < 0 || commEnd+2 >= len(content) {
		return snap
	}

	fields := strings.Fields(content[commEnd+2:])
	// fields[0]=state, fields[11]=utime, fields[12]=stime, fields[21]=rss
	if len(fields) > 12 {
		snap.utime, _ = strconv.ParseUint(fields[11], 10, 64)
		snap.stime, _ = strconv.ParseUint(fields[12], 10, 64)
	}
	if len(fields) > 21 {
		snap.rss, _ = strconv.ParseInt(fields[21], 10, 64)
	}

	return snap
}

// parseProcIO extracts read_bytes and write_bytes from /proc/[pid]/io.
func parseProcIO(content string) (readBytes, writeBytes int64) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.SplitN(line, ": ", 2)
		if len(fields) != 2 {
			continue
		}
		val, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		switch fields[0] {
		case "read_bytes":
			readBytes = val
		case "write_bytes":
			writeBytes = val
		}
	}
	return
}

// parseProcStatus extracts voluntary/nonvoluntary context switches from /proc/[pid]/status.
func parseProcStatus(content string) (voluntary, nonvoluntary int64) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.SplitN(line, ":\t", 2)
		if len(fields) != 2 {
			continue
		}
		val, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		switch fields[0] {
		case "voluntary_ctxt_switches":
			voluntary = val
		case "nonvoluntary_ctxt_switches":
			nonvoluntary = val
		}
	}
	return
}
