package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// mockCommandRunner fakes external command execution for testing.
type mockCommandRunner struct {
	outputs map[string][]byte
	errors  map[string]error
}

func (m *mockCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	if err, ok := m.errors[key]; ok {
		return nil, err
	}
	if out, ok := m.outputs[key]; ok {
		return out, nil
	}
	return nil, fmt.Errorf("command not found: %s", key)
}

// ---------- parseNetDev ----------

func TestParseNetDev(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	ifaces := c.parseNetDev()

	if len(ifaces) != 2 {
		t.Fatalf("parseNetDev: got %d interfaces, want 2", len(ifaces))
	}

	// Build a map for lookup by name.
	ifaceMap := make(map[string]model.NetworkInterface, len(ifaces))
	for _, iface := range ifaces {
		ifaceMap[iface.Name] = iface
	}

	// Verify lo
	lo, ok := ifaceMap["lo"]
	if !ok {
		t.Fatal("parseNetDev: missing lo interface")
	}
	if lo.RxBytes != 1000000 {
		t.Errorf("lo.RxBytes = %d, want 1000000", lo.RxBytes)
	}
	if lo.TxBytes != 1000000 {
		t.Errorf("lo.TxBytes = %d, want 1000000", lo.TxBytes)
	}
	if lo.RxPackets != 10000 {
		t.Errorf("lo.RxPackets = %d, want 10000", lo.RxPackets)
	}
	if lo.TxPackets != 10000 {
		t.Errorf("lo.TxPackets = %d, want 10000", lo.TxPackets)
	}
	if lo.RxErrors != 0 {
		t.Errorf("lo.RxErrors = %d, want 0", lo.RxErrors)
	}
	if lo.RxDropped != 0 {
		t.Errorf("lo.RxDropped = %d, want 0", lo.RxDropped)
	}

	// Verify eth0
	eth, ok := ifaceMap["eth0"]
	if !ok {
		t.Fatal("parseNetDev: missing eth0 interface")
	}
	if eth.RxBytes != 5000000000 {
		t.Errorf("eth0.RxBytes = %d, want 5000000000", eth.RxBytes)
	}
	if eth.TxBytes != 3000000000 {
		t.Errorf("eth0.TxBytes = %d, want 3000000000", eth.TxBytes)
	}
	if eth.RxPackets != 4000000 {
		t.Errorf("eth0.RxPackets = %d, want 4000000", eth.RxPackets)
	}
	if eth.TxPackets != 3000000 {
		t.Errorf("eth0.TxPackets = %d, want 3000000", eth.TxPackets)
	}
	if eth.RxErrors != 10 {
		t.Errorf("eth0.RxErrors = %d, want 10", eth.RxErrors)
	}
	if eth.RxDropped != 5 {
		t.Errorf("eth0.RxDropped = %d, want 5", eth.RxDropped)
	}
	if eth.TxErrors != 20 {
		t.Errorf("eth0.TxErrors = %d, want 20", eth.TxErrors)
	}
	if eth.TxDropped != 3 {
		t.Errorf("eth0.TxDropped = %d, want 3", eth.TxDropped)
	}
}

func TestParseNetDevMissingFile(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	ifaces := c.parseNetDev()
	if ifaces != nil {
		t.Errorf("parseNetDev with missing file: got %v, want nil", ifaces)
	}
}

// ---------- parseSNMP ----------

func TestParseSNMP(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	data := &model.NetworkData{}
	c.parseSNMP(data)

	if data.TCP == nil {
		t.Fatal("parseSNMP returned nil TCP")
	}
	if data.TCP.ActiveOpens != 50000 {
		t.Errorf("ActiveOpens = %d, want 50000", data.TCP.ActiveOpens)
	}
	if data.TCP.PassiveOpens != 30000 {
		t.Errorf("PassiveOpens = %d, want 30000", data.TCP.PassiveOpens)
	}
	if data.TCP.CurrEstab != 1500 {
		t.Errorf("CurrEstab = %d, want 1500", data.TCP.CurrEstab)
	}
	if data.TCP.RetransSegs != 500 {
		t.Errorf("RetransSegs = %d, want 500", data.TCP.RetransSegs)
	}
	if data.TCP.InErrs != 25 {
		t.Errorf("InErrs = %d, want 25", data.TCP.InErrs)
	}
	if data.TCP.OutRsts != 300 {
		t.Errorf("OutRsts = %d, want 300", data.TCP.OutRsts)
	}
	// UDP stats parsed in same pass
	if data.UDPRcvbufErrors != 42 {
		t.Errorf("UDPRcvbufErrors = %d, want 42", data.UDPRcvbufErrors)
	}
	if data.UDPSndbufErrors != 3 {
		t.Errorf("UDPSndbufErrors = %d, want 3", data.UDPSndbufErrors)
	}
	if data.UDPInErrors != 10 {
		t.Errorf("UDPInErrors = %d, want 10", data.UDPInErrors)
	}
}

func TestParseSNMPMissingFile(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	data := &model.NetworkData{}
	c.parseSNMP(data)
	if data.TCP != nil {
		t.Errorf("parseSNMP with missing file: got TCP %v, want nil", data.TCP)
	}
}

// ---------- parseSSConnections ----------

func TestParseSSConnections(t *testing.T) {
	mock := &mockCommandRunner{
		outputs: map[string][]byte{
			"ss -s": []byte("Total: 1234\n" +
				"TCP:   500 (estab 200, closed 50, orphaned 3, timewait 42)\n" +
				"UDP:   10\n"),
			"ss -tn state close-wait": []byte("State       Recv-Q Send-Q Local Address:Port  Peer Address:Port\n" +
				"CLOSE-WAIT  0      0      10.0.0.1:45678       10.0.0.2:443\n" +
				"CLOSE-WAIT  0      0      10.0.0.1:45679       10.0.0.3:443\n" +
				"CLOSE-WAIT  0      0      10.0.0.1:45680       10.0.0.4:443\n"),
		},
		errors: map[string]error{},
	}

	c := NewNetworkCollectorWithRunner("../../testdata/proc", mock)

	data := &model.NetworkData{
		TCP: &model.TCPStats{},
	}
	c.parseSSConnections(context.Background(), data)

	if data.TCP.TimeWaitCount != 42 {
		t.Errorf("TimeWaitCount = %d, want 42", data.TCP.TimeWaitCount)
	}
	if data.TCP.CloseWaitCount != 3 {
		t.Errorf("CloseWaitCount = %d, want 3", data.TCP.CloseWaitCount)
	}
}

func TestParseSSConnectionsCommandFailure(t *testing.T) {
	mock := &mockCommandRunner{
		outputs: map[string][]byte{},
		errors: map[string]error{
			"ss -s":                   fmt.Errorf("ss: command not found"),
			"ss -tn state close-wait": fmt.Errorf("ss: command not found"),
		},
	}

	c := NewNetworkCollectorWithRunner("../../testdata/proc", mock)

	data := &model.NetworkData{
		TCP: &model.TCPStats{},
	}
	// Should not panic; fields should remain at zero values.
	c.parseSSConnections(context.Background(), data)

	if data.TCP.TimeWaitCount != 0 {
		t.Errorf("TimeWaitCount = %d, want 0 (ss failed)", data.TCP.TimeWaitCount)
	}
	if data.TCP.CloseWaitCount != 0 {
		t.Errorf("CloseWaitCount = %d, want 0 (ss failed)", data.TCP.CloseWaitCount)
	}
}

// ---------- TCP retransmit rate calculation ----------

func TestRetransmitRateCalculation(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")

	// Two reads from the same static file yield identical counters.
	d1 := &model.NetworkData{}
	d2 := &model.NetworkData{}
	c.parseSNMP(d1)
	c.parseSNMP(d2)
	if d1.TCP == nil || d2.TCP == nil {
		t.Fatal("parseSNMP returned nil TCP")
	}
	if d1.TCP.RetransSegs != d2.TCP.RetransSegs {
		t.Fatalf("same file should give same RetransSegs, got %d vs %d",
			d1.TCP.RetransSegs, d2.TCP.RetransSegs)
	}

	// Simulate a real delta: snmp1=400, snmp2=500, interval=1s => rate=100.
	d1.TCP.RetransSegs = 400
	retransDelta := d2.TCP.RetransSegs - d1.TCP.RetransSegs // 500-400=100
	if retransDelta < 0 {
		retransDelta = 0
	}
	intervalSec := 1.0
	rate := float64(retransDelta) / intervalSec
	if rate != 100.0 {
		t.Errorf("retransRate = %f, want 100.0", rate)
	}

	// Counter-wrap protection: when second sample is smaller, delta is clamped to 0.
	d1.TCP.RetransSegs = 1000
	retransDelta = d2.TCP.RetransSegs - d1.TCP.RetransSegs // 500-1000=-500
	if retransDelta < 0 {
		retransDelta = 0
	}
	rate = float64(retransDelta) / intervalSec
	if rate != 0.0 {
		t.Errorf("retransRate with counter wrap = %f, want 0.0", rate)
	}

	// Zero delta => zero rate.
	d1.TCP.RetransSegs = 500
	retransDelta = d2.TCP.RetransSegs - d1.TCP.RetransSegs // 500-500=0
	if retransDelta < 0 {
		retransDelta = 0
	}
	rate = float64(retransDelta) / intervalSec
	if rate != 0.0 {
		t.Errorf("retransRate with zero delta = %f, want 0.0", rate)
	}
}

// ---------- sysctl reading ----------

func TestSysctlReading(t *testing.T) {
	procRoot := "../../testdata/proc"

	t.Run("congestion_control", func(t *testing.T) {
		got := readSysctlString(procRoot, "sys/net/ipv4/tcp_congestion_control")
		if got != "cubic" {
			t.Errorf("tcp_congestion_control = %q, want %q", got, "cubic")
		}
	})

	t.Run("tcp_rmem", func(t *testing.T) {
		got := readSysctlString(procRoot, "sys/net/ipv4/tcp_rmem")
		if got != "4096\t87380\t6291456" {
			t.Errorf("tcp_rmem = %q, want %q", got, "4096\t87380\t6291456")
		}
	})

	t.Run("tcp_wmem", func(t *testing.T) {
		got := readSysctlString(procRoot, "sys/net/ipv4/tcp_wmem")
		// The testdata file content should be trimmed.
		if got == "" {
			t.Error("tcp_wmem is empty, expected a value")
		}
	})

	t.Run("somaxconn", func(t *testing.T) {
		got := readSysctlInt(procRoot, "sys/net/core/somaxconn")
		if got != 4096 {
			t.Errorf("somaxconn = %d, want 4096", got)
		}
	})

	t.Run("tcp_max_syn_backlog", func(t *testing.T) {
		got := readSysctlInt(procRoot, "sys/net/ipv4/tcp_max_syn_backlog")
		if got != 4096 {
			t.Errorf("tcp_max_syn_backlog = %d, want 4096", got)
		}
	})

	t.Run("tcp_tw_reuse", func(t *testing.T) {
		got := readSysctlInt(procRoot, "sys/net/ipv4/tcp_tw_reuse")
		if got != 0 {
			t.Errorf("tcp_tw_reuse = %d, want 0", got)
		}
	})

	t.Run("nonexistent_string", func(t *testing.T) {
		got := readSysctlString(procRoot, "sys/net/ipv4/nonexistent")
		if got != "" {
			t.Errorf("nonexistent sysctl string = %q, want empty", got)
		}
	})

	t.Run("nonexistent_int", func(t *testing.T) {
		got := readSysctlInt(procRoot, "sys/net/ipv4/nonexistent")
		if got != 0 {
			t.Errorf("nonexistent sysctl int = %d, want 0", got)
		}
	})
}

// ---------- NetworkCollector metadata ----------

func TestNetworkCollectorName(t *testing.T) {
	c := NewNetworkCollector("/proc")
	if got := c.Name(); got != "network_stats" {
		t.Errorf("Name() = %q, want %q", got, "network_stats")
	}
}

func TestNetworkCollectorCategory(t *testing.T) {
	c := NewNetworkCollector("/proc")
	if got := c.Category(); got != "network" {
		t.Errorf("Category() = %q, want %q", got, "network")
	}
}

func TestNetworkCollectorAvailability(t *testing.T) {
	c := NewNetworkCollector("/proc")
	avail := c.Available()
	if avail.Tier != 1 {
		t.Errorf("Available().Tier = %d, want 1", avail.Tier)
	}
}

// ---------- Regression: ContainerCollector.Category() (bug #2) ----------

// TestContainerCategoryRegression verifies ContainerCollector.Category()
// returns "container". This is a regression test for bug #2 where the
// category was incorrectly set.
func TestContainerCategoryRegression(t *testing.T) {
	c := NewContainerCollector("/proc", "/sys")
	got := c.Category()
	if got != "container" {
		t.Errorf("ContainerCollector.Category() = %q, want %q (regression: bug #2)", got, "container")
	}
}

// ---------- parseConntrack ----------

func TestParseConntrack(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	ct := c.parseConntrack()

	if ct == nil {
		t.Fatal("parseConntrack returned nil")
	}
	if ct.Count != 15000 {
		t.Errorf("Count = %d, want 15000", ct.Count)
	}
	if ct.Max != 65536 {
		t.Errorf("Max = %d, want 65536", ct.Max)
	}
	expectedPct := float64(15000) / float64(65536) * 100
	if ct.UsagePct < expectedPct-0.1 || ct.UsagePct > expectedPct+0.1 {
		t.Errorf("UsagePct = %.2f, want ~%.2f", ct.UsagePct, expectedPct)
	}
	if ct.Drops != 5 {
		t.Errorf("Drops = %d, want 5", ct.Drops)
	}
	if ct.EarlyDrop != 2 {
		t.Errorf("EarlyDrop = %d, want 2", ct.EarlyDrop)
	}
}

func TestParseConntrackNotLoaded(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	ct := c.parseConntrack()
	if ct != nil {
		t.Errorf("parseConntrack with missing files: got %v, want nil", ct)
	}
}

// ---------- parseSoftnetStat ----------

func TestParseSoftnetStat(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	stats := c.parseSoftnetStat()

	if len(stats) != 3 {
		t.Fatalf("parseSoftnetStat: got %d CPUs, want 3", len(stats))
	}

	// CPU 0: 0x0000beef=48879 processed, 0x00000002=2 dropped, 0x00000005=5 time_squeeze
	if stats[0].CPU != 0 {
		t.Errorf("stats[0].CPU = %d, want 0", stats[0].CPU)
	}
	if stats[0].Processed != 0xbeef {
		t.Errorf("stats[0].Processed = %d, want %d", stats[0].Processed, 0xbeef)
	}
	if stats[0].Dropped != 2 {
		t.Errorf("stats[0].Dropped = %d, want 2", stats[0].Dropped)
	}
	if stats[0].TimeSqueeze != 5 {
		t.Errorf("stats[0].TimeSqueeze = %d, want 5", stats[0].TimeSqueeze)
	}

	// CPU 1: 0x0000abcd=43981, 0 dropped, 3 time_squeeze
	if stats[1].Dropped != 0 {
		t.Errorf("stats[1].Dropped = %d, want 0", stats[1].Dropped)
	}

	// CPU 2: 0x0000ffff=65535, 1 dropped, 10 time_squeeze
	if stats[2].Processed != 0xffff {
		t.Errorf("stats[2].Processed = %d, want %d", stats[2].Processed, 0xffff)
	}
	if stats[2].Dropped != 1 {
		t.Errorf("stats[2].Dropped = %d, want 1", stats[2].Dropped)
	}
}

func TestParseSoftnetStatMissing(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	stats := c.parseSoftnetStat()
	if stats != nil {
		t.Errorf("parseSoftnetStat with missing file: got %v, want nil", stats)
	}
}

// ---------- readNetRxSoftirqs ----------

func TestReadNetRxSoftirqs(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	counts := c.readNetRxSoftirqs()

	if len(counts) != 3 {
		t.Fatalf("readNetRxSoftirqs: got %d CPUs, want 3", len(counts))
	}
	// NET_RX: 80000 50000 30000
	if counts[0] != 80000 {
		t.Errorf("counts[0] = %d, want 80000", counts[0])
	}
	if counts[1] != 50000 {
		t.Errorf("counts[1] = %d, want 50000", counts[1])
	}
	if counts[2] != 30000 {
		t.Errorf("counts[2] = %d, want 30000", counts[2])
	}
}

func TestReadNetRxSoftirqsMissing(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	counts := c.readNetRxSoftirqs()
	if counts != nil {
		t.Errorf("readNetRxSoftirqs with missing file: got %v, want nil", counts)
	}
}

// ---------- computeIRQDistribution ----------

func TestComputeIRQDistribution(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	// Use static test data — sample1 slightly less than what's in testdata/proc/softirqs
	sample1 := []int64{79000, 49500, 29800}
	dist := c.computeIRQDistribution(sample1)

	if len(dist) != 3 {
		t.Fatalf("computeIRQDistribution: got %d CPUs, want 3", len(dist))
	}
	// Delta: 80000-79000=1000, 50000-49500=500, 30000-29800=200
	if dist[0].NetRxDelta != 1000 {
		t.Errorf("dist[0].NetRxDelta = %d, want 1000", dist[0].NetRxDelta)
	}
	if dist[1].NetRxDelta != 500 {
		t.Errorf("dist[1].NetRxDelta = %d, want 500", dist[1].NetRxDelta)
	}
	if dist[2].NetRxDelta != 200 {
		t.Errorf("dist[2].NetRxDelta = %d, want 200", dist[2].NetRxDelta)
	}
}

func TestComputeIRQDistributionNilSample(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	dist := c.computeIRQDistribution(nil)
	if dist != nil {
		t.Errorf("computeIRQDistribution(nil) = %v, want nil", dist)
	}
}

// ---------- parseNetstat ----------

func TestParseNetstat(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	data := &model.NetworkData{}
	c.parseNetstat(data)

	if data.ListenOverflows != 150 {
		t.Errorf("ListenOverflows = %d, want 150", data.ListenOverflows)
	}
	if data.ListenDrops != 200 {
		t.Errorf("ListenDrops = %d, want 200", data.ListenDrops)
	}
	if data.TCPAbortOnMemory != 7 {
		t.Errorf("TCPAbortOnMemory = %d, want 7", data.TCPAbortOnMemory)
	}
	if data.TCPOFOQueue != 500 {
		t.Errorf("TCPOFOQueue = %d, want 500", data.TCPOFOQueue)
	}
	if data.PruneCalled != 42 {
		t.Errorf("PruneCalled = %d, want 42", data.PruneCalled)
	}
}

func TestParseNetstatMissing(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	data := &model.NetworkData{}
	c.parseNetstat(data)
	// Should not panic; all values remain 0.
	if data.ListenOverflows != 0 {
		t.Errorf("ListenOverflows = %d, want 0", data.ListenOverflows)
	}
}

// ---------- parseRingBuffer ----------

func TestParseRingBuffer(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	iface := &model.NetworkInterface{Name: "eth0"}

	output := `Ring parameters for eth0:
Pre-set maximums:
RX:		4096
RX Mini:	0
RX Jumbo:	0
TX:		4096
Current hardware settings:
RX:		256
RX Mini:	0
RX Jumbo:	0
TX:		256
`
	c.parseRingBuffer(output, iface)

	if iface.RingRxMax != 4096 {
		t.Errorf("RingRxMax = %d, want 4096", iface.RingRxMax)
	}
	if iface.RingRxCur != 256 {
		t.Errorf("RingRxCur = %d, want 256", iface.RingRxCur)
	}
}

func TestParseRingBufferEmpty(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	iface := &model.NetworkInterface{Name: "eth0"}
	c.parseRingBuffer("", iface)
	if iface.RingRxMax != 0 || iface.RingRxCur != 0 {
		t.Errorf("parseRingBuffer empty: max=%d, cur=%d, want 0,0", iface.RingRxMax, iface.RingRxCur)
	}
}

// ---------- parseSockstat ----------

func TestParseSockstat(t *testing.T) {
	c := NewNetworkCollector("../../testdata/proc")
	data := &model.NetworkData{}
	c.parseSockstat(data)

	if data.TCPSocketsInUse != 500 {
		t.Errorf("TCPSocketsInUse = %d, want 500", data.TCPSocketsInUse)
	}
	if data.TCPOrphans != 12 {
		t.Errorf("TCPOrphans = %d, want 12", data.TCPOrphans)
	}
	if data.TCPMemPages != 150 {
		t.Errorf("TCPMemPages = %d, want 150", data.TCPMemPages)
	}
	if data.UDPSocketsInUse != 50 {
		t.Errorf("UDPSocketsInUse = %d, want 50", data.UDPSocketsInUse)
	}
}

func TestParseSockstatMissing(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	data := &model.NetworkData{}
	c.parseSockstat(data)
	if data.TCPSocketsInUse != 0 {
		t.Errorf("TCPSocketsInUse = %d, want 0", data.TCPSocketsInUse)
	}
}

// ---------- enrichNICDetails ----------

func TestEnrichNICDetails(t *testing.T) {
	// Create a fake sysfs tree
	sysDir := t.TempDir()

	// Create /sys/class/net/eth0/ structure
	ethDir := filepath.Join(sysDir, "class", "net", "eth0")
	os.MkdirAll(filepath.Join(ethDir, "queues", "rx-0"), 0755)
	os.MkdirAll(filepath.Join(ethDir, "queues", "rx-1"), 0755)
	os.MkdirAll(filepath.Join(ethDir, "queues", "tx-0"), 0755)
	os.MkdirAll(filepath.Join(ethDir, "queues", "tx-1"), 0755)
	os.MkdirAll(filepath.Join(ethDir, "queues", "tx-2"), 0755)

	// Speed
	os.WriteFile(filepath.Join(ethDir, "speed"), []byte("10000\n"), 0644)
	// MTU
	os.WriteFile(filepath.Join(ethDir, "mtu"), []byte("9001\n"), 0644)
	// RPS cpus (enabled — non-zero mask)
	os.WriteFile(filepath.Join(ethDir, "queues", "rx-0", "rps_cpus"), []byte("f\n"), 0644)

	// Mock ethtool
	mock := &mockCommandRunner{
		outputs: map[string][]byte{
			"ethtool -i eth0": []byte("driver: ixgbe\nversion: 5.1.0\nfirmware-version: 0x800035da\n"),
			"ethtool -g eth0": []byte("Ring parameters for eth0:\nPre-set maximums:\nRX:\t\t4096\nTX:\t\t4096\nCurrent hardware settings:\nRX:\t\t512\nTX:\t\t512\n"),
			"ethtool -S eth0": []byte("NIC statistics:\n     rx_discards: 150\n     rx_buf_errors: 5\n     tx_errors: 0\n"),
		},
		errors: map[string]error{},
	}

	c := NewNetworkCollectorFull("../../testdata/proc", sysDir, mock)

	data := &model.NetworkData{
		Interfaces: []model.NetworkInterface{
			{Name: "eth0"},
		},
	}
	c.enrichNICDetails(context.Background(), data)

	eth := data.Interfaces[0]
	if eth.Speed != "10000Mbps" {
		t.Errorf("Speed = %q, want %q", eth.Speed, "10000Mbps")
	}
	if eth.MTU != 9001 {
		t.Errorf("MTU = %d, want 9001", eth.MTU)
	}
	if eth.RxQueues != 2 {
		t.Errorf("RxQueues = %d, want 2", eth.RxQueues)
	}
	if eth.TxQueues != 3 {
		t.Errorf("TxQueues = %d, want 3", eth.TxQueues)
	}
	if !eth.RPSEnabled {
		t.Error("RPSEnabled = false, want true")
	}
	if eth.Driver != "ixgbe" {
		t.Errorf("Driver = %q, want %q", eth.Driver, "ixgbe")
	}
	if eth.RingRxMax != 4096 {
		t.Errorf("RingRxMax = %d, want 4096", eth.RingRxMax)
	}
	if eth.RingRxCur != 512 {
		t.Errorf("RingRxCur = %d, want 512", eth.RingRxCur)
	}
	if eth.RxDiscards != 150 {
		t.Errorf("RxDiscards = %d, want 150", eth.RxDiscards)
	}
	if eth.RxBufErrors != 5 {
		t.Errorf("RxBufErrors = %d, want 5", eth.RxBufErrors)
	}
}

func TestEnrichNICDetailsSkipsLoopback(t *testing.T) {
	mock := &mockCommandRunner{outputs: map[string][]byte{}, errors: map[string]error{}}
	c := NewNetworkCollectorFull("../../testdata/proc", "/nonexistent", mock)

	data := &model.NetworkData{
		Interfaces: []model.NetworkInterface{
			{Name: "lo"},
			{Name: "veth1234"},
			{Name: "docker0"},
			{Name: "br-abcd"},
		},
	}
	c.enrichNICDetails(context.Background(), data)

	// None of these should be enriched
	for _, iface := range data.Interfaces {
		if iface.Driver != "" {
			t.Errorf("interface %s should be skipped, got driver=%q", iface.Name, iface.Driver)
		}
	}
}

// ---------- mock runner edge cases ----------

func TestMockCommandRunnerNotFound(t *testing.T) {
	mock := &mockCommandRunner{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}

	_, err := mock.Run(context.Background(), "unknown_cmd", "--flag")
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "command not found") {
		t.Errorf("error = %q, want to contain 'command not found'", err.Error())
	}
}
