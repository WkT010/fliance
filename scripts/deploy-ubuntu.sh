#!/bin/bash
# ============================================================================
# Fliance（梵响） — Ubuntu 生产环境一键部署脚本
# ----------------------------------------------------------------------------
# 用法（root 执行，DOMAIN 必填，用于 CORS 白名单/提示）：
#   sudo DOMAIN=exchange.example.com bash scripts/deploy-ubuntu.sh
#   sudo DOMAIN=exchange.example.com bash -x scripts/deploy-ubuntu.sh   # 调试
#
# 流程：系统依赖 → Go/Node → PostgreSQL → 源码准备 → 前端构建 →
#       三服务构建（matching-engine/wallet-service/api-gateway）→
#       建库 → 数据库迁移（scripts/migrate，001-015）→
#       三个 systemd 服务 → 健康检查（:8080 /:8082 /health、:50051 TCP）
#
# 端口：matching-engine gRPC :50051 + 监控 :8081；wallet-service :8082；
#       api-gateway :8080（对外由 nginx/反代暴露）。
# ============================================================================

set -euo pipefail

REPO="https://github.com/WkT010/nexa-exchange.git"

# Default to the repo root (parent directory of this script).
# Can be overridden by passing APP_DIR as environment variable.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
APP_DIR="${APP_DIR:-$DEFAULT_APP_DIR}"

# 版本优先读仓库根 VERSION 文件（与发布流程对齐），缺省回退 v5.0.0
VERSION="$(cat "$APP_DIR/VERSION" 2>/dev/null || echo v5.0.0)"

APP_USER="nexa"
# 与 go.mod 的 go 1.25.0 对齐
GO_VERSION="1.25.3"
LOG_FILE="${APP_DIR}/deploy.log"

# 三个服务的 systemd 单元名
ENGINE_UNIT="fliance-matching-engine"
WALLET_UNIT="fliance-wallet-service"
GATEWAY_UNIT="fliance-api-gateway"

# DOMAIN 必填：ENVIRONMENT=production 下 CORS 禁止通配符，
# 白名单必须为真实对外域名（不含 scheme），否则 api-gateway 启动失败。
DOMAIN="${DOMAIN:-}"

DB_NAME="nexa"
DB_USER="nexa"
DB_PASS="nexa_$(openssl rand -hex 12)"
JWT_SECRET="$(openssl rand -hex 32)"
GRPC_SHARED_TOKEN="$(openssl rand -base64 32)"

export DEBIAN_FRONTEND=noninteractive

log() {
    local line="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    echo "$line"
    echo "$line" >> "$LOG_FILE"
}

fail() {
    log "[Fliance] ERROR: $1"
    log "[Fliance] Deploy failed. See $LOG_FILE"
    exit 1
}

# Ensure running as root
if [ "$(id -u)" -ne 0 ]; then
    fail "This script must be run as root. Use: sudo bash $0"
fi

# DOMAIN is required: production forbids wildcard CORS (CORS_ALLOW_ORIGINS=*).
if [ -z "$DOMAIN" ]; then
    fail "DOMAIN is not set. Usage: sudo DOMAIN=exchange.example.com bash $0 (used for the CORS allow-list; production refuses wildcard origins)"
fi

mkdir -p "$(dirname "$LOG_FILE")"
: > "$LOG_FILE"

log "[Fliance] Deploying Fliance（梵响） ${VERSION} on Ubuntu..."
log "[Fliance] Log file: $LOG_FILE"

# Helper: locate a binary in common install locations (works even when sudo resets PATH)
find_binary() {
    local name="$1"
    shift
    local candidates=("$@")
    local c p
    for c in "${candidates[@]}"; do
        for p in $c; do
            if [ -x "$p" ]; then
                echo "$p"
                return 0
            fi
        done
    done
    command -v "$name" 2>/dev/null
}

GO_BIN="$(find_binary go \
    /usr/local/go/bin/go \
    /usr/lib/go*/bin/go \
    /root/.local/share/mise/shims/go \
    /home/*/.local/share/mise/shims/go \
    /usr/local/bin/go \
    /usr/bin/go)"

NODE_BIN="$(find_binary node \
    /usr/local/bin/node \
    /usr/bin/node \
    /root/.local/share/mise/shims/node \
    /home/*/.local/share/mise/shims/node \
    /root/.nvm/versions/node/*/bin/node)"

NPM_BIN="$(find_binary npm \
    /usr/local/bin/npm \
    /usr/bin/npm \
    /root/.local/share/mise/shims/npm \
    /home/*/.local/share/mise/shims/npm \
    /root/.nvm/versions/node/*/bin/npm)"

# --- 1. Update and install base packages ---
log "[Fliance] Installing system dependencies..."
for attempt in 1 2 3; do
    log "[Fliance] apt-get update attempt ${attempt}/3..."
    if apt-get update; then break; fi
    if [ "$attempt" -eq 3 ]; then log "[Fliance] WARNING: apt-get update failed, continuing with existing package lists..."; fi
    sleep 3
done
apt-get install -y git curl wget build-essential ca-certificates gnupg lsb-release software-properties-common || fail "apt-get install failed"

# --- 2. Install Go ---
GO_OK=false
if [ -n "$GO_BIN" ] && "$GO_BIN" version &>/dev/null; then
    GO_VERSION_STR=$("$GO_BIN" version | awk '{print $3}')
    # go.mod requires go >= 1.25; accept only matching or newer toolchains
    if [[ "$GO_VERSION_STR" =~ ^go1\.(2[5-9]|[3-9][0-9]) ]]; then
        log "[Fliance] Go already installed: $GO_VERSION_STR ($GO_BIN)"
        GO_OK=true
    fi
fi

if [ "$GO_OK" != true ]; then
    log "[Fliance] Installing Go ${GO_VERSION}..."
    rm -rf /usr/local/go
    mkdir -p /usr/local/bin

    for attempt in 1 2 3; do
        log "[Fliance] Go download attempt ${attempt}/3..."
        if curl -fsSL --max-time 300 "https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz; then
            break
        fi
        if curl -fsSL --max-time 300 "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz; then
            break
        fi
        if [ "$attempt" -eq 3 ]; then fail "Go download failed after 3 attempts"; fi
        sleep 5
    done

    tar -C /usr/local -xzf /tmp/go.tar.gz || fail "Go extraction failed"
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    GO_BIN="/usr/local/go/bin/go"
fi
export PATH="$(dirname "$GO_BIN"):$PATH"
export GOTOOLCHAIN=auto

# --- 3. Install Node.js 20 ---
NODE_OK=false
if [ -n "$NODE_BIN" ] && "$NODE_BIN" -v &>/dev/null; then
    NODE_MAJOR=$("$NODE_BIN" -v | sed 's/^v//; s/\..*//')
    if [ "$NODE_MAJOR" -ge 18 ]; then
        log "[Fliance] Node.js already installed: $("$NODE_BIN" -v) ($NODE_BIN)"
        NODE_OK=true
    fi
fi

if [ "$NODE_OK" != true ]; then
    log "[Fliance] Installing Node.js 20..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - || fail "NodeSource setup failed"
    apt-get install -y nodejs || fail "Node.js install failed"
    NODE_BIN="/usr/bin/node"
    NPM_BIN="/usr/bin/npm"
fi

if [ -n "$NPM_BIN" ]; then
    export PATH="$(dirname "$NPM_BIN"):$PATH"
fi

# --- 4. Install PostgreSQL ---
if command -v psql &>/dev/null; then
    log "[Fliance] PostgreSQL already installed: $(psql --version | head -n1)"
else
    log "[Fliance] Installing PostgreSQL..."
    if ! apt-get install -y postgresql postgresql-contrib; then
        log ""
        log "[Fliance] PostgreSQL could not be installed automatically. Common fixes:"
        log "[Fliance]   1. Run: sudo apt-get update"
        log "[Fliance]   2. Run: sudo apt-get install -y postgresql postgresql-contrib"
        log "[Fliance]   3. Then rerun this script."
        fail "PostgreSQL install failed"
    fi
fi
systemctl enable --now postgresql || fail "Failed to start PostgreSQL"

# --- 5. Clone or update source ---
log "[Fliance] Preparing application directory: $APP_DIR"
if [ -d "$APP_DIR/.git" ]; then
    cd "$APP_DIR"
    git fetch --all --tags || fail "git fetch failed"
    git checkout "$VERSION" || fail "git checkout failed"
    git pull origin "$VERSION" || true
elif [ -f "$APP_DIR/frontend/package.json" ]; then
    log "[Fliance] Source already present (no .git). Building from current files..."
    cd "$APP_DIR"
else
    log "[Fliance] Source not found. Cloning repository to $APP_DIR ..."
    if [ -d "$APP_DIR" ] && [ "$(ls -A "$APP_DIR" 2>/dev/null)" ]; then
        fail "$APP_DIR is not empty. Please empty it or run this script from the cloned repo."
    fi
    git clone --branch "$VERSION" "$REPO" "$APP_DIR" || fail "git clone failed"
    cd "$APP_DIR"
fi

# --- 6. Build frontend ---
log "[Fliance] Building frontend..."
cd "$APP_DIR/frontend"
npm ci || npm install || fail "npm install failed"
npm run build || fail "npm run build failed"

# --- 7. Build backend（三服务全部构建，输出到 build/） ---
log "[Fliance] Building backend (api-gateway / matching-engine / wallet-service)..."
cd "$APP_DIR"
mkdir -p "$APP_DIR/build"
CGO_ENABLED=0 "$GO_BIN" build -ldflags='-s -w' -o "$APP_DIR/build/api-gateway" ./cmd/api-gateway || fail "go build api-gateway failed"
CGO_ENABLED=0 "$GO_BIN" build -ldflags='-s -w' -o "$APP_DIR/build/matching-engine" ./cmd/matching-engine || fail "go build matching-engine failed"
CGO_ENABLED=0 "$GO_BIN" build -ldflags='-s -w' -o "$APP_DIR/build/wallet-service" ./cmd/wallet-service || fail "go build wallet-service failed"

# --- 8. Setup PostgreSQL database and user ---
log "[Fliance] Configuring PostgreSQL..."
sudo -u postgres psql -c "CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASS}';" 2>/dev/null || true
sudo -u postgres psql -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>/dev/null || true

# --- 8.5 数据库迁移（001-015；重复执行自动跳过已应用版本） ---
log "[Fliance] Running database migrations..."
POSTGRES_DSN="postgres://${DB_USER}:${DB_PASS}@localhost:5432/${DB_NAME}?sslmode=disable" \
    "$GO_BIN" run ./scripts/migrate || fail "database migration failed"
log "[Fliance] Migrations complete"

# --- 9. Create application user ---
if ! id "$APP_USER" &>/dev/null; then
    useradd -r -s /bin/false -d "$APP_DIR" "$APP_USER" || fail "Failed to create user $APP_USER"
fi

# --- 10. Environment file ---
log "[Fliance] Writing environment file..."
cat > "$APP_DIR/.env" <<EOF
JWT_SECRET=${JWT_SECRET}
JWT_ISSUER=fliance-exchange
LISTEN_ADDR=:8080
GRPC_ADDR=:50051
ENVIRONMENT=production
POSTGRES_DSN=postgres://${DB_USER}:${DB_PASS}@localhost:5432/${DB_NAME}?sslmode=disable
DOMAIN=${DOMAIN}
GRPC_SHARED_TOKEN=${GRPC_SHARED_TOKEN}
CORS_ALLOW_ORIGINS=https://${DOMAIN}
ALCHEMY_API_KEY=your_alchemy_key_here
STATIC_DIR=${APP_DIR}/frontend/dist
EOF
chmod 600 "$APP_DIR/.env"

# WAL/快照目录：独立引擎与 gateway 内嵌引擎各用一套，避免互相踩踏
mkdir -p "$APP_DIR/data/engine-wal" "$APP_DIR/data/engine-snapshots" \
         "$APP_DIR/data/wal" "$APP_DIR/data/snapshots"
chown -R "$APP_USER:$APP_USER" "$APP_DIR"

log "[Fliance] IMPORTANT: edit $APP_DIR/.env and replace 'your_alchemy_key_here' with a real Alchemy API key."

# --- 11. systemd services（三服务各一个单元） ---
log "[Fliance] Registering systemd services..."

cat > "/etc/systemd/system/${ENGINE_UNIT}.service" <<EOF
[Unit]
Description=Fliance（梵响） matching engine (gRPC :50051, monitor :8081)
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${APP_DIR}/.env
Environment=WAL_DIR=${APP_DIR}/data/engine-wal
Environment=SNAPSHOT_DIR=${APP_DIR}/data/engine-snapshots
ExecStart=${APP_DIR}/build/matching-engine
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/matching-engine.log
StandardError=append:${APP_DIR}/matching-engine.log

[Install]
WantedBy=multi-user.target
EOF

cat > "/etc/systemd/system/${WALLET_UNIT}.service" <<EOF
[Unit]
Description=Fliance（梵响） wallet service (HTTP :8082)
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${APP_DIR}/.env
Environment=LISTEN_ADDR=:8082
Environment=WAL_DIR=${APP_DIR}/data/wal
Environment=SNAPSHOT_DIR=${APP_DIR}/data/snapshots
ExecStart=${APP_DIR}/build/wallet-service
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/wallet-service.log
StandardError=append:${APP_DIR}/wallet-service.log

[Install]
WantedBy=multi-user.target
EOF

cat > "/etc/systemd/system/${GATEWAY_UNIT}.service" <<EOF
[Unit]
Description=Fliance（梵响） API gateway and web frontend (HTTP :8080)
After=network.target postgresql.service ${ENGINE_UNIT}.service ${WALLET_UNIT}.service
Wants=postgresql.service ${ENGINE_UNIT}.service ${WALLET_UNIT}.service

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${APP_DIR}/.env
Environment=LISTEN_ADDR=:8080
ExecStart=${APP_DIR}/build/api-gateway
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/api-gateway.log
StandardError=append:${APP_DIR}/api-gateway.log

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload || fail "systemctl daemon-reload failed"
systemctl enable "$ENGINE_UNIT" "$WALLET_UNIT" "$GATEWAY_UNIT" || fail "systemctl enable failed"
systemctl restart "$ENGINE_UNIT" || fail "systemctl restart ${ENGINE_UNIT} failed"
sleep 2
systemctl restart "$WALLET_UNIT" || fail "systemctl restart ${WALLET_UNIT} failed"
sleep 2
systemctl restart "$GATEWAY_UNIT" || fail "systemctl restart ${GATEWAY_UNIT} failed"

# --- 12. Health checks（重试等待服务就绪） ---
log "[Fliance] Running health checks..."
check_http() {
    local url="$1" name="$2" i
    for i in 1 2 3 4 5 6 7 8 9 10; do
        if curl -sf --max-time 5 "$url" >/dev/null 2>&1; then
            log "[Fliance] [OK] $name -> $url"
            return 0
        fi
        sleep 3
    done
    log "[Fliance] [WARN] $name not healthy at $url (check journalctl -u and $APP_DIR/*.log)"
    return 1
}
check_tcp() {
    local port="$1" name="$2" i
    for i in 1 2 3 4 5 6 7 8 9 10; do
        if (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
            exec 3>&- 3<&- || true
            log "[Fliance] [OK] $name -> tcp :${port}"
            return 0
        fi
        sleep 3
    done
    log "[Fliance] [WARN] $name not listening on :${port} (check journalctl -u ${ENGINE_UNIT})"
    return 1
}
HEALTH_OK=true
check_http "http://localhost:8080/health" "api-gateway" || HEALTH_OK=false
check_http "http://localhost:8082/health" "wallet-service" || HEALTH_OK=false
check_tcp 50051 "matching-engine gRPC" || HEALTH_OK=false

IP=$(hostname -I | awk '{print $1}')

log ""
if [ "$HEALTH_OK" = true ]; then
    log "[Fliance] Deployment complete — all health checks passed!"
else
    log "[Fliance] Deployment finished with health warnings — inspect logs below."
fi
log "[Fliance] Open http://${IP}:8080 in your browser (production should front this with nginx/TLS)."
log "[Fliance] Services: sudo systemctl status ${ENGINE_UNIT} ${WALLET_UNIT} ${GATEWAY_UNIT}"
log "[Fliance] Logs:     ${APP_DIR}/matching-engine.log / wallet-service.log / api-gateway.log"
log "[Fliance] Env:      ${APP_DIR}/.env"
