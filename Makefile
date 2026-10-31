GO       ?= go
BIN      := bin
EXE      := $(shell $(GO) env GOEXE)
SERVICES := gateway sync outbox-relay worker
COMPOSE  := docker compose -f deploy/docker-compose.yml

.DEFAULT_GOAL := help

.PHONY: help build wasm test test-race lint fmt proto up migrate down reset logs clean

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
