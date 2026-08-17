.PHONY: help serve build test test-race lint nilaway vulncheck check tidy ui-install ui-dev ui-build clean

GO       ?= go
BINARY   ?= flamerouter
ifeq ($(OS),Windows_NT)
BINARY   := flamerouter.exe
endif

help: ## Show targets
	@echo "FlameRouter — available targets:"
	@echo "  make serve      - run gateway (go run ./cmd/flamerouter serve)"
	@echo "  make build      - build binary to ./$(BINARY)"
	@echo "  make test       - go test ./... -count=1"
	@echo "  make test-race  - go test ./... -race -count=1 -shuffle=on"
	@echo "  make lint       - golangci-lint run --config .golangci.yml ./..."
	@echo "  make nilaway    - nilaway ./..."
	@echo "  make vulncheck  - govulncheck ./..."
	@echo "  make check      - run all quality gates (lint + nilaway + vulncheck + test-race)"
	@echo "  make tidy       - go mod tidy"
	@echo "  make ui-install - npm install"
	@echo "  make ui-dev     - npm run dev"
	@echo "  make ui-build   - npm run build"
	@echo "  make clean      - remove local binary"

serve: ## Run server
	$(GO) run ./cmd/flamerouter serve

build: ## Compile binary
	$(GO) build -o $(BINARY) ./cmd/flamerouter

test: ## Run all Go tests
	$(GO) test ./... -count=1

test-race: ## Run tests with race detector and shuffle
	$(GO) test ./... -race -count=1 -shuffle=on

lint: ## Run golangci-lint
	golangci-lint run --config .golangci.yml ./...

nilaway: ## Run nilaway static analysis
	nilaway ./...

vulncheck: ## Run govulncheck
	govulncheck ./...

check: lint nilaway vulncheck test-race ## Run full quality gates

tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

ui-install: ## Install frontend deps
	npm install

ui-dev: ## Frontend dev server
	npm run dev

ui-build: ## Build frontend
	npm run build

clean: ## Remove built binary
	rm -f flamerouter flamerouter.exe 2>/dev/null || true
	-del /Q flamerouter.exe 2>nul || true
