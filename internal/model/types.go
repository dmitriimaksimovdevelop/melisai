// Package model defines all data types for the melisai report output.
// These types are serialized to JSON and consumed by AI/LLM for analysis.
// Schema version: 1.1.0
package model

import "time"

// --- Report: top-level output ---

// Report is the complete melisai output document.
type Report struct {
	Metadata   Metadata            `json:"metadata"`
	System     SystemInfo          `json:"system"`
	Categories map[string][]Result `json:"categories"`
	Summary    Summary             `json:"summary"`
	AIContext  *AIContext          `json:"ai_context,omitempty"`
}

// Metadata identifies the collection run.
type Metadata struct {
	Tool             string            `json:"tool"`
	Version          string            `json:"version"`
	SchemaVersion    string            `json:"schema_version"`
	Hostname         string            `json:"hostname"`
	Timestamp        string            `json:"timestamp"`
	Duration         string            `json:"duration"`
	Profile          string            `json:"profile"`
	FocusAreas       []string          `json:"focus_areas"`
	KernelVersion    string            `json:"kernel_version"`
	Arch             string            `json:"arch"`
	CPUs             int               `json:"cpus"`
	MemoryGB         int               `json:"memory_gb"`
	Capabilities     []string          `json:"capabilities"`
	ContainerEnv     string            `json:"container_env"`
	CgroupVersion    int               `json:"cgroup_version"`
	ObserverOverhead *ObserverOverhead `json:"observer_overhead,omitempty"`
}

// ObserverOverhead records melisai's own resource consumption during collection.
type ObserverOverhead struct {
	SelfPID         int   `json:"self_pid"`
	ChildPIDs       []int `json:"child_pids"`
	CPUUserMs       int64 `json:"cpu_user_ms"`
	CPUSystemMs     int64 `json:"cpu_system_ms"`
	MemoryRSSBytes  int64 `json:"memory_rss_bytes"`
	DiskReadBytes   int64 `json:"disk_read_bytes"`
	DiskWriteBytes  int64 `json:"disk_write_bytes"`
	ContextSwitches int64 `json:"context_switches"`
}

// SystemInfo describes the host OS, filesystems, and recent errors.
type SystemInfo struct {
	OS            string           `json:"os"`
	Kernel        string           `json:"kernel"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	BootParams    string           `json:"boot_params"`
	Filesystems   []FilesystemInfo `json:"filesystems,omitempty"`
	BlockDevices  []BlockDevice    `json:"block_devices,omitempty"`
	DmesgErrors   []LogEntry       `json:"dmesg_errors,omitempty"`
	JournalErrors []LogEntry       `json:"journal_errors,omitempty"`
}

type FilesystemInfo struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	Type    string  `json:"type"`
	SizeGB  float64 `json:"size_gb"`
	UsedPct float64 `json:"used_pct"`
}

type BlockDevice struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	SizeGB float64 `json:"size_gb"`
	Model  string  `json:"model,omitempty"`
}

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level,omitempty"`
	Unit    string `json:"unit,omitempty"`
	Message string `json:"message"`
}

// --- Result: unified collector output ---

// Result is the normalized output from any collector (Tier 1, 2, or 3).
type Result struct {
	Collector  string       `json:"collector"`
	Category   string       `json:"category"`
	Tier       int          `json:"tier"`
	StartTime  time.Time    `json:"start_time"`
	EndTime    time.Time    `json:"end_time"`
	Data       interface{}  `json:"data"`
	Histograms []Histogram  `json:"histograms,omitempty"`
	Events     []Event      `json:"events,omitempty"`
	Stacks     []StackTrace `json:"stacks,omitempty"`
	Errors     []string     `json:"errors,omitempty"`
	Truncated  bool         `json:"truncated,omitempty"`
}

// --- Histogram: power-of-2 latency distributions ---

type Histogram struct {
	Name       string       `json:"name"`
	Unit       string       `json:"unit"`
	Buckets    []HistBucket `json:"buckets"`
	TotalCount int64        `json:"total_count"`
	P50        float64      `json:"p50"`
	P90        float64      `json:"p90"`
	P99        float64      `json:"p99"`
	P999       float64      `json:"p999"`
	Max        float64      `json:"max"`
	Mean       float64      `json:"mean"`
}

type HistBucket struct {
	Low   int64 `json:"low"`
	High  int64 `json:"high"`
	Count int64 `json:"count"`
}

// --- Events: per-line trace output ---

type Event struct {
	Time    string                 `json:"time"`
	PID     int                    `json:"pid,omitempty"`
	Comm    string                 `json:"comm,omitempty"`
	Details map[string]interface{} `json:"details"`
}

// --- Stack traces: folded format ---

type StackTrace struct {
	Stack string `json:"stack"`
	Count int    `json:"count"`
	Type  string `json:"type"`
}

// --- Typed data structs per category ---

type CPUData struct {
	UserPct               float64  `json:"user_pct"`
	SystemPct             float64  `json:"system_pct"`
	IOWaitPct             float64  `json:"iowait_pct"`
	IdlePct               float64  `json:"idle_pct"`
	StealPct              float64  `json:"steal_pct"`
	IRQPct                float64  `json:"irq_pct"`
	SoftIRQPct            float64  `json:"softirq_pct"`
	ContextSwitchesPerSec int64    `json:"context_switches_per_sec"`
	LoadAvg1              float64  `json:"load_avg_1"`
	LoadAvg5              float64  `json:"load_avg_5"`
	LoadAvg15             float64  `json:"load_avg_15"`
	NumCPUs               int      `json:"num_cpus"`
	PerCPU                []PerCPU `json:"per_cpu,omitempty"`
	SchedLatencyNS        int64    `json:"sched_latency_ns,omitempty"`
	SchedMinGranularityNS int64    `json:"sched_min_granularity_ns,omitempty"`
	PSISome10             float64  `json:"psi_some_10,omitempty"`
	PSISome60             float64  `json:"psi_some_60,omitempty"`
}

type PerCPU struct {
	CPU       int     `json:"cpu"`
	UserPct   float64 `json:"user_pct"`
	SystemPct float64 `json:"system_pct"`
	IOWaitPct float64 `json:"iowait_pct"`
	IdlePct   float64 `json:"idle_pct"`
}

type MemoryData struct {
	TotalBytes           int64            `json:"total_bytes"`
	FreeBytes            int64            `json:"free_bytes"`
	AvailableBytes       int64            `json:"available_bytes"`
	CachedBytes          int64            `json:"cached_bytes"`
	BuffersBytes         int64            `json:"buffers_bytes"`
	SwapTotalBytes       int64            `json:"swap_total_bytes"`
	SwapUsedBytes        int64            `json:"swap_used_bytes"`
	DirtyBytes           int64            `json:"dirty_bytes"`
	MajorFaults          int64            `json:"major_faults"`
	MinorFaults          int64            `json:"minor_faults"`
	Swappiness           int              `json:"swappiness"`
	OvercommitMemory     int              `json:"overcommit_memory"`
	OvercommitRatio      int              `json:"overcommit_ratio"`
	DirtyRatio           int              `json:"dirty_ratio"`
	DirtyBackgroundRatio int              `json:"dirty_background_ratio"`
	HugePagesTotal       int              `json:"huge_pages_total"`
	HugePagesFree        int              `json:"huge_pages_free"`
	PSIFull10            float64          `json:"psi_full_10,omitempty"`
	PSIFull60            float64          `json:"psi_full_60,omitempty"`
	PSISome10            float64          `json:"psi_some_10,omitempty"`
	PSISome60            float64          `json:"psi_some_60,omitempty"`
	THPEnabled           string           `json:"thp_enabled,omitempty"`
	THPDefrag            string           `json:"thp_defrag,omitempty"`
	MinFreeKbytes        int              `json:"min_free_kbytes,omitempty"`
	NUMANodes            []NUMANode       `json:"numa_nodes,omitempty"`
	BuddyInfo            map[string][]int `json:"buddy_info,omitempty"`
	// Page reclaim and compaction from /proc/vmstat
	Reclaim *ReclaimStats `json:"reclaim,omitempty"`
	// Additional vm.* sysctls
	WatermarkScaleFactor  int `json:"watermark_scale_factor,omitempty"`
	DirtyExpireCentisecs  int `json:"dirty_expire_centisecs,omitempty"`
	DirtyWritebackCentisecs int `json:"dirty_writeback_centisecs,omitempty"`
	ZoneReclaimMode       int `json:"zone_reclaim_mode,omitempty"`
	SchedNumaBalancing    int `json:"sched_numa_balancing,omitempty"`
}

// ReclaimStats holds page reclaim, compaction, and THP counters from /proc/vmstat.
type ReclaimStats struct {
	// Page reclaim
	PgscanDirect    int64 `json:"pgscan_direct"`
	PgscanKswapd    int64 `json:"pgscan_kswapd"`
	PgstealDirect   int64 `json:"pgsteal_direct"`
	PgstealKswapd   int64 `json:"pgsteal_kswapd"`
	AllocstallNormal int64 `json:"allocstall_normal"`
	AllocstallDMA    int64 `json:"allocstall_dma,omitempty"`
	AllocstallMovable int64 `json:"allocstall_movable,omitempty"`
	// Compaction
	CompactStall   int64 `json:"compact_stall"`
	CompactSuccess int64 `json:"compact_success"`
	CompactFail    int64 `json:"compact_fail"`
	// THP activity
	THPFaultAlloc    int64 `json:"thp_fault_alloc"`
	THPCollapseAlloc int64 `json:"thp_collapse_alloc"`
	THPSplitPage     int64 `json:"thp_split_page"`
	// Rate fields (two-point delta / interval)
	DirectReclaimRate float64 `json:"direct_reclaim_rate,omitempty"`
	CompactStallRate  float64 `json:"compact_stall_rate,omitempty"`
	THPSplitRate      float64 `json:"thp_split_rate,omitempty"`
}

type NUMANode struct {
	Node          int      `json:"node"`
	MemTotalBytes int64    `json:"mem_total_bytes"`
	MemFreeBytes  int64    `json:"mem_free_bytes"`
	NumaHit       int64    `json:"numa_hit"`
	NumaMiss      int64    `json:"numa_miss"`
	NumaForeign   int64    `json:"numa_foreign"`
	MissRatio     float64  `json:"miss_ratio,omitempty"`
	Distance      []int    `json:"distance,omitempty"`
	CPUs          string   `json:"cpus,omitempty"`
}

// GPUDevice represents a detected GPU.
type GPUDevice struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Driver      string `json:"driver,omitempty"`
	PCIBus      string `json:"pci_bus"`
	NUMANode    int    `json:"numa_node"`
	MemoryTotal int64  `json:"memory_total_mb,omitempty"`
	MemoryUsed  int64  `json:"memory_used_mb,omitempty"`
	UtilGPU     int    `json:"utilization_gpu_pct,omitempty"`
	UtilMemory  int    `json:"utilization_memory_pct,omitempty"`
	Temperature int    `json:"temperature_c,omitempty"`
	PowerWatts  int    `json:"power_watts,omitempty"`
}

// PCIeTopology holds GPU and NIC NUMA affinity information.
type PCIeTopology struct {
	GPUs              []GPUDevice       `json:"gpus,omitempty"`
	NICNUMAMap        map[string]int    `json:"nic_numa_map,omitempty"`
	CrossNUMAPairs    []CrossNUMAPair   `json:"cross_numa_pairs,omitempty"`
}

// CrossNUMAPair flags a GPU-NIC pair on different NUMA nodes.
type CrossNUMAPair struct {
	GPU      string `json:"gpu"`
	GPUNode  int    `json:"gpu_numa_node"`
	NIC      string `json:"nic"`
	NICNode  int    `json:"nic_numa_node"`
}

type DiskDevice struct {
	Name         string  `json:"name"`
	ReadOps      int64   `json:"read_ops"`
	WriteOps     int64   `json:"write_ops"`
	ReadBytes    int64   `json:"read_bytes"`
	WriteBytes   int64   `json:"write_bytes"`
	IOInProgress int64   `json:"io_in_progress"`
	IOTimeMs     int64   `json:"io_time_ms"`
	WeightedIOMs int64   `json:"weighted_io_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms,omitempty"`
	Scheduler    string  `json:"scheduler,omitempty"`
	QueueDepth   int     `json:"queue_depth,omitempty"`
	Rotational   bool    `json:"rotational"`
	ReadAheadKB  int     `json:"read_ahead_kb,omitempty"`
}

type DiskData struct {
	Devices   []DiskDevice `json:"devices"`
	TotalOps  int64        `json:"total_ops"`
	ReadOps   int64        `json:"read_ops"`
	WriteOps  int64        `json:"write_ops"`
	PSISome10 float64      `json:"psi_some_10,omitempty"`
	PSISome60 float64      `json:"psi_some_60,omitempty"`
}

type NetworkInterface struct {
	Name         string  `json:"name"`
	RxBytes      int64   `json:"rx_bytes"`
	TxBytes      int64   `json:"tx_bytes"`
	RxPackets    int64   `json:"rx_packets"`
	TxPackets    int64   `json:"tx_packets"`
	RxErrors     int64   `json:"rx_errors"`
	TxErrors     int64   `json:"tx_errors"`
	RxDropped    int64   `json:"rx_dropped"`
	TxDropped    int64   `json:"tx_dropped"`
	ErrorsPerSec float64 `json:"errors_per_sec,omitempty"`
	// NIC hardware details (Tier 1 — sysfs/ethtool)
	Driver      string `json:"driver,omitempty"`
	Speed       string `json:"speed,omitempty"`
	RxQueues    int    `json:"rx_queues,omitempty"`
	TxQueues    int    `json:"tx_queues,omitempty"`
	RingRxCur   int    `json:"ring_rx_current,omitempty"`
	RingRxMax   int    `json:"ring_rx_max,omitempty"`
	RxDiscards  int64  `json:"rx_discards,omitempty"`
	RxBufErrors int64  `json:"rx_buf_errors,omitempty"`
	RPSEnabled  bool   `json:"rps_enabled,omitempty"`
	XPSEnabled  bool   `json:"xps_enabled,omitempty"`
	BondSlave   bool   `json:"bond_slave,omitempty"`
	MTU         int    `json:"mtu,omitempty"`
	// Offload features (ethtool -k)
	TSOEnabled bool `json:"tso_enabled,omitempty"`
	GROEnabled bool `json:"gro_enabled,omitempty"`
	GSOEnabled bool `json:"gso_enabled,omitempty"`
	// Interrupt coalescing (ethtool -c)
	CoalesceRxUsecs int  `json:"coalesce_rx_usecs,omitempty"`
	CoalesceAdaptRx bool `json:"coalesce_adaptive_rx,omitempty"`
}

// ConntrackStats from /proc/sys/net/netfilter/nf_conntrack_*
type ConntrackStats struct {
	Count        int64   `json:"count"`
	Max          int64   `json:"max"`
	UsagePct     float64 `json:"usage_pct"`
	Drops        int64   `json:"drops,omitempty"`
	InsertFailed int64   `json:"insert_failed,omitempty"`
	EarlyDrop    int64   `json:"early_drop,omitempty"`
}

// IRQDistribution tracks per-CPU NET_RX softirq processing (delta over sample interval)
type IRQDistribution struct {
	CPU        int   `json:"cpu"`
	NetRxDelta int64 `json:"net_rx_delta"`
}

// SoftnetStats from /proc/net/softnet_stat — per-CPU network processing counters
type SoftnetStats struct {
	CPU         int   `json:"cpu"`
	Processed   int64 `json:"processed"`
	Dropped     int64 `json:"dropped"`
	TimeSqueeze int64 `json:"time_squeeze"`
}

// ListenSocket represents a listening TCP socket with accept queue depth.
type ListenSocket struct {
	LocalAddr string `json:"local_addr"`
	RecvQ     int    `json:"recv_q"`     // current accept queue depth
	SendQ     int    `json:"send_q"`     // backlog (max queue size)
	FillPct   float64 `json:"fill_pct"`  // recv_q / send_q * 100
}

type TCPStats struct {
	CurrEstab      int     `json:"curr_estab"`
	ActiveOpens    int     `json:"active_opens"`
	PassiveOpens   int     `json:"passive_opens"`
	RetransSegs    int     `json:"retrans_segs"`
	RetransRate    float64 `json:"retrans_rate_per_sec"`
	InErrs         int     `json:"in_errs"`
	OutRsts        int     `json:"out_rsts"`
	TimeWaitCount  int     `json:"time_wait_count"`
	CloseWaitCount int     `json:"close_wait_count"`
}

type NetworkData struct {
	Interfaces []NetworkInterface `json:"interfaces,omitempty"`
	TCP        *TCPStats          `json:"tcp,omitempty"`
	Conntrack  *ConntrackStats    `json:"conntrack,omitempty"`
	// Sub-structs for organized metrics (schema v1.1)
	TCPExt    *TCPExtendedStats `json:"tcp_ext,omitempty"`
	UDP       *UDPStats         `json:"udp,omitempty"`
	Softnet   *SoftnetData      `json:"softnet,omitempty"`
	SocketMem *SocketMemStats   `json:"socket_mem,omitempty"`
	Sysctls   *NetworkSysctls   `json:"sysctls,omitempty"`
	// Listen queue depths from ss -tnl
	ListenSockets []ListenSocket `json:"listen_sockets,omitempty"`
	// ESTABLISHED sockets with high Recv-Q
	RecvQSaturated int `json:"recvq_saturated_count,omitempty"`
	// BCC tool aggregated fields
	TotalConnections int     `json:"total_connections,omitempty"`
	AvgLatencyMs     float64 `json:"avg_latency_ms,omitempty"`
	P99LatencyMs     float64 `json:"p99_latency_ms,omitempty"`
	TotalRetransmits int     `json:"total_retransmits,omitempty"`
	RatePerMin       float64 `json:"rate_per_min,omitempty"`
	UniqueConns      int     `json:"unique_connections,omitempty"`
	TotalLookups     int     `json:"total_lookups,omitempty"`
}

// TCPExtendedStats holds counters from /proc/net/netstat TcpExt section.
type TCPExtendedStats struct {
	ListenOverflows      int64   `json:"listen_overflows,omitempty"`
	ListenDrops          int64   `json:"listen_drops,omitempty"`
	TCPAbortOnMemory     int64   `json:"tcp_abort_on_memory,omitempty"`
	TCPOFOQueue          int64   `json:"tcp_ofo_queue,omitempty"`
	PruneCalled          int64   `json:"prune_called,omitempty"`
	TCPRcvQDrop          int64   `json:"tcp_rcvq_drop,omitempty"`
	TCPZeroWindowDrop    int64   `json:"tcp_zero_window_drop,omitempty"`
	TCPToZeroWindowAdv   int64   `json:"tcp_to_zero_window_adv,omitempty"`
	TCPFromZeroWindowAdv int64   `json:"tcp_from_zero_window_adv,omitempty"`
	// Rate fields (computed from two-point delta / interval)
	ListenOverflowRate    float64 `json:"listen_overflow_rate,omitempty"`
	TCPAbortMemRate       float64 `json:"tcp_abort_mem_rate,omitempty"`
	TCPRcvQDropRate       float64 `json:"tcp_rcvq_drop_rate,omitempty"`
	TCPZeroWindowDropRate float64 `json:"tcp_zero_window_drop_rate,omitempty"`
}

// UDPStats holds UDP protocol counters from /proc/net/snmp.
type UDPStats struct {
	RcvbufErrors int64   `json:"rcvbuf_errors,omitempty"`
	SndbufErrors int64   `json:"sndbuf_errors,omitempty"`
	InErrors     int64   `json:"in_errors,omitempty"`
	RcvbufErrRate float64 `json:"rcvbuf_err_rate,omitempty"`
}

// SoftnetData holds per-CPU packet processing stats and IRQ distribution.
type SoftnetData struct {
	Stats           []SoftnetStats    `json:"stats,omitempty"`
	IRQDistribution []IRQDistribution `json:"irq_distribution,omitempty"`
	DropRate        float64           `json:"drop_rate,omitempty"`
	SqueezeRate     float64           `json:"squeeze_rate,omitempty"`
}

// SocketMemStats holds socket memory usage from /proc/net/sockstat.
type SocketMemStats struct {
	TCPInUse    int `json:"tcp_inuse,omitempty"`
	TCPOrphans  int `json:"tcp_orphans,omitempty"`
	TCPMemPages int `json:"tcp_mem_pages,omitempty"`
	UDPInUse    int `json:"udp_inuse,omitempty"`
}

// NetworkSysctls holds all collected kernel network tuning parameters.
type NetworkSysctls struct {
	CongestionCtrl        string `json:"congestion_control,omitempty"`
	TCPRmem               string `json:"tcp_rmem,omitempty"`
	TCPWmem               string `json:"tcp_wmem,omitempty"`
	TCPMem                string `json:"tcp_mem,omitempty"`
	SomaxConn             int    `json:"somaxconn,omitempty"`
	TCPMaxSynBacklog      int    `json:"tcp_max_syn_backlog,omitempty"`
	TCPTWReuse            int    `json:"tcp_tw_reuse,omitempty"`
	TCPMaxTwBuckets       int    `json:"tcp_max_tw_buckets,omitempty"`
	TCPKeepaliveTime      int    `json:"tcp_keepalive_time,omitempty"`
	TCPKeepaliveIntvl     int    `json:"tcp_keepalive_intvl,omitempty"`
	TCPKeepaliveProbes    int    `json:"tcp_keepalive_probes,omitempty"`
	NetdevBudget          int    `json:"netdev_budget,omitempty"`
	NetdevBudgetUsecs     int    `json:"netdev_budget_usecs,omitempty"`
	NetdevMaxBacklog      int    `json:"netdev_max_backlog,omitempty"`
	RmemMax               int    `json:"rmem_max,omitempty"`
	WmemMax               int    `json:"wmem_max,omitempty"`
	IPLocalPortRange      string `json:"ip_local_port_range,omitempty"`
	TCPFinTimeout         int    `json:"tcp_fin_timeout,omitempty"`
	TCPSlowStartAfterIdle int    `json:"tcp_slow_start_after_idle,omitempty"`
	TCPFastOpen           int    `json:"tcp_fastopen,omitempty"`
	TCPSyncookies         int    `json:"tcp_syncookies,omitempty"`
	TCPNotsentLowat       int    `json:"tcp_notsent_lowat,omitempty"`
	DefaultQdisc          string `json:"default_qdisc,omitempty"`
	TCPMtuProbing         int    `json:"tcp_mtu_probing,omitempty"`
	ARPGcThresh1          int    `json:"arp_gc_thresh1,omitempty"`
	ARPGcThresh2          int    `json:"arp_gc_thresh2,omitempty"`
	ARPGcThresh3          int    `json:"arp_gc_thresh3,omitempty"`
}

type ProcessInfo struct {
	PID     int     `json:"pid"`
	Comm    string  `json:"comm"`
	CPUPct  float64 `json:"cpu_pct,omitempty"`
	MemRSS  int64   `json:"mem_rss_bytes,omitempty"`
	MemPct  float64 `json:"mem_pct,omitempty"`
	Threads int     `json:"threads,omitempty"`
	FDs     int     `json:"fds,omitempty"`
	State   string  `json:"state,omitempty"`
}

type ProcessData struct {
	TopByCPU     []ProcessInfo `json:"top_by_cpu,omitempty"`
	TopByMem     []ProcessInfo `json:"top_by_mem,omitempty"`
	Total        int           `json:"total_processes"`
	Running      int           `json:"running"`
	Sleeping     int           `json:"sleeping"`
	Zombie       int           `json:"zombie"`
	ExcludedPIDs []int         `json:"excluded_pids,omitempty"`
}

type StackData struct {
	TotalSamples  int    `json:"total_samples"`
	UniqueStacks  int    `json:"unique_stacks"`
	TotalUs       int64  `json:"total_us,omitempty"`
	FlameGraphSVG string `json:"flamegraph_svg,omitempty"`
}

type ContainerData struct {
	Runtime             string `json:"runtime"`
	CgroupVersion       int    `json:"cgroup_version"`
	CgroupPath          string `json:"cgroup_path,omitempty"`
	ContainerID         string `json:"container_id,omitempty"`
	PodName             string `json:"pod_name,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	CPUQuota            int64  `json:"cpu_quota,omitempty"`
	CPUPeriod           int64  `json:"cpu_period,omitempty"`
	CPUThrottledPeriods int64  `json:"cpu_throttled_periods,omitempty"`
	CPUThrottledTime    int64  `json:"cpu_throttled_time_us,omitempty"`
	MemoryLimit         int64  `json:"memory_limit_bytes,omitempty"`
	MemoryUsage         int64  `json:"memory_usage_bytes,omitempty"`
}

// --- Summary: pre-computed analysis ---

type Summary struct {
	HealthScore     int                  `json:"health_score"`
	Anomalies       []Anomaly            `json:"anomalies"`
	Hotspots        []Hotspot            `json:"hotspots,omitempty"`
	Resources       map[string]USEMetric `json:"resources"`
	Recommendations []Recommendation     `json:"recommendations,omitempty"`
}

type USEMetric struct {
	Utilization float64 `json:"utilization_pct"`
	Saturation  float64 `json:"saturation_pct"`
	Errors      int     `json:"errors"`
}

type Anomaly struct {
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Metric    string `json:"metric"`
	Message   string `json:"message"`
	Value     string `json:"value"`
	Threshold string `json:"threshold"`
}

type Hotspot struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
}

type Recommendation struct {
	Priority       int      `json:"priority"`
	Category       string   `json:"category"`
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Commands       []string `json:"commands"`
	Persistent     []string `json:"persistent,omitempty"`
	ExpectedImpact string   `json:"expected_impact"`
	Evidence       string   `json:"evidence"`
	Source         string   `json:"source,omitempty"`
}

// --- AI Context ---

type AIContext struct {
	Prompt        string   `json:"prompt"`
	Methodology   string   `json:"methodology"`
	KnownPatterns []string `json:"known_patterns"`
}
