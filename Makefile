# Per-worktree cache root, keyed by a hash of this worktree's toplevel
# path. golangci-lint loads packages through go/packages, which reads
# the Go build cache (GOCACHE); cached package data carries the
# absolute file paths of whichever worktree populated the cache first.
# With a single shared cache, linting in one worktree then another
# reports phantom issues pointing at the sibling worktree's paths.
# Isolating GOCACHE and GOLANGCI_LINT_CACHE per worktree makes `make
# lint` immune to this cross-worktree contamination.
# sha256sum (coreutils) on Linux/CI images; shasum (perl) covers macOS,
# which ships without sha256sum.
LINT_CACHE_ROOT := $(or $(XDG_CACHE_HOME),$(HOME)/.cache)/ci-github-notifier-lint/$(shell git rev-parse --show-toplevel | { sha256sum 2>/dev/null || shasum; } | cut -c1-16)

MODULE_DIR := ci-github-notifier

.PHONY: lint build test

# Lint Go sources against .golangci.yaml. Runs before build so a
# failing lint blocks the build (locally and in CI).
lint: export GOCACHE = $(LINT_CACHE_ROOT)/go-build
lint: export GOLANGCI_LINT_CACHE = $(LINT_CACHE_ROOT)/golangci-lint
lint:
	cd $(MODULE_DIR) && golangci-lint run ./...

build: lint
	cd $(MODULE_DIR) && go build ./...

test:
	cd $(MODULE_DIR) && go test ./...
