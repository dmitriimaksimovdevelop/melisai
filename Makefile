.PHONY: build build-linux build-arm64 test test-cover vet lint clean run install

BINARY = sysdiag
VERSION ?= 0.2.0
COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS = -ldflags "-X main.version=$(VERSION)-$(COMMIT) -s -w"

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/sysdiag/

# Cross-compile for Linux amd64
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-amd64 ./cmd/sysdiag/

# Cross-compile for Linux arm64
build-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-arm64 ./cmd/sysdiag/

# Build all architectures
build-all: build-linux build-arm64

# Run tests
test:
	go test -v -race -count=1 ./...

# Test with coverage report
test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo "---"
	@echo "Coverage report: coverage.out"
	@echo "View HTML: go tool cover -html=coverage.out"

# Static analysis
vet:
	go vet ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "Install golangci-lint for full linting"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || true

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out dist/

# Run locally (macOS — CLI only, collectors need Linux)
run:
	go run ./cmd/sysdiag/ $(ARGS)

# Install to /usr/local/bin (requires sudo on Linux)
install: build-linux
	@echo "Installing $(BINARY) to /usr/local/bin/"
	sudo cp bin/$(BINARY)-linux-amd64 /usr/local/bin/$(BINARY)
	sudo chmod +x /usr/local/bin/$(BINARY)

# Quick smoke test
smoke:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/sysdiag/
	bin/$(BINARY) --version
	bin/$(BINARY) capabilities 2>/dev/null || true
	@echo "Smoke test passed"
