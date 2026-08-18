# orrery — standard library only, so there is nothing to fetch.

BINDIR      := bin
CMDS        := orreryd orreryctl
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-alpha")
COMMIT      ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILT_AT    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG         := github.com/msaaqib20/orrery
LDFLAGS     := -X $(PKG)/internal/version.Version=$(VERSION) \
               -X $(PKG)/internal/version.Commit=$(COMMIT) \
               -X $(PKG)/internal/version.BuiltAt=$(BUILT_AT)

GO          ?= go

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build every binary into ./bin
	@mkdir -p $(BINDIR)
	@for cmd in $(CMDS); do \
		echo "building $$cmd"; \
		$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/$$cmd ./cmd/$$cmd || exit 1; \
	done

.PHONY: install
install: ## Install both binaries into GOBIN
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/...

.PHONY: run
run: build ## Build and run the daemon
	$(BINDIR)/orreryd

.PHONY: test
test: ## Run the test suite
	$(GO) test ./...

.PHONY: race
race: ## Run the test suite under the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Produce a coverage profile and print the summary
	$(GO) test -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -run '^$$' -bench . -benchmem ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the tree
	$(GO) fmt ./...

.PHONY: fmtcheck
fmtcheck: ## Fail if anything is unformatted
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "these files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy the module (should be a no-op; there are no dependencies)
	$(GO) mod tidy

.PHONY: check
check: fmtcheck vet race ## The full pre-push gate

.PHONY: clean
clean: ## Remove build and test artefacts
	rm -rf $(BINDIR) coverage.out data
