#!/usr/bin/env bash
# ============================================================================
# Fliance（梵响） — One-Click Deployment Script
# ============================================================================
# Usage:
#   ./deploy.sh              Start in development mode (default)
#   ./deploy.sh prod         Start in production mode (Docker Compose + Nginx)
#   ./deploy.sh stop         Stop all services
#   ./deploy.sh restart      Restart all services
#   ./deploy.sh logs         View service logs
#   ./deploy.sh status       Check service and infra status
#   ./deploy.sh ssl-init     Initialize Let's Encrypt SSL certificates
#   ./deploy.sh infra-up     Start infrastructure only
#   ./deploy.sh infra-down   Stop infrastructure
# ============================================================================

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
log_info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$PROJECT_ROOT/build"
ENV_FILE="$PROJECT_ROOT/.env"
COMPOSE_FILE="$PROJECT_ROOT/deploy/docker/docker-compose.yml"
PROD_COMPOSE_FILE="$PROJECT_ROOT/deploy/docker/docker-compose.prod.yml"

# ── Production mode ────────────────────────────────────────────────────
prod_deploy() {
    log_info "=== Fliance（梵响） — Production Deployment ==="
    echo ""

    if [ ! -f "$ENV_FILE" ]; then
        log_error ".env file not found! Copy .env.example to .env and configure:"
        echo "  cp .env.example .env"
        echo "  # Then edit .env with your production values"
        echo "  # Especially: JWT_SECRET, POSTGRES_PASSWORD, ALCHEMY_API_KEY, DOMAIN"
        exit 1
    fi

    # Load .env
    set -a; source "$ENV_FILE"; set +a

    # Check required vars
    if [ "${JWT_SECRET:-}" = "change-me-in-production" ] || [ -z "${JWT_SECRET:-}" ]; then
        log_error "JWT_SECRET must be set in .env for production!"
        exit 1
    fi

    # Ensure SSL certificates exist (or generate self-signed)
    if [ ! -f "deploy/ssl/certs/fullchain.pem" ]; then
        log_warn "No SSL certificates found at deploy/ssl/certs/"
        log_info "Generating self-signed certificate for development..."
        mkdir -p deploy/ssl/certs
        openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout deploy/ssl/certs/privkey.pem \
            -out deploy/ssl/certs/fullchain.pem \
            -subj "/CN=localhost" 2>/dev/null
        log_ok "Self-signed certificate generated (for testing only)"
        log_warn "For production, use: DOMAIN=yourdomain.com ./deploy.sh ssl-init"
    fi

    # If domain is set, update nginx config
    if [ -n "${DOMAIN:-}" ]; then
        log_info "Configuring Nginx for domain: $DOMAIN"
        # Update nginx.conf with the actual domain
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' "s/server_name _;/server_name $DOMAIN;/g" deploy/nginx/nginx.conf
        else
            sed -i "s/server_name _;/server_name $DOMAIN;/g" deploy/nginx/nginx.conf
        fi
    fi

    # Pull images and start
    log_info "Starting production stack..."
    docker compose -f "$PROD_COMPOSE_FILE" pull
    docker compose -f "$PROD_COMPOSE_FILE" up -d --build --remove-orphans

    echo ""
    log_ok "Production deployment complete!"
    echo ""
    echo "  Access:  https://${DOMAIN:-localhost}"
    echo "  API:     https://${DOMAIN:-localhost}/api/v2"
    echo "  WS:      wss://${DOMAIN:-localhost}/ws"
    echo "  Health:  https://${DOMAIN:-localhost}/health"
    echo ""
    echo "  To view logs:"
    echo "    docker compose -f $PROD_COMPOSE_FILE logs -f"
    echo ""
}

# ── Production SSL init ─────────────────────────────────────────────────
ssl_init() {
    if [ -z "${DOMAIN:-}" ]; then
        log_error "DOMAIN environment variable not set."
        echo "  Usage: DOMAIN=trade.fliance.com $0 ssl-init"
        exit 1
    fi
    EMAIL="${EMAIL:-admin@${DOMAIN}}"
    log_info "Initializing SSL for $DOMAIN with email $EMAIL"
    DOMAIN="$DOMAIN" EMAIL="$EMAIL" bash "$PROJECT_ROOT/deploy/ssl/init-letsencrypt.sh"
}

# ── Config defaults ────────────────────────────────────────────────────
export JWT_SECRET="${JWT_SECRET:-fliance-dev-secret-change-me}"
export JWT_ISSUER="${JWT_ISSUER:-fliance-exchange}"
export LISTEN_ADDR="${LISTEN_ADDR:-:8080}"
export GRPC_ADDR="${GRPC_ADDR:-:50051}"
export ENVIRONMENT="${ENVIRONMENT:-development}"
export POSTGRES_DSN="${POSTGRES_DSN:-postgres://nexa:nexa_dev@localhost:5432/nexa?sslmode=disable}"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
export REDIS_PASSWORD="${REDIS_PASSWORD:-}"
export KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:9092}"
export KAFKA_TOPIC="${KAFKA_TOPIC:-fliance-exchange}"
export TRADING_PAIRS="${TRADING_PAIRS:-BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT,ADA/USDT}"
export LOG_LEVEL="${LOG_LEVEL:-info}"
export LOG_FORMAT="${LOG_FORMAT:-text}"

# ── Pre-flight ─────────────────────────────────────────────────────────
preflight() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}   Fliance（梵响） — Deployment${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo ""

    local fail=0
    if ! command -v docker &>/dev/null; then log_error "Docker not installed"; fail=1
    else log_ok "Docker $(docker --version | cut -d' ' -f3 | tr -d ',')"; fi

    if docker compose version &>/dev/null; then export COMPOSE_CMD="docker compose"
    elif command -v docker-compose &>/dev/null; then export COMPOSE_CMD="docker-compose"
    else log_error "Docker Compose not found"; fail=1; fi

    if ! command -v go &>/dev/null; then log_error "Go not installed"; fail=1
    else log_ok "Go $(go version | grep -oP 'go\S+' | tr -d 'go')"; fi

    if [ "$fail" -eq 1 ]; then echo ""; log_error "Prerequisites missing. Aborting."; exit 1; fi
    echo ""
}

write_env() {
    [ -f "$ENV_FILE" ] && [ "${ENVIRONMENT}" = "production" ] && cp "$ENV_FILE" "$ENV_FILE.backup"
    cat > "$ENV_FILE" <<-EOF
# Fliance（梵响） — Auto-generated by deploy.sh
JWT_SECRET=${JWT_SECRET}
JWT_ISSUER=${JWT_ISSUER}
LISTEN_ADDR=${LISTEN_ADDR}
GRPC_ADDR=${GRPC_ADDR}
ENVIRONMENT=${ENVIRONMENT}
POSTGRES_DSN=${POSTGRES_DSN}
REDIS_ADDR=${REDIS_ADDR}
REDIS_PASSWORD=${REDIS_PASSWORD}
KAFKA_BROKERS=${KAFKA_BROKERS}
KAFKA_TOPIC=${KAFKA_TOPIC}
TRADING_PAIRS=${TRADING_PAIRS}
ALCHEMY_API_KEY=${ALCHEMY_API_KEY:-}
LOG_LEVEL=${LOG_LEVEL}
LOG_FORMAT=${LOG_FORMAT}
ENABLE_REDIS_RATE_LIMIT=${ENABLE_REDIS_RATE_LIMIT:-false}
EOF
    log_ok ".env file written"
}

start_infra() {
    log_info "Starting infrastructure (PostgreSQL, Redis, Kafka)..."
    $COMPOSE_CMD -f "$COMPOSE_FILE" up -d --wait --wait-timeout 120 2>&1 || {
        log_warn "Timeout — checking container health..."
        $COMPOSE_CMD -f "$COMPOSE_FILE" ps
    }
    log_ok "Infrastructure services started"
}

run_migrations() {
    log_info "Running database migrations..."
    (cd "$PROJECT_ROOT" && POSTGRES_DSN="$POSTGRES_DSN" go run ./scripts/migrate/main.go)
    log_ok "Migrations complete"
}

build_services() {
    log_info "Building microservices..."
    mkdir -p "$BUILD_DIR"
    (cd "$PROJECT_ROOT" && \
        CGO_ENABLED=0 go build -ldflags='-s -w' -o "$BUILD_DIR/api-gateway" ./cmd/api-gateway/ && \
        CGO_ENABLED=0 go build -ldflags='-s -w' -o "$BUILD_DIR/matching-engine" ./cmd/matching-engine/ && \
        CGO_ENABLED=0 go build -ldflags='-s -w' -o "$BUILD_DIR/wallet-service" ./cmd/wallet-service/)
    log_ok "All services built successfully"
}

start_services() {
    for proc in matching-engine api-gateway wallet-service; do pkill -f "$BUILD_DIR/$proc" 2>/dev/null || true; done
    sleep 1
    log_info "Starting Matching Engine..."
    (cd "$PROJECT_ROOT" && "$BUILD_DIR/matching-engine" &>/tmp/fliance-engine.log) &
    echo $! > /tmp/fliance-engine.pid
    sleep 2
    log_info "Starting API Gateway..."
    (cd "$PROJECT_ROOT" && "$BUILD_DIR/api-gateway" &>/tmp/fliance-api.log) &
    echo $! > /tmp/fliance-api.pid
    sleep 1
    log_info "Starting Wallet Service..."
    (cd "$PROJECT_ROOT" && "$BUILD_DIR/wallet-service" &>/tmp/fliance-wallet.log) &
    echo $! > /tmp/fliance-wallet.pid
    log_ok "All services started"
}

health_check() {
    echo ""; log_info "Running health checks..."; sleep 3
    local ok=0 url name ep
    # 用 | 做分隔符（URL 内含 :，不能用 : 切分）
    for ep in "http://localhost:8080/health|API Gateway" "http://localhost:8082/health|Wallet Service"; do
        url="${ep%%|*}"; name="${ep##*|}"
        if curl -sf "$url" &>/dev/null; then log_ok "$name — $url"; else log_warn "$name — not ready"; ok=1; fi
    done
    if [ -f /tmp/fliance-engine.pid ] && kill -0 "$(cat /tmp/fliance-engine.pid)" 2>/dev/null; then log_ok "Matching Engine — running"; else log_warn "Matching Engine — check logs"; ok=1; fi
    [ "$ok" -eq 0 ] && log_ok "All services healthy!" || log_warn "Some services not ready — check /tmp/fliance-*.log"
}

# ── Main ────────────────────────────────────────────────────────────────
main() {
    case "${1:-start}" in
        start)
            preflight
            write_env
            start_infra
            run_migrations
            build_services
            start_services
            health_check
            echo ""
            echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
            echo -e "${GREEN}   Fliance（梵响） — Dev Deployment Complete${NC}"
            echo -e "${GREEN}═══════════════════════════════════════════════════════════${NC}"
            echo ""
            echo "  REST API:       http://localhost:8080"
            echo "  WebSocket:      ws://localhost:8080/ws"
            echo "  Wallet Service: http://localhost:8082"
            echo ""
            ;;
        prod)
            prod_deploy
            ;;
        ssl-init)
            ssl_init
            ;;
        stop)
            log_info "Stopping all services..."
            for pid_file in /tmp/fliance-*.pid; do [ -f "$pid_file" ] && kill "$(cat "$pid_file")" 2>/dev/null || true; rm -f "$pid_file"; done
            log_ok "Services stopped"
            ;;
        restart)
            main stop; sleep 2; main start
            ;;
        logs)
            for name in engine api wallet; do
                echo "=== Matching $name ==="; tail -50 "/tmp/fliance-${name}.log" 2>/dev/null || echo "(no logs)"
            done
            ;;
        status)
            for pid_file in /tmp/fliance-*.pid; do
                local name=$(basename "$pid_file" .pid)
                [ -f "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null && echo "  $name: RUNNING" || echo "  $name: STOPPED"
            done
            echo ""
            echo "Docker Containers:"
            $COMPOSE_CMD -f "$COMPOSE_FILE" ps 2>/dev/null || echo "  (not running)"
            ;;
        infra-up)
            preflight; start_infra
            ;;
        infra-down)
            $COMPOSE_CMD -f "$COMPOSE_FILE" down
            ;;
        *)
            echo "Usage: $0 {start|prod|stop|restart|logs|status|ssl-init|infra-up|infra-down}"
            echo ""
            echo "  start      Full development deployment"
            echo "  prod       Full production deployment (Docker + Nginx + SSL)"
            echo "  ssl-init   Initialize Let's Encrypt SSL (requires DOMAIN= env)"
            echo "  stop       Stop all services"
            echo "  restart    Restart all services"
            echo "  logs       Tail service logs"
            echo "  status     Check service and infra status"
            echo "  infra-up   Start infrastructure only"
            echo "  infra-down Stop infrastructure"
            exit 1
            ;;
    esac
}

main "$@"