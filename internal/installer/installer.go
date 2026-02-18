// Package installer handles detection and installation of BPF tools
// on various Linux distributions.
package installer

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Installer detects the Linux distribution and installs BPF dependencies.
type Installer struct {
	DryRun bool
}

// DistroInfo holds OS and package manager details.
type DistroInfo struct {
	ID         string // "ubuntu", "centos", "fedora", "arch"
	VersionID  string // "22.04", "8", etc.
	PkgManager string // "apt", "yum", "dnf", "pacman", "zypper"
}

// PackageSet defines packages for a step.
type PackageSet struct {
	Step     string
	Packages map[string][]string // pkg manager → package names
}

// Run performs the installation.
func (inst *Installer) Run() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("sysdiag install is only supported on Linux (current: %s)", runtime.GOOS)
	}

	// Check root
	if os.Geteuid() != 0 {
		return fmt.Errorf("sysdiag install requires root privileges (use sudo)")
	}

	// Detect distro
	distro, err := DetectDistro()
	if err != nil {
		return fmt.Errorf("detect distro: %w", err)
	}

	fmt.Printf("Detected: %s %s (package manager: %s)\n", distro.ID, distro.VersionID, distro.PkgManager)

	// Check kernel version
	kernel, err := KernelVersion()
	if err == nil {
		fmt.Printf("Kernel: %s\n", kernel)
	}

	// Update package index first
	if !inst.DryRun {
		fmt.Println("\nUpdating package index...")
		if err := updatePackageIndex(distro.PkgManager); err != nil {
			fmt.Printf("  WARNING: %v\n", err)
		}
	}

	// Install packages in order
	steps := BuildPackageSteps(distro)
	for _, step := range steps {
		pkgs := step.Packages[distro.PkgManager]
		if len(pkgs) == 0 {
			continue
		}

		fmt.Printf("\n[%s] Installing: %s\n", step.Step, strings.Join(pkgs, " "))

		if inst.DryRun {
			fmt.Printf("  (dry-run) Would run: %s install %s\n", distro.PkgManager, strings.Join(pkgs, " "))
			continue
		}

		// Install packages individually so one failure doesn't block others
		for _, pkg := range pkgs {
			if err := installPackages(distro.PkgManager, []string{pkg}); err != nil {
				fmt.Printf("  WARNING: failed to install %s: %v\n", pkg, err)
			} else {
				fmt.Printf("  OK: %s\n", pkg)
			}
		}
	}

	// Install FlameGraph
	fmt.Println("\n[flamegraph] Cloning FlameGraph tools...")
	if inst.DryRun {
		fmt.Println("  (dry-run) Would clone to /opt/FlameGraph/")
	} else {
		if err := installFlameGraph(); err != nil {
			fmt.Printf("  WARNING: %v\n", err)
		}
	}

	fmt.Println("\nInstallation complete. Run 'sysdiag capabilities' to verify.")
	return nil
}

// DetectDistro reads /etc/os-release to identify the distribution.
func DetectDistro() (*DistroInfo, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil, fmt.Errorf("read /etc/os-release: %w", err)
	}

	info := &DistroInfo{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], "\"")
		switch key {
		case "ID":
			info.ID = val
		case "VERSION_ID":
			info.VersionID = val
		}
	}

	// Map ID to package manager
	switch info.ID {
	case "ubuntu", "debian", "linuxmint", "pop":
		info.PkgManager = "apt"
	case "centos", "rhel", "rocky", "almalinux", "ol":
		info.PkgManager = "yum"
	case "fedora":
		info.PkgManager = "dnf"
	case "arch", "manjaro":
		info.PkgManager = "pacman"
	case "opensuse", "sles":
		info.PkgManager = "zypper"
	default:
		return nil, fmt.Errorf("unsupported distribution: %s", info.ID)
	}

	return info, nil
}

// KernelVersion returns the running kernel version.
func KernelVersion() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// BuildPackageSteps returns the ordered list of package installations.
func BuildPackageSteps(distro *DistroInfo) []PackageSet {
	kernelVer, _ := KernelVersion()

	// For kernel headers on apt, try version-specific first, then generic
	aptHeaders := []string{"linux-headers-" + kernelVer}
	aptPerfTools := []string{"linux-tools-" + kernelVer}
	// Fallback: generic meta-package if exact version not available
	if kernelVer != "" {
		aptHeaders = append(aptHeaders, "linux-headers-generic")
		aptPerfTools = append(aptPerfTools, "linux-tools-generic")
	}

	return []PackageSet{
		{
			Step: "kernel-headers",
			Packages: map[string][]string{
				"apt":    aptHeaders,
				"yum":    {"kernel-devel-" + kernelVer, "kernel-devel"},
				"dnf":    {"kernel-devel"},
				"pacman": {"linux-headers"},
			},
		},
		{
			Step: "bcc-tools",
			Packages: map[string][]string{
				"apt":    {"bpfcc-tools", "python3-bpfcc"},
				"yum":    {"bcc-tools", "python3-bcc"},
				"dnf":    {"bcc-tools", "python3-bcc"},
				"pacman": {"bcc-tools", "python-bcc"},
			},
		},
		{
			Step: "bpftrace",
			Packages: map[string][]string{
				"apt":    {"bpftrace"},
				"yum":    {"bpftrace"},
				"dnf":    {"bpftrace"},
				"pacman": {"bpftrace"},
			},
		},
		{
			Step: "perf",
			Packages: map[string][]string{
				"apt":    aptPerfTools,
				"yum":    {"perf"},
				"dnf":    {"perf"},
				"pacman": {"perf"},
			},
		},
		{
			Step: "utilities",
			Packages: map[string][]string{
				"apt":    {"iproute2", "sysstat", "procps"},
				"yum":    {"iproute", "sysstat", "procps-ng"},
				"dnf":    {"iproute", "sysstat", "procps-ng"},
				"pacman": {"iproute2", "sysstat", "procps-ng"},
			},
		},
	}
}

func updatePackageIndex(pkgManager string) error {
	var cmd *exec.Cmd
	switch pkgManager {
	case "apt":
		cmd = exec.Command("apt-get", "update", "-qq")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	case "yum":
		cmd = exec.Command("yum", "makecache", "-q")
	case "dnf":
		cmd = exec.Command("dnf", "makecache", "-q")
	case "pacman":
		cmd = exec.Command("pacman", "-Sy")
	default:
		return nil
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func installPackages(pkgManager string, packages []string) error {
	var cmd *exec.Cmd
	switch pkgManager {
	case "apt":
		args := append([]string{"install", "-y", "-qq"}, packages...)
		cmd = exec.Command("apt-get", args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	case "yum":
		args := append([]string{"install", "-y"}, packages...)
		cmd = exec.Command("yum", args...)
	case "dnf":
		args := append([]string{"install", "-y"}, packages...)
		cmd = exec.Command("dnf", args...)
	case "pacman":
		args := append([]string{"-S", "--noconfirm"}, packages...)
		cmd = exec.Command("pacman", args...)
	case "zypper":
		args := append([]string{"install", "-y"}, packages...)
		cmd = exec.Command("zypper", args...)
	default:
		return fmt.Errorf("unsupported package manager: %s", pkgManager)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func installFlameGraph() error {
	targetDir := "/opt/FlameGraph"
	if _, err := os.Stat(targetDir); err == nil {
		// Already exists, do a pull
		cmd := exec.Command("git", "-C", targetDir, "pull")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("git", "clone", "https://github.com/brendangregg/FlameGraph", targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
