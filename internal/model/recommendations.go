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
						Type:           "fix",
						Title:          "CPU saturation detected — investigate high-CPU processes",
						Commands:       []string{"melisai collect --profile deep --focus stacks"},
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
						Type:     "optimization",
						Title:    "Reduce CFS scheduling latency for interactive workloads",
						Commands: []string{
							"sysctl -w kernel.sched_latency_ns=6000000",
							"sysctl -w kernel.sched_min_granularity_ns=750000",
						},
						Persistent: []string{
							"echo 'kernel.sched_latency_ns=6000000' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'kernel.sched_min_granularity_ns=750000' >> /etc/sysctl.d/99-melisai.conf",
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
						Type:     "fix",
						Title:    "Reduce swappiness for database/latency-sensitive workloads",
						Commands: []string{"sysctl -w vm.swappiness=10"},
						Persistent: []string{
							"echo 'vm.swappiness=10' >> /etc/sysctl.d/99-melisai.conf",
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
						Type:     "fix",
						Title:    "Lower dirty_ratio to prevent write stalls",
						Commands: []string{
							"sysctl -w vm.dirty_ratio=10",
							"sysctl -w vm.dirty_background_ratio=5",
						},
						Persistent: []string{
							"echo 'vm.dirty_ratio=10' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'vm.dirty_background_ratio=5' >> /etc/sysctl.d/99-melisai.conf",
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
						Type:           "optimization",
						Title:          "Consider disabling memory overcommit for production",
						Commands:       []string{"sysctl -w vm.overcommit_memory=2"},
						Persistent:     []string{"echo 'vm.overcommit_memory=2' >> /etc/sysctl.d/99-melisai.conf"},
						ExpectedImpact: "Prevent OOM kills by enforcing commit limit",
						Evidence:       formatEvidence("overcommit_memory=%d", mem.OvercommitMemory),
						Source:         "Linux kernel documentation",
					})
					priority++
				}
				// Direct reclaim pressure — increase watermarks
				if mem.Reclaim != nil && mem.Reclaim.DirectReclaimRate > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Type:     "fix",
						Title:    "Direct reclaim active — increase watermark reserves",
						Commands: []string{
							"sysctl -w vm.watermark_scale_factor=200",
							"sysctl -w vm.min_free_kbytes=131072",
						},
						Persistent: []string{
							"echo 'vm.watermark_scale_factor=200' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'vm.min_free_kbytes=131072' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Trigger kswapd earlier, prevent applications from blocking on page reclaim",
						Evidence:       formatEvidence("direct_reclaim_rate=%.0f/s, pgscan_direct=%d, allocstall=%d", mem.Reclaim.DirectReclaimRate, mem.Reclaim.PgscanDirect, mem.Reclaim.AllocstallNormal),
						Source:         "Linux VM subsystem, Brendan Gregg Systems Performance ch.7",
					})
					priority++
				}
				// THP splits — consider madvise mode
				if mem.Reclaim != nil && mem.Reclaim.THPSplitRate > 1 && mem.THPEnabled == "always" {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Type:     "fix",
						Title:    "THP splits detected with THP=always — switch to madvise",
						Commands: []string{
							"echo madvise > /sys/kernel/mm/transparent_hugepage/enabled",
							"echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag",
						},
						ExpectedImpact: "Eliminate THP compaction stalls and TLB thrashing from forced splits",
						Evidence:       formatEvidence("thp_split_rate=%.1f/s, thp_enabled=%s, thp_defrag=%s", mem.Reclaim.THPSplitRate, mem.THPEnabled, mem.THPDefrag),
						Source:         "Linux THP documentation, PostgreSQL/MySQL best practices",
					})
					priority++
				}
				// NUMA miss ratio
				for _, node := range mem.NUMANodes {
					if node.MissRatio > 5 {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "memory",
							Type:     "fix",
							Title:    fmt.Sprintf("NUMA node %d has %.1f%% miss ratio — cross-node memory access", node.Node, node.MissRatio),
							Commands: []string{
								"# Pin critical processes to correct NUMA node:",
								fmt.Sprintf("# numactl --cpunodebind=%d --membind=%d <command>", node.Node, node.Node),
								"# Or enable automatic NUMA balancing:",
								"sysctl -w kernel.sched_numa_balancing=1",
							},
							Persistent: []string{
								"echo 'kernel.sched_numa_balancing=1' >> /etc/sysctl.d/99-melisai.conf",
							},
							ExpectedImpact: "Reduce cross-NUMA memory access (30-50% latency penalty per hop)",
							Evidence:       formatEvidence("node%d: miss_ratio=%.1f%%, hit=%d, miss=%d, cpus=%s", node.Node, node.MissRatio, node.NumaHit, node.NumaMiss, node.CPUs),
							Source:         "Linux NUMA, Brendan Gregg Systems Performance ch.7",
						})
						priority++
						break // one NUMA recommendation is enough
					}
				}
				// Compaction stalls + dirty writeback too slow
				if mem.Reclaim != nil && mem.Reclaim.CompactStallRate > 1 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Type:     "fix",
						Title:    "Compaction stalls detected — memory fragmented",
						Commands: []string{
							"echo 1 > /proc/sys/vm/compact_memory",
							"sysctl -w vm.extfrag_threshold=500",
						},
						ExpectedImpact: "Reduce allocation latency from memory fragmentation",
						Evidence:       formatEvidence("compact_stall_rate=%.1f/s, compact_stall=%d, compact_fail=%d", mem.Reclaim.CompactStallRate, mem.Reclaim.CompactStall, mem.Reclaim.CompactFail),
						Source:         "Linux VM compaction, kernel mm documentation",
					})
					priority++
				}
			}
		}
	}

	// Network recommendations
	if netResults, ok := report.Categories["network"]; ok {
		for _, r := range netResults {
			if net, ok := r.Data.(*NetworkData); ok && net.Sysctls != nil {
				sc := net.Sysctls
				if sc.CongestionCtrl != "" && sc.CongestionCtrl != "bbr" {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Enable BBR congestion control",
						Commands: []string{
							"sysctl -w net.core.default_qdisc=fq",
							"sysctl -w net.ipv4.tcp_congestion_control=bbr",
						},
						Persistent: []string{
							"echo 'net.core.default_qdisc=fq' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "2-3x throughput on high-BDP links, lower retransmits",
						Evidence:       formatEvidence("tcp_congestion_control=%s", sc.CongestionCtrl),
						Source:         "Google BBR paper, Brendan Gregg Systems Performance ch.10",
					})
					priority++
				}
				if net.TCP != nil && net.TCP.RetransRate > 1.0 {
					recs = append(recs, Recommendation{
						Priority:       priority,
						Category:       "network",
						Type:           "fix",
						Title:          "Investigate TCP retransmissions",
						Commands:       []string{"melisai collect --profile deep --focus network"},
						ExpectedImpact: "Identify network path issues causing packet loss",
						Evidence:       formatEvidence("retrans_rate=%.1f/s", net.TCP.RetransRate),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if sc.SomaxConn > 0 && sc.SomaxConn < 4096 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase listen backlog for high-traffic servers",
						Commands: []string{"sysctl -w net.core.somaxconn=4096"},
						Persistent: []string{
							"echo 'net.core.somaxconn=4096' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Prevent connection drops under burst load",
						Evidence:       formatEvidence("somaxconn=%d", sc.SomaxConn),
						Source:         "Linux networking documentation",
					})
					priority++
				}
				if isLowTCPBuffer(sc.TCPRmem) {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase TCP receive buffer sizes",
						Commands: []string{"sysctl -w net.ipv4.tcp_rmem='4096 87380 6291456'"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_rmem=4096 87380 6291456' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Better throughput on high-BDP paths",
						Evidence:       formatEvidence("tcp_rmem=%s", sc.TCPRmem),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if isLowTCPBuffer(sc.TCPWmem) {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase TCP send buffer sizes",
						Commands: []string{"sysctl -w net.ipv4.tcp_wmem='4096 65536 6291456'"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_wmem=4096 65536 6291456' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Better throughput on high-BDP paths",
						Evidence:       formatEvidence("tcp_wmem=%s", sc.TCPWmem),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if sc.TCPTWReuse == 0 && net.TCP != nil && net.TCP.TimeWaitCount > 1000 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Enable TIME_WAIT socket reuse",
						Commands: []string{"sysctl -w net.ipv4.tcp_tw_reuse=1"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_tw_reuse=1' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Reduce TIME_WAIT socket accumulation on busy servers",
						Evidence:       formatEvidence("tcp_tw_reuse=%d, timewait_count=%d", sc.TCPTWReuse, net.TCP.TimeWaitCount),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if sc.TCPMaxSynBacklog > 0 && sc.TCPMaxSynBacklog < 4096 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase SYN backlog for high-connection-rate servers",
						Commands: []string{"sysctl -w net.ipv4.tcp_max_syn_backlog=8192"},
						Persistent: []string{
							"echo 'net.ipv4.tcp_max_syn_backlog=8192' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Handle connection bursts without SYN drops",
						Evidence:       formatEvidence("tcp_max_syn_backlog=%d", sc.TCPMaxSynBacklog),
						Source:         "Brendan Gregg, Systems Performance ch.10",
					})
					priority++
				}
				if net.Conntrack != nil && net.Conntrack.UsagePct > 70 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Conntrack table approaching capacity",
						Commands: []string{
							fmt.Sprintf("sysctl -w net.netfilter.nf_conntrack_max=%d", net.Conntrack.Max*2),
						},
						Persistent: []string{
							fmt.Sprintf("echo 'net.netfilter.nf_conntrack_max=%d' >> /etc/sysctl.d/99-melisai.conf", net.Conntrack.Max*2),
						},
						ExpectedImpact: "Prevent conntrack table full drops",
						Evidence:       formatEvidence("conntrack count=%d, max=%d, usage=%.1f%%", net.Conntrack.Count, net.Conntrack.Max, net.Conntrack.UsagePct),
						Source:         "Linux conntrack documentation",
					})
					priority++
				}
				for _, iface := range net.Interfaces {
					if iface.RingRxCur > 0 && iface.RingRxMax > 0 && iface.RingRxCur < iface.RingRxMax/2 && iface.RxDiscards > 0 {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "network",
							Type:     "fix",
							Title:    fmt.Sprintf("Increase ring buffer on %s (rx_discards detected)", iface.Name),
							Commands: []string{
								fmt.Sprintf("ethtool -G %s rx %d", iface.Name, iface.RingRxMax),
							},
							ExpectedImpact: "Reduce NIC-level packet drops during traffic bursts",
							Evidence:       formatEvidence("%s: ring_rx=%d/%d, rx_discards=%d, driver=%s", iface.Name, iface.RingRxCur, iface.RingRxMax, iface.RxDiscards, iface.Driver),
							Source:         "Brendan Gregg, Systems Performance ch.10",
						})
						priority++
					}
				}
				if net.TCPExt != nil && net.TCPExt.ListenOverflows > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Listen queue overflows detected — increase backlog and enable SO_REUSEPORT",
						Commands: []string{
							"sysctl -w net.core.somaxconn=65535",
							"# For nginx: listen 80 reuseport backlog=8192;",
						},
						Persistent: []string{
							"echo 'net.core.somaxconn=65535' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Eliminate accept queue overflow drops under burst load",
						Evidence:       formatEvidence("ListenOverflows=%d, ListenDrops=%d", net.TCPExt.ListenOverflows, net.TCPExt.ListenDrops),
						Source:         "Linux networking, SO_REUSEPORT documentation",
					})
					priority++
				}
				if sc.NetdevMaxBacklog > 0 && sc.NetdevMaxBacklog < 5000 && net.Softnet != nil {
					hasSoftnetDrops := false
					for _, s := range net.Softnet.Stats {
						if s.Dropped > 0 {
							hasSoftnetDrops = true
							break
						}
					}
					if hasSoftnetDrops {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "network",
							Type:     "fix",
							Title:    "Increase netdev_max_backlog (softnet drops detected)",
							Commands: []string{
								"sysctl -w net.core.netdev_max_backlog=10000",
							},
							Persistent: []string{
								"echo 'net.core.netdev_max_backlog=10000' >> /etc/sysctl.d/99-melisai.conf",
							},
							ExpectedImpact: "Reduce packet drops between NIC and kernel processing",
							Evidence:       formatEvidence("netdev_max_backlog=%d", sc.NetdevMaxBacklog),
							Source:         "RHEL Network Performance Tuning, Brendan Gregg Systems Performance ch.10",
						})
						priority++
					}
				}
				if sc.RmemMax > 0 && sc.RmemMax < 16777216 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase socket buffer maximums (rmem_max/wmem_max)",
						Commands: []string{
							"sysctl -w net.core.rmem_max=16777216",
							"sysctl -w net.core.wmem_max=16777216",
						},
						Persistent: []string{
							"echo 'net.core.rmem_max=16777216' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'net.core.wmem_max=16777216' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Allow applications to request larger socket buffers (required for high-BDP and UDP workloads)",
						Evidence:       formatEvidence("rmem_max=%d, wmem_max=%d", sc.RmemMax, sc.WmemMax),
						Source:         "ESnet Fasterdata, RHEL Network Tuning Guide",
					})
					priority++
				}
				if sc.IPLocalPortRange != "" {
					fields := strings.Fields(sc.IPLocalPortRange)
					if len(fields) == 2 {
						lo, _ := strconv.Atoi(fields[0])
						hi, _ := strconv.Atoi(fields[1])
						if hi-lo < 40000 {
							recs = append(recs, Recommendation{
								Priority: priority,
								Category: "network",
								Type:     "optimization",
								Title:    "Expand ephemeral port range",
								Commands: []string{
									"sysctl -w net.ipv4.ip_local_port_range='1024 65535'",
								},
								Persistent: []string{
									"echo 'net.ipv4.ip_local_port_range=1024 65535' >> /etc/sysctl.d/99-melisai.conf",
								},
								ExpectedImpact: "More ports for outbound connections (prevents EADDRNOTAVAIL under load)",
								Evidence:       formatEvidence("ip_local_port_range=%s (%d ports)", sc.IPLocalPortRange, hi-lo),
								Source:         "RHEL 9 Network Performance Tuning",
							})
							priority++
						}
					}
				}
				if sc.TCPSlowStartAfterIdle == 1 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Disable TCP slow start after idle for persistent connections",
						Commands: []string{
							"sysctl -w net.ipv4.tcp_slow_start_after_idle=0",
						},
						Persistent: []string{
							"echo 'net.ipv4.tcp_slow_start_after_idle=0' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Prevent congestion window reset on idle keepalive connections",
						Evidence:       formatEvidence("tcp_slow_start_after_idle=%d", sc.TCPSlowStartAfterIdle),
						Source:         "Cloudflare TCP optimization, RFC 7661",
					})
					priority++
				}
				if sc.TCPFastOpen >= 0 && sc.TCPFastOpen < 3 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Enable TCP Fast Open (client + server)",
						Commands: []string{
							"sysctl -w net.ipv4.tcp_fastopen=3",
						},
						Persistent: []string{
							"echo 'net.ipv4.tcp_fastopen=3' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Save 1 RTT on reconnections (especially for web servers)",
						Evidence:       formatEvidence("tcp_fastopen=%d", sc.TCPFastOpen),
						Source:         "RHEL 9 Network Tuning, Linux tcp(7)",
					})
					priority++
				}
				hasSqueeze := false
				if net.Softnet != nil {
				for _, s := range net.Softnet.Stats {
					if s.TimeSqueeze > 0 {
						hasSqueeze = true
						break
					}
				}
				}
				if hasSqueeze && sc.NetdevBudget > 0 && sc.NetdevBudget < 600 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Increase netdev_budget (time_squeeze detected)",
						Commands: []string{
							"sysctl -w net.core.netdev_budget=600",
							"sysctl -w net.core.netdev_budget_usecs=8000",
						},
						Persistent: []string{
							"echo 'net.core.netdev_budget=600' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'net.core.netdev_budget_usecs=8000' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Allow kernel more time/packets per NAPI poll cycle",
						Evidence:       formatEvidence("netdev_budget=%d, netdev_budget_usecs=%d", sc.NetdevBudget, sc.NetdevBudgetUsecs),
						Source:         "Brendan Gregg USE Method, Packagecloud Linux Networking Stack",
					})
					priority++
				}
				if net.UDP != nil && net.UDP.RcvbufErrors > 0 && sc.RmemMax > 0 && sc.RmemMax < 26214400 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Increase rmem_max for UDP workloads (RcvbufErrors detected)",
						Commands: []string{
							"sysctl -w net.core.rmem_max=26214400",
						},
						Persistent: []string{
							"echo 'net.core.rmem_max=26214400' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Allow UDP sockets to use larger receive buffers, prevent drops",
						Evidence:       formatEvidence("udp_rcvbuf_errors=%d, rmem_max=%d", net.UDP.RcvbufErrors, sc.RmemMax),
						Source:         "Packagecloud Linux Networking Stack Monitoring",
					})
					priority++
				}
				// TCP Recv-Q saturation — app not reading fast enough
				if net.RecvQSaturated > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "Application not consuming TCP data fast enough (Recv-Q saturation)",
						Commands: []string{
							"ss -tnp | awk '$2 > 65536 {print}'",
							"# Identify the process and optimize its read loop or add worker threads",
						},
						ExpectedImpact: "Prevent TCP zero-window stalls and improve application throughput",
						Evidence:       formatEvidence("established_sockets_with_recvq>64KB=%d", net.RecvQSaturated),
						Source:         "Linux TCP socket buffers, Brendan Gregg Systems Performance ch.10",
					})
					priority++
				}

				// Listen queue near full — need SO_REUSEPORT
				for _, ls := range net.ListenSockets {
					if ls.FillPct >= 70 {
						recs = append(recs, Recommendation{
							Priority: priority,
							Category: "network",
							Type:     "fix",
							Title:    fmt.Sprintf("Accept queue %s is %.0f%% full — enable SO_REUSEPORT", ls.LocalAddr, ls.FillPct),
							Commands: []string{
								"# Enable SO_REUSEPORT so each worker thread gets its own accept queue:",
								"# nginx: listen <port> reuseport;",
								"# Go:    net.ListenConfig{Control: func(...) { unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1) }}",
								fmt.Sprintf("sysctl -w net.core.somaxconn=%d", max(ls.SendQ*2, 65535)),
							},
							ExpectedImpact: "Distribute accept() across threads, prevent SYN drops",
							Evidence:       formatEvidence("%s: recv_q=%d, backlog=%d, fill=%.1f%%", ls.LocalAddr, ls.RecvQ, ls.SendQ, ls.FillPct),
							Source:         "Linux SO_REUSEPORT, Cloudflare SYN packet handling",
						})
						priority++
						break // one recommendation per socket is enough
					}
				}

				// TCP zero window drops — app can't keep up with incoming data
				if net.TCPExt != nil && net.TCPExt.TCPZeroWindowDropRate > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "TCP zero-window drops detected — application not consuming data fast enough",
						Commands: []string{
							"sysctl -w net.ipv4.tcp_rmem='4096 131072 16777216'",
							"# Profile the application: is read() blocking on disk, locks, or compute?",
						},
						Persistent: []string{
							"echo 'net.ipv4.tcp_rmem=4096 131072 16777216' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Larger receive buffers give the application more headroom before window closes",
						Evidence:       formatEvidence("tcp_zero_window_drop_rate=%.1f/s, tcp_to_zero_window=%d", net.TCPExt.TCPZeroWindowDropRate, net.TCPExt.TCPToZeroWindowAdv),
						Source:         "Linux TCP flow control, Brendan Gregg Systems Performance ch.10",
					})
					priority++
				}

				// ARP table too small for container/K8s environments
				if sc.ARPGcThresh3 > 0 && sc.ARPGcThresh3 <= 1024 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "optimization",
						Title:    "Increase ARP neighbor table for container/K8s environments",
						Commands: []string{
							"sysctl -w net.ipv4.neigh.default.gc_thresh1=2048",
							"sysctl -w net.ipv4.neigh.default.gc_thresh2=4096",
							"sysctl -w net.ipv4.neigh.default.gc_thresh3=8192",
						},
						Persistent: []string{
							"echo 'net.ipv4.neigh.default.gc_thresh1=2048' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'net.ipv4.neigh.default.gc_thresh2=4096' >> /etc/sysctl.d/99-melisai.conf",
							"echo 'net.ipv4.neigh.default.gc_thresh3=8192' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Prevent 'neighbour table overflow' errors in large subnets or container environments",
						Evidence:       formatEvidence("arp_gc_thresh3=%d", sc.ARPGcThresh3),
						Source:         "Kubernetes networking, Linux neigh(7)",
					})
					priority++
				}

				if net.TCPExt != nil && net.TCPExt.PruneCalled > 0 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "network",
						Type:     "fix",
						Title:    "TCP memory pressure detected (PruneCalled) — increase tcp_mem",
						Commands: []string{
							"sysctl -w net.ipv4.tcp_mem='1048576 2097152 4194304'",
						},
						Persistent: []string{
							"echo 'net.ipv4.tcp_mem=1048576 2097152 4194304' >> /etc/sysctl.d/99-melisai.conf",
						},
						ExpectedImpact: "Prevent kernel from pruning TCP receive buffers under high connection count",
						Evidence:       formatEvidence("PruneCalled=%d, tcp_mem=%s", net.TCPExt.PruneCalled, sc.TCPMem),
						Source:         "Linux TCP memory management, Brendan Gregg Systems Performance ch.10",
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
							Type:     "optimization",
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
							Type:     "optimization",
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
				if mem.THPEnabled == "always" {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Type:     "optimization",
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
				if mem.TotalBytes > 16*1024*1024*1024 && mem.MinFreeKbytes > 0 && mem.MinFreeKbytes < 65536 {
					recs = append(recs, Recommendation{
						Priority: priority,
						Category: "memory",
						Type:     "optimization",
						Title:    "Increase vm.min_free_kbytes for large memory system",
						Commands: []string{"sysctl -w vm.min_free_kbytes=131072"},
						Persistent: []string{
							"echo 'vm.min_free_kbytes=131072' >> /etc/sysctl.d/99-melisai.conf",
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
