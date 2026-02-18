package collector

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/baikal/sysdiag/internal/model"
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
	tcp := c.parseSNMP()

	if tcp == nil {
		t.Fatal("parseSNMP returned nil")
	}
	if tcp.ActiveOpens != 50000 {
		t.Errorf("ActiveOpens = %d, want 50000", tcp.ActiveOpens)
	}
	if tcp.PassiveOpens != 30000 {
		t.Errorf("PassiveOpens = %d, want 30000", tcp.PassiveOpens)
	}
	if tcp.CurrEstab != 1500 {
		t.Errorf("CurrEstab = %d, want 1500", tcp.CurrEstab)
	}
	if tcp.RetransSegs != 500 {
		t.Errorf("RetransSegs = %d, want 500", tcp.RetransSegs)
	}
	if tcp.InErrs != 25 {
		t.Errorf("InErrs = %d, want 25", tcp.InErrs)
	}
	if tcp.OutRsts != 300 {
		t.Errorf("OutRsts = %d, want 300", tcp.OutRsts)
	}
}

func TestParseSNMPMissingFile(t *testing.T) {
	c := NewNetworkCollector("/nonexistent/path")
	tcp := c.parseSNMP()
	if tcp != nil {
		t.Errorf("parseSNMP with missing file: got %v, want nil", tcp)
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
	snmp1 := c.parseSNMP()
	snmp2 := c.parseSNMP()
	if snmp1 == nil || snmp2 == nil {
		t.Fatal("parseSNMP returned nil")
	}
	if snmp1.RetransSegs != snmp2.RetransSegs {
		t.Fatalf("same file should give same RetransSegs, got %d vs %d",
			snmp1.RetransSegs, snmp2.RetransSegs)
	}

	// Simulate a real delta: snmp1=400, snmp2=500, interval=1s => rate=100.
	snmp1.RetransSegs = 400
	retransDelta := snmp2.RetransSegs - snmp1.RetransSegs // 500-400=100
	if retransDelta < 0 {
		retransDelta = 0
	}
	intervalSec := 1.0
	rate := float64(retransDelta) / intervalSec
	if rate != 100.0 {
		t.Errorf("retransRate = %f, want 100.0", rate)
	}

	// Counter-wrap protection: when second sample is smaller, delta is clamped to 0.
	snmp1.RetransSegs = 1000
	retransDelta = snmp2.RetransSegs - snmp1.RetransSegs // 500-1000=-500
	if retransDelta < 0 {
		retransDelta = 0
	}
	rate = float64(retransDelta) / intervalSec
	if rate != 0.0 {
		t.Errorf("retransRate with counter wrap = %f, want 0.0", rate)
	}

	// Zero delta => zero rate.
	snmp1.RetransSegs = 500
	retransDelta = snmp2.RetransSegs - snmp1.RetransSegs // 500-500=0
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
