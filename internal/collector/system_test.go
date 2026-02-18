package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ---------- SystemCollector metadata ----------

func TestSystemCollectorName(t *testing.T) {
	c := NewSystemCollector("/proc", "/sys")

	if got := c.Name(); got != "system_info" {
		t.Errorf("Name() = %q, want %q", got, "system_info")
	}
	if got := c.Category(); got != "system" {
		t.Errorf("Category() = %q, want %q", got, "system")
	}
	avail := c.Available()
	if avail.Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", avail.Tier)
	}
}

// ---------- readOSRelease ----------

// readOSRelease reads from the hard-coded path /etc/os-release, so we cannot
// easily redirect it via procRoot. Instead we verify the method returns a
// non-empty string (the real /etc/os-release on the host, or runtime.GOOS
// as fallback). The important contract: it never returns "".
func TestReadOSRelease(t *testing.T) {
	c := NewSystemCollector("/proc", "/sys")
	got := c.readOSRelease()
	if got == "" {
		t.Error("readOSRelease() returned empty string, expected OS name or runtime.GOOS fallback")
	}
}

// ---------- collectFilesystems ----------

func TestCollectFilesystems(t *testing.T) {
	dfOutput := "Filesystem     Type  1024-blocks      Used Available Capacity Mounted on\n" +
		"/dev/sda1      ext4   102400000  51200000  46080000      53% /\n" +
		"tmpfs          tmpfs    8192000         0   8192000       0% /dev/shm\n" +
		"/dev/sdb1      xfs    204800000 153600000  51200000      75% /data\n"

	mock := &mockCommandRunner{
		outputs: map[string][]byte{
			"df -P -T": []byte(dfOutput),
		},
		errors: map[string]error{},
	}

	c := NewSystemCollectorWithRunner("/proc", "/sys", mock)
	fss := c.collectFilesystems(context.Background())

	if len(fss) != 3 {
		t.Fatalf("collectFilesystems: got %d entries, want 3", len(fss))
	}

	// Verify first filesystem: /dev/sda1 ext4 mounted on /
	if fss[0].Device != "/dev/sda1" {
		t.Errorf("fs[0].Device = %q, want %q", fss[0].Device, "/dev/sda1")
	}
	if fss[0].Type != "ext4" {
		t.Errorf("fs[0].Type = %q, want %q", fss[0].Type, "ext4")
	}
	if fss[0].Mount != "/" {
		t.Errorf("fs[0].Mount = %q, want %q", fss[0].Mount, "/")
	}
	// SizeGB = 102400000 KB / 1024 / 1024 ≈ 97.65 GB
	expectedSizeGB := 102400000.0 / 1024.0 / 1024.0
	if diff := fss[0].SizeGB - expectedSizeGB; diff > 0.01 || diff < -0.01 {
		t.Errorf("fs[0].SizeGB = %.4f, want ~%.4f", fss[0].SizeGB, expectedSizeGB)
	}
	// UsedPct = 51200000 / 102400000 * 100 = 50%
	if fss[0].UsedPct != 50 {
		t.Errorf("fs[0].UsedPct = %.2f, want 50.00", fss[0].UsedPct)
	}

	// Verify tmpfs with 0 usage
	if fss[1].Type != "tmpfs" {
		t.Errorf("fs[1].Type = %q, want %q", fss[1].Type, "tmpfs")
	}
	if fss[1].UsedPct != 0 {
		t.Errorf("fs[1].UsedPct = %.2f, want 0", fss[1].UsedPct)
	}

	// Verify /data
	if fss[2].Mount != "/data" {
		t.Errorf("fs[2].Mount = %q, want %q", fss[2].Mount, "/data")
	}
	if fss[2].Type != "xfs" {
		t.Errorf("fs[2].Type = %q, want %q", fss[2].Type, "xfs")
	}
	// UsedPct = 153600000 / 204800000 * 100 = 75%
	if fss[2].UsedPct != 75 {
		t.Errorf("fs[2].UsedPct = %.2f, want 75.00", fss[2].UsedPct)
	}
}

func TestCollectFilesystemsCommandFailure(t *testing.T) {
	mock := &mockCommandRunner{
		outputs: map[string][]byte{},
		errors: map[string]error{
			"df -P -T": errCommandNotFound("df"),
		},
	}

	c := NewSystemCollectorWithRunner("/proc", "/sys", mock)
	fss := c.collectFilesystems(context.Background())
	if fss != nil {
		t.Errorf("collectFilesystems on error: got %v, want nil", fss)
	}
}

// ---------- collectDmesg ----------

func TestCollectDmesg(t *testing.T) {
	dmesgOutput := "[Mon Jan 15 10:30:00 2024] ACPI Error: some ACPI problem\n" +
		"[Mon Jan 15 10:31:00 2024] usb 3-1: device descriptor read/64, error -110\n" +
		"[Mon Jan 15 10:32:00 2024] EXT4-fs warning: mounting unchecked fs\n" +
		"[Mon Jan 15 10:33:00 2024] segfault at 0000000000000000 ip 00007f...\n" +
		"[Mon Jan 15 10:34:00 2024] CPU soft lockup detected\n"

	mock := &mockCommandRunner{
		outputs: map[string][]byte{
			"dmesg --level=err,warn -T --nopager": []byte(dmesgOutput),
		},
		errors: map[string]error{},
	}

	c := NewSystemCollectorWithRunner("/proc", "/sys", mock)
	entries := c.collectDmesg(context.Background())

	if len(entries) != 5 {
		t.Fatalf("collectDmesg: got %d entries, want 5", len(entries))
	}

	// "ACPI Error" contains "error" => level "err"
	if entries[0].Level != "err" {
		t.Errorf("entry[0].Level = %q, want %q (contains 'Error')", entries[0].Level, "err")
	}

	// "error -110" contains "error" => level "err"
	if entries[1].Level != "err" {
		t.Errorf("entry[1].Level = %q, want %q (contains 'error')", entries[1].Level, "err")
	}

	// "warning" does not contain error/fail/panic/bug:/oops/segfault => level "warn"
	if entries[2].Level != "warn" {
		t.Errorf("entry[2].Level = %q, want %q (warning without error keywords)", entries[2].Level, "warn")
	}

	// "segfault" => level "err"
	if entries[3].Level != "err" {
		t.Errorf("entry[3].Level = %q, want %q (contains 'segfault')", entries[3].Level, "err")
	}

	// "soft lockup" => no error keywords, level "warn"
	if entries[4].Level != "warn" {
		t.Errorf("entry[4].Level = %q, want %q (no error keyword match)", entries[4].Level, "warn")
	}

	// Verify messages are preserved
	if entries[0].Message == "" {
		t.Error("entry[0].Message is empty")
	}
}

func TestCollectDmesgCommandFailure(t *testing.T) {
	mock := &mockCommandRunner{
		outputs: map[string][]byte{},
		errors: map[string]error{
			"dmesg --level=err,warn -T --nopager": errCommandNotFound("dmesg"),
		},
	}

	c := NewSystemCollectorWithRunner("/proc", "/sys", mock)
	entries := c.collectDmesg(context.Background())
	if entries != nil {
		t.Errorf("collectDmesg on error: got %v, want nil", entries)
	}
}

func TestCollectDmesgTruncation(t *testing.T) {
	// Build more than 50 lines to test the truncation logic.
	var lines string
	for i := 0; i < 60; i++ {
		lines += "[Mon Jan 15 10:30:00 2024] some warning message\n"
	}

	mock := &mockCommandRunner{
		outputs: map[string][]byte{
			"dmesg --level=err,warn -T --nopager": []byte(lines),
		},
		errors: map[string]error{},
	}

	c := NewSystemCollectorWithRunner("/proc", "/sys", mock)
	entries := c.collectDmesg(context.Background())

	if len(entries) > 50 {
		t.Errorf("collectDmesg returned %d entries, want <= 50 (truncated)", len(entries))
	}
}

// ---------- readSysctlInt / readSysctlInt64 ----------

func TestReadSysctlInt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake sysctl file with an integer value.
	sysctlDir := filepath.Join(tmpDir, "sys", "net", "core")
	if err := os.MkdirAll(sysctlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysctlDir, "somaxconn"), []byte("4096\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readSysctlInt(tmpDir, "sys/net/core/somaxconn")
	if got != 4096 {
		t.Errorf("readSysctlInt = %d, want 4096", got)
	}

	// Test with zero value.
	if err := os.WriteFile(filepath.Join(sysctlDir, "zero"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = readSysctlInt(tmpDir, "sys/net/core/zero")
	if got != 0 {
		t.Errorf("readSysctlInt(zero) = %d, want 0", got)
	}

	// Nonexistent file should return 0.
	got = readSysctlInt(tmpDir, "sys/net/core/nonexistent")
	if got != 0 {
		t.Errorf("readSysctlInt(nonexistent) = %d, want 0", got)
	}

	// readSysctlInt64 test.
	if err := os.WriteFile(filepath.Join(sysctlDir, "bigval"), []byte("9223372036854775807\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got64 := readSysctlInt64(tmpDir, "sys/net/core/bigval")
	if got64 != 9223372036854775807 {
		t.Errorf("readSysctlInt64 = %d, want 9223372036854775807", got64)
	}

	// readSysctlInt64 nonexistent file.
	got64 = readSysctlInt64(tmpDir, "sys/net/core/nonexistent")
	if got64 != 0 {
		t.Errorf("readSysctlInt64(nonexistent) = %d, want 0", got64)
	}
}

// ---------- readSysctlString ----------

func TestReadSysctlString(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a fake sysctl file with a string value.
	sysctlDir := filepath.Join(tmpDir, "sys", "net", "ipv4")
	if err := os.MkdirAll(sysctlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysctlDir, "tcp_congestion_control"), []byte("bbr\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readSysctlString(tmpDir, "sys/net/ipv4/tcp_congestion_control")
	if got != "bbr" {
		t.Errorf("readSysctlString = %q, want %q", got, "bbr")
	}

	// Multi-value string (tcp_rmem style).
	if err := os.WriteFile(filepath.Join(sysctlDir, "tcp_rmem"), []byte("4096\t87380\t6291456\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = readSysctlString(tmpDir, "sys/net/ipv4/tcp_rmem")
	if got != "4096\t87380\t6291456" {
		t.Errorf("readSysctlString(tcp_rmem) = %q, want %q", got, "4096\t87380\t6291456")
	}

	// Nonexistent file returns empty string.
	got = readSysctlString(tmpDir, "sys/net/ipv4/nonexistent")
	if got != "" {
		t.Errorf("readSysctlString(nonexistent) = %q, want empty", got)
	}

	// Empty file returns empty string.
	if err := os.WriteFile(filepath.Join(sysctlDir, "empty"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got = readSysctlString(tmpDir, "sys/net/ipv4/empty")
	if got != "" {
		t.Errorf("readSysctlString(empty) = %q, want empty", got)
	}
}

// helper to create a "command not found" error for mock
func errCommandNotFound(name string) error {
	return &commandNotFoundError{name: name}
}

type commandNotFoundError struct {
	name string
}

func (e *commandNotFoundError) Error() string {
	return e.name + ": command not found"
}
