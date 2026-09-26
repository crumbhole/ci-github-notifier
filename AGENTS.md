# AGENTS.md

This file provides guidance to coding agents working with code in this repository.

## What this is

A single Go binary, shipped as a `scratch` container image, that posts CI state to GitHub. It posts either a commit status (default) or a check run (`api=checks`). It is configured entirely through environment variables. Their lowercase names (`state`, `git_sha`, `app_repo`, `tokenFile`, ...) are the public interface documented in `README.md`, so renaming one is a breaking change.

## Commands

The Go module lives in `ci-github-notifier/`, not at the repo root. The Makefile `cd`s into it.

```bash
make lint    # golangci-lint v2 against .golangci.yaml (same config and pinned version as CI)
make build   # lint, then go build
make test    # go test ./...

# Single test
cd ci-github-notifier && go test -run TestNotifyPostsCommitStatus ./...
```

`make lint` keeps a separate lint and Go build cache for each worktree, so linting in several worktrees doesn't report phantom issues from a sibling's paths. Keep the golangci-lint version in `.github/workflows/ci.yaml` in step with the local tool. The Go toolchain version comes from `go.mod` and must match the `golang` base image in the `Dockerfile`.

## Architecture

Everything is in `package main`, in two layers:

- **Loading (`config.go`)**: the only code that reads the environment. It builds two structs up front:
  - `notification`: what to report and where. This includes `api` and the check run ID threading.
  - `credentials`: a GitHub App (`appID` plus a parsed RSA key) or an access token, never both.

  The two are kept separate on purpose. A planned Argo Workflows executor-plugin mode will fill `notification` per request from JSON while `credentials` stay in the sidecar's environment. Don't let code outside `config.go` read env vars.
- **Acting (`notify.go`, `checks.go`, `appauth.go`)**: `notify(client, notification, credentials)` rejects checks mode without App credentials and resolves the token. It then posts either a status or a check run. With App credentials, `appauth.go` signs an App JWT and looks up the installation from owner/repo. It then mints an installation token scoped to that one repo and one permission (`statuses: write` or `checks: write`). An access token that parses as a JWT is sent with `Bearer`, and anything else with `token`. These functions return errors. Only `main` exits.

Design rules visible throughout the code and tests:
- Configuration mistakes fail loudly rather than falling back. Examples: partial App config, an unknown `api` value, checks mode with a PAT.
- Error messages name the environment variable the user needs to fix.

## Testing conventions

- Tests stub GitHub with `httptest.NewTLSServer` and point the code at it by setting `apiHost` (the `gh_url` env var) to the server's host, because the scheme is hardcoded to `https://`. Use `req.C().EnableInsecureSkipVerify()` with those stubs.
- Tests of logic build `notification`/`credentials` structs directly. Only the tests for the `config.go` loaders use `t.Setenv`.
- `testKeyPEM` (in `appauth_test.go`) generates a throwaway App key.

## Lint gotchas

`.golangci.yaml` is strict. The linters that most often fire on new code:
- `wrapcheck`: wrap errors from external packages with `fmt.Errorf("...: %w", err)`.
- `govet` shadow and `fieldalignment`: don't redeclare `err` in a nested scope, and order struct fields to minimise padding.
- `godot`: comments end with a period.
- `gosec`: reading paths supplied by the operator is intentional. Suppress it with a `// #nosec G304 G703 -- reason` comment, as the existing code does.

## Container

`Dockerfile` builds a static binary into `scratch`. The image has only CA certificates and an empty world-writable `/tmp`. The `/tmp` exists because `check_run_id_file=/tmp/check_run_id` is the documented pattern, and without it the write fails after the check run has already been created. CI builds and pushes `ghcr.io/crumbhole/ci-github-notifier:latest` for amd64 and arm64 on pushes to `main`. `examples/argo-workflows/` holds the Argo usage examples referenced from the README.
