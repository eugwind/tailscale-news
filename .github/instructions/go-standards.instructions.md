---
name: "Go Standards"
description: "Coding conventions for all Go source files in this repo"
applyTo: "**/*.go"
---

# Go Standards

## Formatting & Tooling

- Code must be `gofmt`-clean (tabs for indentation, gofmt line breaks) — run `gofmt -l .` before finishing
- Run `go vet ./...` and `staticcheck ./...`; fix all findings
- Keep imports in the standard grouping: stdlib, third-party, internal — separated by blank lines (`goimports` order)
- No unused imports, variables, or dead code

## Naming

- Packages: short, lower-case, single word, no underscores or plurals (`feed`, `store`, `httpx`)
- Exported identifiers: `PascalCase`; unexported: `camelCase`
- Initialisms keep their case: `URL`, `ID`, `HTTPClient`, `RSSFeed` — never `Url`, `Id`, `HttpClient`
- Interfaces describing one method end in `-er` (`Fetcher`, `Aggregator`)
- Avoid stutter: `feed.Parser`, not `feed.FeedParser`
- Test files end in `_test.go`; helpers in the same package unless testing the public API (`package feed_test`)

## Code Style

- Return early; keep the happy path at the leftmost indentation
- Accept interfaces, return concrete types
- Use `any` instead of `interface{}`
- Prefer slices/maps with pre-sized `make` when the size is known
- Zero values should be useful — avoid constructors that only set defaults
- Struct literals use field names: `Item{Title: t, URL: u}`
- Use `for i := range n` (Go 1.22+) and range-over-func iterators where they simplify code
- Keep functions short and single-purpose; extract helpers instead of deep nesting

## Errors

- Return `error` as the last value; never panic in library code
- Wrap with context: `fmt.Errorf("fetch %s: %w", url, err)` — lower-case, no trailing punctuation
- Compare with `errors.Is` / `errors.As`, never string matching
- Define sentinel errors as `var ErrNotFound = errors.New("not found")`
- Never ignore errors silently — assign to `_` only with a one-line reason comment
- `defer` cleanup immediately after acquiring a resource; check `Close()` errors on writes

## Context & Concurrency

- Every I/O-bound exported function takes `ctx context.Context` as its first parameter
- Never store a `context.Context` in a struct field
- Always propagate `ctx` to HTTP requests (`http.NewRequestWithContext`) and DB calls
- Guard shared state with `sync.Mutex` or channels; run `go test -race ./...`
- Use `errgroup.Group` for bounded parallel fetches; always bound concurrency
- Every goroutine must have a clear exit path — no leaks

## HTTP & External Feeds

- Use a shared `*http.Client` with an explicit `Timeout`; never `http.DefaultClient` for outbound calls
- Always `defer resp.Body.Close()` and check `resp.StatusCode`
- Limit response reads with `io.LimitReader`
- Set a descriptive `User-Agent`; honour `ETag` / `If-Modified-Since` for feed polling
- Retry transient failures with capped exponential backoff and jitter

## Documentation

- Every exported identifier has a doc comment starting with its own name:
  ```go
  // Fetcher retrieves Tailscale news items from a remote source.
  type Fetcher interface { ... }

  // Fetch returns items published after since, or an error if the source is unreachable.
  func (f *RSSFetcher) Fetch(ctx context.Context, since time.Time) ([]Item, error)
  ```
- Each package has a `doc.go` or a package comment on one file
- Comments explain *why*, not *what*

## Security

- Never hard-code secrets or API keys — read from environment via `os.Getenv` / config struct
- Never log tokens, cookies, or PII
- Validate and sanitise all external feed content before rendering; use `html/template` (never `text/template`) for HTML output
- Use parameterised SQL queries only — never string concatenation
- Keep dependencies minimal; run `govulncheck ./...` before release

## Testing

- Standard library `testing` package; table-driven tests are the default
- Test names: `TestFuncName_Scenario_Expected`, subtests via `t.Run(name, ...)`
- Use `t.Parallel()` where safe, `t.Cleanup()` for teardown, `t.Context()` for contexts
- Use `httptest.Server` for HTTP behaviour; store fixtures under `testdata/`
- Assert with stdlib + `github.com/google/go-cmp/cmp` — no assertion frameworks
- Cover error paths, not just the happy path
