package orchestrator

import "time"

// ProfileConfig defines collection parameters for a named profile.
type ProfileConfig struct {
	Duration      time.Duration
	Collectors    []string                 // list of collector names, or ["all"]
	FocusDuration map[string]time.Duration // extended durations per focus area
	Extra         []string                 // additional collectors for deep profiles
}

// profiles contains the built-in profile presets.
var profiles = map[string]ProfileConfig{
	"quick": {
		Duration: 10 * time.Second,
		Collectors: []string{
			"cpu_utilization",
			"memory_info",
			"disk_stats",
			"network_stats",
			"system_info",
			"process_info",
			"biolatency", // Tier 2 if available
			"tcpretrans", // Tier 2 if available
			"opensnoop",  // high signal process tracing
			"oomkill",    // OOM kill events
		},
	},
	"standard": {
		Duration:   30 * time.Second,
		Collectors: []string{"all"},
		FocusDuration: map[string]time.Duration{
			"stacks":  15 * time.Second,
			"network": 30 * time.Second,
			"disk":    30 * time.Second,
		},
	},
	"deep": {
		Duration:   60 * time.Second,
		Collectors: []string{"all"},
		FocusDuration: map[string]time.Duration{
			"stacks":  30 * time.Second,
			"network": 60 * time.Second,
			"disk":    60 * time.Second,
		},
		Extra: []string{
			"memleak",
			"offwaketime",
			"biostacks",
			"wakeuptime",
			"biotop",
			"tcpstates",
			"tcplife",
		},
	},
}

// GetProfile returns the profile config for the given name.
// Falls back to "standard" if unknown.
func GetProfile(name string) ProfileConfig {
	if p, ok := profiles[name]; ok {
		return p
	}
	return profiles["standard"]
}

// ProfileNames returns available profile names.
func ProfileNames() []string {
	return []string{"quick", "standard", "deep"}
}

// GetDuration returns the effective duration for a focus area,
// falling back to the profile default.
func (p ProfileConfig) GetDuration(focusArea string) time.Duration {
	if d, ok := p.FocusDuration[focusArea]; ok {
		return d
	}
	return p.Duration
}
