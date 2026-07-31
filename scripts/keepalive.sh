#!/usr/bin/env bash
# NEXA Exchange sandbox keep-alive script.
# Periodically pings the frontend preview and backend health endpoints
# to prevent the sandbox from going idle and to restart services if they stop.

set -uo pipefail

FRONTEND_URL="${FRONTEND_URL:-http://localhost:3000}"
BACKEND_URL="${BACKEND_URL:-http://localhost:8080/health}"
INTERVAL="${KEEPALIVE_INTERVAL:-60}"
LOG_FILE="${KEEPALIVE_LOG:-/tmp/nexa-keepalive.log}"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" | tee -a "$LOG_FILE"
}

restart_backend() {
    log "[WARN] backend not healthy, restarting api-gateway..."
    pkill -f "${PROJECT_ROOT}/api-gateway" 2>/dev/null || true
    sleep 1
    cd "$PROJECT_ROOT" && nohup sh -c 'POSTGRES_DSN="postgres://nexa:@localhost:5432/nexa?sslmode=disable" ./api-gateway' > /tmp/nexa-api.log 2>&1 &
    echo $! > /tmp/nexa-api.pid
    sleep 3
}

restart_frontend() {
    log "[WARN] frontend not responding, restarting vite preview..."
    pkill -f "vite preview" 2>/dev/null || true
    sleep 1
    cd "$PROJECT_ROOT/frontend" && nohup sh -c 'npm run preview' > /tmp/vite-preview.log 2>&1 &
    echo $! > /tmp/vite-preview.pid
    sleep 3
}

log "[INFO] keep-alive started (interval=${INTERVAL}s)"

while true; do
    backend_ok=false
    frontend_ok=false

    if curl -sf "$BACKEND_URL" >/dev/null 2>&1; then
        backend_ok=true
    fi

    if curl -sf "$FRONTEND_URL" >/dev/null 2>&1; then
        frontend_ok=true
    fi

    if "$backend_ok" && "$frontend_ok"; then
        log "[OK] backend + frontend alive"
    else
        log "[WARN] backend=${backend_ok} frontend=${frontend_ok}"
        if ! "$backend_ok"; then
            restart_backend
        fi
        if ! "$frontend_ok"; then
            restart_frontend
        fi
    fi

    sleep "$INTERVAL"
done
