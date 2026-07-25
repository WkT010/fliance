.PHONY: build test lint run-engine run-api run-wallet migrate infra-up infra-down clean bench

APP_NAME := nexa-exchange
GO       := go
BUILD_DIR := build

build: $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/api-gateway ./cmd/api-gateway/
	$(GO) build -o $(BUILD_DIR)/matching-engine ./cmd/matching-engine/
	$(GO) build -o $(BUILD_DIR)/wallet-service ./cmd/wallet-service/

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

test:
	$(GO) test -race -count=1 -v ./...

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

infra-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

infra-down:
	docker compose -f deploy/docker/docker-compose.yml down

clean:
	rm -rf $(BUILD_DIR)
