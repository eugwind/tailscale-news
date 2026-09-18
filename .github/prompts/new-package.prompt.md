---
mode: agent
description: "Scaffold a new internal Go package with doc comment and table-driven tests"
---

# New Internal Package

Scaffold a new package under `internal/`. Ask me for the package name and its single
responsibility if I have not stated them.

## Rules

- Path `internal/<name>/`, package name short, lower-case, one word, no underscores
- No stutter: `feed.Parser`, not `feed.FeedParser`
- Start with a package comment (`// Package <name> ...`) on the primary file
- Expose the smallest possible API; keep helpers unexported until a second caller exists
- Define interfaces in the *consuming* package, not here — export concrete types
- Constructor `New<Type>(deps...) (*Type, error)` taking dependencies explicitly:
  a `*slog.Logger`, an `*http.Client`, a store — never package-level globals
- Every exported identifier gets a doc comment starting with its own name
- Every I/O-bound method takes `ctx context.Context` first and propagates it
- Errors wrapped with `%w`; sentinel errors declared as package-level `var Err...`
- No new third-party dependency without telling me the reason first

## Deliverables

1. `internal/<name>/<name>.go` — the type, constructor, and its methods
2. `internal/<name>/<name>_test.go` — table-driven tests covering happy path and error paths
3. `internal/<name>/testdata/` — only if fixtures are needed

Then run `gofmt -l .`, `go vet ./...`, `go build ./...`, and `go test -race ./...`
and fix anything reported.
