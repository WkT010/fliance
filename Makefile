# ============================================================================
# Fliance（梵响） — 构建与运维入口
# 用法：make build / make test / make migrate / make install / make start /
#       make stop / make deploy
#   - Windows：install/start/stop 分别转发到 install.ps1 / start-dev.ps1 /
#     stop-dev.ps1（PowerShell 5.1）。
#   - Linux：install/start/stop 转发到 ./deploy.sh（开发全栈：infra 容器 +
#     迁移 + 构建 + 启动 + 健康检查）；deploy 为 Ubuntu 生产部署
#     （需 root 与 DOMAIN= 域名，见 scripts/deploy-ubuntu.sh 头部注释）。
# ============================================================================
.PHONY: build build-all test test-short bench lint run-engine run-api run-wallet \
        migrate infra-up infra-down clean proto install start stop deploy

APP_NAME   := nexa-exchange
GO         := go
BUILD_DIR  := build

build: $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/api-gateway ./cmd/api-gateway/
	$(GO) build -o $(BUILD_DIR)/matching-engine ./cmd/matching-engine/
	$(GO) build -o $(BUILD_DIR)/wallet-service ./cmd/wallet-service/

build-all: build
	$(GO) build -o $(BUILD_DIR)/migrate ./scripts/migrate/

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

test:
	$(GO) test -race -count=1 -v ./...

test-short:
	$(GO) test -short -count=1 ./...

bench:
	$(GO) test -bench=. -benchmem ./internal/matching/...

lint:
	golangci-lint run ./...

run-engine:
	$(GO) run ./cmd/matching-engine/main.go

run-api:
	$(GO) run ./cmd/api-gateway/main.go

run-wallet:
	$(GO) run ./cmd/wallet-service/main.go

migrate:
	$(GO) run ./scripts/migrate/main.go

infra-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

infra-down:
	docker compose -f deploy/docker/docker-compose.yml down

clean:
	rm -rf $(BUILD_DIR)

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/exchange/v1/exchange.proto

# ── 一键入口（与根目录脚本保持一致） ──────────────────────────────────
ifeq ($(OS),Windows_NT)
install:
	powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1

start:
	powershell -NoProfile -ExecutionPolicy Bypass -File start-dev.ps1

stop:
	powershell -NoProfile -ExecutionPolicy Bypass -File stop-dev.ps1
else
# Linux 开发环境一键：infra(docker compose) → 迁移 → 构建 → 启动 → 健康检查
install:
	./deploy.sh start

start:
	./deploy.sh start

stop:
	./deploy.sh stop
endif

# Ubuntu 生产部署（需 root；必须传域名：sudo DOMAIN=exchange.example.com make deploy）
deploy:
	sudo DOMAIN=$(DOMAIN) bash scripts/deploy-ubuntu.sh

.DEFAULT_GOAL := build
