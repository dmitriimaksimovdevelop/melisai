// GPU and PCIe topology collector (Tier 1).
// Detects NVIDIA GPUs via nvidia-smi, maps PCI devices to NUMA nodes,
// flags GPU-NIC pairs on different NUMA nodes.
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

// GPUCollector detects GPUs and PCIe topology.
type GPUCollector struct {
	sysRoot string
	cmdRun  CommandRunner
}

func NewGPUCollector(sysRoot string) *GPUCollector {
	return &GPUCollector{sysRoot: sysRoot, cmdRun: &ExecCommandRunner{}}
}

func (c *GPUCollector) Name() string     { return "gpu_pcie" }
func (c *GPUCollector) Category() string { return "system" }
func (c *GPUCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *GPUCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()
	topo := &model.PCIeTopology{
		NICNUMAMap: make(map[string]int),
	}

	// Detect NVIDIA GPUs via nvidia-smi
	topo.GPUs = c.detectNvidiaGPUs(ctx)

	// Build NIC → NUMA node map from sysfs
	c.buildNICNUMAMap(topo)

	// Find GPU-NIC cross-NUMA pairs
	c.findCrossNUMAPairs(topo)

	// Skip if nothing detected
	if len(topo.GPUs) == 0 && len(topo.NICNUMAMap) == 0 {
		return nil, nil
	}

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      topo,
	}, nil
}

// detectNvidiaGPUs queries nvidia-smi for GPU information.
func (c *GPUCollector) detectNvidiaGPUs(ctx context.Context) []model.GPUDevice {
	// Dedicated 5s timeout — nvidia-smi can hang on driver issues
	nvsmiCtx, nvsmiCancel := context.WithTimeout(ctx, 5*time.Second)
	defer nvsmiCancel()
	out, err := c.cmdRun.Run(nvsmiCtx, "nvidia-smi",
		"--query-gpu=index,name,driver_version,pci.bus_id,memory.total,memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits")
	if err != nil {
		return nil // nvidia-smi not available
	}

	var gpus []model.GPUDevice
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ", ")
		if len(fields) < 4 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		gpu := model.GPUDevice{
			Index:  idx,
			Name:   strings.TrimSpace(fields[1]),
			Driver: strings.TrimSpace(fields[2]),
			PCIBus: strings.TrimSpace(fields[3]),
		}
		if len(fields) > 4 {
			gpu.MemoryTotal, _ = strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64)
		}
		if len(fields) > 5 {
			gpu.MemoryUsed, _ = strconv.ParseInt(strings.TrimSpace(fields[5]), 10, 64)
		}
		if len(fields) > 6 {
			gpu.UtilGPU, _ = strconv.Atoi(strings.TrimSpace(fields[6]))
		}
		if len(fields) > 7 {
			gpu.UtilMemory, _ = strconv.Atoi(strings.TrimSpace(fields[7]))
		}
		if len(fields) > 8 {
			gpu.Temperature, _ = strconv.Atoi(strings.TrimSpace(fields[8]))
		}
		if len(fields) > 9 {
			pw, _ := strconv.ParseFloat(strings.TrimSpace(fields[9]), 64)
			gpu.PowerWatts = int(pw)
		}

		// Get NUMA node from sysfs
		gpu.NUMANode = c.pciNUMANode(gpu.PCIBus)

		gpus = append(gpus, gpu)
	}
	return gpus
}

// buildNICNUMAMap maps each physical NIC to its NUMA node.
func (c *GPUCollector) buildNICNUMAMap(topo *model.PCIeTopology) {
	netDir := filepath.Join(c.sysRoot, "class", "net")
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") {
			continue
		}
		numaFile := filepath.Join(netDir, name, "device", "numa_node")
		if data, err := os.ReadFile(numaFile); err == nil {
			node, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			if node >= 0 {
				topo.NICNUMAMap[name] = node
			}
		}
	}
}

// findCrossNUMAPairs identifies GPU-NIC pairs on different NUMA nodes.
func (c *GPUCollector) findCrossNUMAPairs(topo *model.PCIeTopology) {
	for _, gpu := range topo.GPUs {
		for nic, nicNode := range topo.NICNUMAMap {
			if gpu.NUMANode != nicNode && gpu.NUMANode >= 0 && nicNode >= 0 {
				topo.CrossNUMAPairs = append(topo.CrossNUMAPairs, model.CrossNUMAPair{
					GPU:     gpu.Name,
					GPUNode: gpu.NUMANode,
					NIC:     nic,
					NICNode: nicNode,
				})
			}
		}
	}
}

// pciNUMANode reads the NUMA node for a PCI device from sysfs.
// PCIBus format from nvidia-smi: "00000000:01:00.0"
func (c *GPUCollector) pciNUMANode(pciBus string) int {
	// Try direct sysfs path
	numaFile := filepath.Join(c.sysRoot, "bus", "pci", "devices", pciBus, "numa_node")
	if data, err := os.ReadFile(numaFile); err == nil {
		node, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return node
	}
	return -1
}
