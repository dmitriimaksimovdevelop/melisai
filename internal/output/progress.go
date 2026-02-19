// Package output handles report serialization and progress reporting.
package output

import (
	"fmt"
	"os"
	"time"
)

// Progress reports collection status to stderr.
type Progress struct {
	enabled bool
	verbose bool
	start   time.Time
}

// NewProgress creates a Progress reporter. Set enabled=false for --quiet mode.
func NewProgress(enabled bool) *Progress {
	return &Progress{
		enabled: enabled,
		start:   time.Now(),
	}
}

// NewVerboseProgress creates a Progress reporter with debug logging enabled.
func NewVerboseProgress(enabled, verbose bool) *Progress {
	return &Progress{
		enabled: enabled || verbose, // verbose implies enabled
		verbose: verbose,
		start:   time.Now(),
	}
}

// Log prints a progress message to stderr if enabled.
func (p *Progress) Log(format string, args ...interface{}) {
	if !p.enabled {
		return
	}
	elapsed := time.Since(p.start).Round(time.Millisecond)
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[%s] %s\n", elapsed, msg)
}

// Debug prints a debug message to stderr if verbose is enabled.
func (p *Progress) Debug(format string, args ...interface{}) {
	if !p.verbose {
		return
	}
	elapsed := time.Since(p.start).Round(time.Millisecond)
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[%s] DEBUG: %s\n", elapsed, msg)
}
