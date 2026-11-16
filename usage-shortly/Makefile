BINARY := usage-shortly
BUILD_DIR := bin
BINDIR ?= $(HOME)/.local/bin
GO ?= go

.DEFAULT_GOAL := build
.PHONY: help build install uninstall test vet fmt check clean

help: ## Show available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

build: ## Build ./bin/usage-shortly
	$(GO) build -trimpath -o $(BUILD_DIR)/$(BINARY) ./cmd/usage-shortly

install: build ## Install usage-shortly into ~/.local/bin (override with BINDIR=...)
	install -d $(BINDIR)
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(BINDIR)/$(BINARY)
	@echo "installed $(BINDIR)/$(BINARY)"

uninstall: ## Remove usage-shortly from ~/.local/bin (or BINDIR=...)
	rm -f $(BINDIR)/$(BINARY)

test: ## Run all tests with the race detector
	$(GO) test -race ./...

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format all Go files
	gofmt -w .

check: vet test ## Fail on unformatted files, then vet and test
	@test -z "$$(gofmt -l .)" || { echo "unformatted files:"; gofmt -l .; exit 1; }

clean: ## Remove build output
	rm -rf $(BUILD_DIR)
