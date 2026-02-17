package model

import "fmt"

// GenerateRecommendations produces actionable sysctl/config recommendations
// based on collected metrics and detected anomalies.
func GenerateRecommendations(report *Report) []Recommendation {
	var recs []Recommendation
	priority := 1

	// Check CPU recommendations
	if cpuResults, ok := report.Categories["cpu"]; ok {
		for _, r := range cpuResults {
			if cpu, ok := r.Data.(*CPUData); ok {
				if cpu.LoadAvg1/float64(max(cpu.NumCPUs, 1)) > 2.0 {
					recs = append(recs, Recommendation{
						Priority:       priority,
						Category:       "cpu",
						Title:          "CPU saturation detected — investigate high-CPU processes",
						Commands:       []string{"sysdiag collect --profile deep --focus stacks"},
						Persistent:     []string{},
						ExpectedImpact: "Identify CPU-bound bottleneck",
						Evidence:       formatEvidence("load_avg_1=%.2f, num_cpus=%d", cpu.LoadAvg1, cpu.NumCPUs),
						Source:         "Brendan Gregg, Systems Performance ch.6",
					})
					priority++
				}
				if cpu.SchedLatencyNS > 24000000 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "cpu",
						Title:    "Reduce CFS scheduling latency for interactive workloads",
						Commands: []string{
							"sysctl -w kernel.sched_latency_ns=6000000",
							"sysctl -w kernel.sched_min_granularity_ns=750000",
						},
						Persistent: []string{
							"echo 'kernel.sched_latency_ns=6000000' >> /etc/sysctl.d/99-sysdiag.conf",
							"echo 'kernel.sched_min_granularity_ns=750000' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Lower tail latency for latency-sensitive workloads",
						Evidence:       formatEvidence("sched_latency_ns=%d", cpu.SchedLatencyNS),
						Source:         "Linux kernel CFS documentation",
					})
					priority++
				}
			}
		}
	}

	// Memory recommendations
	if memResults, ok := report.Categories["memory"]; ok {
		for _, r := range memResults {
			if mem, ok := r.Data.(*MemoryData); ok {
				if mem.Swappiness > 30 && mem.SwapUsedBytes > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Title:    "Reduce swappiness for database/latency-sensitive workloads",
						Commands: []string{"sysctl -w vm.swappiness=10"},
						Persistent: []string{
							"echo 'vm.swappiness=10' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Reduce swap-induced latency spikes",
						Evidence:       formatEvidence("swappiness=%d, swap_used=%d", mem.Swappiness, mem.SwapUsedBytes),
						Source:         "Brendan Gregg, Systems Performance ch.7",
					})
					priority++
				}
				if mem.DirtyRatio > 20 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Title:    "Lower dirty_ratio to prevent write stalls",
						Commands: []string{
							"sysctl -w vm.dirty_ratio=10",
							"sysctl -w vm.dirty_background_ratio=5",
						},
						Persistent: []string{
							"echo 'vm.dirty_ratio=10' >> /etc/sysctl.d/99-sysdiag.conf",
							"echo 'vm.dirty_background_ratio=5' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Reduce periodic write stalls on heavy I/O workloads",
						Evidence:       formatEvidence("dirty_ratio=%d", mem.DirtyRatio),
						Source:         "Linux kernel documentation (vm.dirty_ratio)",
					})
					priority++
				}
				if mem.OvercommitMemory == 0 {
					recs = append(recs, Recommendation{
						Priority:       priority,
						Category:       "memory",
						Title:          "Consider disabling memory overcommit for production",
						Commands:       []string{"sysctl -w vm.overcommit_memory=2"},
						Persistent:     []string{"echo 'vm.overcommit_memory=2' >> /etc/sysctl.d/99-sysdiag.conf"},
						ExpectedImpact: "Prevent OOM kills by enforcing commit limit",
						Evidence:       formatEvidence("overcommit_memory=%d", mem.OvercommitMemory),
						Source:         "Linux kernel documentation",
					})
					priority++
				}
			}
		}
	}

	// Network recommendations
	if netResults, ok := report.Categories["network"]; ok {
		for _, r := range netResults {
			if net, ok := r.Data.(*NetworkData); ok {
				if net.CongestionCtrl != "" && net.CongestionCtrl != "bbr" {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Enable BBR congestion control",
						Commands: []string{
							"sysctl -w net.core.default_qdisc=fq",
							"sysctl -w net.ipv4.tcp_congestion_control=bbr",
						},
						Persistent: []string{
							"echo 'net.core.default_qdisc=fq' >> /etc/sysctl.d/99-sysdiag.conf",
							"echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "2-3x throughput on high-BDP links, lower retransmits",
						Evidence:       formatEvidence("tcp_congestion_control=%s", net.CongestionCtrl),
						Source:         "Google BBR paper, Brendan Gregg Systems Performance ch.10",
					})
					priority++
				}
				if net.TCP != nil && net.TCP.RetransSegs > 100 {
					recs = append(recs, Recommendation{
						Priority:       priority,
						Category:       "network",
						Title:          "Investigate TCP retransmissions",
						Commands:       []string{"sysdiag collect --profile deep --focus network"},
						ExpectedImpact: "Identify network path issues causing packet loss",
						Evidence:       formatEvidence("retrans_segs=%d", net.TCP.RetransSegs),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if net.SomaxConn > 0 && net.SomaxConn < 4096 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Increase listen backlog for high-traffic servers",
						Commands: []string{"sysctl -w net.core.somaxconn=4096"},
						Persistent: []string{
							"echo 'net.core.somaxconn=4096' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Prevent connection drops under burst load",
						Evidence:       formatEvidence("somaxconn=%d", net.SomaxConn),
						Source:         "Linux networking documentation",
					})
					priority++
				}
			}
		}
	}

	return recs
}

func formatEvidence(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
