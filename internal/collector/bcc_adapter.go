package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/executor"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
	"github.com/dmitriimaksimovdevelop/melisai/internal/observer"
)

// BCCCollector adapts the executor.BCCExecutor to the Collector interface.
type BCCCollector struct {
	toolName string
	category string
	executor *executor.BCCExecutor
}

// NewBCCCollector creates a new collector for a specific BCC tool.
func NewBCCCollector(toolName string, exec *executor.BCCExecutor) (*BCCCollector, error) {
	spec, ok := executor.Registry[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown BCC tool: %s", toolName)
	}

	return &BCCCollector{
		toolName: toolName,
		category: spec.Category,
		executor: exec,
	}, nil
}

func (c *BCCCollector) Name() string {
	return c.toolName
}

func (c *BCCCollector) Category() string {
	return c.category
}

func (c *BCCCollector) Available() Availability {
	// Check if the binary exists using the executor's Available method
	if !c.executor.Available(c.toolName) {
		return Availability{
			Tier:   0,
			Reason: fmt.Sprintf("BCC tool '%s' not found", c.toolName),
		}
	}
	return Availability{
		Tier: 2,
	}
}

func (c *BCCCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	spec, ok := executor.Registry[c.toolName]
	if !ok {
		return nil, fmt.Errorf("tool %s not in registry", c.toolName)
	}

	// Determine duration
	duration := cfg.Duration
	// Some tools need a minimum duration
	if duration < 5*time.Second && (c.toolName == "profile" || c.toolName == "offcputime") {
		duration = 5 * time.Second
	}

	// Create a sub-context with the tool's own deadline.
	// This ensures event-tracing tools (oomkill, opensnoop, etc.) that don't
	// accept a duration argument are killed after the collection period,
	// rather than waiting for the orchestrator's full timeout buffer.
	toolCtx, toolCancel := context.WithTimeout(ctx, duration+5*time.Second)
	defer toolCancel()

	// Build arguments
	args := spec.BuildArgs(duration)

	// Run executor
	raw, err := c.executor.Run(toolCtx, c.toolName, args, duration)
	if err != nil {
		return nil, err
	}

	// Register child PID with tracker (if available)
	var tracker *observer.PIDTracker
	if cfg.PIDTracker != nil {
		tracker = cfg.PIDTracker
	}
	if tracker != nil && raw.PID > 0 {
		tracker.Add(raw.PID, c.toolName)
		defer tracker.Remove(raw.PID)
	}

	// Parse output
	if spec.Parser == nil {
		return nil, fmt.Errorf("no parser defined for %s", c.toolName)
	}

	result, err := spec.Parser(raw.Stdout)
	if err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}

	// Enrich result with metadata
	result.Collector = c.toolName
	result.Category = c.category
	result.Tier = 2
	result.StartTime = time.Now().Add(-raw.Duration)
	result.EndTime = time.Now()

	// Filter self-generated events from tabular output
	if tracker != nil && len(result.Events) > 0 {
		filtered := make([]model.Event, 0, len(result.Events))
		for _, ev := range result.Events {
			if ev.PID != 0 && tracker.IsOwnPID(ev.PID) {
				continue
			}
			filtered = append(filtered, ev)
		}
		result.Events = filtered
	}

	return result, nil
}
