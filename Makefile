GO       ?= go
BIN      := bin
EXE      := $(shell $(GO) env GOEXE)
SERVICES := gateway sync outbox-relay worker
COMPOSE  := docker compose -f deploy/docker-compose.yml

.DEFAULT_GOAL := help

FUZZTIME ?= 30s

.PHONY: help build wasm test test-race cover bench fuzz lint fmt proto up migrate down reset logs clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

build: ## Build every service binary into bin/
	@for s in $(SERVICES); do echo "build $$s"; $(GO) build -trimpath -o $(BIN)/$$s$(EXE) ./cmd/$$s || exit 1; done

wasm: ## Build the CRDT WebAssembly module into bin/crdt.wasm
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/crdt.wasm ./cmd/crdt-wasm

test: ## Run all Go tests
	$(GO) test -count=1 ./...

test-race: ## Run all Go tests with the race detector (needs cgo)
	$(GO) test -race -count=1 ./...

cover: ## Run tests with coverage; writes coverage.out and prints the total
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

bench: ## Run benchmarks (no tests)
	$(GO) test -run '^$$' -bench . -benchmem ./...

fuzz: ## Fuzz the CRDT for FUZZTIME per target (default 30s)
	$(GO) test -run '^$$' -fuzz '^FuzzApply$$' -fuzztime $(FUZZTIME) ./internal/crdt
	$(GO) test -run '^$$' -fuzz '^FuzzSession$$' -fuzztime $(FUZZTIME) ./internal/crdt

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format Go code
	golangci-lint fmt ./...

proto: ## Lint .proto files and generate Go code into gen/ (needs buf)
	buf lint
	buf generate

up: ## Start Postgres, Redis and Redpanda, then apply migrations
	$(COMPOSE) up -d --wait postgres redis redpanda
	$(MAKE) migrate

migrate: ## Apply database migrations
	$(COMPOSE) run --rm migrate

down: ## Stop local infrastructure (keeps data)
	$(COMPOSE) down

reset: ## Stop local infrastructure and delete its data
	$(COMPOSE) down -v

logs: ## Follow infrastructure logs
	$(COMPOSE) logs -f

clean: ## Remove build output
	rm -rf $(BIN)
