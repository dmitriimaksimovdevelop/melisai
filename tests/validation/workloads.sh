#!/usr/bin/env bash
# workloads.sh — workload library for melisai validation tests
# Sourced by run_validation.sh; each function launches a background workload
# and populates WORKLOAD_PIDS for cleanup.

set -euo pipefail

WORKLOAD_PIDS=()
WORKLOAD_TMPDIR="${WORKLOAD_TMPDIR:-/tmp/melisai-validation}"
mkdir -p "$WORKLOAD_TMPDIR"

# cleanup_workload — kill all tracked PIDs, remove tc qdisc, clean temp files
cleanup_workload() {
    local pid
    for pid in "${WORKLOAD_PIDS[@]:-}"; do
        [[ -z "$pid" ]] && continue
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    done
    WORKLOAD_PIDS=()

    # Kill any stray stress-ng / iperf3 / dd
    pkill -9 stress-ng 2>/dev/null || true
    pkill -9 iperf3 2>/dev/null || true

    # Remove tc netem from loopback
    tc qdisc del dev lo root 2>/dev/null || true

    # Unmount tmpfs memhog if present
    umount /tmp/memhog 2>/dev/null || true

    # Re-enable swap if it was disabled
    swapon -a 2>/dev/null || true

    # Clean temp files
    rm -f "$WORKLOAD_TMPDIR"/hdd_* 2>/dev/null || true
}

# --- Test 1: CPU burn (2× oversubscribed) ---
workload_cpu_burn() {
    local cpus
    cpus=$(nproc)
    local workers=$(( cpus * 2 ))
    stress-ng --cpu "$workers" --cpu-load 98 -t 25s &>/dev/null &
    WORKLOAD_PIDS+=($!)
}

# --- Test 2: Memory pressure (~90% of RAM) ---
# Use tmpfs to reliably consume physical memory.
# Disable swap so kernel can't push tmpfs pages to swap.
# Fill synchronously (dd in foreground) before returning.
workload_memory_pressure() {
    local total_gb
    total_gb=$(free -g | awk '/^Mem:/{print $2}')
    local target_gb=$(( total_gb - 5 ))  # leave 5G for OS + melisai

    # Disable swap so tmpfs pages must stay in RAM
    swapoff -a 2>/dev/null || true

    mkdir -p /tmp/memhog
    mount -t tmpfs -o "size=${target_gb}G" tmpfs /tmp/memhog 2>/dev/null || true

    # Fill tmpfs synchronously (takes ~15s at 3.6 GB/s for 57G)
    dd if=/dev/zero of=/tmp/memhog/fill bs=1G count="$target_gb" &>/dev/null || true

    # Keep a background sleep so the "alive" check has a PID to monitor
    sleep 120 &
    WORKLOAD_PIDS+=($!)
}

# --- Test 3: Disk flood ---
workload_disk_flood() {
    stress-ng --hdd 10 --hdd-bytes 1G --hdd-write-size 1M --io 5 \
        --temp-path "$WORKLOAD_TMPDIR" -t 25s &>/dev/null &
    WORKLOAD_PIDS+=($!)
}

# --- Test 4: Fork storm ---
workload_fork_storm() {
    stress-ng --fork 20 -t 25s &>/dev/null &
    WORKLOAD_PIDS+=($!)
}

# --- Test 5: Run-queue saturation (4× oversubscribed) ---
workload_runq_saturation() {
    local cpus
    cpus=$(nproc)
    local workers=$(( cpus * 4 ))
    stress-ng --cpu "$workers" -t 45s &>/dev/null &
    WORKLOAD_PIDS+=($!)
}

# --- Test 6: TCP retransmissions (5% packet loss on loopback) ---
workload_tcp_retrans() {
    # Add packet loss to loopback
    tc qdisc add dev lo root netem loss 5%

    # Start iperf3 server
    iperf3 -s -D -p 5201 --logfile "$WORKLOAD_TMPDIR/iperf3_server.log"
    sleep 1

    # Start iperf3 client (4 parallel streams)
    iperf3 -c 127.0.0.1 -p 5201 -P 4 -t 40s &>"$WORKLOAD_TMPDIR/iperf3_client.log" &
    WORKLOAD_PIDS+=($!)

    # Track server PID
    local server_pid
    server_pid=$(pgrep -f "iperf3 -s" | head -1) || true
    if [[ -n "$server_pid" ]]; then
        WORKLOAD_PIDS+=("$server_pid")
    fi
}

# --- Test 7: Combined stress (CPU 50% + memory pressure + disk I/O) ---
workload_combined() {
    local cpus
    cpus=$(nproc)
    local cpu_workers=$(( cpus / 2 ))
    local total_gb
    total_gb=$(free -g | awk '/^Mem:/{print $2}')
    local mem_target_gb=$(( total_gb * 85 / 100 ))

    # CPU at ~50%
    stress-ng --cpu "$cpu_workers" --cpu-load 98 -t 45s &>/dev/null &
    WORKLOAD_PIDS+=($!)

    # Memory pressure via tmpfs (~85%)
    mkdir -p /tmp/memhog
    mount -t tmpfs -o "size=${mem_target_gb}G" tmpfs /tmp/memhog 2>/dev/null || true
    dd if=/dev/zero of=/tmp/memhog/fill bs=1G count="$mem_target_gb" &>/dev/null &
    WORKLOAD_PIDS+=($!)

    # Disk I/O
    stress-ng --hdd 5 --hdd-bytes 512M --io 3 \
        --temp-path "$WORKLOAD_TMPDIR" -t 45s &>/dev/null &
    WORKLOAD_PIDS+=($!)
}
