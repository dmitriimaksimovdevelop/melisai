package ebpf

import "embed"

// bpfFS contains compiled eBPF object files embedded at build time via
// `make generate` (which produces internal/ebpf/bpf/*.o). When a fresh
// checkout is built without running `make generate` first, the FS holds
// only the .gitkeep placeholder and LoadEmbeddedObject returns nil — in
// that case, native Tier 3 collectors fall back to the disk path encoded
// in ProgramSpec.ObjectFile.
//
// The all: prefix is required so the hidden .gitkeep file is embedded,
// which lets //go:embed succeed on clean checkouts where no .o files
// have been generated yet.
//
//go:embed all:bpf
var bpfFS embed.FS

// LoadEmbeddedObject returns the embedded bytes for a compiled BPF object
// file (e.g. "tcpretrans.o"), or nil if the file is absent or empty.
// Callers should treat a nil return as "not embedded" and try a disk-based
// fallback.
func LoadEmbeddedObject(name string) []byte {
	data, err := bpfFS.ReadFile("bpf/" + name)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}
