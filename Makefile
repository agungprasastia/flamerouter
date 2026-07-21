.PHONY: help serve version build test vet tidy ui-install ui-dev ui-build all clean

GO       ?= go
WEB      ?= web
BINARY   ?= flamerouter
ifeq ($(OS),Windows_NT)
BINARY   := flamerouter.exe
endif

help: ## Show targets
	@echo "FlameRouter — common targets:"
	@echo "  make serve      - run gateway (go run ./cmd/flamerouter serve)"
	@echo "  make version    - print version"
	@echo "  make build      - build binary to ./$(BINARY)"
	@echo "  make test       - go test ./..."
	@echo "  make vet        - go vet ./..."
	@echo "  make tidy       - go mod tidy"
	@echo "  make ui-install - npm install in web/"
	@echo "  make ui-dev     - Vite dev server (proxy to :20128)"
	@echo "  make ui-build   - build SPA into internal/gateway/ui/dist"
	@echo "  make all        - vet + test + ui-build + build"
	@echo "  make clean      - remove local binary"

serve: ## Run server
	$(GO) run ./cmd/flamerouter serve

version: ## Print version
	$(GO) run ./cmd/flamerouter version

build: ## Compile binary
	$(GO) build -o $(BINARY) ./cmd/flamerouter

test: ## Run all Go tests
	$(GO) test ./... -count=1

vet: ## Static analysis
	$(GO) vet ./...

tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

ui-install: ## Install frontend deps
	cd $(WEB) && npm install

ui-dev: ## Frontend dev server
	cd $(WEB) && npm run dev

ui-build: ## Build & embed dashboard assets
	cd $(WEB) && npm run build

all: vet test ui-build build ## Full local CI-ish pipeline

clean: ## Remove built binary
	rm -f flamerouter flamerouter.exe 2>/dev/null || true
	-del /Q flamerouter.exe 2>nul || true
