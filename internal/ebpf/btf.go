// Package ebpf provides BTF/CO-RE detection, BPF program loading,
// and graceful fallback when native eBPF is unavailable.
package ebpf

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// BTFInfo describes the BTF availability on the system.
type BTFInfo struct {
	Available     bool   `json:"available"`
	VmlinuxPath   string `json:"vmlinux_path,omitempty"`
	KernelVersion string `json:"kernel_version"`
	MajorVersion  int    `json:"major_version"`
	MinorVersion  int    `json:"minor_version"`
	CORESupport   bool   `json:"core_support"` // true if kernel >= 5.8
}

// DetectBTF checks for BTF availability.
func DetectBTF() *BTFInfo {
	info := &BTFInfo{}
	info.KernelVersion = readKernelVersion()
	info.MajorVersion, info.MinorVersion = parseKernelVersion(info.KernelVersion)

	// Check for vmlinux BTF
	btfPath := "/sys/kernel/btf/vmlinux"
	if _, err := os.Stat(btfPath); err == nil {
		info.Available = true
		info.VmlinuxPath = btfPath
	}

	// CO-RE requires kernel >= 5.8
	if info.MajorVersion > 5 || (info.MajorVersion == 5 && info.MinorVersion >= 8) {
		info.CORESupport = true
	}

	return info
}

// DetectBPFCapabilities checks what BPF features are available.
func DetectBPFCapabilities() map[string]bool {
	caps := make(map[string]bool)

	// Check if BPF syscall is available
	caps["bpf_syscall"] = fileExists("/proc/sys/kernel/unprivileged_bpf_disabled")

	// Check BTF
	caps["btf_vmlinux"] = fileExists("/sys/kernel/btf/vmlinux")

	// Check BPF filesystem
	caps["bpffs"] = fileExists("/sys/fs/bpf")

	// Check kernel config options
	kconfig := readKConfig()
	for _, opt := range []string{
		"CONFIG_BPF",
		"CONFIG_BPF_SYSCALL",
		"CONFIG_BPF_JIT",
		"CONFIG_HAVE_EBPF_JIT",
		"CONFIG_BPF_EVENTS",
		"CONFIG_KPROBE_EVENTS",
		"CONFIG_UPROBE_EVENTS",
		"CONFIG_TRACING",
		"CONFIG_DEBUG_INFO_BTF",
	} {
		caps[strings.ToLower(opt)] = kconfig[opt]
	}

	// Check perf_event_open
	caps["perf_events"] = fileExists("/proc/sys/kernel/perf_event_paranoid")

	// Check kprobes
	caps["kprobes"] = fileExists("/sys/kernel/debug/kprobes/list") ||
		fileExists("/sys/kernel/tracing/kprobe_events")

	return caps
}

func readKernelVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		return fields[2]
	}
	return ""
}

func parseKernelVersion(version string) (int, int) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0
	}
	major, _ := strconv.Atoi(parts[0])
	// Minor might contain a dash (e.g., "8-generic")
	minorStr := parts[1]
	if idx := strings.IndexAny(minorStr, "-+~"); idx >= 0 {
		minorStr = minorStr[:idx]
	}
	minor, _ := strconv.Atoi(minorStr)
	return major, minor
}

func readKConfig() map[string]bool {
	configs := make(map[string]bool)

	// Try multiple locations
	paths := []string{
		fmt.Sprintf("/boot/config-%s", readKernelRelease()),
		"/proc/config.gz",
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			if idx := strings.Index(line, "="); idx >= 0 {
				key := line[:idx]
				val := line[idx+1:]
				configs[key] = val == "y" || val == "m"
			}
		}
		break
	}
	return configs
}

func readKernelRelease() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CapabilityLevel returns the highest BPF tier supported.
func CapabilityLevel(caps map[string]bool) int {
	if caps["btf_vmlinux"] && caps["config_bpf_syscall"] && caps["config_debug_info_btf"] {
		return 3 // Native eBPF with CO-RE
	}
	if caps["bpf_syscall"] && caps["config_bpf"] {
		return 2 // BCC tools (requires installed tools)
	}
	return 1 // procfs/sysfs only
}

// FormatCapabilities returns a human-readable capabilities summary.
func FormatCapabilities(caps map[string]bool) string {
	var sb strings.Builder

	level := CapabilityLevel(caps)
	sb.WriteString(fmt.Sprintf("BPF Capability Level: Tier %d\n\n", level))

	groups := []struct {
		title string
		keys  []string
	}{
		{"Core BPF", []string{"bpf_syscall", "bpffs", "config_bpf", "config_bpf_syscall", "config_bpf_jit"}},
		{"Tracing", []string{"config_bpf_events", "config_kprobe_events", "config_uprobe_events", "config_tracing", "kprobes", "perf_events"}},
		{"BTF/CO-RE", []string{"btf_vmlinux", "config_debug_info_btf", "config_have_ebpf_jit"}},
	}

	for _, g := range groups {
		sb.WriteString(fmt.Sprintf("%s:\n", g.title))
		for _, key := range g.keys {
			status := "✗"
			if caps[key] {
				status = "✓"
			}
			sb.WriteString(fmt.Sprintf("  %s %s\n", status, key))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
