// Container/cgroup collector (Tier 1): detects Docker/Kubernetes
// environments and collects cgroup v1/v2 metrics.
package collector

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// ContainerCollector detects container environments and cgroup metrics (Tier 1).
type ContainerCollector struct {
	procRoot string
	sysRoot  string
}

func NewContainerCollector(procRoot, sysRoot string) *ContainerCollector {
	return &ContainerCollector{procRoot: procRoot, sysRoot: sysRoot}
}

func (c *ContainerCollector) Name() string     { return "container_info" }
func (c *ContainerCollector) Category() string { return "container" }
func (c *ContainerCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *ContainerCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()
	data := &model.ContainerData{}

	// Detect container runtime
	data.Runtime = c.detectRuntime()

	// Detect cgroup version
	data.CgroupVersion = c.detectCgroupVersion()

	// Read cgroup path for PID 1 (or use target cgroup)
	if len(cfg.TargetCgroups) > 0 {
		data.CgroupPath = cfg.TargetCgroups[0]
	} else {
		data.CgroupPath = c.readCgroupPath()
	}

	// Kubernetes pod info
	if data.Runtime == "kubernetes" {
		data.PodName = os.Getenv("HOSTNAME")
		data.Namespace = c.readKubeNamespace()
	}

	// Container ID from cgroup path
	data.ContainerID = c.extractContainerID(data.CgroupPath)

	// Cgroup metrics — read from target cgroup path if specified
	if data.CgroupVersion == 2 {
		c.collectCgroupV2MetricsFromPath(data, cfg.TargetCgroups)
	} else if data.CgroupVersion == 1 {
		c.collectCgroupV1MetricsFromPath(data, cfg.TargetCgroups)
	}

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      data,
	}, nil
}

// detectRuntime checks common indicators for container environments.
func (c *ContainerCollector) detectRuntime() string {
	// Check for Kubernetes service account
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err == nil {
		return "kubernetes"
	}

	// Check for /.dockerenv
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}

	// Check cgroup for container patterns
	cgroupData, err := os.ReadFile(filepath.Join(c.procRoot, "1", "cgroup"))
	if err == nil {
		content := string(cgroupData)
		if strings.Contains(content, "docker") || strings.Contains(content, "containerd") {
			return "docker"
		}
		if strings.Contains(content, "kubepods") {
			return "kubernetes"
		}
		if strings.Contains(content, "lxc") {
			return "lxc"
		}
	}

	return "none"
}

// detectCgroupVersion checks which cgroup hierarchy is in use.
func (c *ContainerCollector) detectCgroupVersion() int {
	// cgroup v2: /sys/fs/cgroup/cgroup.controllers exists
	if _, err := os.Stat(filepath.Join(c.sysRoot, "fs", "cgroup", "cgroup.controllers")); err == nil {
		return 2
	}
	// cgroup v1: /sys/fs/cgroup/cpu exists
	if _, err := os.Stat(filepath.Join(c.sysRoot, "fs", "cgroup", "cpu")); err == nil {
		return 1
	}
	return 0
}

func (c *ContainerCollector) readCgroupPath() string {
	data, err := os.ReadFile(filepath.Join(c.procRoot, "1", "cgroup"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 {
			return parts[2]
		}
	}
	return ""
}

func (c *ContainerCollector) readKubeNamespace() string {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (c *ContainerCollector) extractContainerID(cgroupPath string) string {
	// Docker: /docker/<id> or /system.slice/docker-<id>.scope
	// K8s: /kubepods/pod<uid>/<id>
	parts := strings.Split(cgroupPath, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		// Container IDs are 64-char hex strings
		if len(part) == 64 && isHex(part) {
			return part
		}
		// Docker scope format
		if strings.HasPrefix(part, "docker-") && strings.HasSuffix(part, ".scope") {
			id := strings.TrimPrefix(part, "docker-")
			id = strings.TrimSuffix(id, ".scope")
			if len(id) == 64 {
				return id
			}
		}
	}
	return ""
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// collectCgroupV2MetricsFromPath reads cgroup v2 metrics, optionally from a specific cgroup path.
func (c *ContainerCollector) collectCgroupV2MetricsFromPath(data *model.ContainerData, targetCgroups []string) {
	cgroupBase := filepath.Join(c.sysRoot, "fs", "cgroup")
	if len(targetCgroups) > 0 {
		cgroupBase = filepath.Join(c.sysRoot, "fs", "cgroup", targetCgroups[0])
	}

	// CPU quota: cpu.max (format: "quota period" or "max period")
	cpuMax := c.readCgroupFile(cgroupBase, "cpu.max")
	if cpuMax != "" {
		parts := strings.Fields(cpuMax)
		if len(parts) == 2 && parts[0] != "max" {
			data.CPUQuota, _ = strconv.ParseInt(parts[0], 10, 64)
			data.CPUPeriod, _ = strconv.ParseInt(parts[1], 10, 64)
		}
	}

	// CPU throttling: cpu.stat
	cpuStat := c.readCgroupFile(cgroupBase, "cpu.stat")
	for _, line := range strings.Split(cpuStat, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "nr_throttled":
			data.CPUThrottledPeriods, _ = strconv.ParseInt(fields[1], 10, 64)
		case "throttled_usec":
			data.CPUThrottledTime, _ = strconv.ParseInt(fields[1], 10, 64)
		}
	}

	// Memory: memory.max, memory.current
	memMax := c.readCgroupFile(cgroupBase, "memory.max")
	if memMax != "" && memMax != "max" {
		data.MemoryLimit, _ = strconv.ParseInt(memMax, 10, 64)
	}
	memCurrent := c.readCgroupFile(cgroupBase, "memory.current")
	data.MemoryUsage, _ = strconv.ParseInt(memCurrent, 10, 64)
}

// collectCgroupV1MetricsFromPath reads cgroup v1 metrics, optionally from a specific cgroup path.
func (c *ContainerCollector) collectCgroupV1MetricsFromPath(data *model.ContainerData, targetCgroups []string) {
	cgroupBase := filepath.Join(c.sysRoot, "fs", "cgroup")
	// For v1, target cgroup path is appended to each controller subdirectory
	cgroupSuffix := ""
	if len(targetCgroups) > 0 {
		cgroupSuffix = targetCgroups[0]
	}

	// CPU quota
	cpuDir := filepath.Join(cgroupBase, "cpu", cgroupSuffix)
	quotaStr := c.readCgroupFile(cpuDir, "cpu.cfs_quota_us")
	data.CPUQuota, _ = strconv.ParseInt(quotaStr, 10, 64)
	periodStr := c.readCgroupFile(cpuDir, "cpu.cfs_period_us")
	data.CPUPeriod, _ = strconv.ParseInt(periodStr, 10, 64)

	// CPU throttling
	throttleStat := c.readCgroupFile(cpuDir, "cpu.stat")
	for _, line := range strings.Split(throttleStat, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "nr_throttled":
			data.CPUThrottledPeriods, _ = strconv.ParseInt(fields[1], 10, 64)
		case "throttled_time":
			// v1 reports in nanoseconds, convert to microseconds
			ns, _ := strconv.ParseInt(fields[1], 10, 64)
			data.CPUThrottledTime = ns / 1000
		}
	}

	// Memory limit
	memDir := filepath.Join(cgroupBase, "memory", cgroupSuffix)
	memLimit := c.readCgroupFile(memDir, "memory.limit_in_bytes")
	data.MemoryLimit, _ = strconv.ParseInt(memLimit, 10, 64)
	memUsage := c.readCgroupFile(memDir, "memory.usage_in_bytes")
	data.MemoryUsage, _ = strconv.ParseInt(memUsage, 10, 64)
}

func (c *ContainerCollector) readCgroupFile(base, name string) string {
	data, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
