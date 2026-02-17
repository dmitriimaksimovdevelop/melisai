package ebpf

import (
	"context"
	"fmt"
	"log"
)

// ProgramSpec describes a native eBPF program to load.
type ProgramSpec struct {
	Name       string
	Category   string
	ObjectFile string // path to compiled .o (bpf2go output)
	MapNames   []string
}

// Loader handles loading and unloading native eBPF programs.
type Loader struct {
	btfInfo *BTFInfo
	verbose bool
}

// NewLoader creates a new eBPF program loader.
func NewLoader(verbose bool) *Loader {
	return &Loader{
		btfInfo: DetectBTF(),
		verbose: verbose,
	}
}

// CanLoad returns whether the system supports native eBPF loading.
func (l *Loader) CanLoad() bool {
	return l.btfInfo.Available && l.btfInfo.CORESupport
}

// LoadError represents a BPF program load failure.
type LoadError struct {
	Program string
	Err     error
}

func (e *LoadError) Error() string {
	return fmt.Sprintf("BPF program %q: %v", e.Program, e.Err)
}

// TryLoad attempts to load a BPF program. On failure, returns an error
// that the caller should use to fall back to Tier 2.
// This is a stub — actual BPF loading requires cilium/ebpf integration
// with bpf2go-generated code that must be compiled on Linux.
func (l *Loader) TryLoad(ctx context.Context, spec *ProgramSpec) error {
	if !l.CanLoad() {
		return &LoadError{
			Program: spec.Name,
			Err:     fmt.Errorf("BTF/CO-RE not available (kernel %s)", l.btfInfo.KernelVersion),
		}
	}

	// In a real implementation, this would:
	// 1. Load the compiled BPF object
	// 2. Attach to kprobes/tracepoints
	// 3. Create perf event arrays
	//
	// For now, return an error indicating this is a stub
	// that triggers the Tier 2 fallback gracefully.
	if l.verbose {
		log.Printf("[ebpf] would load %s (BTF: %s, CO-RE: %v)",
			spec.Name, l.btfInfo.VmlinuxPath, l.btfInfo.CORESupport)
	}

	return &LoadError{
		Program: spec.Name,
		Err:     fmt.Errorf("native eBPF programs not yet compiled (Phase 3 stub)"),
	}
}

// NativePrograms returns the list of BPF programs that will be embedded.
var NativePrograms = []ProgramSpec{
	{
		Name:       "biolatency",
		Category:   "disk",
		ObjectFile: "internal/ebpf/bpf/biolatency.o",
		MapNames:   []string{"hist"},
	},
	{
		Name:       "runqlat",
		Category:   "cpu",
		ObjectFile: "internal/ebpf/bpf/runqlat.o",
		MapNames:   []string{"hist"},
	},
	{
		Name:       "tcpconnect",
		Category:   "network",
		ObjectFile: "internal/ebpf/bpf/tcpconnect.o",
		MapNames:   []string{"events"},
	},
	{
		Name:       "tcpretrans",
		Category:   "network",
		ObjectFile: "internal/ebpf/bpf/tcpretrans.o",
		MapNames:   []string{"events"},
	},
	{
		Name:       "offcputime",
		Category:   "stacktrace",
		ObjectFile: "internal/ebpf/bpf/offcputime.o",
		MapNames:   []string{"stacks", "info"},
	},
}

// FallbackDecision determines whether to use Tier 3 or fall back to Tier 2.
type FallbackDecision struct {
	UseTier3 bool
	Reason   string
}

// DecideTier checks native eBPF availability and decides which tier to use.
func DecideTier(tool string, loader *Loader) FallbackDecision {
	if !loader.CanLoad() {
		return FallbackDecision{
			UseTier3: false,
			Reason:   fmt.Sprintf("BTF unavailable for native %s, falling back to BCC", tool),
		}
	}

	// Check if we have a native program for this tool
	for _, prog := range NativePrograms {
		if prog.Name == tool {
			return FallbackDecision{
				UseTier3: true,
				Reason:   fmt.Sprintf("native eBPF available for %s", tool),
			}
		}
	}

	return FallbackDecision{
		UseTier3: false,
		Reason:   fmt.Sprintf("no native eBPF program for %s, using BCC", tool),
	}
}
