#!/usr/bin/env bash
#
# Integration test: run sysdiag install + collect inside Docker containers
# for each supported distro.
#
# Usage:
#   bash docker_test.sh                          # build binary, test all distros
#   bash docker_test.sh --binary /path/to/sysdiag  # skip build, use existing binary
#   bash docker_test.sh --distro ubuntu:24.04      # test single distro
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DISTROS_FILE="$SCRIPT_DIR/distros.txt"
VERIFY_SCRIPT="$SCRIPT_DIR/verify_report.py"
BINARY=""
SINGLE_DISTRO=""

# --- Parse args ---
while [[ $# -gt 0 ]]; do
    case "$1" in
        --binary)  BINARY="$2"; shift 2 ;;
        --distro)  SINGLE_DISTRO="$2"; shift 2 ;;
        -h|--help)
            echo "Usage: $0 [--binary <path>] [--distro <image>]"
            exit 0 ;;
        *) echo "Unknown arg: $1"; exit 2 ;;
    esac
done

# --- Build binary if not provided ---
if [[ -z "$BINARY" ]]; then
    echo "==> Building sysdiag (linux/amd64)..."
    BINARY="/tmp/sysdiag-integration-test"
    (cd "$PROJECT_ROOT" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
        -o "$BINARY" ./cmd/sysdiag/)
    echo "    Built: $BINARY"
fi

if [[ ! -f "$BINARY" ]]; then
    echo "ERROR: binary not found: $BINARY"
    exit 2
fi

# --- Collect distros ---
if [[ -n "$SINGLE_DISTRO" ]]; then
    DISTROS=("$SINGLE_DISTRO")
else
    mapfile -t DISTROS < <(grep -v '^\s*#' "$DISTROS_FILE" | grep -v '^\s*$')
fi

echo "==> Testing ${#DISTROS[@]} distro(s): ${DISTROS[*]}"
echo ""

# --- Results tracking ---
declare -A RES_INSTALL RES_COLLECT RES_REPORT

pass() { echo "PASS"; }
FAIL() { echo "FAIL"; }

# --- Run each distro ---
for distro in "${DISTROS[@]}"; do
    echo "━━━ $distro ━━━"

    RES_INSTALL[$distro]="SKIP"
    RES_COLLECT[$distro]="SKIP"
    RES_REPORT[$distro]="SKIP"

    # Pull image
    if ! docker pull "$distro" -q >/dev/null 2>&1; then
        echo "  ERROR: docker pull failed"
        RES_INSTALL[$distro]="FAIL"
        continue
    fi

    # Build a container with sysdiag + verify script
    CONTAINER="sysdiag-test-$(echo "$distro" | tr ':/' '-')-$$"

    # Start container with host PID namespace and privileged (needed for BPF)
    docker run -d --rm --privileged \
        --pid=host \
        --name "$CONTAINER" \
        -v "$BINARY":/usr/local/bin/sysdiag:ro \
        -v "$VERIFY_SCRIPT":/tmp/verify_report.py:ro \
        "$distro" sleep 300 >/dev/null

    cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
    trap cleanup EXIT

    # --- Install ---
    echo -n "  install: "
    if docker exec "$CONTAINER" /usr/local/bin/sysdiag install >/dev/null 2>&1; then
        RES_INSTALL[$distro]="PASS"
        echo "PASS"
    else
        RES_INSTALL[$distro]="FAIL"
        echo "FAIL"
        # Show last 20 lines of output for debugging
        docker exec "$CONTAINER" /usr/local/bin/sysdiag install 2>&1 | tail -20 || true
    fi

    # --- Collect ---
    echo -n "  collect: "
    if docker exec "$CONTAINER" /usr/local/bin/sysdiag collect --profile quick -o /tmp/test.json >/dev/null 2>&1; then
        RES_COLLECT[$distro]="PASS"
        echo "PASS"
    else
        RES_COLLECT[$distro]="FAIL"
        echo "FAIL"
        docker exec "$CONTAINER" /usr/local/bin/sysdiag collect --profile quick -o /tmp/test.json 2>&1 | tail -20 || true
    fi

    # --- Report validation ---
    echo -n "  report:  "
    # Ensure python3 is available (may need to install on some distros)
    docker exec "$CONTAINER" bash -c "command -v python3 >/dev/null 2>&1 || \
        (apt-get install -y -qq python3 2>/dev/null || yum install -y python3 2>/dev/null || dnf install -y python3 2>/dev/null) >/dev/null 2>&1" || true

    if docker exec "$CONTAINER" python3 /tmp/verify_report.py /tmp/test.json 2>&1; then
        RES_REPORT[$distro]="PASS"
    else
        RES_REPORT[$distro]="FAIL"
    fi

    # Cleanup container
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    trap - EXIT
    echo ""
done

# --- Summary table ---
echo ""
echo "╔══════════════════════╦═════════╦═════════╦═════════╦════════╗"
printf "║ %-20s ║ %-7s ║ %-7s ║ %-7s ║ %-6s ║\n" "DISTRO" "INSTALL" "COLLECT" "REPORT" "STATUS"
echo "╠══════════════════════╬═════════╬═════════╬═════════╬════════╣"

ALL_PASS=true
for distro in "${DISTROS[@]}"; do
    install="${RES_INSTALL[$distro]}"
    collect="${RES_COLLECT[$distro]}"
    report="${RES_REPORT[$distro]}"

    if [[ "$install" == "PASS" && "$collect" == "PASS" && "$report" == "PASS" ]]; then
        status="OK"
    else
        status="FAIL"
        ALL_PASS=false
    fi

    printf "║ %-20s ║ %-7s ║ %-7s ║ %-7s ║ %-6s ║\n" \
        "$distro" "$install" "$collect" "$report" "$status"
done

echo "╚══════════════════════╩═════════╩═════════╩═════════╩════════╝"
echo ""

if $ALL_PASS; then
    echo "All distros passed."
    exit 0
else
    echo "Some distros FAILED."
    exit 1
fi
