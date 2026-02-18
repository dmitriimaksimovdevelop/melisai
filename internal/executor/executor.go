// Package executor handles running external BCC/bpftrace tools securely.
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RawOutput captures the stdout/stderr from an external tool.
type RawOutput struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Duration  time.Duration
	Truncated bool // true if output was capped
	PID       int  // OS process ID of the spawned tool
}

// Executor runs external tools and captures their output.
type Executor interface {
	Run(ctx context.Context, tool string, args []string, duration time.Duration) (*RawOutput, error)
	Available(tool string) bool
}

// BCCExecutor runs BCC tools with security controls.
type BCCExecutor struct {
	security       *SecurityChecker
	maxOutputBytes int64 // default 50MB
	auditLog       bool
}

// NewBCCExecutor creates a new BCC executor with security controls.
func NewBCCExecutor(auditLog bool) *BCCExecutor {
	return &BCCExecutor{
		security:       NewSecurityChecker(),
		maxOutputBytes: 50 * 1024 * 1024, // 50MB
		auditLog:       auditLog,
	}
}

// Run executes a BCC tool with security verification and output capping.
func (e *BCCExecutor) Run(ctx context.Context, tool string, args []string, duration time.Duration) (*RawOutput, error) {
	start := time.Now()

	// Resolve binary path
	binPath, err := e.security.ResolveBinary(tool)
	if err != nil {
		return nil, fmt.Errorf("security check for %q: %w", tool, err)
	}

	// Verify binary
	if err := e.security.VerifyBinary(binPath); err != nil {
		return nil, fmt.Errorf("binary verification for %q: %w", binPath, err)
	}

	// Build command with sanitized environment
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = e.security.SanitizeEnv()

	var stdout, stderr bytes.Buffer
	// Use a limited writer to cap output
	cmd.Stdout = &LimitedWriter{W: &stdout, N: e.maxOutputBytes}
	cmd.Stderr = &stderr

	if e.auditLog {
		fmt.Fprintf(&stderr, "[AUDIT] exec: %s %s\n", binPath, strings.Join(args, " "))
	}

	// Use Start+Wait instead of Run to capture the child PID
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", tool, err)
	}

	raw := &RawOutput{
		PID: cmd.Process.Pid,
	}

	err = cmd.Wait()

	raw.Stdout = stdout.String()
	raw.Stderr = stderr.String()
	raw.Duration = time.Since(start)

	if cmd.ProcessState != nil {
		raw.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Check if output was truncated
	if lw, ok := cmd.Stdout.(*LimitedWriter); ok && lw.Truncated {
		raw.Truncated = true
	}

	// Context errors (timeout) take priority
	if ctx.Err() != nil {
		return raw, nil // partial output is fine on timeout
	}

	if err != nil {
		// ExitError is expected for tools that are killed by timeout
		if _, ok := err.(*exec.ExitError); ok {
			return raw, nil
		}
		return nil, fmt.Errorf("execute %s: %w", tool, err)
	}

	return raw, nil
}

// Available checks if a BCC tool binary exists in allowed paths.
func (e *BCCExecutor) Available(tool string) bool {
	_, err := e.security.ResolveBinary(tool)
	return err == nil
}

// LimitedWriter wraps a writer with a byte limit.
type LimitedWriter struct {
	W         *bytes.Buffer
	N         int64
	written   int64
	Truncated bool
}

func (lw *LimitedWriter) Write(p []byte) (int, error) {
	if lw.written >= lw.N {
		lw.Truncated = true
		return len(p), nil // silently discard
	}
	remaining := lw.N - lw.written
	if int64(len(p)) > remaining {
		p = p[:remaining]
		lw.Truncated = true
	}
	n, err := lw.W.Write(p)
	lw.written += int64(n)
	return n, err
}
