
.PHONY: all build clean generate test test-integration test-validation

all: build

build:
	go build -o melisai cmd/melisai/main.go

# Compile BPF programs
# Requires clang and llvm
# Ensure internal/ebpf/bpf directory exists
generate:
	mkdir -p internal/ebpf/bpf
	clang -g -O2 -target bpf -D__TARGET_ARCH_x86 -I/usr/include/x86_64-linux-gnu \
		-c internal/ebpf/c/tcpretrans.bpf.c -o internal/ebpf/bpf/tcpretrans.o

test:
	go test ./...

test-integration:
	bash tests/integration/docker_test.sh

test-validation:
	bash tests/validation/run_validation.sh

clean:
	rm -f melisai
	rm -f internal/ebpf/bpf/*.o
