package executor

import (
	"fmt"
	"time"

	"github.com/baikal/sysdiag/internal/model"
)

// ToolSpec defines how to invoke and parse a specific BCC tool.
type ToolSpec struct {
	Name       string                                  // "biolatency"
	Binary     string                                  // "biolatency" (resolved via security checker)
	Category   string                                  // "disk"
	NeedsRoot  bool                                    // true for most BCC tools
	OutputType OutputType                              // determines parser
	BuildArgs  func(duration time.Duration) []string   // returns invocation args
	Parser     func(raw string) (*model.Result, error) // parses stdout to Result
}

// OutputType classifies the parser to use.
type OutputType int

const (
	HISTOGRAM          OutputType = iota // runqlat, biolatency, cpudist, tcprtt
	HISTOGRAM_PER_DISK                   // biolatency -D
	TABULAR                              // tcpconnlat, tcpretrans, gethostlatency, biosnoop
	FOLDED                               // profile -f, offcputime -f
	PERIODIC                             // cachestat, slabratetop
	TRACING                              // tcpdrop (event + kernel stack)
	JSON_OUTPUT                          // biolatency -j (newer BCC)
)

// Registry maps tool name to its specification.
var Registry = map[string]*ToolSpec{
	"runqlat": {
		Name: "runqlat", Binary: "runqlat", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-m", formatDuration(d), "1"}
		},
		Parser: ParseRunqlat,
	},
	"runqlen": {
		Name: "runqlen", Binary: "runqlen", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "runqlen", "cpu", "run_queue_length", "slots")
		},
	},
	"cpudist": {
		Name: "cpudist", Binary: "cpudist", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "cpudist", "cpu", "cpu_distribution", "us")
		},
	},
	"biolatency": {
		Name: "biolatency", Binary: "biolatency", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM_PER_DISK,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-D", formatDuration(d), "1"}
		},
		Parser: ParseBiolatency,
	},
	"biosnoop": {
		Name: "biosnoop", Binary: "biosnoop", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseBiosnoop(raw, 1000) },
	},
	"ext4slower": {
		Name: "ext4slower", Binary: "ext4slower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			// Reuse generic event parser for now
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "ext4slower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"fileslower": {
		Name: "fileslower", Binary: "fileslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "fileslower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"bitesize": {
		Name: "bitesize", Binary: "bitesize", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "bitesize", "disk", "io_size", "bytes")
		},
	},
	"tcpconnlat": {
		Name: "tcpconnlat", Binary: "tcpconnlat", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseTcpconnlat(raw, 1000) },
	},
	"tcpretrans": {
		Name: "tcpretrans", Binary: "tcpretrans", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseTcpretrans(raw, 1000) },
	},
	"tcprtt": {
		Name: "tcprtt", Binary: "tcprtt", Category: "network",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: ParseTcprtt,
	},
	"gethostlatency": {
		Name: "gethostlatency", Binary: "gethostlatency", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseGethostlatency(raw, 1000) },
	},
	"tcpdrop": {
		Name: "tcpdrop", Binary: "tcpdrop", Category: "network",
		NeedsRoot: true, OutputType: TRACING,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseTcpdrop(raw, 1000) },
	},
	"tcpstates": {
		Name: "tcpstates", Binary: "tcpstates", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			// tcpstates output is complex, treat as tabular for now
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "tcpstates", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"profile": {
		Name: "profile", Binary: "profile", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-af", formatDuration(d)}
		},
		Parser: ParseProfileStacks,
	},
	"offcputime": {
		Name: "offcputime", Binary: "offcputime", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-fK", formatDuration(d)}
		},
		Parser: ParseOffcputime,
	},
	"cachestat": {
		Name: "cachestat", Binary: "cachestat", Category: "memory",
		NeedsRoot: true, OutputType: PERIODIC,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: ParseCachestat,
	},
	"execsnoop": {
		Name: "execsnoop", Binary: "execsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) { return ParseExecsnoop(raw, 1000) },
	},
	"hardirqs": {
		Name: "hardirqs", Binary: "hardirqs", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "hardirqs", "cpu", "hardirq_time", "us")
		},
	},
	"softirqs": {
		Name: "softirqs", Binary: "softirqs", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "softirqs", "cpu", "softirq_time", "us")
		},
	},

	// --- Group 1: TABULAR tools ---

	// Applications/Process category
	"opensnoop": {
		Name: "opensnoop", Binary: "opensnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "opensnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"killsnoop": {
		Name: "killsnoop", Binary: "killsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "killsnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"threadsnoop": {
		Name: "threadsnoop", Binary: "threadsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "threadsnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"syncsnoop": {
		Name: "syncsnoop", Binary: "syncsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "syncsnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"exitsnoop": {
		Name: "exitsnoop", Binary: "exitsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "exitsnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"statsnoop": {
		Name: "statsnoop", Binary: "statsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "statsnoop", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"capable": {
		Name: "capable", Binary: "capable", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "capable", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// VFS/Filesystem category
	"filelife": {
		Name: "filelife", Binary: "filelife", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "filelife", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"mountsnoop": {
		Name: "mountsnoop", Binary: "mountsnoop", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "mountsnoop", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"btrfsslower": {
		Name: "btrfsslower", Binary: "btrfsslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "btrfsslower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"xfsslower": {
		Name: "xfsslower", Binary: "xfsslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "xfsslower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"nfsslower": {
		Name: "nfsslower", Binary: "nfsslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "nfsslower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"zfsslower": {
		Name: "zfsslower", Binary: "zfsslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "zfsslower", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// Block Device
	"mdflush": {
		Name: "mdflush", Binary: "mdflush", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "mdflush", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// Scheduler/CPU
	"runqslower": {
		Name: "runqslower", Binary: "runqslower", Category: "cpu",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"10000", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "runqslower", Category: "cpu", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"cpufreq": {
		Name: "cpufreq", Binary: "cpufreq", Category: "cpu",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "cpufreq", Category: "cpu", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"cpuunclaimed": {
		Name: "cpuunclaimed", Binary: "cpuunclaimed", Category: "cpu",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "cpuunclaimed", Category: "cpu", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// Network
	"tcpconnect": {
		Name: "tcpconnect", Binary: "tcpconnect", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "tcpconnect", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"tcpaccept": {
		Name: "tcpaccept", Binary: "tcpaccept", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "tcpaccept", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"tcplife": {
		Name: "tcplife", Binary: "tcplife", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "tcplife", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"udpconnect": {
		Name: "udpconnect", Binary: "udpconnect", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "udpconnect", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"sofdsnoop": {
		Name: "sofdsnoop", Binary: "sofdsnoop", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "sofdsnoop", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"sockstat": {
		Name: "sockstat", Binary: "sockstat", Category: "network",
		NeedsRoot: false, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return nil
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "sockstat", Category: "network", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"skbdrop": {
		Name: "skbdrop", Binary: "skbdrop", Category: "network",
		NeedsRoot: true, OutputType: TRACING,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			stacks, _ := extractInlineStacks(raw)
			return &model.Result{Collector: "skbdrop", Category: "network", Tier: 2, Events: events, Stacks: stacks, Truncated: trunc}, nil
		},
	},

	// Virtual Memory
	"oomkill": {
		Name: "oomkill", Binary: "oomkill", Category: "memory",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "oomkill", Category: "memory", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"shmsnoop": {
		Name: "shmsnoop", Binary: "shmsnoop", Category: "memory",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "shmsnoop", Category: "memory", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"drsnoop": {
		Name: "drsnoop", Binary: "drsnoop", Category: "memory",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "drsnoop", Category: "memory", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// Hardware/NUMA
	"numamove": {
		Name: "numamove", Binary: "numamove", Category: "memory",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "numamove", Category: "memory", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"llcstat": {
		Name: "llcstat", Binary: "llcstat", Category: "cpu",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "llcstat", Category: "cpu", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// --- Group 2: HISTOGRAM tools ---

	"ext4dist": {
		Name: "ext4dist", Binary: "ext4dist", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "ext4dist", "disk", "ext4_latency", "us")
		},
	},
	"btrfsdist": {
		Name: "btrfsdist", Binary: "btrfsdist", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "btrfsdist", "disk", "btrfs_latency", "us")
		},
	},
	"xfsdist": {
		Name: "xfsdist", Binary: "xfsdist", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "xfsdist", "disk", "xfs_latency", "us")
		},
	},
	"nfsdist": {
		Name: "nfsdist", Binary: "nfsdist", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "nfsdist", "disk", "nfs_latency", "us")
		},
	},
	"zfsdist": {
		Name: "zfsdist", Binary: "zfsdist", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "zfsdist", "disk", "zfs_latency", "us")
		},
	},
	"tcpsynbl": {
		Name: "tcpsynbl", Binary: "tcpsynbl", Category: "network",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "tcpsynbl", "network", "tcp_syn_backlog", "connections")
		},
	},
	"scsilatency": {
		Name: "scsilatency", Binary: "scsilatency", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "scsilatency", "disk", "scsi_latency", "us")
		},
	},
	"nvmelatency": {
		Name: "nvmelatency", Binary: "nvmelatency", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			return ParseHistogramResult(raw, "nvmelatency", "disk", "nvme_latency", "us")
		},
	},

	// --- Group 3: FOLDED stack tools ---

	"wakeuptime": {
		Name: "wakeuptime", Binary: "wakeuptime", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-fK", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			stacks, err := ParseFoldedStacks(raw, "wakeup")
			if err != nil {
				return nil, err
			}
			var totalUs int64
			for _, s := range stacks {
				totalUs += int64(s.Count)
			}
			return &model.Result{Collector: "wakeuptime", Category: "stacktrace", Tier: 2, Stacks: stacks, Data: &model.StackData{TotalSamples: len(stacks), UniqueStacks: len(stacks), TotalUs: totalUs}}, nil
		},
	},
	"offwaketime": {
		Name: "offwaketime", Binary: "offwaketime", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-fK", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			stacks, err := ParseFoldedStacks(raw, "off-wake")
			if err != nil {
				return nil, err
			}
			var totalUs int64
			for _, s := range stacks {
				totalUs += int64(s.Count)
			}
			return &model.Result{Collector: "offwaketime", Category: "stacktrace", Tier: 2, Stacks: stacks, Data: &model.StackData{TotalSamples: len(stacks), UniqueStacks: len(stacks), TotalUs: totalUs}}, nil
		},
	},
	"biostacks": {
		Name: "biostacks", Binary: "biostacks", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-f", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			stacks, err := ParseFoldedStacks(raw, "block-io")
			if err != nil {
				return nil, err
			}
			totalSamples := 0
			for _, s := range stacks {
				totalSamples += s.Count
			}
			return &model.Result{Collector: "biostacks", Category: "stacktrace", Tier: 2, Stacks: stacks, Data: &model.StackData{TotalSamples: totalSamples, UniqueStacks: len(stacks)}}, nil
		},
	},
	"stackcount": {
		Name: "stackcount", Binary: "stackcount", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-fK", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			stacks, err := ParseFoldedStacks(raw, "function")
			if err != nil {
				return nil, err
			}
			totalSamples := 0
			for _, s := range stacks {
				totalSamples += s.Count
			}
			return &model.Result{Collector: "stackcount", Category: "stacktrace", Tier: 2, Stacks: stacks, Data: &model.StackData{TotalSamples: totalSamples, UniqueStacks: len(stacks)}}, nil
		},
	},

	// --- Group 4: PERIODIC tools ---

	"biotop": {
		Name: "biotop", Binary: "biotop", Category: "disk",
		NeedsRoot: true, OutputType: PERIODIC,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "biotop", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"slabratetop": {
		Name: "slabratetop", Binary: "slabratetop", Category: "memory",
		NeedsRoot: true, OutputType: PERIODIC,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "slabratetop", Category: "memory", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"vfsstat": {
		Name: "vfsstat", Binary: "vfsstat", Category: "disk",
		NeedsRoot: true, OutputType: PERIODIC,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "vfsstat", Category: "disk", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"syscount": {
		Name: "syscount", Binary: "syscount", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "syscount", Category: "process", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},

	// --- Group 5: Special tools ---

	"funccount": {
		Name: "funccount", Binary: "funccount", Category: "cpu",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d), "tcp_*"}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			return &model.Result{Collector: "funccount", Category: "cpu", Tier: 2, Events: events, Truncated: trunc}, nil
		},
	},
	"memleak": {
		Name: "memleak", Binary: "memleak", Category: "memory",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-a", formatDuration(d)}
		},
		Parser: func(raw string) (*model.Result, error) {
			events, trunc := ParseTabularEvents(raw, 1000)
			stacks, _ := ParseFoldedStacks(raw, "alloc")
			result := &model.Result{Collector: "memleak", Category: "memory", Tier: 2, Events: events, Truncated: trunc}
			if len(stacks) > 0 {
				result.Stacks = stacks
			}
			return result, nil
		},
	},
}

func formatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf("%d", secs)
}
