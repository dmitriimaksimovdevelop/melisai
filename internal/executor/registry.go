package executor

import (
	"fmt"
	"time"
)

// ToolSpec defines how to invoke and parse a specific BCC tool.
type ToolSpec struct {
	Name       string                                // "biolatency"
	Binary     string                                // "biolatency" (resolved via security checker)
	Category   string                                // "disk"
	NeedsRoot  bool                                  // true for most BCC tools
	OutputType OutputType                            // determines parser
	BuildArgs  func(duration time.Duration) []string // returns invocation args
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
	},
	"runqlen": {
		Name: "runqlen", Binary: "runqlen", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
	},
	"cpudist": {
		Name: "cpudist", Binary: "cpudist", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
	},
	"biolatency": {
		Name: "biolatency", Binary: "biolatency", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM_PER_DISK,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-D", formatDuration(d), "1"}
		},
	},
	"biosnoop": {
		Name: "biosnoop", Binary: "biosnoop", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
	},
	"ext4slower": {
		Name: "ext4slower", Binary: "ext4slower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
	},
	"fileslower": {
		Name: "fileslower", Binary: "fileslower", Category: "disk",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"1", formatDuration(d)}
		},
	},
	"bitesize": {
		Name: "bitesize", Binary: "bitesize", Category: "disk",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
	},
	"tcpconnlat": {
		Name: "tcpconnlat", Binary: "tcpconnlat", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"tcpretrans": {
		Name: "tcpretrans", Binary: "tcpretrans", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"tcprtt": {
		Name: "tcprtt", Binary: "tcprtt", Category: "network",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"gethostlatency": {
		Name: "gethostlatency", Binary: "gethostlatency", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"tcpdrop": {
		Name: "tcpdrop", Binary: "tcpdrop", Category: "network",
		NeedsRoot: true, OutputType: TRACING,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"tcpstates": {
		Name: "tcpstates", Binary: "tcpstates", Category: "network",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d)}
		},
	},
	"profile": {
		Name: "profile", Binary: "profile", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-af", formatDuration(d)}
		},
	},
	"offcputime": {
		Name: "offcputime", Binary: "offcputime", Category: "stacktrace",
		NeedsRoot: true, OutputType: FOLDED,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-fK", formatDuration(d)}
		},
	},
	"cachestat": {
		Name: "cachestat", Binary: "cachestat", Category: "memory",
		NeedsRoot: true, OutputType: PERIODIC,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
	},
	"execsnoop": {
		Name: "execsnoop", Binary: "execsnoop", Category: "process",
		NeedsRoot: true, OutputType: TABULAR,
		BuildArgs: func(d time.Duration) []string {
			return []string{"-d", formatDuration(d)}
		},
	},
	"hardirqs": {
		Name: "hardirqs", Binary: "hardirqs", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
		},
	},
	"softirqs": {
		Name: "softirqs", Binary: "softirqs", Category: "cpu",
		NeedsRoot: true, OutputType: HISTOGRAM,
		BuildArgs: func(d time.Duration) []string {
			return []string{formatDuration(d), "1"}
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
