// Package collector defines the Collector interface
// that all metric sources (Tier 1, 2, 3) must implement.
package collector

import (
	"context"
	"os/exec"
	"time"

	"github.com/baikal/sysdiag/internal/model"
	"github.com/baikal/sysdiag/internal/observer"
)

// CommandRunner abstracts external command execution for testability.
type CommandRunner interface {
	// Run executes a command and returns its combined output.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecCommandRunner is the default CommandRunner using os/exec.
type ExecCommandRunner struct{}

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Collector gathers metrics from a specific source.
// Each collector is responsible for a single metric domain.
type Collector interface {
	// Name returns a unique identifier, e.g. "cpu_utilization".
	Name() string

	// Category returns the metric group: "cpu", "memory", "disk", "network",
	// "stacktrace", "process", "system", "container".
	Category() string

	// Collect runs the collection with the given config and returns a Result.
	// The context carries the deadline/timeout.
	Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error)

	// Available reports whether this collector can run and at which tier.
	Available() Availability
}

// Availability describes whether a collector can run and why.
type Availability struct {
	Tier   int    // 1, 2, or 3. 0 = unavailable.
	Reason string // why unavailable, e.g. "bcc not installed"
}

// CollectConfig is passed to every collector.
type CollectConfig struct {
	// Duration is how long to collect data (profile-dependent).
	Duration time.Duration

	// SampleInterval is the interval between delta reads (default 1s).
	// Used by CPU/disk/network collectors for two-point sampling.
	SampleInterval time.Duration

	// Profile is the collection profile: "quick", "standard", "deep".
	Profile string

	// Focus extends sampling duration for specific areas.
	Focus []string

	// TargetPIDs filters collection to specific processes.
	TargetPIDs []int

	// TargetCgroups filters to specific cgroup paths (container-aware).
	TargetCgroups []string

	// MaxEventsPerCollector caps event count per collector (default 1000).
	// Prevents memory explosion on busy systems.
	MaxEventsPerCollector int

	// Quiet suppresses progress output.
	Quiet bool

	// ProcRoot is the path to procfs mount (default "/proc").
	// Can be overridden for testing.
	ProcRoot string

	// SysRoot is the path to sysfs mount (default "/sys").
	SysRoot string

	// PIDTracker tracks sysdiag's own PID and child BCC tool PIDs
	// for observer-effect mitigation. May be nil if not configured.
	PIDTracker *observer.PIDTracker
}

// DefaultConfig returns a CollectConfig with sensible defaults.
func DefaultConfig() CollectConfig {
	return CollectConfig{
		Duration:              30 * time.Second,
		SampleInterval:        1 * time.Second,
		Profile:               "standard",
		MaxEventsPerCollector: 1000,
		ProcRoot:              "/proc",
		SysRoot:               "/sys",
	}
}
