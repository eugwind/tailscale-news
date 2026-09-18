---
name: feed-health-check
description: Verify every configured Tailscale news source is reachable, well-formed, and still publishing — and capture a fresh testdata fixture when a source's format changes. Use when adding a source, when a source is reported stale or broken, or as a periodic audit.
---

# Feed Health Check

A repeatable audit of the sources the aggregator polls. Run it when adding a source,
when a source starts producing `Warn` logs, or on a schedule before a release.

## Workflow

1. **Enumerate sources.** Read the source registry under `internal/feed/`. If it does not
   exist yet, use [resources/sources.md](resources/sources.md) as the candidate list.
2. **Probe each source.** Run the helper script:

   ```bash
   .github/skills/feed-health-check/scripts/check-feeds.sh <url>...
   ```

   It reports HTTP status, content type, byte size, `ETag`/`Last-Modified` support,
   redirect target, and TLS validity for each URL. It never writes to the repository.
3. **Classify every result:**
   - `OK` — 2xx, a feed content type, and a parseable body
   - `MOVED` — 3xx to a different host or path; update the registered URL
   - `DEGRADED` — reachable but no new items within the source's expected window,
     or `ETag`/`Last-Modified` missing (polling cost is higher than it should be)
   - `BROKEN` — 4xx/5xx, TLS failure, timeout, or unparseable body
4. **Parse-check locally.** For each reachable source, confirm the repo's parser still
   handles it: item count > 0, every item has an absolute URL, and every timestamp parses
   to a non-zero UTC value.
5. **Refresh fixtures when the format changed.** Save a trimmed live response to
   `internal/feed/testdata/<source>.xml` (keep 2–3 items, strip unrelated boilerplate,
   remove any tokens or personal data), then update the table-driven parser test.
6. **Fix forward.** Update the registry for `MOVED`, add a parser branch for a changed
   format, and mark a persistently `BROKEN` source disabled with a comment naming the date
   and reason — never delete history of a source silently.
7. **Verify.** `gofmt -l .` · `go vet ./...` · `go build ./...` · `go test -race ./...`

## Rules

- Probing is read-only: no writes outside `internal/feed/testdata/` and the registry.
- Be a polite client — the script sends a descriptive `User-Agent`, uses a timeout, and
  makes a single request per URL. Do not loop it in tight succession against live hosts.
- Never commit credentials, cookies, or `Authorization` headers into a fixture.
- Tests must keep using fixtures via `httptest.Server`; this skill is the only place that
  touches the live network.

## Completion Criteria

- Every source is classified `OK`, `MOVED` (and updated), `DEGRADED` (and explained), or
  `BROKEN` (and disabled with a reason).
- Any format change is covered by a refreshed fixture plus a passing parser test.
- The quality gate is clean, and the report lists each source with its verdict and action.
