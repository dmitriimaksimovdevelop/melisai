#!/bin/bash
set -euo pipefail
MELISAI="${1:-/tmp/melisai}"

stress-ng --cpu 5 --timeout 30s --quiet &
STRESS_PID=$!
sleep 1
echo "Targeting PID: $STRESS_PID"

INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"claude-agent","version":"1.0"}}}'
NOTIF='{"jsonrpc":"2.0","method":"notifications/initialized"}'
CALL="{\"jsonrpc\":\"2.0\",\"id\":90,\"method\":\"tools/call\",\"params\":{\"name\":\"collect_metrics\",\"arguments\":{\"profile\":\"quick\",\"pid\":${STRESS_PID}}}}"

echo "DEBUG: Sending CALL=$CALL"

RESULT=$({
  echo "$INIT"
  sleep 0.1
  echo "$NOTIF"
  sleep 0.1
  echo "$CALL"
  sleep 25
} | timeout 45 "$MELISAI" mcp 2>/tmp/mcp_stderr.log)

wait $STRESS_PID 2>/dev/null || true

echo "DEBUG: raw output line count: $(echo "$RESULT" | wc -l)"
echo "DEBUG: raw output:"
echo "$RESULT"
echo ""
echo "DEBUG: stderr:"
cat /tmp/mcp_stderr.log | tail -10
