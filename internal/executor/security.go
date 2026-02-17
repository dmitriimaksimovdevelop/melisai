package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// AllowedBinaryPaths are the directories where BCC/bpftrace tools are expected.
var AllowedBinaryPaths = []string{
	"/usr/share/bcc/tools",
	"/usr/sbin",
	"/usr/bin",
	"/usr/local/bin",
	"/usr/local/sbin",
	"/usr/share/bcc/tools/old", // some distros
	"/snap/bin",
}

// SecurityChecker verifies binary integrity and sanitizes execution environment.
type SecurityChecker struct {
	allowedPaths []string
}

// NewSecurityChecker creates a SecurityChecker with default allowed paths.
func NewSecurityChecker() *SecurityChecker {
	return &SecurityChecker{
		allowedPaths: AllowedBinaryPaths,
	}
}

// ResolveBinary finds the tool binary in allowed paths.
func (sc *SecurityChecker) ResolveBinary(tool string) (string, error) {
	for _, dir := range sc.allowedPaths {
		path := filepath.Join(dir, tool)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		// BCC Python tools may have a -bpfcc suffix on some distros
		pathBpfcc := filepath.Join(dir, tool+"-bpfcc")
		if _, err := os.Stat(pathBpfcc); err == nil {
			return pathBpfcc, nil
		}
	}
	return "", fmt.Errorf("tool %q not found in allowed paths: %v", tool, sc.allowedPaths)
}

// VerifyBinary checks that a binary meets security requirements:
//   - Must be in an allowed directory
//   - Must be owned by root
//   - Must not be world-writable
func (sc *SecurityChecker) VerifyBinary(path string) error {
	// Check the binary is in an allowed directory
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)
	allowed := false
	for _, allowedDir := range sc.allowedPaths {
		if dir == allowedDir {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("binary %q is not in an allowed directory", absPath)
	}

	// Check file info
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat %q: %w", absPath, err)
	}

	// Check not a directory
	if info.IsDir() {
		return fmt.Errorf("%q is a directory", absPath)
	}

	// Check ownership (root) and permissions on Linux
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if stat.Uid != 0 {
			return fmt.Errorf("binary %q is not owned by root (uid=%d)", absPath, stat.Uid)
		}
	}

	// Check not world-writable
	perm := info.Mode().Perm()
	if perm&0002 != 0 {
		return fmt.Errorf("binary %q is world-writable (mode=%s)", absPath, info.Mode())
	}

	return nil
}

// SanitizeEnv creates a minimal, safe subprocess environment.
// Only essential variables are kept — prevents environment injection attacks.
func (sc *SecurityChecker) SanitizeEnv() []string {
	safeVars := map[string]bool{
		"PATH":   true,
		"HOME":   true,
		"LANG":   true,
		"LC_ALL": true,
		"TERM":   true,
		"TMPDIR": true,
	}

	var env []string
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 && safeVars[parts[0]] {
			env = append(env, e)
		}
	}

	// Ensure PATH is set with safe defaults
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}

	return env
}
