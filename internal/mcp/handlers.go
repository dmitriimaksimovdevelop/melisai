package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/collector"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
	"github.com/dmitriimaksimovdevelop/melisai/internal/orchestrator"
	"github.com/dmitriimaksimovdevelop/melisai/internal/output"
	"github.com/mark3labs/mcp-go/mcp"
)

// healthCheckTimeout is the maximum time for a quick health check.
const healthCheckTimeout = 30 * time.Second

// collectMetricsTimeout is the maximum time for a full profile run.
const collectMetricsTimeout = 5 * time.Minute

// handleGetHealth runs a quick Tier 1 check and returns health score summary.
func handleGetHealth(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// Use "quick" profile so RegisterCollectors only creates Tier 1 collectors,
	// avoiding unnecessary BCC tool discovery overhead.
	cfg := collector.DefaultConfig()
	cfg.Profile = "quick"
	cfg.Duration = 1 * time.Second
	cfg.Quiet = true

	allCollectors := orchestrator.RegisterCollectors(cfg)

	// Keep only Tier 1 (procfs-based) collectors.
	tier1Names := map[string]bool{
		"system_info":     true,
		"cpu_utilization": true,
		"memory_info":     true,
		"disk_stats":      true,
		"network_stats":   true,
		"process_info":    true,
		"container_info":  true,
	}
	var filtered []collector.Collector
	for _, c := range allCollectors {
		if tier1Names[c.Name()] {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) == 0 {
		return errResult("no collectors available"), nil
	}

	orch := orchestrator.New(filtered, cfg)
	report, err := orch.Run(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("collection failed: %v", err)), nil
	}

	loadAvg := 0.0
	if cpuRes, ok := report.Summary.Resources["cpu"]; ok {
		loadAvg = cpuRes.Saturation
	}

	// Ensure anomalies is always an array, never null, for easier consumption by AI agents.
	anomalies := report.Summary.Anomalies
	if anomalies == nil {
		anomalies = []model.Anomaly{}
	}

	summary := map[string]interface{}{
		"health_score": report.Summary.HealthScore,
		"anomalies":    anomalies,
		"load_avg":     loadAvg,
		"message":      "System health check complete. Use 'collect_metrics' for deep diagnosis if score is low.",
	}

	jsonData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("json marshal failed: %v", err)), nil
	}
	return newTextResult(string(jsonData)), nil
}

// handleCollectMetrics runs a full profile.
func handleCollectMetrics(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, collectMetricsTimeout)
	defer cancel()

	args := getArgs(request)

	profileStr := stringArg(args, "profile", "quick")
	focusStr := stringArg(args, "focus", "")
	var focusAreas []string
	if focusStr != "" && focusStr != "all" {
		focusAreas = []string{focusStr}
	}

	var pids []int
	if pidVal, ok := args["pid"]; ok && pidVal != nil {
		if pidFloat, ok := pidVal.(float64); ok {
			pids = []int{int(pidFloat)}
		}
	}

	cfg := collector.DefaultConfig()
	cfg.Profile = profileStr
	cfg.Quiet = true
	cfg.Focus = focusAreas
	cfg.TargetPIDs = pids

	profConfig := orchestrator.GetProfile(profileStr)
	cfg.Duration = profConfig.Duration

	collectors := orchestrator.RegisterCollectors(cfg)
	if len(collectors) == 0 {
		return errResult("no collectors available"), nil
	}

	orch := orchestrator.New(collectors, cfg)
	report, err := orch.Run(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("collection failed: %v", err)), nil
	}

	report.AIContext = output.GenerateAIPrompt(report)

	jsonData, err := json.Marshal(report)
	if err != nil {
		return errResult(fmt.Sprintf("json marshal failed: %v", err)), nil
	}

	return newTextResult(string(jsonData)), nil
}

// handleExplainAnomaly provides detailed explanation for a specific anomaly metric.
func handleExplainAnomaly(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(request)
	anomalyID := stringArg(args, "anomaly_id", "")
	if anomalyID == "" {
		return errResult("anomaly_id is required"), nil
	}

	desc, ok := anomalyExplanations[anomalyID]
	if !ok {
		return newTextResult(fmt.Sprintf(
			"No specific explanation for anomaly '%s'. "+
				"General recommendation: check USE metrics (Utilization, Saturation, Errors) "+
				"for the affected subsystem. Run 'collect_metrics' with appropriate focus area.",
			anomalyID,
		)), nil
	}

	return newTextResult(desc), nil
}

// handleListAnomalies returns all known anomaly metric IDs grouped by category.
func handleListAnomalies(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type entry struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Brief    string `json:"brief"`
	}

	// Build list from anomalyExplanations, extracting category from threshold definitions.
	categoryMap := make(map[string]string)
	for _, t := range model.DefaultThresholds() {
		categoryMap[t.Metric] = t.Category
	}

	var entries []entry
	for id := range anomalyExplanations {
		cat := categoryMap[id]
		if cat == "" {
			cat = "general"
		}
		// Extract the first line (bold title) as brief description.
		brief := id
		if desc, ok := anomalyExplanations[id]; ok {
			for _, line := range strings.Split(desc, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					// Strip markdown bold markers.
					brief = strings.ReplaceAll(line, "**", "")
					break
				}
			}
		}
		entries = append(entries, entry{ID: id, Category: cat, Brief: brief})
	}

	// Sort by category then ID for stable output.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].ID < entries[j].ID
	})

	jsonData, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("json marshal failed: %v", err)), nil
	}
	return newTextResult(string(jsonData)), nil
}

// getArgs safely extracts the arguments map from a CallToolRequest.
// Returns an empty map if Arguments is nil or not a map.
func getArgs(request mcp.CallToolRequest) map[string]interface{} {
	if request.Params.Arguments == nil {
		return map[string]interface{}{}
	}
	args, ok := request.Params.Arguments.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return args
}

// stringArg extracts a string argument with a default value.
func stringArg(args map[string]interface{}, key, defaultVal string) string {
	val, ok := args[key]
	if !ok || val == nil {
		return defaultVal
	}
	s, ok := val.(string)
	if !ok || s == "" {
		return defaultVal
	}
	return s
}

// newTextResult creates a successful MCP tool result with text content.
func newTextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
}

// errResult creates an MCP tool error result (IsError=true).
// This is returned as a tool-level error, not a transport-level JSON-RPC error.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
	}
}

var anomalyExplanations = map[string]string{
	"cpu_utilization": `**High CPU Utilization**
CPU utilization exceeds threshold. Tasks may experience scheduling delays.
**Root Causes:**
- Compute-intensive workload (compilation, encoding, ML training)
- Runaway process or infinite loop
- Insufficient CPU resources for workload
**Recommendations:**
- Run 'collect_metrics' with profile='standard' to get stack traces and run queue latency.
- Check 'runqlat' histogram for scheduling delay distribution.
- Use focus='stacks' to identify hot functions.`,

	"cpu_iowait": `**High CPU I/O Wait**
CPUs are spending significant time blocked on I/O operations.
**Root Causes:**
- Slow disk I/O (saturated disk, rotational HDD with random access)
- NFS or network filesystem latency
- Synchronous I/O in application hot path
**Recommendations:**
- Run 'collect_metrics' with focus='disk' to identify slow devices.
- Check 'biolatency' and 'biotop' for I/O latency breakdown.`,

	"load_average": `**High Load Average**
The system load (run queue + uninterruptible I/O) exceeds the CPU count.
**Root Causes:**
- CPU saturation (too many runnable tasks)
- High I/O wait (tasks stuck in uninterruptible sleep)
- Fork storms or container CPU throttling
**Recommendations:**
- Check CPU utilization and iowait to distinguish CPU vs I/O saturation.
- Run 'collect_metrics' with profile='standard' to see runqlat distribution.`,

	"cpu_saturation": `**CPU Saturation**
The CPU is fully utilized (0% idle) and tasks are waiting in the run queue (high load average).
**Root Causes:**
- Infinite loop in application
- Crypto mining or compute-intensive task
- Container CPU quota throttling (if in container)
**Recommendations:**
- Run 'collect_metrics' with focus='stacks' (profile=standard) to see what functions are consuming CPU.
- Check 'runqlat' histogram for scheduling latency.`,

	"memory_utilization": `**High Memory Utilization**
System memory usage exceeds threshold. Risk of OOM kills or swap pressure.
**Root Causes:**
- Memory leak in application
- Misconfigured application heap (JVM, Go GC, etc.)
- Insufficient memory for workload
**Recommendations:**
- Check process RSS in 'collect_metrics' output.
- Run 'memleak' (deep profile) to identify leaking allocations.
- Check for recent OOM kills in dmesg.`,

	"swap_usage": `**Swap Usage**
System is using swap space, indicating memory pressure.
**Root Causes:**
- Physical memory exhausted
- Memory leak causing gradual swap-in
- Overcommitted memory (vm.overcommit)
**Recommendations:**
- Check 'memory_utilization' anomaly for context.
- Identify top RSS consumers in process list.
- Consider increasing RAM or reducing workload memory footprint.`,

	"memory_psi_pressure": `**Memory PSI Pressure**
Tasks are stalling due to memory reclaim (page scanning, swap I/O).
**Root Causes:**
- System near OOM boundary
- Heavy page cache thrashing
- Container memory limit too tight
**Recommendations:**
- Check swap usage and available memory.
- Run 'cachestat' to measure page cache hit ratio.
- Check 'oomkill' for recent OOM events.`,

	"memory_saturation": `**Memory Saturation**
System is running out of RAM, inducing swapping or OOM kills.
**Root Causes:**
- Memory leak in application.
- Missized JVM heap or container limits.
**Recommendations:**
- Check 'oomkill' for recent kills.
- Run 'memleak' (deep profile) to find leaking stacks.`,

	"tcp_retransmits": `**TCP Retransmits**
High TCP retransmission rate indicates packet loss or network congestion.
**Root Causes:**
- Network congestion or packet drops
- Faulty network interface or cable
- TCP buffer overflow (small receive window)
**Recommendations:**
- Run 'collect_metrics' with focus='network' to get tcpretrans events.
- Check for interface errors in network stats.
- Correlate with 'tcpdrop' events if available.`,

	"tcp_timewait": `**High TIME_WAIT Connections**
Large number of TCP connections in TIME_WAIT state.
**Root Causes:**
- High connection churn (short-lived connections)
- Missing connection pooling / keep-alive
- Ephemeral port exhaustion risk
**Recommendations:**
- Enable connection pooling in application.
- Check net.ipv4.tcp_tw_reuse sysctl setting.
- Consider using persistent connections (HTTP keep-alive).`,

	"disk_utilization": `**High Disk Utilization**
Disk bandwidth or IOPS capacity is near saturation.
**Root Causes:**
- Write-heavy workload (logging, database WAL)
- Large sequential reads (backup, scan)
- Too many random IOPS for device capacity
**Recommendations:**
- Run 'collect_metrics' with focus='disk' for biolatency/biotop data.
- Identify top I/O consumers with 'biotop'.
- Consider I/O scheduling changes or faster storage.`,

	"disk_latency": `**High Disk Latency**
Disk I/O completion time is high (>50ms avg). Use 'collect_metrics' with focus='disk'.
**Root Causes:**
- Saturated disk bandwidth or IOPS.
- Random I/O pattern on rotational HDD.
- fsync() storms (e.g., Kafka, database commit logs).
**Recommendations:**
- Check 'biolatency' for distribution (bimodal? long tail?).
- Check 'biotop' or 'ext4slower' to identify the process.`,

	"disk_avg_latency": `**High Average Disk Latency**
Average I/O latency exceeds threshold. Individual requests taking too long.
**Root Causes:**
- Device queue saturation
- Mixed read/write workload causing head seeks (HDD)
- RAID rebuild or filesystem check in background
**Recommendations:**
- Run 'collect_metrics' with focus='disk'.
- Check biolatency histogram for latency distribution shape.`,

	"cpu_throttling": `**Container CPU Throttling**
Container is hitting its CPU quota limit, causing forced throttling.
**Root Causes:**
- CPU limit set too low for workload
- Bursty CPU usage pattern
- Noisy neighbor in shared host
**Recommendations:**
- Increase container CPU limit or request.
- Profile application to reduce CPU usage.
- Check runqlat to measure the throttling impact on latency.`,

	"container_memory_usage": `**Container Memory Near Limit**
Container memory usage is approaching its configured limit. OOM kill risk.
**Root Causes:**
- Memory leak in application
- Memory limit set too tight
- Large in-memory caches or buffers
**Recommendations:**
- Check for memory leaks with 'memleak' (deep profile).
- Review container memory limit configuration.
- Monitor RSS growth over time with 'collect_metrics' diff.`,

	"runqlat_p99": `**High Run Queue Latency**
Tasks are waiting in the CPU run queue (scheduler delay) for extended periods.
**Root Causes:**
- CPU saturation (more runnable tasks than CPUs)
- Priority inversion or RT scheduling issues
- Container CPU throttling
**Recommendations:**
- Check CPU utilization and load average.
- Use focus='stacks' to see what's consuming CPU.
- Consider increasing CPUs or reducing parallelism.`,

	"dns_latency_p99": `**High DNS Latency**
DNS lookups are taking unusually long, adding latency to network operations.
**Root Causes:**
- Slow or unreachable DNS server
- DNS cache misses (TTL expired, new domains)
- Network issues between host and DNS server
**Recommendations:**
- Check /etc/resolv.conf for DNS server configuration.
- Run 'gethostlatency' to see per-lookup latency.
- Consider local DNS caching (systemd-resolved, dnsmasq).`,

	"cache_miss_ratio": `**High Page Cache Miss Ratio**
Significant portion of page cache lookups result in misses (disk reads).
**Root Causes:**
- Working set larger than available memory
- Sequential scan evicting useful cached pages
- Memory pressure from other processes
**Recommendations:**
- Check memory utilization and available RAM.
- Identify I/O-heavy processes with 'biotop'.
- Consider increasing memory or tuning vm.vfs_cache_pressure.`,

	"cpu_psi_pressure": `**CPU PSI Pressure**
Tasks are stalling waiting for CPU time (Pressure Stall Information).
**Root Causes:**
- CPU oversubscription
- Long-running CPU-bound tasks blocking others
- Container CPU quota exhaustion
**Recommendations:**
- Check load average and CPU utilization.
- Run 'collect_metrics' with profile='standard' for runqlat data.
- Review container CPU limits if applicable.`,

	"io_psi_pressure": `**I/O PSI Pressure**
Tasks are stalling waiting for I/O operations to complete.
**Root Causes:**
- Slow disk or filesystem
- Heavy synchronous I/O workload
- Network filesystem (NFS) latency
**Recommendations:**
- Check disk utilization and latency.
- Run 'collect_metrics' with focus='disk'.
- Consider async I/O or faster storage.`,

	"network_errors_per_sec": `**Network Interface Errors**
Network interface is reporting errors (CRC, frame, carrier errors).
**Root Causes:**
- Faulty cable or connector
- NIC hardware issue
- Driver bug or MTU mismatch
**Recommendations:**
- Check interface error counters in 'collect_metrics' network data.
- Try replacing cable or switching ports.
- Check dmesg for NIC error messages.`,

	"conntrack_usage_pct": `**Conntrack Table Pressure**
The connection tracking table (nf_conntrack) is approaching capacity.
**Root Causes:**
- Too many concurrent connections for the configured conntrack_max
- Aggressive conntrack timeouts not configured (established default is 5 days)
- DDoS or SYN flood filling the table
**Recommendations:**
- Increase nf_conntrack_max: sysctl -w net.netfilter.nf_conntrack_max=<current*2>
- Reduce conntrack timeouts for faster cleanup
- Consider NOTRACK for traffic that doesn't need stateful tracking`,

	"softnet_dropped": `**Softnet Packet Drops**
The kernel is dropping packets because softirq processing can't keep up with NIC ingress rate.
**Root Causes:**
- NIC PPS exceeds single CPU processing capacity without RPS
- netdev_budget too low for traffic volume
- IRQ affinity misconfigured (all interrupts on one CPU)
**Recommendations:**
- Enable RPS to distribute packet processing across CPUs
- Increase net.core.netdev_budget (default 300, recommend 4096)
- Pin IRQ affinity 1:1 (queue N → CPU N)`,

	"listen_overflows": `**Listen Queue Overflows**
The TCP accept queue is full — new SYN packets are being dropped.
**Root Causes:**
- nginx/application listen backlog too small (default 511)
- Application not calling accept() fast enough
- Missing SO_REUSEPORT (single accept queue for all workers)
**Recommendations:**
- Add 'reuseport' to nginx listen directive: listen 80 reuseport;
- Increase backlog: listen 80 reuseport backlog=8192;
- Ensure net.core.somaxconn >= backlog value`,

	"nic_rx_discards": `**NIC Ring Buffer Overflow (rx_discards)**
The NIC is dropping packets at hardware level because the receive ring buffer is full.
**Root Causes:**
- Ring buffer too small for traffic burst rate
- CPU not processing packets fast enough (softirq bottleneck)
- No RPS/RSS distributing packets across CPUs
**Recommendations:**
- Increase ring buffer: ethtool -G <iface> rx <max>
- Increase netdev_budget for faster softirq processing
- Enable RPS or pin IRQ affinity for even distribution`,

	"tcp_close_wait": `**CLOSE_WAIT Socket Accumulation**
Sockets stuck in CLOSE_WAIT state — the remote peer closed the connection but the application never called close().
**Root Causes:**
- Application bug: not closing sockets after use (missing defer conn.Close())
- Connection pool leak: borrowed connections never returned
- Blocked application thread: close() never reached due to deadlock or exception
**Recommendations:**
- Identify the leaking process: ss -tnp state close-wait
- Review application code for missing socket close() calls
- Add connection pool timeouts and leak detection`,

	"softnet_time_squeeze": `**Softnet Time Squeeze**
The kernel NAPI polling budget was exhausted before all packets were processed.
**Root Causes:**
- netdev_budget too low for packet rate (default 300)
- CPU spending too much time in other softirqs
- High PPS on few CPUs without RPS distribution
**Recommendations:**
- Increase net.core.netdev_budget (recommend 600-4096)
- Increase net.core.netdev_budget_usecs (recommend 4000-8000)
- Enable RPS to distribute packet processing across CPUs`,

	"tcp_abort_on_memory": `**TCP Connections Aborted Due to Memory Pressure**
The kernel is killing TCP connections because it ran out of TCP buffer memory.
**Root Causes:**
- tcp_mem limits too low for connection count
- Too many sockets with large receive buffers
- Memory leak in connection handling
**Recommendations:**
- Increase tcp_mem: sysctl -w net.ipv4.tcp_mem='1048576 2097152 4194304'
- Check /proc/net/sockstat for TCP mem usage
- Review application for connection leaks`,

	"irq_imbalance": `**Network IRQ Imbalance**
Network interrupts (NET_RX) are heavily concentrated on one or few CPUs.
**Root Causes:**
- NIC has single RX queue (or RSS not configured)
- IRQ affinity pinned to single CPU
- RPS (Receive Packet Steering) not enabled
**Recommendations:**
- Enable RPS: echo <cpu_mask> > /sys/class/net/<dev>/queues/rx-*/rps_cpus
- Configure IRQ affinity: spread interrupts across CPUs (irqbalance)
- Check if NIC supports multi-queue RSS (ethtool -l)`,

	"udp_rcvbuf_errors": `**UDP Receive Buffer Overflow**
The kernel is dropping UDP packets because the socket receive buffer is full.
**Root Causes:**
- Application not reading from socket fast enough
- net.core.rmem_max too low (default 212992 bytes)
- Application SO_RCVBUF not large enough
- Bursty traffic overwhelming buffer (common in DNS, statsd, syslog)
**Recommendations:**
- Increase rmem_max: sysctl -w net.core.rmem_max=26214400
- Application should set SO_RCVBUF to larger value
- Consider multiple listener threads or SO_REUSEPORT`,

	"tcp_rcvq_drop": `**TCP Receive Queue Drops**
Packets are being dropped because the receiving application is not reading data fast enough from ESTABLISHED sockets.
**Root Causes:**
- Application blocked on disk I/O, locks, or compute while data arrives
- Single-threaded processing can't keep up with incoming data rate
- GC pauses (Java/Go) stalling the read loop
**Recommendations:**
- Profile the application: is read() blocking on disk, locks, or compute?
- Increase tcp_rmem max for more buffer headroom
- Add more worker threads or use non-blocking I/O`,

	"tcp_zero_window_drop": `**TCP Zero Window Drops**
The receiver advertised a window of 0 bytes (meaning "stop sending, I'm full"), and the sender dropped packets.
**Root Causes:**
- Application not calling read()/recv() fast enough
- Receive buffer full (tcp_rmem max too small or app holding data)
- Processing bottleneck in the application (CPU, disk, locks)
**Recommendations:**
- Increase tcp_rmem max: sysctl -w net.ipv4.tcp_rmem='4096 131072 16777216'
- Profile application to find the read() bottleneck
- Check ss -tnm for per-socket buffer usage`,

	"listen_queue_saturation": `**Listen Queue Saturation**
The TCP accept queue for a listening socket is filling up. When full, new SYN packets are dropped (ListenOverflows).
**Root Causes:**
- Application not calling accept() fast enough (single-threaded accept loop)
- No SO_REUSEPORT: all connections funnel through one accept queue
- Burst of new connections overwhelming the backlog
**Recommendations:**
- Enable SO_REUSEPORT: each worker gets its own accept queue
  - nginx: listen 80 reuseport;
  - Go: set SO_REUSEPORT via ListenConfig.Control
- Increase backlog: net.core.somaxconn=65535
- Add more accept() worker threads`,

	"biolatency_p99_ssd": `**High SSD I/O Latency (p99)**
Block I/O p99 latency for SSDs exceeds expected range.
**Root Causes:**
- SSD write amplification or garbage collection
- Device queue depth saturation
- Thermal throttling
**Recommendations:**
- Check biolatency histogram for latency distribution.
- Monitor SSD SMART attributes.
- Consider reducing I/O queue depth.`,

	"biolatency_p99_hdd": `**High HDD I/O Latency (p99)**
Block I/O p99 latency for rotational disks is elevated.
**Root Causes:**
- Random I/O pattern (seek-heavy)
- Disk approaching capacity (long seek times)
- Head parking or power management
**Recommendations:**
- Check biolatency histogram for bimodal distribution.
- Consider migrating hot data to SSD.
- Review I/O scheduler (deadline/mq-deadline for HDD).`,

	"direct_reclaim_rate": `**Direct Page Reclaim Active**
Applications are blocking while the kernel reclaims memory pages (pgscan_direct).
**Root Causes:**
- Free memory dropped below watermarks, kswapd can't keep up
- vm.min_free_kbytes too low for allocation burst rate
- Large memory consumers exhausting page cache
**Recommendations:**
- Increase vm.watermark_scale_factor to trigger kswapd earlier
- Increase vm.min_free_kbytes to maintain larger free pool
- Check for memory leaks or oversized caches`,

	"compaction_stall_rate": `**Memory Compaction Stalls**
Memory allocation is blocking while the kernel compacts (defragments) memory.
**Root Causes:**
- Memory fragmented — no contiguous pages for higher-order allocations
- THP (Transparent Huge Pages) requesting 2MB contiguous blocks
- Long-running system without periodic defragmentation
**Recommendations:**
- Switch THP to madvise mode: echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
- Set THP defrag to defer+madvise: echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag
- Trigger manual compaction: echo 1 > /proc/sys/vm/compact_memory`,

	"thp_split_rate": `**THP Split Rate High**
Transparent Huge Pages are being split back into small 4KB pages at a high rate.
**Root Causes:**
- Partial page access patterns breaking huge pages
- Memory pressure forcing THP demotion
- Workload pattern incompatible with THP (e.g., fork-heavy, partial mmap)
**Recommendations:**
- Switch THP to madvise: only apps that opt-in get huge pages
- Use madvise(MADV_HUGEPAGE) in application code for specific regions
- THP splits cause TLB invalidation storms — monitor dTLB-load-misses with perf`,

	"numa_miss_ratio": `**NUMA Memory Miss Ratio High**
Processes are accessing memory allocated on a different NUMA node, causing 30-50% latency penalty.
**Root Causes:**
- Process migrated to different NUMA node by scheduler
- Memory allocated before process was pinned to CPU
- sched_numa_balancing disabled or ineffective
**Recommendations:**
- Pin critical processes to NUMA node: numactl --cpunodebind=N --membind=N
- Enable NUMA balancing: sysctl -w kernel.sched_numa_balancing=1
- For databases: use numactl --interleave=all to spread evenly`,

	"gpu_nic_cross_numa": `**GPU-NIC Cross-NUMA Topology**
A GPU and NIC are on different NUMA nodes, causing PCIe DMA transfers to cross the interconnect.
**Root Causes:**
- Server hardware design placed GPU and NIC on different CPU sockets
- PCIe slot assignment not optimized for GPU-Direct workloads
**Impact:**
- GPU-Direct RDMA throughput reduced by 30-50%
- Additional latency for every DMA transfer between GPU and NIC
**Recommendations:**
- Move NIC to a PCIe slot on the same NUMA node as the GPU
- If physical move impossible: pin application to the GPU's NUMA node
- Use nvidia-smi topo --matrix to verify topology
- For RDMA: ensure ib_device and GPU share NUMA node`,
}
