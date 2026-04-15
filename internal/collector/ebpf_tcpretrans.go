package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cilium/ebpf/perf"
	"github.com/dmitriimaksimovdevelop/melisai/internal/ebpf"
	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// TcpretransEvent must match the C struct in internal/ebpf/c/tcpretrans.bpf.c.
// Layout: pid(4) + saddr(4) + daddr(4) + lport(2) + dport(2) + state(4) + type(1) + pad(3) + comm(16) = 40 bytes
type TcpretransEvent struct {
	Pid   uint32
	Saddr uint32
	Daddr uint32
	Lport uint16
	Dport uint16
	State uint32
	Type  uint8
	_     [3]byte // padding
	Comm  [16]byte
}

// tcpretransEventMinSize is the minimum expected size of a raw perf event sample.
const tcpretransEventMinSize = 28 // pid + saddr + daddr + lport + dport + state + type

type NativeTcpretransCollector struct {
	loader *ebpf.Loader
}

func NewNativeTcpretransCollector(loader *ebpf.Loader) *NativeTcpretransCollector {
	return &NativeTcpretransCollector{loader: loader}
}

func (c *NativeTcpretransCollector) Name() string {
	return "tcpretrans"
}

func (c *NativeTcpretransCollector) Category() string {
	return "network"
}

func (c *NativeTcpretransCollector) Available() Availability {
	if !c.loader.CanLoad() {
		return Availability{Tier: 0, Reason: "BTF/CO-RE unavailable"}
	}
	// Locate the BPF object — prefer embedded (release builds), fall back to
	// the disk path for local dev builds produced by `make generate`.
	for _, s := range ebpf.NativePrograms {
		if s.Name == "tcpretrans" {
			if len(ebpf.LoadEmbeddedObject(filepath.Base(s.ObjectFile))) > 0 {
				return Availability{Tier: 3}
			}
			if _, err := os.Stat(s.ObjectFile); err == nil {
				return Availability{Tier: 3}
			}
			return Availability{Tier: 0, Reason: "BPF object not embedded and not found on disk: " + s.ObjectFile}
		}
	}
	return Availability{Tier: 0, Reason: "tcpretrans program spec not found"}
}

func (c *NativeTcpretransCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
	// Find spec
	var spec *ebpf.ProgramSpec
	for _, s := range ebpf.NativePrograms {
		if s.Name == "tcpretrans" {
			spec = &s
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("program spec not found")
	}

	// Load program
	// Note: In a real long-running agent, loaded programs might be cached/persistent.
	// Here we load/unload per collection for simplicity.
	prog, err := c.loader.TryLoad(ctx, spec)
	if err != nil {
		return nil, err
	}
	defer prog.Close()

	// Open perf buffer
	eventsMap := prog.Collection.Maps["events"]
	if eventsMap == nil {
		return nil, fmt.Errorf("map 'events' not found")
	}

	rd, err := perf.NewReader(eventsMap, 4096)
	if err != nil {
		return nil, fmt.Errorf("creating perf reader: %w", err)
	}
	defer rd.Close()

	// Collect events
	var events []model.Event
	start := time.Now()

	// Read Loop
	go func() {
		<-ctx.Done()
		rd.Close() // Break the read loop
	}()

	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				break
			}
			continue
		}

		// Parse event
		if len(record.RawSample) < tcpretransEventMinSize {
			continue
		}

		// Manual binary parsing — avoids reflection-heavy binary.Read and
		// per-event bytes.NewReader allocation.
		d := record.RawSample
		pid := binary.LittleEndian.Uint32(d[0:4])
		saddr := binary.LittleEndian.Uint32(d[4:8])
		daddr := binary.LittleEndian.Uint32(d[8:12])
		lport := binary.LittleEndian.Uint16(d[12:14])
		dport := binary.LittleEndian.Uint16(d[14:16])
		state := binary.LittleEndian.Uint32(d[16:20])

		// Comm field starts at offset 24 (after type(1) + pad(3))
		var comm string
		if len(d) >= 40 {
			comm = string(bytes.TrimRight(d[24:40], "\x00"))
		}

		evt := model.Event{
			Time: time.Now().Format("15:04:05"),
			Comm: comm,
			PID:  int(pid),
			Details: map[string]interface{}{
				"laddr": fmt.Sprintf("%d.%d.%d.%d", byte(saddr), byte(saddr>>8), byte(saddr>>16), byte(saddr>>24)),
				"daddr": fmt.Sprintf("%d.%d.%d.%d", byte(daddr), byte(daddr>>8), byte(daddr>>16), byte(daddr>>24)),
				"lport": lport,
				"dport": dport,
				"state": state,
			},
		}
		events = append(events, evt)

		if len(events) >= cfg.MaxEventsPerCollector {
			break
		}
	}

	return &model.Result{
		Collector: "tcpretrans",
		Category:  "network",
		Tier:      3,
		Events:    events,
		StartTime: start,
		EndTime:   time.Now(),
	}, nil
}
