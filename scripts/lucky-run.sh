#!/usr/bin/env bash
# Start Lucky cleanly without self-kill issues.
# Usage: bash /workspace/scripts/lucky-run.sh
set -uo pipefail

LUCKY_BIN="/opt/lucky/lucky"
CONF_DIR="/goodluck"
LOG_FILE="/tmp/lucky.log"
PID_FILE="/tmp/lucky.pid"

# Kill existing lucky by PID file only (avoid pkill -f self-match)
if [ -f "$PID_FILE" ]; then
    old_pid=$(cat "$PID_FILE" 2>/dev/null || true)
    if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
        kill -9 "$old_pid" 2>/dev/null || true
        sleep 1
    fi
fi

# Start Lucky
cd "$CONF_DIR"
setsid "$LUCKY_BIN" -c "$CONF_DIR/lucky.conf" > "$LOG_FILE" 2>&1 &
pid=$!
echo "$pid" > "$PID_FILE"
echo "Lucky started, pid=$pid"
sleep 6

# Verify
if curl -sf -o /dev/null --max-time 5 http://localhost:16601/ 2>/dev/null; then
    echo "[OK] Lucky is running on http://localhost:16601"
else
    echo "[ERROR] Lucky failed to start"
    tail -20 "$LOG_FILE"
fi
