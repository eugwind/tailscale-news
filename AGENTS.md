# AGENTS.md — Tailscale News Aggregator

<!--
  AGENTS.md is recognized by GitHub Copilot, Claude Code, Codex and other AI agents.
  It is the always-on instruction file for this repository and mirrors
  .github/copilot-instructions.md, which stays the canonical source.
  Detailed Go rules: .github/instructions/go-standards.instructions.md
-->

## Project Overview

A Go service that collects, de-duplicates, ranks, and serves Tailscale-related news
from RSS/Atom feeds, blogs, release notes, and community sources. It exposes a small
HTTP API plus server-rendered HTML, and runs as a single static binary.

## Environment

- Go 1.26, target `linux/amd64`, developed in WSL2 on Windows
- POSIX paths only — never emit Windows paths or `\` separators
- Standard toolchain only: `go build ./...`, `go test ./...`, `go run ./cmd/...`
- Release build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/...`

## Key Conventions

- Module path `github.com/eugwind/tailscale-news`; code lives in `cmd/` (wiring) and `internal/` (everything else)
- Standard library first — propose a dependency explicitly, with a reason, before importing it
- Errors are values: wrap with `%w`, never panic outside `main`
- `log/slog` for structured logging; a `*slog.Logger` is injected, never global
- `context.Context` first parameter on every I/O-bound function; bounded concurrency via `errgroup`
- `net/http` + `http.ServeMux`; explicit server timeouts; `html/template` for HTML
- Table-driven tests with the stdlib `testing` package; fixtures in `testdata/`
- Secrets and feed credentials come from environment variables only — never hard-coded, never logged

## Agent-Specific Guidance

- Verify changes with the quality gate before reporting done:
  `gofmt -l .` · `go vet ./...` · `go build ./...` · `go test -race ./...` · `govulncheck ./...`
- Add or update table-driven tests alongside any behaviour change
- Add doc comments on every exported identifier; comments explain *why*, not *what*
- When touching feed parsing, add a fixture under `testdata/` rather than hitting the network in tests
- Never call live external feeds from tests — use `httptest.Server`
- Do not generate C#/.NET, Node, or Python solutions for application code
- Prefer editing existing files over creating new ones; do not add summary markdown files unless asked
