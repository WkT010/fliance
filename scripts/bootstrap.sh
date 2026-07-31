#!/usr/bin/env bash
# NEXA Exchange sandbox bootstrap / recovery script.
# Run this after the sandbox is reset to reinstall PostgreSQL, migrate,
# build/start the backend, start the frontend preview, and start keep-alive.

set -uo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
POSTGRES_DSN="${POSTGRES_DSN:-postgres://nexa:@localhost:5432/nexa?sslmode=disable}"

echo "[NEXA] bootstrap starting..."

# 1. PostgreSQL
echo "[NEXA] installing postgresql-16..."
apt-get update -qq
apt-get install -y -qq postgresql-16

if [ ! -f /var/lib/postgresql/16/main/postgresql.conf ]; then
    echo "[NEXA] initializing postgres data dir..."
    rm -rf /var/lib/postgresql/16/main
    su - postgres -c "/usr/lib/postgresql/16/bin/initdb -D /var/lib/postgresql/16/main --encoding=UTF8 --locale=en_US.UTF-8"
fi

echo "[NEXA] starting postgres..."
su - postgres -c "/usr/lib/postgresql/16/bin/pg_ctl -D /var/lib/postgresql/16/main start -l /var/lib/postgresql/16/main/server.log"
sleep 2

# Create user/database if not exists
su - postgres -c "psql -tc \"SELECT 1 FROM pg_roles WHERE rolname='nexa'\"" | grep -q 1 || \
    su - postgres -c "psql -c \"CREATE USER nexa WITH PASSWORD 'nexa_dev';\""
su - postgres -c "psql -tc \"SELECT 1 FROM pg_database WHERE datname='nexa'\"" | grep -q 1 || \
    su - postgres -c "psql -c 'CREATE DATABASE nexa OWNER nexa;'"

# Trust local connections for easy startup
sed -i 's/scram-sha-256/trust/g; s/peer/trust/g' /var/lib/postgresql/16/main/pg_hba.conf
su - postgres -c "/usr/lib/postgresql/16/bin/pg_ctl -D /var/lib/postgresql/16/main reload"

# 2. Migrations
echo "[NEXA] running migrations..."
cd "$PROJECT_ROOT" && POSTGRES_DSN="$POSTGRES_DSN" go run ./scripts/migrate/main.go

# 3. Frontend (build only; api-gateway serves static files)
echo "[NEXA] preparing frontend..."
cd "$PROJECT_ROOT/frontend"
if [ ! -d node_modules ] || [ ! -f node_modules/.bin/vite ]; then
    echo "[NEXA] installing frontend dependencies..."
    npm install
fi
echo "[NEXA] building frontend..."
npm run build

# 4. Backend (serves API + static frontend on :8080)
echo "[NEXA] starting api-gateway (serves API + frontend)..."
pkill -f "${PROJECT_ROOT}/api-gateway" 2>/dev/null || true
sleep 1
cd "$PROJECT_ROOT" && setsid bash -c 'POSTGRES_DSN="'"$POSTGRES_DSN"'" STATIC_DIR="./frontend/dist" ./api-gateway' > /tmp/nexa-api.log 2>&1 &
echo $! > /tmp/nexa-api.pid
sleep 3

# 5. Sandbox keep-alive
echo "[NEXA] starting sandbox keep-alive..."
pkill -f "${PROJECT_ROOT}/scripts/sandbox-keepalive.sh" 2>/dev/null || true
sleep 1
setsid "${PROJECT_ROOT}/scripts/sandbox-keepalive.sh" &
echo $! > /tmp/sandbox-keepalive.pid

# 6. Health check
echo "[NEXA] waiting for services..."
for i in {1..30}; do
    backend_ok=false
    curl -sf http://localhost:8080/health >/dev/null 2>&1 && backend_ok=true
    if "$backend_ok"; then
        break
    fi
    sleep 1
done

# 7. Intranet penetration (serveo.net)
echo "[NEXA] starting serveo tunnel..."
chmod +x "${PROJECT_ROOT}/scripts/serveo-tunnel.sh"
setsid bash "${PROJECT_ROOT}/scripts/serveo-tunnel.sh" 8080 < /dev/null > /tmp/serveo-setup.log 2>&1 &
sleep 10

echo "[NEXA] bootstrap complete."
echo "  backend : http://localhost:8080/health"
echo "  frontend: http://localhost:8080/"
echo "  keepalive log: /tmp/sandbox-keepalive.log"
if [ -f /tmp/serveo-url.txt ]; then
    echo "  public URL: $(cat /tmp/serveo-url.txt)"
fi
