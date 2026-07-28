#!/bin/bash
# Nexa Exchange v4.0.402 - Ubuntu one-click deploy script
# Run as root: sudo bash deploy-ubuntu.sh
# Debug mode: sudo bash -x deploy-ubuntu.sh

set -euo pipefail

REPO="https://github.com/WkT010/nexa-exchange.git"
VERSION="v4.0.402"

# Default to the repo root (parent directory of this script).
# Can be overridden by passing APP_DIR as environment variable.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
APP_DIR="${APP_DIR:-$DEFAULT_APP_DIR}"

APP_USER="nexa"
SERVICE="nexa-exchange"
GO_VERSION="1.23.4"
LOG_FILE="${APP_DIR}/deploy.log"

DB_NAME="nexa"
DB_USER="nexa"
DB_PASS="nexa_$(openssl rand -hex 12)"
JWT_SECRET="$(openssl rand -hex 32)"

export DEBIAN_FRONTEND=noninteractive

log() {
    local line="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    echo "$line"
    echo "$line" >> "$LOG_FILE"
}

fail() {
    log "[NEXA] ERROR: $1"
    log "[NEXA] Deploy failed. See $LOG_FILE"
    exit 1
}

# Ensure running as root
if [ "$(id -u)" -ne 0 ]; then
    fail "This script must be run as root. Use: sudo bash $0"
fi

mkdir -p "$(dirname "$LOG_FILE")"
: > "$LOG_FILE"

log "[NEXA] Deploying Nexa Exchange ${VERSION} on Ubuntu..."
log "[NEXA] Log file: $LOG_FILE"

# --- 1. Update and install base packages ---
log "[NEXA] Installing system dependencies..."
apt-get update || fail "apt-get update failed"
apt-get install -y git curl wget build-essential ca-certificates gnupg lsb-release software-properties-common || fail "apt-get install failed"

# --- 2. Install Go ---
if command -v go &>/dev/null && [[ "$(go version | awk '{print $3}')" =~ ^go1\.(2[2-9]|[3-9][0-9]) ]]; then
    log "[NEXA] Go already installed: $(go version)"
else
    log "[NEXA] Installing Go ${GO_VERSION}..."
    rm -rf /usr/local/go
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz || fail "Go download failed"
    tar -C /usr/local -xzf /tmp/go.tar.gz || fail "Go extraction failed"
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
fi
export PATH="/usr/local/go/bin:$PATH"

# --- 3. Install Node.js 20 ---
if command -v node &>/dev/null && [[ "$(node -v | cut -d'v' -f2 | cut -d'.' -f1)" -ge 18 ]]; then
    log "[NEXA] Node.js already installed: $(node -v)"
else
    log "[NEXA] Installing Node.js 20..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - || fail "NodeSource setup failed"
    apt-get install -y nodejs || fail "Node.js install failed"
fi

# --- 4. Install PostgreSQL ---
if command -v psql &>/dev/null; then
    log "[NEXA] PostgreSQL already installed: $(psql --version | head -n1)"
else
    log "[NEXA] Installing PostgreSQL..."
    apt-get install -y postgresql postgresql-contrib || fail "PostgreSQL install failed"
fi
systemctl enable --now postgresql || fail "Failed to start PostgreSQL"

# --- 5. Clone or update source ---
log "[NEXA] Preparing application directory: $APP_DIR"
if [ -d "$APP_DIR/.git" ]; then
    cd "$APP_DIR"
    git fetch --all --tags || fail "git fetch failed"
    git checkout "$VERSION" || fail "git checkout failed"
    git pull origin "$VERSION" || true
elif [ -f "$APP_DIR/frontend/package.json" ]; then
    log "[NEXA] Source already present (no .git). Building from current files..."
    cd "$APP_DIR"
else
    log "[NEXA] Source not found. Cloning repository to $APP_DIR ..."
    if [ -d "$APP_DIR" ] && [ "$(ls -A "$APP_DIR" 2>/dev/null)" ]; then
        fail "$APP_DIR is not empty. Please empty it or run this script from the cloned repo."
    fi
    git clone --branch "$VERSION" "$REPO" "$APP_DIR" || fail "git clone failed"
    cd "$APP_DIR"
fi

# --- 6. Build frontend ---
log "[NEXA] Building frontend..."
cd "$APP_DIR/frontend"
npm ci || fail "npm ci failed"
npm run build || fail "npm run build failed"

# --- 7. Build backend ---
log "[NEXA] Building backend..."
cd "$APP_DIR"
go build -o nexa-api ./cmd/api-gateway || fail "go build failed"

# --- 8. Setup PostgreSQL database and user ---
log "[NEXA] Configuring PostgreSQL..."
sudo -u postgres psql -c "CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASS}';" 2>/dev/null || true
sudo -u postgres psql -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>/dev/null || true

# --- 9. Create application user ---
if ! id "$APP_USER" &>/dev/null; then
    useradd -r -s /bin/false -d "$APP_DIR" "$APP_USER" || fail "Failed to create user $APP_USER"
fi
chown -R "$APP_USER:$APP_USER" "$APP_DIR"

# --- 10. Environment file ---
log "[NEXA] Writing environment file..."
cat > "$APP_DIR/.env" <<EOF
JWT_SECRET=${JWT_SECRET}
JWT_ISSUER=nexa-exchange
LISTEN_ADDR=:8080
POSTGRES_DSN=postgres://${DB_USER}:${DB_PASS}@localhost:5432/${DB_NAME}?sslmode=disable
ENVIRONMENT=production
CORS_ALLOW_ORIGINS=*
ALCHEMY_API_KEY=your_alchemy_key_here
STATIC_DIR=${APP_DIR}/frontend/dist
EOF
chmod 600 "$APP_DIR/.env"
chown "$APP_USER:$APP_USER" "$APP_DIR/.env"

log "[NEXA] IMPORTANT: edit $APP_DIR/.env and replace 'your_alchemy_key_here' with a real Alchemy API key."

# --- 11. systemd service ---
log "[NEXA] Registering systemd service..."
cat > "/etc/systemd/system/${SERVICE}.service" <<EOF
[Unit]
Description=Nexa Exchange API gateway and web frontend
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${APP_DIR}/.env
ExecStart=${APP_DIR}/nexa-api
Restart=always
RestartSec=5
StandardOutput=append:${APP_DIR}/nexa.log
StandardError=append:${APP_DIR}/nexa.log

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload || fail "systemctl daemon-reload failed"
systemctl enable "$SERVICE" || fail "systemctl enable failed"
systemctl restart "$SERVICE" || fail "systemctl restart failed"

IP=$(hostname -I | awk '{print $1}')

log ""
log "[NEXA] Deployment complete!"
log "[NEXA] Open http://${IP}:8080 in your browser."
log "[NEXA] Service: sudo systemctl status ${SERVICE}"
log "[NEXA] Logs:   sudo tail -f ${APP_DIR}/nexa.log"
log "[NEXA] Env:    ${APP_DIR}/.env"
