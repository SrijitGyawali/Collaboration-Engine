GO       ?= go
BIN      := bin
EXE      := $(shell $(GO) env GOEXE)
SERVICES := gateway sync outbox-relay worker
COMPOSE  := docker compose -f deploy/docker-compose.yml

.DEFAULT_GOAL := help

FUZZTIME ?= 30s
# buf runs at a pinned version through go run: nothing to install. The protoc
# plugins it calls are pinned as tools in go.mod (see buf.gen.yaml).
BUF      := $(GO) run github.com/bufbuild/buf/cmd/buf@v1.75.0

.PHONY: help build wasm web-wasm web-install web-test web-dev test test-short test-race cover bench fuzz lint fmt proto proto-check up migrate down reset logs clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

build: ## Build every service binary into bin/
	@for s in $(SERVICES); do echo "build $$s"; $(GO) build -trimpath -o $(BIN)/$$s$(EXE) ./cmd/$$s || exit 1; done

wasm: ## Build the CRDT WebAssembly module into bin/crdt.wasm
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/crdt.wasm ./cmd/crdt-wasm

# wasm_exec.js is Go's JavaScript glue for WebAssembly. It must come from the
# same Go version that built the module, so it is copied, never committed.
web-wasm: wasm ## Copy the CRDT module and Go's wasm_exec.js into web/public
	mkdir -p web/public
	cp $(BIN)/crdt.wasm web/public/crdt.wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" web/public/wasm_exec.js

web-install: ## Install the web app's npm dependencies (exactly as locked)
	cd web && npm ci

web-test: web-wasm ## Rebuild the module and run the web app's tests
	cd web && npm test

web-dev: web-wasm ## Rebuild the module and start the web dev server on :3000
	cd web && npm run dev

test: ## Run all Go tests (the store's need Docker, and are skipped without it)
	$(GO) test -count=1 ./...

test-short: ## Run Go tests that need no Docker, with fewer random seeds
	$(GO) test -count=1 -short ./...

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
	$(GO) test -run '^$$' -fuzz '^FuzzPositionEdits$$' -fuzztime $(FUZZTIME) ./internal/crdt

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format Go code
	golangci-lint fmt ./...

proto: ## Lint the .proto files and regenerate the Go code in gen/
	$(BUF) lint
	$(BUF) generate

proto-check: proto ## Fail if gen/ is not what the .proto files produce
	git diff --exit-code -- gen/

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
