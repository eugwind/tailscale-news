---
mode: agent
description: "Add a new Tailscale news source (RSS/Atom/JSON/HTML) to the aggregator"
---

# Add a News Source

Add a new source to the aggregator. Ask me for anything missing before writing code:

- Source name and canonical site URL
- Feed URL and format (RSS 2.0, Atom, JSON Feed, or HTML scrape)
- Poll interval and whether authentication is required
- Category (official blog, release notes, security advisory, community, third-party)

## Implementation Steps

1. Register the source in the source registry under `internal/feed/` — configuration
   first; only write a new fetcher if the format is not already supported.
2. If a new parser is needed, implement it behind the existing `Fetcher` interface
   and keep it in its own file (`internal/feed/<format>.go`).
3. Normalise every item to the canonical `Item` type: stable ID, title, URL,
   published time in UTC, summary, source name, category.
4. Derive a deterministic dedup key (canonicalised URL + normalised title) so the
   same story from multiple sources collapses into one entry.
5. Honour polling etiquette: `ETag` / `If-Modified-Since`, a descriptive `User-Agent`,
   `io.LimitReader` on the body, and capped exponential backoff with jitter on failure.
6. Log source health with `slog`: `Info` on successful poll counts, `Warn` when a
   source is degraded or returns stale data, `Error` only on hard failures.
   A single failing source must never break the aggregation run.

## Tests

- Save a trimmed real response as a fixture in `internal/feed/testdata/<source>.xml`
- Add a table-driven parser test: well-formed, empty feed, malformed XML, missing dates
- Serve fixtures via `httptest.Server`; no live network calls
- Assert timestamps are parsed to UTC and dedup keys are stable across runs

## Done When

`gofmt -l .`, `go vet ./...`, `go build ./...`, and `go test -race ./...` are all clean.
