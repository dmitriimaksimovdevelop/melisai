// Package model defines all data types for the sysdiag report output.
// These types are serialized to JSON and consumed by AI/LLM for analysis.
// Schema version: 1.0.0
package model

import "time"

// --- Report: top-level output ---

// Report is the complete sysdiag output document.
type Report struct {
	Metadata   Metadata            `json:"metadata"`
	System     SystemInfo          `json:"system"`
	Categories map[string][]Result `json:"categories"`
	Summary    Summary             `json:"summary"`
	AIContext  *AIContext          `json:"ai_context,omitempty"`
}

// Metadata identifies the collection run.
type Metadata struct {
	Tool          string   `json:"tool"`
	Version       string   `json:"version"`
	SchemaVersion string   `json:"schema_version"`
	Hostname      string   `json:"hostname"`
	Timestamp     string   `json:"timestamp"`
	Duration      string   `json:"duration"`
	Profile       string   `json:"profile"`
	FocusAreas    []string `json:"focus_areas"`
	KernelVersion string   `json:"kernel_version"`
	Arch          string   `json:"arch"`
	CPUs          int      `json:"cpus"`
	MemoryGB      int      `json:"memory_gb"`
	Capabilities  []string `json:"capabilities"`
	ContainerEnv  string   `json:"container_env"`
	CgroupVersion int      `json:"cgroup_version"`
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
	NUMANodes            []NUMANode       `json:"numa_nodes,omitempty"`
	BuddyInfo            map[string][]int `json:"buddy_info,omitempty"`
}

type NUMANode struct {
	Node          int   `json:"node"`
	MemTotalBytes int64 `json:"mem_total_bytes"`
	MemFreeBytes  int64 `json:"mem_free_bytes"`
	NumaHit       int64 `json:"numa_hit"`
	NumaMiss      int64 `json:"numa_miss"`
	NumaForeign   int64 `json:"numa_foreign"`
}

type DiskDevice struct {
	Name         string `json:"name"`
	ReadOps      int64  `json:"read_ops"`
	WriteOps     int64  `json:"write_ops"`
	ReadBytes    int64  `json:"read_bytes"`
	WriteBytes   int64  `json:"write_bytes"`
	IOInProgress int64  `json:"io_in_progress"`
	IOTimeMs     int64  `json:"io_time_ms"`
	WeightedIOMs int64  `json:"weighted_io_ms"`
	Scheduler    string `json:"scheduler,omitempty"`
	QueueDepth   int    `json:"queue_depth,omitempty"`
	Rotational   bool   `json:"rotational"`
	ReadAheadKB  int    `json:"read_ahead_kb,omitempty"`
}

type DiskData struct {
	Devices  []DiskDevice `json:"devices"`
	TotalOps int64        `json:"total_ops"`
	ReadOps  int64        `json:"read_ops"`
	WriteOps int64        `json:"write_ops"`
}

type NetworkInterface struct {
	Name      string `json:"name"`
	RxBytes   int64  `json:"rx_bytes"`
	TxBytes   int64  `json:"tx_bytes"`
	RxPackets int64  `json:"rx_packets"`
	TxPackets int64  `json:"tx_packets"`
	RxErrors  int64  `json:"rx_errors"`
	TxErrors  int64  `json:"tx_errors"`
	RxDropped int64  `json:"rx_dropped"`
	TxDropped int64  `json:"tx_dropped"`
}

type TCPStats struct {
	CurrEstab      int `json:"curr_estab"`
	ActiveOpens    int `json:"active_opens"`
	PassiveOpens   int `json:"passive_opens"`
	RetransSegs    int `json:"retrans_segs"`
	InErrs         int `json:"in_errs"`
	OutRsts        int `json:"out_rsts"`
	TimeWaitCount  int `json:"time_wait_count"`
	CloseWaitCount int `json:"close_wait_count"`
}

type NetworkData struct {
	Interfaces       []NetworkInterface `json:"interfaces,omitempty"`
	TCP              *TCPStats          `json:"tcp,omitempty"`
	CongestionCtrl   string             `json:"congestion_control,omitempty"`
	TCPRmem          string             `json:"tcp_rmem,omitempty"`
	TCPWmem          string             `json:"tcp_wmem,omitempty"`
	SomaxConn        int                `json:"somaxconn,omitempty"`
	TotalConnections int                `json:"total_connections,omitempty"`
	AvgLatencyMs     float64            `json:"avg_latency_ms,omitempty"`
	P99LatencyMs     float64            `json:"p99_latency_ms,omitempty"`
	TotalRetransmits int                `json:"total_retransmits,omitempty"`
	RatePerMin       float64            `json:"rate_per_min,omitempty"`
	UniqueConns      int                `json:"unique_connections,omitempty"`
	TotalLookups     int                `json:"total_lookups,omitempty"`
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
	TopByCPU []ProcessInfo `json:"top_by_cpu,omitempty"`
	TopByMem []ProcessInfo `json:"top_by_mem,omitempty"`
	Total    int           `json:"total_processes"`
	Running  int           `json:"running"`
	Sleeping int           `json:"sleeping"`
	Zombie   int           `json:"zombie"`
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
	Title          string   `json:"title"`
	Commands       []string `json:"commands"`
	Persistent     []string `json:"persistent"`
	ExpectedImpact string   `json:"expected_impact"`
	Evidence       string   `json:"evidence"`
	Source         string   `json:"source"`
}

// --- AI Context ---

type AIContext struct {
	Prompt        string   `json:"prompt"`
	Methodology   string   `json:"methodology"`
	KnownPatterns []string `json:"known_patterns"`
}
