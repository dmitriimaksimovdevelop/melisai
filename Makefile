
.PHONY: all build clean generate test test-integration test-validation lint check

all: build

build:
	go build -o melisai cmd/melisai/main.go

# Compile BPF programs into internal/ebpf/bpf/*.o for embedding via //go:embed.
# Requires (on the host running this target):
#   - clang, llvm        (apt: clang llvm)
#   - bpftool            (apt: linux-tools-generic, then ln -s to /usr/local/bin)
#   - libbpf headers     (apt: libbpf-dev)
#   - kernel BTF         (/sys/kernel/btf/vmlinux — kernel built with CONFIG_DEBUG_INFO_BTF=y)
#
# The generated vmlinux.h is host-kernel specific but the resulting .o is CO-RE
# (BPF Type Format-relocatable) and will run on any kernel with BTF support.
# macOS/dev machines without these tools should skip `make generate` — the
# build will then rely on the on-disk fallback in loader.go.
generate:
	@command -v clang >/dev/null || { echo "ERROR: clang not found (install clang/llvm)"; exit 1; }
	@command -v bpftool >/dev/null || { echo "ERROR: bpftool not found (install linux-tools-generic)"; exit 1; }
	@test -f /sys/kernel/btf/vmlinux || { echo "ERROR: /sys/kernel/btf/vmlinux not present (kernel needs CONFIG_DEBUG_INFO_BTF=y)"; exit 1; }
	mkdir -p internal/ebpf/bpf
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > internal/ebpf/c/vmlinux.h
	clang -g -O2 -target bpf -D__TARGET_ARCH_x86 \
		-I/usr/include/x86_64-linux-gnu \
		-I internal/ebpf/c \
		-c internal/ebpf/c/tcpretrans.bpf.c \
		-o internal/ebpf/bpf/tcpretrans.o

test:
	go test -race -count=1 -timeout 120s ./...

lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "Installing golangci-lint..."; go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; }
	golangci-lint run --timeout=5m

check: test lint
	@echo "All checks passed."

test-integration:
	bash tests/integration/docker_test.sh

test-validation:
	bash tests/validation/run_validation.sh

clean:
	rm -f melisai
	rm -f internal/ebpf/bpf/*.o
	rm -f internal/ebpf/c/vmlinux.h
