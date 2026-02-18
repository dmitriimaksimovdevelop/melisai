package model

import "testing"

// ---------- TCP buffer recommendations ----------

func TestTCPBufferRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{Data: &NetworkData{
					// max=2097152 < 4MB => should trigger rmem recommendation
					TCPRmem: "4096 87380 2097152",
					// max=6291456 > 4MB => should NOT trigger wmem recommendation
					TCPWmem: "4096 65536 6291456",
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasRmem := false
	hasWmem := false
	for _, r := range recs {
		if r.Category == "network" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w net.ipv4.tcp_rmem='4096 87380 6291456'" {
					hasRmem = true
				}
				if cmd == "sysctl -w net.ipv4.tcp_wmem='4096 65536 6291456'" {
					hasWmem = true
				}
			}
		}
	}

	if !hasRmem {
		t.Error("expected TCP rmem recommendation for max=2097152 (< 4MB)")
	}
	if hasWmem {
		t.Error("unexpected TCP wmem recommendation for max=6291456 (> 4MB)")
	}
}

// ---------- TCP TIME_WAIT reuse ----------

func TestTCPTWReuseRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{Data: &NetworkData{
					TCPTWReuse: 0,
					TCP:        &TCPStats{TimeWaitCount: 5000},
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasTWReuse := false
	for _, r := range recs {
		if r.Category == "network" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w net.ipv4.tcp_tw_reuse=1" {
					hasTWReuse = true
				}
			}
		}
	}

	if !hasTWReuse {
		t.Error("expected tcp_tw_reuse recommendation for TCPTWReuse=0 and TimeWaitCount=5000")
	}
}

func TestTCPTWReuseNotTriggeredWhenEnabled(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{Data: &NetworkData{
					TCPTWReuse: 1,
					TCP:        &TCPStats{TimeWaitCount: 5000},
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	for _, r := range recs {
		for _, cmd := range r.Commands {
			if cmd == "sysctl -w net.ipv4.tcp_tw_reuse=1" {
				t.Error("unexpected tcp_tw_reuse recommendation when already enabled")
			}
		}
	}
}

// ---------- Disk scheduler ----------

func TestDiskSchedulerRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"disk": {
				{Data: &DiskData{
					Devices: []DiskDevice{
						{
							Name:       "sda",
							Rotational: false, // SSD
							Scheduler:  "cfq", // should recommend mq-deadline
						},
						{
							Name:       "sdb",
							Rotational: true,       // HDD
							Scheduler:  "deadline", // should recommend bfq
						},
					},
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasMQDeadline := false
	hasBFQ := false
	for _, r := range recs {
		if r.Category == "disk" {
			for _, cmd := range r.Commands {
				if cmd == "echo mq-deadline > /sys/block/sda/queue/scheduler" {
					hasMQDeadline = true
				}
				if cmd == "echo bfq > /sys/block/sdb/queue/scheduler" {
					hasBFQ = true
				}
			}
		}
	}

	if !hasMQDeadline {
		t.Error("expected mq-deadline recommendation for SSD with cfq scheduler")
	}
	if !hasBFQ {
		t.Error("expected bfq recommendation for HDD with deadline scheduler")
	}
}

func TestDiskSchedulerNoRecommendationWhenOptimal(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"disk": {
				{Data: &DiskData{
					Devices: []DiskDevice{
						{Name: "sda", Rotational: false, Scheduler: "mq-deadline"},
						{Name: "sdb", Rotational: true, Scheduler: "bfq"},
					},
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	for _, r := range recs {
		if r.Category == "disk" {
			t.Errorf("unexpected disk recommendation when schedulers are optimal: %s", r.Title)
		}
	}
}

// ---------- THP ----------

func TestTHPRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"memory": {
				{Data: &MemoryData{
					THPEnabled: "always",
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasTHP := false
	for _, r := range recs {
		if r.Category == "memory" {
			for _, cmd := range r.Commands {
				if cmd == "echo madvise > /sys/kernel/mm/transparent_hugepage/enabled" {
					hasTHP = true
				}
			}
		}
	}

	if !hasTHP {
		t.Error("expected THP madvise recommendation for THPEnabled='always'")
	}
}

func TestTHPNoRecommendationWhenMadvise(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"memory": {
				{Data: &MemoryData{
					THPEnabled: "madvise",
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	for _, r := range recs {
		if r.Category == "memory" {
			for _, cmd := range r.Commands {
				if cmd == "echo madvise > /sys/kernel/mm/transparent_hugepage/enabled" {
					t.Error("unexpected THP recommendation when already set to madvise")
				}
			}
		}
	}
}

// ---------- min_free_kbytes ----------

func TestMinFreeKbytesRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"memory": {
				{Data: &MemoryData{
					TotalBytes:    32 * 1024 * 1024 * 1024, // 32 GB
					MinFreeKbytes: 32768,                   // < 65536 threshold
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasMinFree := false
	for _, r := range recs {
		if r.Category == "memory" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w vm.min_free_kbytes=131072" {
					hasMinFree = true
				}
			}
		}
	}

	if !hasMinFree {
		t.Error("expected min_free_kbytes recommendation for 32GB system with min_free_kbytes=32768")
	}
}

func TestMinFreeKbytesNoRecommendationWhenSufficient(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"memory": {
				{Data: &MemoryData{
					TotalBytes:    32 * 1024 * 1024 * 1024, // 32 GB
					MinFreeKbytes: 131072,                  // >= 65536 threshold
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	for _, r := range recs {
		if r.Category == "memory" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w vm.min_free_kbytes=131072" {
					t.Error("unexpected min_free_kbytes recommendation when already sufficient")
				}
			}
		}
	}
}

// ---------- TCP max SYN backlog ----------

func TestTCPMaxSynBacklogRecommendation(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{Data: &NetworkData{
					TCPMaxSynBacklog: 128,
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	hasSynBacklog := false
	for _, r := range recs {
		if r.Category == "network" {
			for _, cmd := range r.Commands {
				if cmd == "sysctl -w net.ipv4.tcp_max_syn_backlog=8192" {
					hasSynBacklog = true
				}
			}
		}
	}

	if !hasSynBacklog {
		t.Error("expected tcp_max_syn_backlog recommendation for value=128 (< 4096)")
	}
}

func TestTCPMaxSynBacklogNoRecommendationWhenSufficient(t *testing.T) {
	report := &Report{
		Categories: map[string][]Result{
			"network": {
				{Data: &NetworkData{
					TCPMaxSynBacklog: 8192,
				}},
			},
		},
	}

	recs := GenerateRecommendations(report)

	for _, r := range recs {
		for _, cmd := range r.Commands {
			if cmd == "sysctl -w net.ipv4.tcp_max_syn_backlog=8192" {
				t.Error("unexpected tcp_max_syn_backlog recommendation when already sufficient")
			}
		}
	}
}

// ---------- isLowTCPBuffer ----------

func TestIsLowTCPBuffer(t *testing.T) {
	tests := []struct {
		name string
		buf  string
		want bool
	}{
		{
			name: "normal_buffer_above_4MB",
			buf:  "4096 87380 6291456",
			want: false,
		},
		{
			name: "low_buffer_2MB",
			buf:  "4096 87380 2097152",
			want: true,
		},
		{
			name: "very_low_buffer_128KB",
			buf:  "4096 16384 131072",
			want: true,
		},
		{
			name: "empty_string",
			buf:  "",
			want: false,
		},
		{
			name: "exactly_4MB",
			buf:  "4096 87380 4194304",
			want: false,
		},
		{
			name: "just_below_4MB",
			buf:  "4096 87380 4194303",
			want: true,
		},
		{
			name: "malformed_input",
			buf:  "not a number",
			want: false,
		},
		{
			name: "too_few_fields",
			buf:  "4096 87380",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLowTCPBuffer(tt.buf)
			if got != tt.want {
				t.Errorf("isLowTCPBuffer(%q) = %v, want %v", tt.buf, got, tt.want)
			}
		})
	}
}
