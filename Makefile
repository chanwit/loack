# loack — Makefile

BIN_DIR := bin
BIN     := $(BIN_DIR)/loack
PKG     := ./cmd/loack
GO      ?= go

# Version stamped into the binary (override: make build VERSION=v0.2.0)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: vendor
vendor: ## Clone/checkout controllers pinned in controllers.lock (+ verify runtime)
	./hack/vendor-controllers.sh

.PHONY: vendor-verify
vendor-verify: ## Verify vendored controllers are pinned and share one runtime
	./hack/vendor-controllers.sh --verify-only

.PHONY: vendor-relock
vendor-relock: ## Re-pin controllers.lock to current clone HEADs (if runtime matches)
	./hack/vendor-controllers.sh --relock

.PHONY: build
build: ## Build the loack core (split; links no controllers) into bin/loack
	@mkdir -p $(BIN_DIR)
	$(GO) build -tags split -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

.PHONY: aio
aio: ## Build the all-in-one binary (every controller in-process) into bin/loack-aio
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/loack-aio $(PKG)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/loack-provider ./cmd/loack-provider

.PHONY: provider-%
provider-%: ## Build one standalone provider module: make provider-iam
	@mkdir -p $(BIN_DIR)
	cd providers/$* && $(GO) build -o ../../$(BIN_DIR)/loack-provider-$* .

.PHONY: providers
providers: ## Build every standalone provider module into bin/
	@mkdir -p $(BIN_DIR)
	@for d in providers/*/; do \
		svc=$$(basename $$d); \
		echo "building loack-provider-$$svc"; \
		(cd $$d && $(GO) build -o ../../$(BIN_DIR)/loack-provider-$$svc .) || exit 1; \
	done

.PHONY: install
install: ## Install the loack core into $GOBIN / $GOPATH/bin
	$(GO) install -tags split -ldflags "$(LDFLAGS)" $(PKG)

.PHONY: test
test: ## Run unit tests
	$(GO) test ./...

.PHONY: vet
vet: ## Run go vet (both build tags: all-in-one + the core)
	$(GO) vet ./...
	$(GO) vet -tags split ./cmd/loack

.PHONY: fmt
fmt: ## Format the code
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: check
check: vendor-verify vet test ## Verify vendored controllers + vet + test

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
