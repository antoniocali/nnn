BINARY     := nnn
CMD        := ./cmd/nnn
MODULE     := $(shell go list -m)
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-s -w \
               -X main.version=$(VERSION) \
               -X main.commit=$(COMMIT) \
               -X main.date=$(DATE)"

INSTALL_DIR := $(HOME)/.local/bin

.DEFAULT_GOAL := help

# ── Build ─────────────────────────────────────────────────────────────────────

.PHONY: build
build: ## Build the binary for the current platform
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

.PHONY: build-all
build-all: ## Cross-compile for common platforms
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64   $(CMD)
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)-linux-arm64   $(CMD)
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64  $(CMD)
	GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe $(CMD)

# ── Install / Uninstall ───────────────────────────────────────────────────────

.PHONY: install
install: build ## Install binary to $(INSTALL_DIR)
	@mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"

.PHONY: uninstall
uninstall: ## Remove installed binary
	rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"

# ── Dev ───────────────────────────────────────────────────────────────────────

.PHONY: run
run: build ## Build and run the TUI
	./$(BINARY)

.PHONY: watch
watch: ## Rebuild and run on source changes (requires entr)
	@which entr > /dev/null || (echo "install entr: brew install entr" && exit 1)
	find . -name '*.go' | entr -r make run

# ── Test ──────────────────────────────────────────────────────────────────────

.PHONY: test
test: ## Run all tests
	go test ./... -v -race

.PHONY: test-short
test-short: ## Run tests without race detector (faster)
	go test ./...

.PHONY: cover
cover: ## Run tests with coverage report
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ── Code quality ──────────────────────────────────────────────────────────────

.PHONY: lint
lint: ## Run golangci-lint (must be installed)
	@which golangci-lint > /dev/null || (echo "install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format all Go source files
	gofmt -s -w .
	goimports -w . 2>/dev/null || true

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check
check: fmt vet test ## Run fmt + vet + tests

# ── Dependencies ──────────────────────────────────────────────────────────────

.PHONY: deps
deps: ## Download and tidy dependencies
	go mod download
	go mod tidy

.PHONY: deps-upgrade
deps-upgrade: ## Upgrade all dependencies to latest
	go get -u ./...
	go mod tidy

# ── Clean ─────────────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist/
	rm -f coverage.out coverage.html

# ── Info ──────────────────────────────────────────────────────────────────────

.PHONY: version
version: ## Print version info
	@echo "version : $(VERSION)"
	@echo "commit  : $(COMMIT)"
	@echo "date    : $(DATE)"
	@echo "module  : $(MODULE)"

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
