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
