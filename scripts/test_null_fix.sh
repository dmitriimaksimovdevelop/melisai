#!/bin/bash
MELISAI="${1:-/tmp/melisai}"

RESULT=$({
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
  sleep 0.1
  echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  sleep 0.1
  echo '{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"get_health","arguments":{}}}'
  sleep 10
} | timeout 20 "$MELISAI" mcp 2>/dev/null)

echo "DEBUG: lines=$(echo "$RESULT" | wc -l)"
echo "DEBUG: raw=$RESULT"
echo ""

# Find the line with id:99
LINE=$(echo "$RESULT" | grep 'id.*99')
if [ -z "$LINE" ]; then
  echo "No response with id 99 found"
  exit 1
fi

echo "$LINE" | python3 << 'PYEOF'
import sys, json
resp = json.loads(sys.stdin.read().strip())
text = resp["result"]["content"][0]["text"]
parsed = json.loads(text)
print(json.dumps(parsed, indent=2))
if parsed["anomalies"] == []:
    print("\nVERIFIED: anomalies is [] (not null)")
elif parsed["anomalies"] is None:
    print("\nFAILED: anomalies is null")
    exit(1)
else:
    print(f"\nanomalies value: {parsed['anomalies']}")
PYEOF
