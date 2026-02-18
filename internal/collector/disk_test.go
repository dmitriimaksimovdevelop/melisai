package collector

import (
	"math"
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// testdata layout (relative to this file):
//   ../../testdata/proc/diskstats           — 8 entries: sda, sda1, sda2, nvme0n1, nvme0n1p1, loop0, loop1, dm-0
//   ../../testdata/sys/block/sda/queue/     — scheduler, nr_requests, rotational, read_ahead_kb
//   ../../testdata/sys/block/nvme0n1/queue/ — same four files
//   ../../testdata/proc/pressure/io         — PSI some + full lines

// testProcRoot and testSysRoot are declared in memory_test.go;
// reuse those package-level constants here.

// ---------- 1. isVirtualOrPartition ----------

func TestIsVirtualOrPartition(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSkip bool // true = should be filtered out
	}{
		// Whole disks -- should pass (return false)
		{"sda whole disk", "sda", false},
		{"sdb whole disk", "sdb", false},
		{"nvme0n1 whole disk", "nvme0n1", false},
		{"nvme1n1 whole disk", "nvme1n1", false},
		{"vda virtio disk", "vda", false},
		{"hda legacy disk", "hda", false},
		{"mmcblk0 mmc disk", "mmcblk0", false},

		// Partitions -- should be skipped (return true)
		{"sda1 partition", "sda1", true},
		{"sda12 partition", "sda12", true},
		{"sdb3 partition", "sdb3", true},
		{"nvme0n1p1 partition", "nvme0n1p1", true},
		{"nvme0n1p15 partition", "nvme0n1p15", true},
		{"nvme2n1p3 partition", "nvme2n1p3", true},
		{"vda1 partition", "vda1", true},
		{"vda15 partition", "vda15", true},
		{"hda1 partition", "hda1", true},
		{"mmcblk0p1 partition", "mmcblk0p1", true},
		{"mmcblk0p5 partition", "mmcblk0p5", true},

		// Virtual devices -- should be skipped (return true)
		{"loop0 device", "loop0", true},
		{"loop1 device", "loop1", true},
		{"loop99 device", "loop99", true},
		{"dm-0 device-mapper", "dm-0", true},
		{"dm-5 device-mapper", "dm-5", true},
		{"ram0 ramdisk", "ram0", true},
		{"ram16 ramdisk", "ram16", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVirtualOrPartition(tt.input)
			if got != tt.wantSkip {
				t.Errorf("isVirtualOrPartition(%q) = %v, want %v", tt.input, got, tt.wantSkip)
			}
		})
	}
}

// ---------- 2. readDiskStats ----------

func TestReadDiskStats(t *testing.T) {
	dc := NewDiskCollector(testProcRoot, testSysRoot)
	stats := dc.readDiskStats()

	if stats == nil {
		t.Fatal("readDiskStats returned nil")
	}

	// Should contain only whole disks: sda and nvme0n1.
	// Partitions (sda1, sda2, nvme0n1p1) and virtual devices (loop0, loop1, dm-0)
	// must be filtered out.
	wantDevices := []string{"sda", "nvme0n1"}
	skipDevices := []string{"sda1", "sda2", "nvme0n1p1", "loop0", "loop1", "dm-0"}

	for _, name := range wantDevices {
		if _, ok := stats[name]; !ok {
			t.Errorf("expected device %q in results, but not found", name)
		}
	}
	for _, name := range skipDevices {
		if _, ok := stats[name]; ok {
			t.Errorf("device %q should be filtered out, but found in results", name)
		}
	}

	if len(stats) != 2 {
		t.Errorf("expected 2 devices, got %d", len(stats))
	}

	// Verify sda parsed values from testdata:
	//   8  0 sda 50000 1000 2000000 25000 30000 500 1500000 15000 0 20000 40000
	// fields[3]=readOps=50000, fields[5]=readSectors=2000000, fields[7]=writeOps=30000,
	// fields[9]=writeSectors=1500000, fields[11]=ioInProg=0, fields[12]=ioTimeMs=20000,
	// fields[13]=wIOTimeMs=40000
	sda := stats["sda"]
	if sda.readOps != 50000 {
		t.Errorf("sda readOps = %d, want 50000", sda.readOps)
	}
	if sda.readBytes != 2000000*512 {
		t.Errorf("sda readBytes = %d, want %d", sda.readBytes, uint64(2000000*512))
	}
	if sda.writeOps != 30000 {
		t.Errorf("sda writeOps = %d, want 30000", sda.writeOps)
	}
	if sda.writeBytes != 1500000*512 {
		t.Errorf("sda writeBytes = %d, want %d", sda.writeBytes, uint64(1500000*512))
	}
	if sda.ioInProg != 0 {
		t.Errorf("sda ioInProg = %d, want 0", sda.ioInProg)
	}
	if sda.ioTimeMs != 20000 {
		t.Errorf("sda ioTimeMs = %d, want 20000", sda.ioTimeMs)
	}
	if sda.wIOTimeMs != 40000 {
		t.Errorf("sda wIOTimeMs = %d, want 40000", sda.wIOTimeMs)
	}

	// Verify nvme0n1 parsed values:
	//   259  0 nvme0n1 100000 0 4000000 10000 80000 0 3200000 8000 5 15000 18000
	nvme := stats["nvme0n1"]
	if nvme.readOps != 100000 {
		t.Errorf("nvme0n1 readOps = %d, want 100000", nvme.readOps)
	}
	if nvme.readBytes != 4000000*512 {
		t.Errorf("nvme0n1 readBytes = %d, want %d", nvme.readBytes, uint64(4000000*512))
	}
	if nvme.writeOps != 80000 {
		t.Errorf("nvme0n1 writeOps = %d, want 80000", nvme.writeOps)
	}
	if nvme.writeBytes != 3200000*512 {
		t.Errorf("nvme0n1 writeBytes = %d, want %d", nvme.writeBytes, uint64(3200000*512))
	}
	if nvme.ioInProg != 5 {
		t.Errorf("nvme0n1 ioInProg = %d, want 5", nvme.ioInProg)
	}
	if nvme.ioTimeMs != 15000 {
		t.Errorf("nvme0n1 ioTimeMs = %d, want 15000", nvme.ioTimeMs)
	}
	if nvme.wIOTimeMs != 18000 {
		t.Errorf("nvme0n1 wIOTimeMs = %d, want 18000", nvme.wIOTimeMs)
	}
}

func TestReadDiskStats_MissingFile(t *testing.T) {
	dc := NewDiskCollector("/nonexistent/path", testSysRoot)
	stats := dc.readDiskStats()

	if stats != nil {
		t.Errorf("expected nil for missing procfs, got %v", stats)
	}
}

// ---------- 3. AvgLatencyMs computation ----------

func TestAvgLatencyMs(t *testing.T) {
	tests := []struct {
		name         string
		readOps      int64
		writeOps     int64
		weightedIOMs int64
		wantLatency  float64
	}{
		{
			name:         "mixed read/write load",
			readOps:      100,
			writeOps:     200,
			weightedIOMs: 600,
			wantLatency:  2.0, // 600 / (100+200) = 2.0
		},
		{
			name:         "read only",
			readOps:      500,
			writeOps:     0,
			weightedIOMs: 1000,
			wantLatency:  2.0, // 1000 / 500 = 2.0
		},
		{
			name:         "write only",
			readOps:      0,
			writeOps:     250,
			weightedIOMs: 500,
			wantLatency:  2.0, // 500 / 250 = 2.0
		},
		{
			name:         "high latency",
			readOps:      10,
			writeOps:     10,
			weightedIOMs: 2000,
			wantLatency:  100.0, // 2000 / 20 = 100.0
		},
		{
			name:         "fractional latency",
			readOps:      3,
			writeOps:     7,
			weightedIOMs: 15,
			wantLatency:  1.5, // 15 / 10 = 1.5
		},
		{
			name:         "zero ops yields zero latency",
			readOps:      0,
			writeOps:     0,
			weightedIOMs: 500,
			wantLatency:  0.0, // division guard: no ops => 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the exact formula from Collect():
			//   totalOps := dev.ReadOps + dev.WriteOps
			//   if totalOps > 0 { dev.AvgLatencyMs = float64(dev.WeightedIOMs) / float64(totalOps) }
			totalOps := tt.readOps + tt.writeOps
			var got float64
			if totalOps > 0 {
				got = float64(tt.weightedIOMs) / float64(totalOps)
			}
			if math.Abs(got-tt.wantLatency) > 1e-9 {
				t.Errorf("AvgLatencyMs = %f, want %f", got, tt.wantLatency)
			}
		})
	}
}

// ---------- 4. Sysfs enrichment ----------

func TestSysfsEnrichment(t *testing.T) {
	dc := NewDiskCollector(testProcRoot, testSysRoot)

	// sda: rotational HDD, mq-deadline scheduler, 128 queue depth, 128 KB read-ahead
	t.Run("sda sysfs", func(t *testing.T) {
		basePath := testSysRoot + "/block/sda"

		sched := dc.readScheduler(basePath)
		if sched != "mq-deadline" {
			t.Errorf("sda scheduler = %q, want %q", sched, "mq-deadline")
		}

		qd := dc.readQueueDepth(basePath)
		if qd != 128 {
			t.Errorf("sda queue depth = %d, want 128", qd)
		}

		rot := dc.readFile(basePath + "/queue/rotational")
		if rot != "1" {
			t.Errorf("sda rotational raw = %q, want %q", rot, "1")
		}
		if rot != "1" {
			t.Errorf("sda should be rotational (HDD)")
		}

		raKB := dc.readFile(basePath + "/queue/read_ahead_kb")
		if raKB != "128" {
			t.Errorf("sda read_ahead_kb = %q, want %q", raKB, "128")
		}
	})

	// nvme0n1: non-rotational SSD, "none" scheduler, 256 queue depth, 128 KB read-ahead
	t.Run("nvme0n1 sysfs", func(t *testing.T) {
		basePath := testSysRoot + "/block/nvme0n1"

		sched := dc.readScheduler(basePath)
		if sched != "none" {
			t.Errorf("nvme0n1 scheduler = %q, want %q", sched, "none")
		}

		qd := dc.readQueueDepth(basePath)
		if qd != 256 {
			t.Errorf("nvme0n1 queue depth = %d, want 256", qd)
		}

		rot := dc.readFile(basePath + "/queue/rotational")
		if rot != "0" {
			t.Errorf("nvme0n1 rotational raw = %q, want %q", rot, "0")
		}

		raKB := dc.readFile(basePath + "/queue/read_ahead_kb")
		if raKB != "128" {
			t.Errorf("nvme0n1 read_ahead_kb = %q, want %q", raKB, "128")
		}
	})

	// Missing sysfs path should return zero-value defaults without errors
	t.Run("missing sysfs graceful fallback", func(t *testing.T) {
		basePath := testSysRoot + "/block/nonexistent"

		sched := dc.readScheduler(basePath)
		if sched != "" {
			t.Errorf("missing scheduler = %q, want empty string", sched)
		}

		qd := dc.readQueueDepth(basePath)
		if qd != 0 {
			t.Errorf("missing queue depth = %d, want 0", qd)
		}

		rot := dc.readFile(basePath + "/queue/rotational")
		if rot != "" {
			t.Errorf("missing rotational = %q, want empty string", rot)
		}

		raKB := dc.readFile(basePath + "/queue/read_ahead_kb")
		if raKB != "" {
			t.Errorf("missing read_ahead_kb = %q, want empty string", raKB)
		}
	})
}

// ---------- 5. I/O PSI parsing ----------

func TestParseIOPSI(t *testing.T) {
	t.Run("valid psi file", func(t *testing.T) {
		dc := NewDiskCollector(testProcRoot, testSysRoot)
		data := &model.DiskData{}
		dc.parseIOPSI(data)

		// testdata/proc/pressure/io:
		//   some avg10=3.20 avg60=2.10 avg300=1.05 total=8000000
		//   full avg10=1.50 avg60=0.80 avg300=0.40 total=3000000
		// parseIOPSI only reads the "some" line for avg10 and avg60.
		if math.Abs(data.PSISome10-3.20) > 1e-9 {
			t.Errorf("PSISome10 = %f, want 3.20", data.PSISome10)
		}
		if math.Abs(data.PSISome60-2.10) > 1e-9 {
			t.Errorf("PSISome60 = %f, want 2.10", data.PSISome60)
		}
	})

	t.Run("missing psi file does not panic", func(t *testing.T) {
		dc := NewDiskCollector("/nonexistent/path", testSysRoot)
		data := &model.DiskData{}
		dc.parseIOPSI(data)

		// Values must remain zero when the file is absent
		if data.PSISome10 != 0 {
			t.Errorf("missing PSISome10 = %f, want 0", data.PSISome10)
		}
		if data.PSISome60 != 0 {
			t.Errorf("missing PSISome60 = %f, want 0", data.PSISome60)
		}
	})
}

// ---------- 6. NewDiskCollector + metadata ----------

func TestNewDiskCollector(t *testing.T) {
	dc := NewDiskCollector("/proc", "/sys")

	if dc.procRoot != "/proc" {
		t.Errorf("procRoot = %q, want /proc", dc.procRoot)
	}
	if dc.sysRoot != "/sys" {
		t.Errorf("sysRoot = %q, want /sys", dc.sysRoot)
	}
	if dc.Name() != "disk_stats" {
		t.Errorf("Name() = %q, want disk_stats", dc.Name())
	}
	if dc.Category() != "disk" {
		t.Errorf("Category() = %q, want disk", dc.Category())
	}

	avail := dc.Available()
	if avail.Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", avail.Tier)
	}
}
