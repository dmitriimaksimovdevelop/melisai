package model

import (
	"fmt"
	"strconv"
	"strings"
)

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
				// TCP rmem/wmem too low
				if isLowTCPBuffer(net.TCPRmem) {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Increase TCP receive buffer sizes",
						Commands: []string{"sysctl -w net.ipv4.tcp_rmem='4096 87380 6291456'"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_rmem=4096 87380 6291456' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Better throughput on high-BDP paths",
						Evidence:       formatEvidence("tcp_rmem=%s", net.TCPRmem),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if isLowTCPBuffer(net.TCPWmem) {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Increase TCP send buffer sizes",
						Commands: []string{"sysctl -w net.ipv4.tcp_wmem='4096 65536 6291456'"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_wmem=4096 65536 6291456' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Better throughput on high-BDP paths",
						Evidence:       formatEvidence("tcp_wmem=%s", net.TCPWmem),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				// tcp_tw_reuse
				if net.TCPTWReuse == 0 && net.TCP != nil && net.TCP.TimeWaitCount > 1000 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Enable TIME_WAIT socket reuse",
						Commands: []string{"sysctl -w net.ipv4.tcp_tw_reuse=1"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_tw_reuse=1' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Reduce TIME_WAIT socket accumulation on busy servers",
						Evidence:       formatEvidence("tcp_tw_reuse=%d, timewait_count=%d", net.TCPTWReuse, net.TCP.TimeWaitCount),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				// tcp_max_syn_backlog
				if net.TCPMaxSynBacklog > 0 && net.TCPMaxSynBacklog < 4096 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Title:    "Increase SYN backlog for high-connection-rate servers",
						Commands: []string{"sysctl -w net.ipv4.tcp_max_syn_backlog=8192"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_max_syn_backlog=8192' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Handle connection bursts without SYN drops",
						Evidence:       formatEvidence("tcp_max_syn_backlog=%d", net.TCPMaxSynBacklog),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
			}
		}
	}

	// Disk scheduler recommendations
	if diskResults, ok := report.Categories["disk"]; ok {
		for _, r := range diskResults {
			if disk, ok := r.Data.(*DiskData); ok {
				for _, dev := range disk.Devices {
					if !dev.Rotational && dev.Scheduler != "" && dev.Scheduler != "mq-deadline" && dev.Scheduler != "none" {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "disk",
							Title:    fmt.Sprintf("Switch %s to mq-deadline scheduler (SSD)", dev.Name),
							Commands: []string{
								fmt.Sprintf("echo mq-deadline > /sys/block/%s/queue/scheduler", dev.Name),
							},
							Persistent: []string{
								fmt.Sprintf("echo 'ACTION==\"add|change\", KERNEL==\"%s\", ATTR{queue/scheduler}=\"mq-deadline\"' >> /etc/udev/rules.d/60-scheduler.rules", dev.Name),
							},
							ExpectedImpact: "Lower latency for SSD workloads",
							Evidence:       formatEvidence("device=%s, scheduler=%s, rotational=false", dev.Name, dev.Scheduler),
							Source:         "Brendan Gregg, Systems Performance ch.9",
						})
						priority++
					}
					if dev.Rotational && dev.Scheduler != "" && dev.Scheduler != "bfq" {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "disk",
							Title:    fmt.Sprintf("Switch %s to BFQ scheduler (HDD)", dev.Name),
							Commands: []string{
								fmt.Sprintf("echo bfq > /sys/block/%s/queue/scheduler", dev.Name),
							},
							Persistent: []string{
								fmt.Sprintf("echo 'ACTION==\"add|change\", KERNEL==\"%s\", ATTR{queue/scheduler}=\"bfq\"' >> /etc/udev/rules.d/60-scheduler.rules", dev.Name),
							},
							ExpectedImpact: "Better I/O fairness for HDD workloads",
							Evidence:       formatEvidence("device=%s, scheduler=%s, rotational=true", dev.Name, dev.Scheduler),
							Source:         "Brendan Gregg, Systems Performance ch.9",
						})
						priority++
					}
				}
			}
		}
	}

	// Memory: THP and min_free_kbytes recommendations
	if memResults, ok := report.Categories["memory"]; ok {
		for _, r := range memResults {
			if mem, ok := r.Data.(*MemoryData); ok {
				// THP recommendation for latency-sensitive workloads
				if mem.THPEnabled == "always" {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Title:    "Consider disabling THP for latency-sensitive workloads",
						Commands: []string{
							"echo madvise > /sys/kernel/mm/transparent_hugepage/enabled",
						},
						Persistent: []string{
							"echo 'echo madvise > /sys/kernel/mm/transparent_hugepage/enabled' >> /etc/rc.local",
						},
						ExpectedImpact: "Eliminate THP compaction stalls and latency spikes",
						Evidence:       formatEvidence("transparent_hugepage=%s", mem.THPEnabled),
						Source:         "Brendan Gregg, Systems Performance ch.7",
					})
					priority++
				}
				// vm.min_free_kbytes too low for large memory systems
				if mem.TotalBytes > 16*1024*1024*1024 && mem.MinFreeKbytes > 0 && mem.MinFreeKbytes < 65536 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Title:    "Increase vm.min_free_kbytes for large memory system",
						Commands: []string{"sysctl -w vm.min_free_kbytes=131072"},
						Persistent: []string{
							"echo 'vm.min_free_kbytes=131072' >> /etc/sysctl.d/99-sysdiag.conf",
						},
						ExpectedImpact: "Reduce direct reclaim stalls under memory pressure",
						Evidence:       formatEvidence("min_free_kbytes=%d, total_bytes=%d", mem.MinFreeKbytes, mem.TotalBytes),
						Source:         "Brendan Gregg, Systems Performance ch.7",
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

// isLowTCPBuffer checks if a tcp_rmem/wmem string has a max value below 4MB.
// Format: "min default max" (e.g., "4096 87380 6291456").
func isLowTCPBuffer(buf string) bool {
	if buf == "" {
		return false
	}
	fields := strings.Fields(buf)
	if len(fields) < 3 {
		return false
	}
	maxVal, err := strconv.Atoi(fields[2])
	if err != nil {
		return false
	}
	return maxVal < 4194304 // 4MB
}

