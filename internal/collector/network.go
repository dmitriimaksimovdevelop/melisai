// Network collector (Tier 1): /proc/net/dev, /proc/net/snmp,
// ss connection stats, TCP sysctl parameters, congestion control.
package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// NetworkCollector gathers network metrics from procfs (Tier 1).
type NetworkCollector struct {
	procRoot string
	cmdRun   CommandRunner
}

func NewNetworkCollector(procRoot string) *NetworkCollector {
	return &NetworkCollector{procRoot: procRoot, cmdRun: &ExecCommandRunner{}}
}

// NewNetworkCollectorWithRunner creates a NetworkCollector with a custom CommandRunner for testing.
func NewNetworkCollectorWithRunner(procRoot string, runner CommandRunner) *NetworkCollector {
	return &NetworkCollector{procRoot: procRoot, cmdRun: runner}
}

func (c *NetworkCollector) Name() string     { return "network_stats" }
func (c *NetworkCollector) Category() string { return "network" }
func (c *NetworkCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *NetworkCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()
	data := &model.NetworkData{}

	// Two-point sampling for /proc/net/dev and /proc/net/snmp to get rates
	ifaces1 := c.parseNetDev()
	snmp1 := c.parseSNMP()

	interval := cfg.SampleInterval
	if interval == 0 {
		interval = 1 * time.Second
	}
	select {
	case <-time.After(interval):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// /proc/net/dev — interface statistics (second sample)
	data.Interfaces = c.parseNetDev()

	// /proc/net/snmp — TCP protocol stats (second sample for rate)
	data.TCP = c.parseSNMP()

	// Compute retransmit rate from delta
	if snmp1 != nil && data.TCP != nil {
		retransDelta := data.TCP.RetransSegs - snmp1.RetransSegs
		if retransDelta < 0 {
			retransDelta = 0 // counter wrapped
		}
		data.TCP.RetransRate = float64(retransDelta) / interval.Seconds()
	}

	// Compute per-interface error rates from delta
	if ifaces1 != nil {
		ifaceMap := make(map[string]model.NetworkInterface, len(ifaces1))
		for _, iface := range ifaces1 {
			ifaceMap[iface.Name] = iface
		}
		for i, iface := range data.Interfaces {
			if prev, ok := ifaceMap[iface.Name]; ok {
				errDelta := (iface.RxErrors - prev.RxErrors) + (iface.TxErrors - prev.TxErrors) +
					(iface.RxDropped - prev.RxDropped) + (iface.TxDropped - prev.TxDropped)
				data.Interfaces[i].ErrorsPerSec = float64(errDelta) / interval.Seconds()
			}
		}
	}

	// ss — connection state summary
	c.parseSSConnections(ctx, data)

	// TCP sysctl tuning parameters
	data.CongestionCtrl = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_congestion_control")
	data.TCPRmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_rmem")
	data.TCPWmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_wmem")
	data.SomaxConn = readSysctlInt(c.procRoot, "sys/net/core/somaxconn")
	data.TCPMaxSynBacklog = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_max_syn_backlog")
	data.TCPTWReuse = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_tw_reuse")

	// Deep network diagnostics
	data.TCPMem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_mem")
	data.TCPMaxTwBuckets = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_max_tw_buckets")
	data.TCPKeepaliveTime = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_keepalive_time")
	data.NetdevBudget = readSysctlInt(c.procRoot, "sys/net/core/netdev_budget")

	// Conntrack table stats
	data.Conntrack = c.parseConntrack()

	// Softnet stats (per-CPU packet processing)
	data.SoftnetStats = c.parseSoftnetStat()

	// IRQ distribution (two-point delta — uses first/second sample interval)
	data.IRQDistribution = c.parseIRQDistribution(ctx, interval)

	// Extended TCP stats from /proc/net/netstat
	c.parseNetstat(data)

	// NIC hardware details (driver, queues, ring buffer, RPS, bond)
	c.enrichNICDetails(ctx, data)

	return &model.Result{
		Collector: c.Name(),
		Category:  c.Category(),
		Tier:      1,
		StartTime: start,
		EndTime:   time.Now(),
		Data:      data,
	}, nil
}

func (c *NetworkCollector) parseNetDev() []model.NetworkInterface {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "dev"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var ifaces []model.NetworkInterface
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 { // skip header lines
			continue
		}
		line := scanner.Text()
		// Format: "  iface: rx_bytes rx_packets rx_errs rx_drop ... tx_bytes tx_packets tx_errs tx_drop ..."
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}

		rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseInt(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseInt(fields[2], 10, 64)
		rxDropped, _ := strconv.ParseInt(fields[3], 10, 64)
		txBytes, _ := strconv.ParseInt(fields[8], 10, 64)
		txPackets, _ := strconv.ParseInt(fields[9], 10, 64)
		txErrors, _ := strconv.ParseInt(fields[10], 10, 64)
		txDropped, _ := strconv.ParseInt(fields[11], 10, 64)

		ifaces = append(ifaces, model.NetworkInterface{
			Name:      name,
			RxBytes:   rxBytes,
			RxPackets: rxPackets,
			RxErrors:  rxErrors,
			RxDropped: rxDropped,
			TxBytes:   txBytes,
			TxPackets: txPackets,
			TxErrors:  txErrors,
			TxDropped: txDropped,
		})
	}
	return ifaces
}

func (c *NetworkCollector) parseSNMP() *model.TCPStats {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "snmp"))
	if err != nil {
		return nil
	}
	defer f.Close()

	tcp := &model.TCPStats{}
	scanner := bufio.NewScanner(f)

	var tcpHeaders []string
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "Tcp:" {
			if tcpHeaders == nil {
				// First occurrence is the header
				tcpHeaders = fields[1:]
			} else {
				// Second occurrence is the values
				vals := fields[1:]
				for i, header := range tcpHeaders {
					if i >= len(vals) {
						break
					}
					v, _ := strconv.Atoi(vals[i])
					switch header {
					case "CurrEstab":
						tcp.CurrEstab = v
					case "ActiveOpens":
						tcp.ActiveOpens = v
					case "PassiveOpens":
						tcp.PassiveOpens = v
					case "RetransSegs":
						tcp.RetransSegs = v
					case "InErrs":
						tcp.InErrs = v
					case "OutRsts":
						tcp.OutRsts = v
					}
				}
			}
		}
	}
	return tcp
}

func (c *NetworkCollector) parseSSConnections(ctx context.Context, data *model.NetworkData) {
	// `ss -s` for summary
	out, err := c.cmdRun.Run(ctx, "ss", "-s")
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// "TCP:   1234 (estab 56, closed 78, orphaned 0, timewait 90)"
		if strings.HasPrefix(line, "TCP:") {
			// Extract timewait and closewait from parenthetical
			if idx := strings.Index(line, "timewait "); idx >= 0 {
				rest := line[idx+len("timewait "):]
				end := strings.IndexAny(rest, ",)")
				if end > 0 {
					data.TCP.TimeWaitCount, _ = strconv.Atoi(rest[:end])
				}
			}
		}
	}

	// `ss -tn state close-wait` for close-wait count
	out2, err := c.cmdRun.Run(ctx, "ss", "-tn", "state", "close-wait")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out2)), "\n")
		if len(lines) > 1 {
			data.TCP.CloseWaitCount = len(lines) - 1 // subtract header
		}
	}
}

// parseConntrack reads conntrack table usage from procfs.
func (c *NetworkCollector) parseConntrack() *model.ConntrackStats {
	count := readSysctlInt64(c.procRoot, "sys/net/netfilter/nf_conntrack_count")
	max := readSysctlInt64(c.procRoot, "sys/net/netfilter/nf_conntrack_max")
	if max == 0 {
		return nil // conntrack not loaded
	}
	return &model.ConntrackStats{
		Count:    count,
		Max:      max,
		UsagePct: float64(count) / float64(max) * 100,
	}
}

// parseSoftnetStat reads /proc/net/softnet_stat — per-CPU packet processing counters.
// Format: hex columns per line (one line per CPU): processed dropped time_squeeze ...
func (c *NetworkCollector) parseSoftnetStat() []model.SoftnetStats {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "softnet_stat"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var stats []model.SoftnetStats
	scanner := bufio.NewScanner(f)
	cpu := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		processed, _ := strconv.ParseInt(fields[0], 16, 64)
		dropped, _ := strconv.ParseInt(fields[1], 16, 64)
		timeSqueeze, _ := strconv.ParseInt(fields[2], 16, 64)
		stats = append(stats, model.SoftnetStats{
			CPU:         cpu,
			Processed:   processed,
			Dropped:     dropped,
			TimeSqueeze: timeSqueeze,
		})
		cpu++
	}
	return stats
}

// parseIRQDistribution samples /proc/softirqs NET_RX line twice to compute per-CPU delta.
func (c *NetworkCollector) parseIRQDistribution(ctx context.Context, interval time.Duration) []model.IRQDistribution {
	sample1 := c.readNetRxSoftirqs()
	if sample1 == nil {
		return nil
	}
	select {
	case <-time.After(interval):
	case <-ctx.Done():
		return nil
	}
	sample2 := c.readNetRxSoftirqs()
	if sample2 == nil || len(sample2) != len(sample1) {
		return nil
	}

	var dist []model.IRQDistribution
	for i := range sample1 {
		delta := sample2[i] - sample1[i]
		if delta < 0 {
			delta = 0
		}
		dist = append(dist, model.IRQDistribution{
			CPU:        i,
			NetRxDelta: delta,
		})
	}
	return dist
}

// readNetRxSoftirqs parses /proc/softirqs and returns NET_RX counts per CPU.
func (c *NetworkCollector) readNetRxSoftirqs() []int64 {
	f, err := os.Open(filepath.Join(c.procRoot, "softirqs"))
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "NET_RX:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil
		}
		var counts []int64
		for _, f := range fields[1:] {
			v, _ := strconv.ParseInt(f, 10, 64)
			counts = append(counts, v)
		}
		return counts
	}
	return nil
}

// parseNetstat reads /proc/net/netstat for TcpExt counters.
func (c *NetworkCollector) parseNetstat(data *model.NetworkData) {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "netstat"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024) // netstat lines can be very long
	scanner.Buffer(buf, 256*1024)

	var tcpExtHeaders []string
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "TcpExt:" {
			if tcpExtHeaders == nil {
				tcpExtHeaders = fields[1:]
			} else {
				vals := fields[1:]
				for i, header := range tcpExtHeaders {
					if i >= len(vals) {
						break
					}
					v, _ := strconv.ParseInt(vals[i], 10, 64)
					switch header {
					case "ListenOverflows":
						data.ListenOverflows = v
					case "ListenDrops":
						data.ListenDrops = v
					case "TCPAbortOnMemory":
						data.TCPAbortOnMemory = v
					case "TCPOFOQueue":
						data.TCPOFOQueue = v
					case "PruneCalled":
						data.PruneCalled = v
					}
				}
				break
			}
		}
	}
}

// enrichNICDetails adds hardware-level info to each interface.
func (c *NetworkCollector) enrichNICDetails(ctx context.Context, data *model.NetworkData) {
	for i := range data.Interfaces {
		iface := &data.Interfaces[i]
		name := iface.Name
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") {
			continue
		}

		// Queue count from sysfs
		rxQueues := countDirs(filepath.Join("/sys/class/net", name, "queues"), "rx-")
		txQueues := countDirs(filepath.Join("/sys/class/net", name, "queues"), "tx-")
		iface.RxQueues = rxQueues
		iface.TxQueues = txQueues

		// RPS status
		rpsFile := filepath.Join("/sys/class/net", name, "queues", "rx-0", "rps_cpus")
		if rpsCPUs, err := os.ReadFile(rpsFile); err == nil {
			trimmed := strings.TrimSpace(string(rpsCPUs))
			trimmed = strings.ReplaceAll(trimmed, ",", "")
			trimmed = strings.TrimLeft(trimmed, "0")
			iface.RPSEnabled = trimmed != "" && trimmed != "0"
		}

		// Bond slave detection
		masterPath := filepath.Join("/sys/class/net", name, "master")
		if info, err := os.Stat(masterPath); err == nil && info.IsDir() {
			iface.BondSlave = true
		}

		// ethtool -i (driver) — graceful fallback if not root
		if out, err := c.cmdRun.Run(ctx, "ethtool", "-i", name); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "driver:") {
					iface.Driver = strings.TrimSpace(strings.TrimPrefix(line, "driver:"))
				}
			}
		}

		// ethtool -g (ring buffer)
		if out, err := c.cmdRun.Run(ctx, "ethtool", "-g", name); err == nil {
			c.parseRingBuffer(string(out), iface)
		}

		// ethtool -S (NIC-specific stats: rx_discards, rx_buf_errors)
		if out, err := c.cmdRun.Run(ctx, "ethtool", "-S", name); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "rx_discards:") || strings.Contains(line, "rx_total_ring_discards:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						v, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
						iface.RxDiscards += v
					}
				}
				if strings.Contains(line, "rx_buf_errors:") || strings.Contains(line, "rx_total_buf_errors:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						v, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
						iface.RxBufErrors += v
					}
				}
			}
		}
	}
}

// parseRingBuffer extracts current/max RX ring buffer from ethtool -g output.
func (c *NetworkCollector) parseRingBuffer(output string, iface *model.NetworkInterface) {
	lines := strings.Split(output, "\n")
	inPreset := false
	inCurrent := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Pre-set") {
			inPreset = true
			inCurrent = false
			continue
		}
		if strings.Contains(line, "Current") {
			inCurrent = true
			inPreset = false
			continue
		}
		if strings.HasPrefix(line, "RX:") {
			val, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "RX:")))
			if inPreset {
				iface.RingRxMax = val
			} else if inCurrent {
				iface.RingRxCur = val
			}
		}
	}
}

// countDirs counts subdirectories matching a prefix in a given path.
func countDirs(basePath, prefix string) int {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			count++
		}
	}
	return count
}
