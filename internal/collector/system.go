// System collector: hostname, kernel version, uptime, dmesg, CPU info,
// filesystem usage, block devices, boot params, PSI.
package collector

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/baikal/sysdiag/internal/model"
)

// SystemCollector gathers general system information (Tier 1).
type SystemCollector struct {
	procRoot string
	sysRoot  string
}

func NewSystemCollector(procRoot, sysRoot string) *SystemCollector {
	return &SystemCollector{procRoot: procRoot, sysRoot: sysRoot}
}

func (c *SystemCollector) Name() string     { return "system_info" }
func (c *SystemCollector) Category() string { return "system" }
func (c *SystemCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *SystemCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()

	sysInfo := model.SystemInfo{}

	// OS identification
	sysInfo.OS = c.readOSRelease()
	sysInfo.Kernel = c.readFile(filepath.Join(c.procRoot, "version"))
	sysInfo.BootParams = c.readFile(filepath.Join(c.procRoot, "cmdline"))

	// Uptime
	if raw := c.readFile(filepath.Join(c.procRoot, "uptime")); raw != "" {
		parts := strings.Fields(raw)
		if len(parts) >= 1 {
			if uptime, err := strconv.ParseFloat(parts[0], 64); err == nil {
				sysInfo.UptimeSeconds = int64(uptime)
			}
		}
	}

	// Filesystem info
	sysInfo.Filesystems = c.collectFilesystems(ctx)

	// Block devices
	sysInfo.BlockDevices = c.collectBlockDevices()

	// dmesg errors
	sysInfo.DmesgErrors = c.collectDmesg(ctx)

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      sysInfo,
	}, nil
}

// readOSRelease parses /etc/os-release for PRETTY_NAME.
func (c *SystemCollector) readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return runtime.GOOS
}

// readFile reads a procfs/sysfs file and returns its trimmed content.
func (c *SystemCollector) readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// collectFilesystems runs `df` to get filesystem info.
func (c *SystemCollector) collectFilesystems(ctx context.Context) []model.FilesystemInfo {
	out, err := exec.CommandContext(ctx, "df", "-P", "-T").Output()
	if err != nil {
		return nil
	}

	var fss []model.FilesystemInfo
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Scan() // skip header

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 {
			continue
		}
		// fields: Filesystem Type 1024-blocks Used Available Capacity Mounted
		sizeKB, _ := strconv.ParseFloat(fields[2], 64)
		usedKB, _ := strconv.ParseFloat(fields[3], 64)

		var usedPct float64
		if sizeKB > 0 {
			usedPct = (usedKB / sizeKB) * 100
		}

		fss = append(fss, model.FilesystemInfo{
			Mount:   fields[6],
			Device:  fields[0],
			Type:    fields[1],
			SizeGB:  sizeKB / 1024 / 1024,
			UsedPct: usedPct,
		})
	}
	return fss
}

// collectBlockDevices reads from sysfs.
func (c *SystemCollector) collectBlockDevices() []model.BlockDevice {
	entries, err := os.ReadDir(filepath.Join(c.sysRoot, "block"))
	if err != nil {
		return nil
	}

	var devs []model.BlockDevice
	for _, entry := range entries {
		name := entry.Name()
		basePath := filepath.Join(c.sysRoot, "block", name)

		// Size in 512-byte sectors
		sizeStr := c.readFile(filepath.Join(basePath, "size"))
		sectors, _ := strconv.ParseInt(sizeStr, 10, 64)
		sizeGB := float64(sectors*512) / (1024 * 1024 * 1024)

		// Rotational (0 = SSD, 1 = HDD)
		rotStr := c.readFile(filepath.Join(basePath, "queue", "rotational"))
		devType := "ssd"
		if rotStr == "1" {
			devType = "hdd"
		}

		// Model
		modelStr := c.readFile(filepath.Join(basePath, "device", "model"))

		devs = append(devs, model.BlockDevice{
			Name:   name,
			Type:   devType,
			SizeGB: sizeGB,
			Model:  modelStr,
		})
	}
	return devs
}

// collectDmesg gathers error/warning kernel messages.
func (c *SystemCollector) collectDmesg(ctx context.Context) []model.LogEntry {
	out, err := exec.CommandContext(ctx, "dmesg", "--level=err,warn", "-T", "--nopager").Output()
	if err != nil {
		// dmesg may need root; not fatal
		return nil
	}

	var entries []model.LogEntry
	lines := strings.Split(string(out), "\n")
	// Take last 50 entries max
	start := 0
	if len(lines) > 50 {
		start = len(lines) - 50
	}
	for _, line := range lines[start:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entries = append(entries, model.LogEntry{
			Message: line,
			Level:   "err",
		})
	}
	return entries
}

// readSysctlInt reads a sysctl value as int.
func readSysctlInt(procRoot, path string) int {
	data, err := os.ReadFile(filepath.Join(procRoot, path))
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return v
}

// readSysctlInt64 reads a sysctl value as int64.
func readSysctlInt64(procRoot, path string) int64 {
	data, err := os.ReadFile(filepath.Join(procRoot, path))
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return v
}

// readSysctlString reads a sysctl value as string.
func readSysctlString(procRoot, path string) string {
	data, err := os.ReadFile(filepath.Join(procRoot, path))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
