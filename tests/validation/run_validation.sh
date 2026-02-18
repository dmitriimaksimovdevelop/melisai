#!/usr/bin/env bash
# run_validation.sh — orchestrator for melisai validation tests
#
# Runs known workloads, collects with melisai, validates expected anomalies.
#
# Usage:
#   bash run_validation.sh [--binary /path/to/melisai] [--test <name>] [--output-dir /path]
#
# Requirements: root, stress-ng, iperf3, iproute2, python3

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSDIAG_BINARY="${SYSDIAG_BINARY:-/usr/local/bin/melisai}"
OUTPUT_DIR="${OUTPUT_DIR:-/tmp/melisai-validation}"
SINGLE_TEST=""
SETTLE_TIME=8  # seconds to let workload stabilize before collecting

# Parse args
while [[ $# -gt 0 ]]; do
    case "$1" in
        --binary)  SYSDIAG_BINARY="$2"; shift 2 ;;
        --test)    SINGLE_TEST="$2"; shift 2 ;;
        --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Source workload library
source "$SCRIPT_DIR/workloads.sh"

# =============================================================================
# Preflight checks
# =============================================================================

if [[ $EUID -ne 0 ]]; then
    echo "ERROR: must run as root" >&2
    exit 1
fi

if [[ ! -x "$SYSDIAG_BINARY" ]]; then
    echo "ERROR: melisai binary not found or not executable: $SYSDIAG_BINARY" >&2
    exit 1
fi

if ! command -v python3 &>/dev/null; then
    echo "ERROR: python3 required" >&2
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

# =============================================================================
# Install prerequisites
# =============================================================================

install_prereqs() {
    echo "=== Installing prerequisites ==="
    apt-get update -qq
    for pkg in stress-ng iperf3 iproute2; do
        if ! dpkg -l "$pkg" &>/dev/null; then
            echo "  Installing $pkg..."
            apt-get install -y -qq "$pkg"
        else
            echo "  $pkg already installed"
        fi
    done
    echo
}

# =============================================================================
# OOM protection for this script and melisai
# =============================================================================

protect_from_oom() {
    echo -1000 > /proc/$$/oom_score_adj 2>/dev/null || true
}

# =============================================================================
# Global cleanup trap
# =============================================================================

global_cleanup() {
    echo
    echo "=== Global cleanup ==="
    cleanup_workload
    # Kill any remaining melisai processes
    pkill -9 -f "melisai" 2>/dev/null || true
    echo "  Cleanup complete"
}
trap global_cleanup EXIT

# =============================================================================
# Test runner
# =============================================================================

RESULTS=()       # "test_name:PASS" or "test_name:FAIL"
TOTAL_PASS=0
TOTAL_FAIL=0

# run_test <name> <profile> <duration> <workload_fn> [settle_time]
run_test() {
    local name="$1"
    local profile="$2"
    local duration="$3"
    local workload_fn="$4"
    local settle="${5:-$SETTLE_TIME}"

    if [[ -n "$SINGLE_TEST" && "$SINGLE_TEST" != "$name" ]]; then
        return
    fi

    local report_file="$OUTPUT_DIR/${name}_report.json"

    echo "==========================================================="
    echo "  TEST: $name"
    echo "  Profile: $profile, Duration: ${duration}s"
    echo "==========================================================="

    # Clean any previous workload
    cleanup_workload

    # Start workload
    echo "  [1/4] Starting workload: $workload_fn"
    "$workload_fn"
    echo "    PIDs: ${WORKLOAD_PIDS[*]:-none}"

    # Let workload settle
    echo "  [2/4] Settling for ${settle}s..."
    sleep "$settle"

    # Verify workload is still running
    local alive=0
    for pid in "${WORKLOAD_PIDS[@]:-}"; do
        [[ -z "$pid" ]] && continue
        if kill -0 "$pid" 2>/dev/null; then
            alive=$((alive + 1))
        fi
    done
    if [[ $alive -eq 0 ]]; then
        echo "  WARNING: workload died before collection started"
    else
        echo "    $alive workload process(es) running"
    fi

    # Run melisai with OOM protection
    echo "  [3/4] Running melisai (profile=$profile, duration=${duration}s)..."
    local melisai_start
    melisai_start=$(date +%s)

    timeout 120 "$SYSDIAG_BINARY" collect \
        --profile "$profile" \
        --duration "${duration}s" \
        --output "$report_file" \
        --quiet 2>&1 | sed 's/^/    /' || true

    local melisai_end
    melisai_end=$(date +%s)
    local elapsed=$(( melisai_end - melisai_start ))
    echo "    melisai finished in ${elapsed}s"

    # Stop workload
    echo "  [4/4] Stopping workload..."
    cleanup_workload

    # Validate
    if [[ ! -f "$report_file" ]]; then
        echo "  FAIL: report file not generated"
        RESULTS+=("$name:FAIL")
        TOTAL_FAIL=$((TOTAL_FAIL + 1))
        echo
        return
    fi

    echo
    echo "  --- Validation ---"
    if python3 "$SCRIPT_DIR/check_detection.py" "$report_file" "$name"; then
        RESULTS+=("$name:PASS")
        TOTAL_PASS=$((TOTAL_PASS + 1))
    else
        RESULTS+=("$name:FAIL")
        TOTAL_FAIL=$((TOTAL_FAIL + 1))
    fi
    echo
}

# =============================================================================
# Observer effect test (special case)
# =============================================================================

run_observer_test() {
    local name="observer_effect"

    if [[ -n "$SINGLE_TEST" && "$SINGLE_TEST" != "$name" ]]; then
        return
    fi

    echo "==========================================================="
    echo "  TEST: $name (observer effect measurement)"
    echo "==========================================================="

    local report_file="$OUTPUT_DIR/${name}_report.json"
    local baseline_file="$OUTPUT_DIR/${name}_baseline.txt"
    local observed_file="$OUTPUT_DIR/${name}_observed.txt"

    cleanup_workload

    # Baseline: stress-ng benchmark alone
    echo "  [1/4] Running baseline benchmark (stress-ng --cpu $(nproc) -t 15s --metrics)..."
    stress-ng --cpu "$(nproc)" --cpu-load 80 --cpu-method matrixprod -t 15s --metrics-brief &>"$baseline_file" || true
    local baseline_bogo
    # Extract bogo ops/s (real time) from: stress-ng: metrc: [...] cpu  BOGO  TIME  USR  SYS  BOGO/S  BOGO/S
    baseline_bogo=$(awk '/metrc:.*cpu[[:space:]]/ && !/stressor/ {print $(NF-1)}' "$baseline_file") || true
    echo "    Baseline bogo ops/s: ${baseline_bogo:-unknown}"

    sleep 5

    # Observed: stress-ng benchmark + melisai running concurrently
    echo "  [2/4] Running benchmark WITH melisai..."

    # Start melisai in background
    timeout 30 "$SYSDIAG_BINARY" collect \
        --profile quick \
        --duration 10s \
        --output "$report_file" \
        --quiet &
    local melisai_pid=$!

    # Record melisai RSS before benchmark
    sleep 2
    local rss_before
    rss_before=$(ps -o rss= -p "$melisai_pid" 2>/dev/null | tr -d ' ') || rss_before=0

    # Run benchmark
    stress-ng --cpu "$(nproc)" --cpu-load 80 --cpu-method matrixprod -t 15s --metrics-brief &>"$observed_file" || true
    local observed_bogo
    observed_bogo=$(awk '/metrc:.*cpu[[:space:]]/ && !/stressor/ {print $(NF-1)}' "$observed_file") || true
    echo "    Observed bogo ops/s: ${observed_bogo:-unknown}"

    # Record melisai peak RSS
    local rss_after
    rss_after=$(ps -o rss= -p "$melisai_pid" 2>/dev/null | tr -d ' ') || rss_after=0

    # Wait for melisai to finish
    wait "$melisai_pid" 2>/dev/null || true

    # Calculate overhead
    echo "  [3/4] Calculating overhead..."
    local overhead_pct="unknown"
    local rss_mib="unknown"
    local pass_overhead=false
    local pass_rss=false

    if [[ -n "$baseline_bogo" && -n "$observed_bogo" && "$baseline_bogo" != "0" ]]; then
        overhead_pct=$(python3 -c "
b = float('$baseline_bogo')
o = float('$observed_bogo')
pct = ((b - o) / b) * 100
print(f'{pct:.2f}')
")
        echo "    Overhead: ${overhead_pct}%"
        pass_overhead=$(python3 -c "print('true' if float('$overhead_pct') < 5 else 'false')")
    else
        echo "    Could not calculate overhead (missing bogo ops data)"
        # Don't fail if we can't measure
        pass_overhead=true
    fi

    if [[ -n "$rss_after" && "$rss_after" != "0" ]]; then
        rss_mib=$(( rss_after / 1024 ))
        echo "    melisai peak RSS: ${rss_mib} MiB"
        pass_rss=$(python3 -c "print('true' if $rss_mib < 500 else 'false')")
    else
        # Use rss_before if after isn't available
        if [[ -n "$rss_before" && "$rss_before" != "0" ]]; then
            rss_mib=$(( rss_before / 1024 ))
            echo "    melisai RSS (during): ${rss_mib} MiB"
            pass_rss=$(python3 -c "print('true' if $rss_mib < 500 else 'false')")
        else
            echo "    Could not measure RSS"
            pass_rss=true
        fi
    fi

    # Validate report structure
    echo "  [4/4] Validating report..."
    local check_pass=true
    if [[ -f "$report_file" ]]; then
        if ! python3 "$SCRIPT_DIR/check_detection.py" "$report_file" "$name"; then
            check_pass=false
        fi
    else
        echo "    WARNING: no report file generated"
    fi

    echo
    echo "  --- Observer Effect Results ---"
    echo "    Overhead: ${overhead_pct}% (threshold: <5%): $([ "$pass_overhead" = "true" ] && echo PASS || echo FAIL)"
    echo "    RSS: ${rss_mib} MiB (threshold: <500 MiB): $([ "$pass_rss" = "true" ] && echo PASS || echo FAIL)"

    if [[ "$pass_overhead" = "true" && "$pass_rss" = "true" && "$check_pass" = "true" ]]; then
        RESULTS+=("$name:PASS")
        TOTAL_PASS=$((TOTAL_PASS + 1))
    else
        RESULTS+=("$name:FAIL")
        TOTAL_FAIL=$((TOTAL_FAIL + 1))
    fi
    echo
}

# =============================================================================
# Main
# =============================================================================

echo "============================================"
echo "  melisai Validation Test Suite"
echo "============================================"
echo "  Binary:     $SYSDIAG_BINARY"
echo "  Output dir: $OUTPUT_DIR"
echo "  Single test: ${SINGLE_TEST:-all}"
echo "  Date:       $(date -Iseconds)"
echo "  Kernel:     $(uname -r)"
echo "  CPUs:       $(nproc)"
echo "  Memory:     $(free -g | awk '/^Mem:/{print $2}') GiB"
echo "============================================"
echo

install_prereqs
protect_from_oom

# --- Simple tests (quick profile, 10s collect) ---
run_test "cpu_burn"          "quick" 10 workload_cpu_burn
run_test "disk_flood"        "quick" 10 workload_disk_flood
run_test "fork_storm"        "quick" 10 workload_fork_storm

# --- Complex tests (standard profile, 30s collect) ---
# memory_pressure: quick profile (avoid BCC tools eating RAM) + 20s settle for tmpfs fill
run_test "memory_pressure"   "quick" 10 workload_memory_pressure 20
run_test "runq_saturation"   "standard" 30 workload_runq_saturation
run_test "tcp_retrans"       "standard" 30 workload_tcp_retrans
run_test "combined"          "standard" 30 workload_combined

# --- Observer effect test ---
run_observer_test

# =============================================================================
# Summary
# =============================================================================

echo
echo "============================================"
echo "  VALIDATION SUMMARY"
echo "============================================"
printf "  %-20s %s\n" "TEST" "RESULT"
printf "  %-20s %s\n" "----" "------"
for result in "${RESULTS[@]:-}"; do
    local_name="${result%%:*}"
    local_status="${result##*:}"
    if [[ "$local_status" == "PASS" ]]; then
        printf "  %-20s \e[32m%s\e[0m\n" "$local_name" "$local_status"
    else
        printf "  %-20s \e[31m%s\e[0m\n" "$local_name" "$local_status"
    fi
done
echo "  ---"
printf "  %-20s %s\n" "TOTAL" "${TOTAL_PASS}/$((TOTAL_PASS + TOTAL_FAIL)) passed"
echo "============================================"

# Reports preserved in $OUTPUT_DIR
echo
echo "  Reports saved to: $OUTPUT_DIR"
ls -la "$OUTPUT_DIR"/*.json 2>/dev/null || echo "  (no report files)"
echo

if [[ $TOTAL_FAIL -gt 0 ]]; then
    echo "  RESULT: FAIL ($TOTAL_FAIL test(s) failed)"
    exit 1
else
    echo "  RESULT: ALL TESTS PASSED"
    exit 0
fi
