#!/bin/bash
# MCP server integration tests — run on Linux with the melisai binary built.
set -euo pipefail

MELISAI="${1:-/tmp/melisai}"
PASS=0
FAIL=0

INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
NOTIF='{"jsonrpc":"2.0","method":"notifications/initialized"}'

# Helper: send JSON-RPC messages to MCP server with proper pacing.
# Writes each message line-by-line with a small delay so the server
# processes them before stdin EOF.
mcp_session() {
    local timeout_sec="$1"
    shift
    {
        for msg in "$@"; do
            echo "$msg"
            sleep 0.1
        done
        # Keep stdin open briefly so server can finish processing
        sleep 0.5
    } | timeout "$timeout_sec" "$MELISAI" mcp 2>/dev/null || true
}

check_contains() {
    local label="$1"
    local result="$2"
    shift 2
    for pattern in "$@"; do
        if ! echo "$result" | grep -q "$pattern"; then
            echo "FAIL: $label — missing '$pattern'"
            echo "  Response (first 800 chars): $(echo "$result" | head -c 800)"
            FAIL=$((FAIL+1))
            return 1
        fi
    done
    echo "PASS: $label"
    PASS=$((PASS+1))
    return 0
}

echo "=== MCP Integration Tests ==="
echo "Binary: $MELISAI"
echo ""

# Test 1: tools/list — verify all 4 tools registered
echo "--- Test 1: tools/list ---"
RESULT=$(mcp_session 10 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}')
check_contains "All 4 tools registered" "$RESULT" "get_health" "collect_metrics" "explain_anomaly" "list_anomalies"

# Test 2: explain_anomaly — valid ID
echo ""
echo "--- Test 2: explain_anomaly (cpu_saturation) ---"
RESULT=$(mcp_session 10 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"explain_anomaly","arguments":{"anomaly_id":"cpu_saturation"}}}')
check_contains "explain_anomaly returns CPU Saturation" "$RESULT" "CPU Saturation" "Recommendations"

# Test 3: explain_anomaly — missing argument → IsError
echo ""
echo "--- Test 3: explain_anomaly (missing arg) ---"
RESULT=$(mcp_session 10 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"explain_anomaly","arguments":{}}}')
check_contains "explain_anomaly returns error for missing arg" "$RESULT" "anomaly_id is required" "isError"

# Test 4: explain_anomaly — unknown ID → fallback
echo ""
echo "--- Test 4: explain_anomaly (unknown ID) ---"
RESULT=$(mcp_session 10 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"explain_anomaly","arguments":{"anomaly_id":"xxx_unknown"}}}')
check_contains "explain_anomaly returns fallback for unknown" "$RESULT" "No specific explanation" "xxx_unknown"

# Test 5: list_anomalies
echo ""
echo "--- Test 5: list_anomalies ---"
RESULT=$(mcp_session 10 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_anomalies","arguments":{}}}')
check_contains "list_anomalies returns entries" "$RESULT" "cpu_utilization" "disk_latency" "memory_saturation" "category"

# Test 6: get_health — live Tier 1 collection
echo ""
echo "--- Test 6: get_health (live) ---"
RESULT=$(mcp_session 30 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_health","arguments":{}}}')
check_contains "get_health returns health_score" "$RESULT" "health_score"

# Test 7: collect_metrics quick — live full profile
echo ""
echo "--- Test 7: collect_metrics quick (live) ---"
RESULT=$(mcp_session 60 \
    "$INIT" \
    "$NOTIF" \
    '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"collect_metrics","arguments":{"profile":"quick"}}}')
check_contains "collect_metrics returns report with ai_context" "$RESULT" "ai_context" "summary" "categories"

echo ""
echo "==================================="
echo "Results: $PASS passed, $FAIL failed out of $((PASS+FAIL)) tests"
echo "==================================="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
