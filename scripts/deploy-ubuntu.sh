#!/bin/bash
# Nexa Exchange v4.0.402 - Ubuntu one-click deploy script
# Run as root: sudo bash deploy-ubuntu.sh

set -e

REPO="https://github.com/WkT010/nexa-exchange.git"
VERSION="v4.0.402"
APP_DIR="/opt/nexa-exchange"
APP_USER="nexa"
SERVICE="nexa-exchange"
GO_VERSION="1.23.4"

DB_NAME="nexa"
DB_USER="nexa"
DB_PASS="nexa_$(openssl rand -hex 12)"
JWT_SECRET="$(openssl rand -hex 32)"

export DEBIAN_FRONTEND=noninteractive

echo "[NEXA] Deploying Nexa Exchange ${VERSION} on Ubuntu..."

# 1. Update and install base packages
echo "[NEXA] Installing system dependencies..."
apt-get update
apt-get install -y git curl wget build-essential ca-certificates gnupg lsb-release software-properties-common

# 2. Install Go
if ! command -v go &>/dev/null || [[ "$(go version | awk '{print $3}')" < "go1.21" ]]; then
    echo "[NEXA] Installing Go ${GO_VERSION}..."
    rm -rf /usr/local/go
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    tar -C /usr/local -xzf /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
fi

# 3. Install Node.js 20
if ! command -v node &>/dev/null || [[ "$(node -v | cut -d'v' -f2 | cut -d'.' -f1)" -lt 18 ]]; then
    echo "[NEXA] Installing Node.js 20..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y nodejs
fi

# 4. Install PostgreSQL
if ! command -v psql &>/dev/null; then
    echo "[NEXA] Installing PostgreSQL..."
    apt-get install -y postgresql postgresql-contrib
    systemctl enable --now postgresql
fi

# 5. Clone or update source
echo "[NEXA] Preparing application directory..."
if [ -d "$APP_DIR" ]; then
    cd "$APP_DIR"
    git fetch --tags
    git checkout "$VERSION"
    git pull origin "$VERSION" || true
else
    git clone --depth 1 --branch "$VERSION" "$REPO" "$APP_DIR"
    cd "$APP_DIR"
fi

# 6. Build frontend
echo "[NEXA] Building frontend..."
cd "$APP_DIR/frontend"
npm ci
npm run build

# 7. Build backend
echo "[NEXA] Building backend..."
cd "$APP_DIR"
go build -o nexa-api ./cmd/api-gateway

# 8. Setup PostgreSQL database and user
echo "[NEXA] Configuring PostgreSQL..."
sudo -u postgres psql -c "CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASS}';" 2>/dev/null || true
sudo -u postgres psql -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>/dev/null || true

# 9. Create application user
if ! id "$APP_USER" &>/dev/null; then
    useradd -r -s /bin/false -d "$APP_DIR" "$APP_USER"
fi
chown -R "$APP_USER:$APP_USER" "$APP_DIR"

# 10. Environment file
echo "[NEXA] Writing environment file..."
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

echo "[NEXA] IMPORTANT: edit $APP_DIR/.env and replace 'your_alchemy_key_here' with a real Alchemy API key."

# 11. systemd service
echo "[NEXA] Registering systemd service..."
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

systemctl daemon-reload
systemctl enable "$SERVICE"
systemctl restart "$SERVICE"

IP=$(hostname -I | awk '{print $1}')

echo ""
echo "[NEXA] Deployment complete!"
echo "[NEXA] Open http://${IP}:8080 in your browser."
echo "[NEXA] Service: sudo systemctl status ${SERVICE}"
echo "[NEXA] Logs:   sudo tail -f ${APP_DIR}/nexa.log"
echo "[NEXA] Env:    ${APP_DIR}/.env"
