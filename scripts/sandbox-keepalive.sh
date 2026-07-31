#!/usr/bin/env bash
# Trae sandbox keep-alive daemon.
# Prevents the remote sandbox from being torn down due to idle timeout by
# generating periodic file-system and network activity.

set -uo pipefail

INTERVAL="${SANDBOX_KEEPALIVE_INTERVAL:-30}"
LOG_FILE="${SANDBOX_KEEPALIVE_LOG:-/tmp/sandbox-keepalive.log}"
HEARTBEAT_FILE="/tmp/.sandbox-heartbeat"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> "$LOG_FILE"
}

log "[INFO] sandbox keep-alive started (interval=${INTERVAL}s)"

while true; do
    # 1. File-system activity: write timestamp to heartbeat file
    date '+%Y-%m-%dT%H:%M:%S' > "$HEARTBEAT_FILE"

    # 2. Network activity: try a lightweight HTTP request (keepalive probe)
    #    Use the preview port if a service is running, otherwise just curl localhost.
    curl -sf -o /dev/null --max-time 5 http://localhost:8080/health 2>/dev/null || true
    curl -sf -o /dev/null --max-time 5 http://localhost:3000/ 2>/dev/null || true

    # 3. Minimal CPU activity: touch a temp file to reset atime
    touch "$HEARTBEAT_FILE"

    log "[OK] heartbeat"
    sleep "$INTERVAL"
done
