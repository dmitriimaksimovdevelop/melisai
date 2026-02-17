package executor

import (
	"testing"
)

func TestSecuritySanitizeEnv(t *testing.T) {
	sc := NewSecurityChecker()
	env := sc.SanitizeEnv()

	// Should have PATH
	hasPath := false
	for _, e := range env {
		if len(e) >= 5 && e[:5] == "PATH=" {
			hasPath = true
		}
		// Should NOT have sensitive variables
		for _, prefix := range []string{"AWS_", "GITHUB_", "SSH_", "GPG_", "SECRET"} {
			if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
				t.Errorf("leaked sensitive env var: %s", e)
			}
		}
	}
	if !hasPath {
		t.Error("sanitized env missing PATH")
	}
}

func TestSecurityVerifyBinaryBadPath(t *testing.T) {
	sc := NewSecurityChecker()

	// Non-allowed path should be rejected
	err := sc.VerifyBinary("/tmp/malicious-tool")
	if err == nil {
		t.Error("expected error for non-allowed path")
	}
}

func TestSecurityResolveNonexistent(t *testing.T) {
	sc := NewSecurityChecker()
	_, err := sc.ResolveBinary("nonexistent-tool-xyz")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

func TestAllowedPaths(t *testing.T) {
	expectedPaths := []string{
		"/usr/share/bcc/tools",
		"/usr/sbin",
		"/usr/bin",
		"/usr/local/bin",
	}
	for _, p := range expectedPaths {
		found := false
		for _, ap := range AllowedBinaryPaths {
			if ap == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected allowed path: %s", p)
		}
	}
}
