#!/usr/bin/env bash
# ============================================================================
# NEXA Exchange — Let's Encrypt SSL Certificate Initializer
# ============================================================================
# Prerequisites:
#   - Domain pointing to this server's public IP
#   - Ports 80 and 443 publicly accessible
#   - Docker and docker-compose installed
#
# Usage:
#   export DOMAIN=exchange.nexa.com
#   export EMAIL=admin@nexa.com
#   ./deploy/ssl/init-letsencrypt.sh
# ============================================================================

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
log_info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

DOMAIN="${DOMAIN:-}"
EMAIL="${EMAIL:-admin@nexa.com}"
DATA_DIR="./deploy/ssl/certbot"

if [ -z "$DOMAIN" ]; then
    log_error "DOMAIN is not set. Usage: export DOMAIN=exchange.nexa.com && $0"
    exit 1
fi

log_info "Initializing Let's Encrypt SSL for domain: $DOMAIN"
log_info "Email: $EMAIL"

# Create directories for certbot data
mkdir -p "$DATA_DIR/conf"
mkdir -p "$DATA_DIR/www"

# Start a temporary nginx just for certbot challenge
log_info "Starting temporary nginx for SSL challenge..."
docker run --rm -d \
    --name nexa-ssl-init \
    -p 80:80 \
    -v "$(pwd)/$DATA_DIR/www:/var/www/certbot" \
    nginx:1.27-alpine \
    sh -c "echo 'server { listen 80; location / { root /var/www/certbot; } }' > /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"

sleep 2

# Obtain certificate
log_info "Obtaining SSL certificate from Let's Encrypt..."
docker run --rm \
    -v "$(pwd)/$DATA_DIR/conf:/etc/letsencrypt" \
    -v "$(pwd)/$DATA_DIR/www:/var/www/certbot" \
    certbot/certbot:v2 \
    certonly --webroot -w /var/www/certbot \
    -d "$DOMAIN" \
    --email "$EMAIL" \
    --agree-tos \
    --non-interactive \
    --force-renewal

# Stop temp nginx
docker stop nexa-ssl-init 2>/dev/null || true

# Copy certificates to the deployment location
log_info "Copying certificates to deploy/ssl/certs/..."
mkdir -p deploy/ssl/certs
cp "$DATA_DIR/conf/live/$DOMAIN/fullchain.pem" deploy/ssl/certs/
cp "$DATA_DIR/conf/live/$DOMAIN/privkey.pem" deploy/ssl/certs/
cp "$DATA_DIR/conf/live/$DOMAIN/chain.pem" deploy/ssl/certs/

log_ok "SSL certificates obtained successfully!"
log_info "Certificate location: deploy/ssl/certs/"
log_info ""
log_info "Next steps:"
log_info "  1. Set DOMAIN=$DOMAIN in .env"
log_info "  2. Run: ./deploy.sh prod"
log_info "  3. Set up auto-renewal (crontab):"
log_info "     0 0 * * * cd $(pwd) && docker compose -f deploy/docker/docker-compose.prod.yml run --rm certbot renew && docker exec nexa-nginx nginx -s reload"