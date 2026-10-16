# NEXUS — Developer Makefile (M0 foundation)
#
# All targets are reproducible and depend only on the Go toolchain and the
# standard library. No third-party dependencies are required.

GO ?= go
# Build-time identity.
VERSION ?= 0.0.0-dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  = -X github.com/Nomssky/NEXUS/internal/foundation/version.Version=$(VERSION)
LDFLAGS += -X github.com/Nomssky/NEXUS/internal/foundation/version.Commit=$(COMMIT)

.PHONY: help build build-backup build-retention release release-verify smoke run test test-race cover vet lint fmt fmt-check check clean

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Compile all packages into ./bin/nexus (dev; -trimpath).
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/nexus ./cmd/nexus

build-backup: ## Compile the backup/restore operator tool into ./bin/nexus-backup.
	$(GO) build -trimpath -o bin/nexus-backup ./cmd/nexus-backup

build-retention: ## Compile the execution-journal retention tool into ./bin/nexus-retention.
	$(GO) build -trimpath -o bin/nexus-retention ./cmd/nexus-retention

release: ## Build versioned release artifacts + checksums into ./dist (VERSION=... optional).
	@bash scripts/release.sh $(if $(filter command line,$(origin VERSION)),$(VERSION),)

release-verify: ## Verify ./dist SHA-256 checksums, manifest and inventory.
	@bash scripts/release-verify.sh $(if $(filter command line,$(origin VERSION)),$(VERSION),)

smoke: ## Isolated release smoke test (build → boot → backup → restore → reopen).
	@bash scripts/smoke.sh

run: ## Run the process locally.
	$(GO) run ./cmd/nexus

test: ## Run the full test suite.
	$(GO) test ./...

test-race: ## Run tests with the race detector.
	$(GO) test -race ./...

cover: ## Run tests with coverage profile and summary.
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

vet: ## Run static analysis (go vet).
	$(GO) vet ./...

lint: vet ## Alias for vet — runs static analysis.

fmt: ## Format all Go source.
	gofmt -w .

fmt-check: ## Fail if any file is not gofmt-clean.
	@files=$$(gofmt -l .); if [ -n "$$files" ]; then \
	  echo "not gofmt-clean:"; echo "$$files"; exit 1; fi

check: fmt-check vet build build-backup build-retention test ## Full local quality gate.

clean: ## Remove build artifacts (bin/ and dist/).
	rm -rf bin dist
