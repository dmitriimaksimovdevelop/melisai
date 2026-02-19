// Package executor handles running external BCC/bpftrace tools securely.
package executor

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
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

// gracefulShutdownTimeout is how long we wait after SIGINT before sending SIGKILL.
const gracefulShutdownTimeout = 3 * time.Second

// Run executes a BCC tool with security verification and output capping.
// It uses exec.Command (not CommandContext) and implements graceful shutdown:
// when the context is cancelled, SIGINT is sent to the process group first so
// BCC Python tools can flush their buffered histogram/event output before
// terminating.  If the process has not exited after gracefulShutdownTimeout,
// SIGKILL is sent as a fallback.
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

	// Use exec.Command (not CommandContext) so we control the signal sequence.
	// SysProcAttr.Setpgid creates a new process group so SIGINT reaches the
	// entire BCC Python interpreter tree (parent + any child processes).
	cmd := exec.Command(binPath, args...)
	cmd.Env = e.security.SanitizeEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &LimitedWriter{W: &stdout, N: e.maxOutputBytes}
	cmd.Stderr = &stderr

	if e.auditLog {
		fmt.Fprintf(&stderr, "[AUDIT] exec: %s %s\n", binPath, strings.Join(args, " "))
	}

	// Use Start+Wait instead of Run to capture the child PID.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", tool, err)
	}

	raw := &RawOutput{
		PID: cmd.Process.Pid,
	}

	// done receives the error from cmd.Wait() when the child exits.
	// exited is closed once done has been written, allowing multiple goroutines
	// to observe process exit without consuming the error value.
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		done <- err
		close(exited)
	}()

	// Watch for context cancellation and implement SIGINT -> wait -> SIGKILL.
	go func() {
		select {
		case <-ctx.Done():
			// Send SIGINT to the entire process group so BCC Python tools flush
			// their buffered histogram/event output before terminating.
			pgid := cmd.Process.Pid
			if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
				// If sending to the group fails (e.g. already exited), try the
				// process directly -- ignore errors since it may have already gone.
				_ = cmd.Process.Signal(syscall.SIGINT)
			}

			// Give the process up to gracefulShutdownTimeout to flush & exit.
			select {
			case <-exited:
				// Exited cleanly after SIGINT -- output is available.
			case <-time.After(gracefulShutdownTimeout):
				// Timed out; escalate to SIGKILL.
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				_ = cmd.Process.Signal(os.Kill)
			}
		case <-exited:
			// Process exited on its own before context was cancelled.
		}
	}()

	// Wait for the child to finish (either naturally or after signal handling).
	// Only this goroutine receives from done; the signal goroutine uses exited.
	waitErr := <-done

	raw.Stdout = stdout.String()
	raw.Stderr = stderr.String()
	raw.Duration = time.Since(start)

	if cmd.ProcessState != nil {
		raw.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Check if output was truncated.
	if lw, ok := cmd.Stdout.(*LimitedWriter); ok && lw.Truncated {
		raw.Truncated = true
	}

	// Diagnostic hint: empty stdout but non-empty stderr is unusual and often
	// indicates the process was killed before it could flush output.
	if len(raw.Stdout) == 0 && len(raw.Stderr) > 0 {
		log.Printf("[executor] %s: stdout empty, stderr=%q -- tool may have been killed before flushing output",
			tool, raw.Stderr)
	}

	// Context errors (timeout/cancel) take priority; partial output is fine.
	if ctx.Err() != nil {
		return raw, nil
	}

	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); ok {
			return raw, nil
		}
		return nil, fmt.Errorf("execute %s: %w", tool, waitErr)
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
		// Return len(p) to satisfy exec.Cmd which expects all bytes consumed.
		// The Truncated flag signals that data was discarded.
		return len(p), nil
	}
	remaining := lw.N - lw.written
	if int64(len(p)) > remaining {
		n, err := lw.W.Write(p[:remaining])
		lw.written += int64(n)
		lw.Truncated = true
		// Return original len to avoid broken pipe from exec.Cmd
		return len(p), err
	}
	n, err := lw.W.Write(p)
	lw.written += int64(n)
	return n, err
}
