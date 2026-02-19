package output

import (
	"fmt"
	"strings"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// GenerateAIPrompt creates a dynamic, context-aware prompt for AI analysis.
func GenerateAIPrompt(report *model.Report) *model.AIContext {
	ctx := &model.AIContext{
		Methodology:   "USE Method (Utilization, Saturation, Errors) by Brendan Gregg",
		KnownPatterns: knownAntiPatterns(),
	}

	var sb strings.Builder
	sb.WriteString("You are a Linux systems performance expert. ")
	sb.WriteString("Analyze the following melisai report and provide:\n")
	sb.WriteString("1. Root cause analysis for any detected anomalies\n")
	sb.WriteString("2. Performance optimization recommendations with specific commands\n")
	sb.WriteString("3. Risk assessment for production workloads\n")
	sb.WriteString("4. Investigation priorities ordered by impact\n\n")

	// System context
	sb.WriteString(fmt.Sprintf("System: %s, Kernel: %s, CPUs: %d, Memory: %dGB\n",
		report.Metadata.Hostname,
		report.Metadata.KernelVersion,
		report.Metadata.CPUs,
		report.Metadata.MemoryGB))

	if report.Metadata.ContainerEnv != "" && report.Metadata.ContainerEnv != "none" {
		sb.WriteString(fmt.Sprintf("Container: %s (cgroup v%d)\n",
			report.Metadata.ContainerEnv, report.Metadata.CgroupVersion))
	}

	sb.WriteString(fmt.Sprintf("Profile: %s, Duration: %s\n",
		report.Metadata.Profile, report.Metadata.Duration))

	// PID/cgroup targeting context
	hasPIDTarget := false
	hasCgroupTarget := false
	if procResults, ok := report.Categories["process"]; ok {
		for _, r := range procResults {
			if pd, ok := r.Data.(*model.ProcessData); ok {
				if len(pd.TopByCPU) > 0 && len(pd.TopByCPU) <= 5 {
					// Likely PID-filtered (few processes)
					hasPIDTarget = true
				}
			}
		}
	}
	if containerResults, ok := report.Categories["container"]; ok {
		for _, r := range containerResults {
			if cd, ok := r.Data.(*model.ContainerData); ok {
				if cd.CgroupPath != "" && cd.CgroupPath != "/" {
					hasCgroupTarget = true
				}
			}
		}
	}

	if hasPIDTarget || hasCgroupTarget {
		sb.WriteString("\n** TARGETED ANALYSIS MODE **\n")
		if hasPIDTarget {
			sb.WriteString("This report is scoped to specific PIDs. ")
			sb.WriteString("BCC tools that support PID filtering (24 of 67) traced only the target process(es). ")
			sb.WriteString("Tier 1 metrics (CPU, memory, disk, network) show system-wide baselines for context. ")
			sb.WriteString("Focus your analysis on the target application:\n")
			sb.WriteString("- Compare process CPU/memory against system totals to assess resource share\n")
			sb.WriteString("- Latency histograms (runqlat, biolatency, tcpconnlat) reflect only the target PID\n")
			sb.WriteString("- Stack traces (profile, offcputime) show only the target application code paths\n")
			sb.WriteString("- Events (opensnoop, tcpconnect, syscount) are filtered to the target process\n\n")
		}
		if hasCgroupTarget {
			sb.WriteString("This report is scoped to a specific cgroup (container/service). ")
			sb.WriteString("Container metrics (CPU throttling, memory usage vs limit) reflect the target cgroup. ")
			sb.WriteString("Process list is filtered to PIDs within the cgroup.\n\n")
		}
	} else {
		sb.WriteString("Collection mode: system-wide (all processes)\n\n")
	}

	// Two-phase collection note
	sb.WriteString("COLLECTION METHOD: Two-phase collection was used to avoid observer effect.\n")
	sb.WriteString("Phase 1: Tier 1 (procfs) collectors ran on a clean system — CPU, memory, disk, network baselines are accurate.\n")
	sb.WriteString("Phase 2: Tier 2/3 (BCC/eBPF) tools ran after baseline collection — latency histograms, events, and stack traces.\n\n")

	// Health score context
	sb.WriteString(fmt.Sprintf("Health Score: %d/100\n", report.Summary.HealthScore))

	// Anomalies
	anomalies := report.Summary.Anomalies
	if len(anomalies) > 0 {
		sb.WriteString(fmt.Sprintf("\nDetected Anomalies (%d):\n", len(anomalies)))
		for _, a := range anomalies {
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s (value=%s, threshold=%s)\n",
				strings.ToUpper(a.Severity), a.Category, a.Message, a.Value, a.Threshold))
		}
	}

	// USE metrics
	if len(report.Summary.Resources) > 0 {
		sb.WriteString("\nUSE Metrics:\n")
		for resource, use := range report.Summary.Resources {
			sb.WriteString(fmt.Sprintf("  %s: util=%.1f%%, sat=%.1f%%, err=%d\n",
				resource, use.Utilization, use.Saturation, use.Errors))
		}
	}

	// Focus area hints
	if len(report.Metadata.FocusAreas) > 0 {
		sb.WriteString(fmt.Sprintf("\nFocus areas requested: %s\n",
			strings.Join(report.Metadata.FocusAreas, ", ")))
		sb.WriteString("Pay special attention to these subsystems.\n")
	}

	// Stack trace hints
	hasStacks := false
	for _, results := range report.Categories {
		for _, r := range results {
			if len(r.Stacks) > 0 {
				hasStacks = true
				break
			}
		}
	}
	if hasStacks {
		sb.WriteString("\nStack traces are available. Analyze hot code paths and ")
		sb.WriteString("identify contention points (futex, mutex, I/O waits).\n")
		if hasPIDTarget {
			sb.WriteString("Stacks are filtered to the target PID — all code paths belong to the target application.\n")
		}
	}

	// Histogram hints
	hasHistograms := false
	for _, results := range report.Categories {
		for _, r := range results {
			if len(r.Histograms) > 0 {
				hasHistograms = true
				break
			}
		}
	}
	if hasHistograms {
		sb.WriteString("\nLatency histograms are available. Focus on p99/p999 for ")
		sb.WriteString("tail latency issues and multimodal distributions.\n")
		if hasPIDTarget {
			sb.WriteString("Histograms from PID-filtered tools reflect only the target application's latency profile.\n")
		}
	}

	// Observer effect note
	if report.Metadata.ObserverOverhead != nil {
		oh := report.Metadata.ObserverOverhead
		sb.WriteString(fmt.Sprintf(
			"\nOBSERVER EFFECT NOTE: melisai overhead during collection: "+
				"CPU=%dms user + %dms system, Memory=%dMB RSS. "+
				"Two-phase collection ensures Tier 1 baselines are unaffected by BCC tool overhead. "+
				"melisai PIDs excluded from TopByCPU/TopByMem lists.\n",
			oh.CPUUserMs, oh.CPUSystemMs,
			oh.MemoryRSSBytes/(1024*1024)))
	}

	sb.WriteString("\nProvide actionable, specific commands. ")
	sb.WriteString("Cite relevant kernel documentation or performance references.\n")

	ctx.Prompt = sb.String()
	return ctx
}

// knownAntiPatterns returns a list of common performance anti-patterns.
func knownAntiPatterns() []string {
	return []string{
		"P1: CPU saturation with single-threaded bottleneck (load_avg > num_cpus but one CPU at 100%)",
		"P2: Memory pressure cascade (high dirty_ratio → write stalls → iowait → apparent CPU saturation)",
		"P3: Swap death spiral (swap active + high major faults → exponential performance degradation)",
		"P4: Network retransmit storm (cubic congestion control + high RTT → throughput collapse)",
		"P5: Disk I/O amplification (random reads on rotational → queue depth explosion)",
		"P6: Lock contention hotspot (futex_wait in stack traces → serialized processing)",
		"P7: Container CPU throttling (cfs_quota too low → periodic latency spikes)",
		"P8: NUMA imbalance (cross-node memory access → 2-3x latency penalty)",
		"P9: IRQ imbalance (all interrupts on CPU 0 → single-core bottleneck)",
		"P10: File descriptor exhaustion (approaching ulimit → EMFILE errors)",
		"P11: Conntrack table overflow (nf_conntrack_max → packet drops)",
		"P12: TIME_WAIT accumulation (short-lived connections → port exhaustion)",
		"P13: Dirty page writeback storm (vm.dirty_ratio too high → periodic I/O stalls)",
		"P14: THP defragmentation stall (transparent hugepages + fragmented memory → allocation latency)",
		"P15: Scheduler migration overhead (processes bouncing between NUMA nodes)",
		"P16: I/O scheduler mismatch (using cfq on SSD instead of none/mq-deadline)",
		"P17: TCP buffer autotune failure (rmem/wmem too small for high-BDP links)",
		"P18: cgroup memory thrashing (near limit → constant reclaim → high PSI)",
		"P19: Kernel softlockup (debug logging storm → RCU stall)",
		"P20: DNS resolution blocking (gethostlatency spikes → application timeout cascade)",
		"P21: AppArmor per-packet overhead (LSM hooks on high-PPS workloads)",
		"P22: Per-process resource leak (FD count growing, RSS growing without release → eventual OOM/EMFILE)",
		"P23: Application thread pool exhaustion (all threads blocked on I/O or locks → request queuing)",
	}
}
