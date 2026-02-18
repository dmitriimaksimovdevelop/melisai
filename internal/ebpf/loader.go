package ebpf

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// ProgramSpec describes a native eBPF program to load.
type ProgramSpec struct {
	Name       string
	Category   string
	ObjectFile string // path to compiled .o
	MapNames   []string
	AttachTo   string // kprobe function name
	Section    string // section name in .o executable (e.g. kprobe/tcp_retransmit_skb)
}

// LoadedProgram represents a running BPF program.
type LoadedProgram struct {
	Spec       *ProgramSpec
	Collection *ebpf.Collection
	Link       link.Link
}

// Close cleans up resources.
func (p *LoadedProgram) Close() error {
	if p.Link != nil {
		p.Link.Close()
	}
	if p.Collection != nil {
		p.Collection.Close()
	}
	return nil
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

// TryLoad attempts to load a BPF program.
func (l *Loader) TryLoad(ctx context.Context, spec *ProgramSpec) (*LoadedProgram, error) {
	if !l.CanLoad() {
		return nil, &LoadError{
			Program: spec.Name,
			Err:     fmt.Errorf("BTF/CO-RE not available (kernel %s)", l.btfInfo.KernelVersion),
		}
	}

	// 1. Load the compiled BPF object
	// We assume the object file is relative to executable or CWD
	// Real implementation might search in a standard path
	path := spec.ObjectFile
	if !filepath.IsAbs(path) {
		// Just use as is, assuming running from root
	}

	collSpec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, &LoadError{Program: spec.Name, Err: fmt.Errorf("load spec: %w", err)}
	}

	// 2. Instantiate the collection (load into kernel)
	// We do this simply; genericCO-RE might need more options
	coll, err := ebpf.NewCollection(collSpec)
	if err != nil {
		return nil, &LoadError{Program: spec.Name, Err: fmt.Errorf("load collection: %w", err)}
	}

	// 3. Attach kprobe
	// Find the program
	prog := coll.Programs[spec.Section]
	if prog == nil {
		// Try to find by name if section fails
		// Iterating over Spec.Programs map might be safer
		for _, p := range coll.Programs {
			prog = p
			break // Just take the first one for now?
			// A more robust implementation would match by name/section explicitly.
		}
	}

	if prog == nil {
		coll.Close()
		return nil, &LoadError{Program: spec.Name, Err: fmt.Errorf("program not found in collection")}
	}

	kp, err := link.Kprobe(spec.AttachTo, prog, nil)
	if err != nil {
		coll.Close()
		return nil, &LoadError{Program: spec.Name, Err: fmt.Errorf("attach kprobe %s: %w", spec.AttachTo, err)}
	}

	if l.verbose {
		log.Printf("[ebpf] loaded %s (kprobe: %s)", spec.Name, spec.AttachTo)
	}

	return &LoadedProgram{
		Spec:       spec,
		Collection: coll,
		Link:       kp,
	}, nil
}

// NativePrograms defines the known programs.
var NativePrograms = []ProgramSpec{
	{
		Name:       "tcpretrans",
		Category:   "network",
		ObjectFile: "internal/ebpf/bpf/tcpretrans.o",
		MapNames:   []string{"events"},
		AttachTo:   "tcp_retransmit_skb",
		Section:    "tcp_retransmit_skb", // Usually function name for kprobes in libbpf
	},
}
