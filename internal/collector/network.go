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
	sysRoot  string // /sys root for sysfs access (testable)
	cmdRun   CommandRunner
}

func NewNetworkCollector(procRoot string) *NetworkCollector {
	return &NetworkCollector{procRoot: procRoot, sysRoot: "/sys", cmdRun: &ExecCommandRunner{}}
}

// NewNetworkCollectorWithRunner creates a NetworkCollector with a custom CommandRunner for testing.
func NewNetworkCollectorWithRunner(procRoot string, runner CommandRunner) *NetworkCollector {
	return &NetworkCollector{procRoot: procRoot, sysRoot: "/sys", cmdRun: runner}
}

// NewNetworkCollectorFull creates a NetworkCollector with all parameters for testing.
func NewNetworkCollectorFull(procRoot, sysRoot string, runner CommandRunner) *NetworkCollector {
	return &NetworkCollector{procRoot: procRoot, sysRoot: sysRoot, cmdRun: runner}
}

func (c *NetworkCollector) Name() string     { return "network_stats" }
func (c *NetworkCollector) Category() string { return "network" }
func (c *NetworkCollector) Available() Availability {
	return Availability{Tier: 1}
}

func (c *NetworkCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	start := time.Now()
	data := &model.NetworkData{}

	// Two-point sampling for rates: first sample before interval
	ifaces1 := c.parseNetDev()
	snmp1 := &model.NetworkData{}
	c.parseSNMP(snmp1)
	irqSample1 := c.readNetRxSoftirqs()
	softnet1 := c.parseSoftnetStat()
	netstat1 := &model.NetworkData{}
	c.parseNetstat(netstat1)

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

	// /proc/net/snmp — TCP + UDP protocol stats (second sample for rate)
	c.parseSNMP(data)

	// Compute retransmit rate from delta
	if snmp1.TCP != nil && data.TCP != nil {
		retransDelta := data.TCP.RetransSegs - snmp1.TCP.RetransSegs
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

	// Initialize sub-structs
	data.Sysctls = &model.NetworkSysctls{}
	data.TCPExt = &model.TCPExtendedStats{}
	data.UDP = &model.UDPStats{}
	data.Softnet = &model.SoftnetData{}
	data.SocketMem = &model.SocketMemStats{}

	// TCP/network sysctl tuning parameters
	sc := data.Sysctls
	sc.CongestionCtrl = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_congestion_control")
	sc.TCPRmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_rmem")
	sc.TCPWmem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_wmem")
	sc.SomaxConn = readSysctlInt(c.procRoot, "sys/net/core/somaxconn")
	sc.TCPMaxSynBacklog = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_max_syn_backlog")
	sc.TCPTWReuse = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_tw_reuse")
	sc.TCPMem = readSysctlString(c.procRoot, "sys/net/ipv4/tcp_mem")
	sc.TCPMaxTwBuckets = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_max_tw_buckets")
	sc.TCPKeepaliveTime = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_keepalive_time")
	sc.TCPKeepaliveIntvl = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_keepalive_intvl")
	sc.TCPKeepaliveProbes = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_keepalive_probes")
	sc.NetdevBudget = readSysctlInt(c.procRoot, "sys/net/core/netdev_budget")
	sc.NetdevBudgetUsecs = readSysctlInt(c.procRoot, "sys/net/core/netdev_budget_usecs")
	sc.NetdevMaxBacklog = readSysctlInt(c.procRoot, "sys/net/core/netdev_max_backlog")
	sc.RmemMax = readSysctlInt(c.procRoot, "sys/net/core/rmem_max")
	sc.WmemMax = readSysctlInt(c.procRoot, "sys/net/core/wmem_max")
	sc.IPLocalPortRange = readSysctlString(c.procRoot, "sys/net/ipv4/ip_local_port_range")
	sc.TCPFinTimeout = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_fin_timeout")
	sc.TCPSlowStartAfterIdle = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_slow_start_after_idle")
	sc.TCPFastOpen = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_fastopen")
	sc.TCPSyncookies = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_syncookies")
	sc.TCPNotsentLowat = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_notsent_lowat")
	sc.DefaultQdisc = readSysctlString(c.procRoot, "sys/net/core/default_qdisc")
	sc.TCPMtuProbing = readSysctlInt(c.procRoot, "sys/net/ipv4/tcp_mtu_probing")
	sc.ARPGcThresh1 = readSysctlInt(c.procRoot, "sys/net/ipv4/neigh/default/gc_thresh1")
	sc.ARPGcThresh2 = readSysctlInt(c.procRoot, "sys/net/ipv4/neigh/default/gc_thresh2")
	sc.ARPGcThresh3 = readSysctlInt(c.procRoot, "sys/net/ipv4/neigh/default/gc_thresh3")

	// Conntrack table stats
	data.Conntrack = c.parseConntrack()

	// Softnet stats (per-CPU packet processing)
	data.Softnet.Stats = c.parseSoftnetStat()

	// IRQ distribution (two-point delta — reuse pre/post interval samples)
	data.Softnet.IRQDistribution = c.computeIRQDistribution(irqSample1)

	// Extended TCP stats from /proc/net/netstat
	c.parseNetstat(data)

	// Compute rate fields from two-point deltas
	secs := interval.Seconds()
	ext := data.TCPExt
	if secs > 0 {
		// Softnet drop/squeeze rates
		if softnet1 != nil && len(data.Softnet.Stats) == len(softnet1) {
			var dropDelta, squeezeDelta int64
			for i := range softnet1 {
				dd := data.Softnet.Stats[i].Dropped - softnet1[i].Dropped
				sd := data.Softnet.Stats[i].TimeSqueeze - softnet1[i].TimeSqueeze
				if dd > 0 {
					dropDelta += dd
				}
				if sd > 0 {
					squeezeDelta += sd
				}
			}
			data.Softnet.DropRate = float64(dropDelta) / secs
			data.Softnet.SqueezeRate = float64(squeezeDelta) / secs
		}
		// TCP extended counter rates
		if netstat1.TCPExt != nil {
			n1 := netstat1.TCPExt
			if n1.ListenOverflows > 0 || ext.ListenOverflows > 0 {
				d := ext.ListenOverflows - n1.ListenOverflows
				if d > 0 {
					ext.ListenOverflowRate = float64(d) / secs
				}
			}
			if n1.TCPAbortOnMemory > 0 || ext.TCPAbortOnMemory > 0 {
				d := ext.TCPAbortOnMemory - n1.TCPAbortOnMemory
				if d > 0 {
					ext.TCPAbortMemRate = float64(d) / secs
				}
			}
			if ext.TCPRcvQDrop > n1.TCPRcvQDrop {
				ext.TCPRcvQDropRate = float64(ext.TCPRcvQDrop-n1.TCPRcvQDrop) / secs
			}
			if ext.TCPZeroWindowDrop > n1.TCPZeroWindowDrop {
				ext.TCPZeroWindowDropRate = float64(ext.TCPZeroWindowDrop-n1.TCPZeroWindowDrop) / secs
			}
		}
		// UDP rcvbuf error rate
		if snmp1.UDP != nil && data.UDP != nil {
			if snmp1.UDP.RcvbufErrors > 0 || data.UDP.RcvbufErrors > 0 {
				d := data.UDP.RcvbufErrors - snmp1.UDP.RcvbufErrors
				if d > 0 {
					data.UDP.RcvbufErrRate = float64(d) / secs
				}
			}
		}
	}

	// Listen queue depths + ESTABLISHED Recv-Q saturation
	c.parseListenQueues(ctx, data)

	// Socket memory and orphan info from /proc/net/sockstat
	c.parseSockstat(data)

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

// parseSNMP reads both TCP and UDP stats from /proc/net/snmp in a single pass.
func (c *NetworkCollector) parseSNMP(data *model.NetworkData) {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "snmp"))
	if err != nil {
		return
	}
	defer f.Close()

	tcp := &model.TCPStats{}
	scanner := bufio.NewScanner(f)

	var tcpHeaders, udpHeaders []string
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "Tcp:":
			if tcpHeaders == nil {
				tcpHeaders = fields[1:]
			} else {
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
		case "Udp:":
			if udpHeaders == nil {
				udpHeaders = fields[1:]
			} else {
				if data.UDP == nil {
					data.UDP = &model.UDPStats{}
				}
				vals := fields[1:]
				for i, header := range udpHeaders {
					if i >= len(vals) {
						break
					}
					v, _ := strconv.ParseInt(vals[i], 10, 64)
					switch header {
					case "RcvbufErrors":
						data.UDP.RcvbufErrors = v
					case "SndbufErrors":
						data.UDP.SndbufErrors = v
					case "InErrors":
						data.UDP.InErrors = v
					}
				}
			}
		}
	}
	data.TCP = tcp
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
		Count:        count,
		Max:          max,
		UsagePct:     float64(count) / float64(max) * 100,
		Drops:        readSysctlInt64(c.procRoot, "sys/net/netfilter/nf_conntrack_drop"),
		InsertFailed: readSysctlInt64(c.procRoot, "sys/net/netfilter/nf_conntrack_insert_failed"),
		EarlyDrop:    readSysctlInt64(c.procRoot, "sys/net/netfilter/nf_conntrack_early_drop"),
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

// computeIRQDistribution computes per-CPU NET_RX softirq delta from pre/post interval samples.
// The first sample is taken before the interval sleep in Collect(), the second after.
func (c *NetworkCollector) computeIRQDistribution(sample1 []int64) []model.IRQDistribution {
	if sample1 == nil {
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
					if data.TCPExt == nil {
						data.TCPExt = &model.TCPExtendedStats{}
					}
					ext := data.TCPExt
					switch header {
					case "ListenOverflows":
						ext.ListenOverflows = v
					case "ListenDrops":
						ext.ListenDrops = v
					case "TCPAbortOnMemory":
						ext.TCPAbortOnMemory = v
					case "TCPOFOQueue":
						ext.TCPOFOQueue = v
					case "PruneCalled":
						ext.PruneCalled = v
					case "TCPRcvQDrop":
						ext.TCPRcvQDrop = v
					case "TCPZeroWindowDrop":
						ext.TCPZeroWindowDrop = v
					case "TCPToZeroWindowAdv":
						ext.TCPToZeroWindowAdv = v
					case "TCPFromZeroWindowAdv":
						ext.TCPFromZeroWindowAdv = v
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

		sysNetDir := filepath.Join(c.sysRoot, "class", "net", name)

		// Speed from sysfs (e.g., "1000" for 1Gbps)
		if speedBytes, err := os.ReadFile(filepath.Join(sysNetDir, "speed")); err == nil {
			trimmed := strings.TrimSpace(string(speedBytes))
			if trimmed != "" && trimmed != "-1" {
				iface.Speed = trimmed + "Mbps"
			}
		}

		// MTU from sysfs
		if mtuBytes, err := os.ReadFile(filepath.Join(sysNetDir, "mtu")); err == nil {
			iface.MTU, _ = strconv.Atoi(strings.TrimSpace(string(mtuBytes)))
		}

		// Queue count from sysfs
		rxQueues := countDirs(filepath.Join(sysNetDir, "queues"), "rx-")
		txQueues := countDirs(filepath.Join(sysNetDir, "queues"), "tx-")
		iface.RxQueues = rxQueues
		iface.TxQueues = txQueues

		// RPS status
		rpsFile := filepath.Join(sysNetDir, "queues", "rx-0", "rps_cpus")
		if rpsCPUs, err := os.ReadFile(rpsFile); err == nil {
			trimmed := strings.TrimSpace(string(rpsCPUs))
			trimmed = strings.ReplaceAll(trimmed, ",", "")
			trimmed = strings.TrimLeft(trimmed, "0")
			iface.RPSEnabled = trimmed != "" && trimmed != "0"
		}

		// Bond slave detection
		masterPath := filepath.Join(sysNetDir, "master")
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

		// XPS status (tx queue 0)
		xpsFile := filepath.Join(sysNetDir, "queues", "tx-0", "xps_cpus")
		if xpsCPUs, err := os.ReadFile(xpsFile); err == nil {
			trimmed := strings.TrimSpace(string(xpsCPUs))
			trimmed = strings.ReplaceAll(trimmed, ",", "")
			trimmed = strings.TrimLeft(trimmed, "0")
			iface.XPSEnabled = trimmed != "" && trimmed != "0"
		}

		// ethtool -k (offload features)
		if out, err := c.cmdRun.Run(ctx, "ethtool", "-k", name); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "tcp-segmentation-offload:") {
					iface.TSOEnabled = strings.Contains(line, " on")
				}
				if strings.HasPrefix(line, "generic-receive-offload:") {
					iface.GROEnabled = strings.Contains(line, " on")
				}
				if strings.HasPrefix(line, "generic-segmentation-offload:") {
					iface.GSOEnabled = strings.Contains(line, " on")
				}
			}
		}

		// ethtool -c (interrupt coalescing)
		if out, err := c.cmdRun.Run(ctx, "ethtool", "-c", name); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "rx-usecs:") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						iface.CoalesceRxUsecs, _ = strconv.Atoi(parts[1])
					}
				}
				if strings.HasPrefix(line, "Adaptive RX:") {
					iface.CoalesceAdaptRx = strings.Contains(line, " on")
				}
			}
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

// parseListenQueues collects accept queue depths and ESTABLISHED recv-Q saturation.
func (c *NetworkCollector) parseListenQueues(ctx context.Context, data *model.NetworkData) {
	// ss -tnl: LISTEN sockets with Recv-Q (current queue) and Send-Q (backlog)
	out, err := c.cmdRun.Run(ctx, "ss", "-tnl")
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			// State Recv-Q Send-Q Local-Address:Port Peer-Address:Port
			if len(fields) < 5 || fields[0] != "LISTEN" {
				continue
			}
			recvQ, _ := strconv.Atoi(fields[1])
			sendQ, _ := strconv.Atoi(fields[2])
			if sendQ > 0 {
				fillPct := float64(recvQ) / float64(sendQ) * 100
				if fillPct >= 50 { // only report sockets with >= 50% fill
					data.ListenSockets = append(data.ListenSockets, model.ListenSocket{
						LocalAddr: fields[3],
						RecvQ:     recvQ,
						SendQ:     sendQ,
						FillPct:   fillPct,
					})
				}
			}
		}
	}

	// ss -tn state established: count sockets with non-zero Recv-Q
	// Non-zero Recv-Q on ESTABLISHED = app not reading fast enough
	out2, err := c.cmdRun.Run(ctx, "ss", "-tn", "state", "established")
	if err == nil {
		for _, line := range strings.Split(string(out2), "\n") {
			fields := strings.Fields(line)
			// Recv-Q Send-Q Local-Address:Port Peer-Address:Port
			if len(fields) < 4 {
				continue
			}
			recvQ, _ := strconv.Atoi(fields[0])
			if recvQ > 65536 { // > 64KB queued = significant backpressure
				data.RecvQSaturated++
			}
		}
	}
}

// parseSockstat reads /proc/net/sockstat for socket memory and orphan counts.
func (c *NetworkCollector) parseSockstat(data *model.NetworkData) {
	f, err := os.Open(filepath.Join(c.procRoot, "net", "sockstat"))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Format: "TCP: inuse 123 orphan 4 tw 567 alloc 890 mem 12"
		if fields[0] == "TCP:" {
			if data.SocketMem == nil {
				data.SocketMem = &model.SocketMemStats{}
			}
			for i := 1; i+1 < len(fields); i += 2 {
				v, _ := strconv.Atoi(fields[i+1])
				switch fields[i] {
				case "inuse":
					data.SocketMem.TCPInUse = v
				case "orphan":
					data.SocketMem.TCPOrphans = v
				case "mem":
					data.SocketMem.TCPMemPages = v
				}
			}
		}
		if fields[0] == "UDP:" {
			if data.SocketMem == nil {
				data.SocketMem = &model.SocketMemStats{}
			}
			for i := 1; i+1 < len(fields); i += 2 {
				v, _ := strconv.Atoi(fields[i+1])
				if fields[i] == "inuse" {
					data.SocketMem.UDPInUse = v
				}
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
