.PHONY: build test lint run-engine run-api run-wallet migrate infra-up infra-down clean bench proto

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

.DEFAULT_GOAL := build
