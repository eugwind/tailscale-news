# Copilot Instructions — Tailscale News Aggregator

Applied to every Copilot Chat request in this workspace.

## Project

A news aggregator that collects, de-duplicates, and serves Tailscale-related
news from RSS/Atom feeds, blogs, release notes, and community sources.

## Language & Runtime

- Primary language: **Go 1.26** (`go.mod` pins the current patch, e.g. `go 1.26.8` — keep it on the latest patch)
- Target platform: **linux/amd64**, developed under **WSL2** on Windows
- Build with the standard toolchain: `go build ./...`, `go test ./...`
- Keep all paths POSIX-style; never emit Windows paths or `\` separators
- Standard library first — add a dependency only when it removes real complexity
- Release build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/...`

## Project Layout

```
cmd/<binary>/main.go      program entry points, wiring only
internal/                 all application packages (not importable externally)
  feed/                   fetching and parsing sources
  store/                  persistence
  aggregate/              dedup, ranking, scheduling
  httpapi/                HTTP handlers and routing
pkg/                      only for code intentionally shared outside this repo
testdata/                 fixtures (per package)
```

- Module path: `github.com/eugwind/tailscale-news`
- Default to `internal/`; use `pkg/` only when external reuse is intended
- `main.go` does configuration, dependency wiring, and signal handling — no business logic

## Conventions

- Detailed Go coding rules live in [instructions/go-standards.instructions.md](instructions/go-standards.instructions.md) and apply to every `.go` file
- Idiomatic Go over clever Go; follow Effective Go and the Go Code Review Comments
- Constructor injection via plain structs and interfaces — no DI frameworks
- Small, consumer-defined interfaces declared where they are used, not next to the implementation
- Configuration through environment variables parsed once into a `Config` struct at startup

## Errors & Logging

- Errors are values: return and wrap them with `%w`, never panic outside `main`
- Structured logging with `log/slog`; pass a `*slog.Logger` into components
- Log levels: `Debug` for tracing, `Info` for lifecycle, `Warn` for degraded sources, `Error` for failures
- Never log feed credentials, tokens, or user data

## Concurrency

- Feed fetches run concurrently with bounded parallelism (`errgroup` + semaphore)
- Every long-running operation is cancellable via `context.Context`
- `main` handles `SIGINT`/`SIGTERM` and shuts the HTTP server down gracefully
- All concurrent code must pass `go test -race ./...`

## HTTP Service

- Use `net/http` with `http.ServeMux` (Go 1.22+ method/pattern routing) — no web framework by default
- Always set `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` on `http.Server`
- Handlers are thin: parse, delegate to a service, encode the response
- JSON via `encoding/json` with explicit struct tags; HTML via `html/template`

## Quality Gate

Before declaring work complete, run and fix:

```bash
gofmt -l .
go vet ./...
go build ./...
go test -race ./...
govulncheck ./...
```

## Guidance for Copilot

- Generate Go 1.26-compatible code; prefer current stdlib APIs over third-party equivalents
- Include table-driven tests with new packages and exported functions
- Add doc comments on every exported identifier
- Suggest `go.mod` additions explicitly rather than silently importing unknown packages
- Do not produce C#/.NET, Node, or Python solutions for application code
