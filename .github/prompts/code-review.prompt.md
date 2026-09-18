---
mode: ask
description: "Structured review of selected Go code against this repo's standards"
---

# Go Code Review

Review the selected Go code against the repository standards in
[go-standards.instructions.md](../instructions/go-standards.instructions.md).
Do not rewrite the whole file — report findings, then show minimal diffs for the fixes.

## 1. Correctness

- Unhandled edge cases: nil maps/slices, empty input, zero `time.Time`, integer overflow
- Are all returned errors checked, or silently dropped into `_`?
- Slice aliasing and `append` reuse bugs; loop-variable capture in goroutines
- Is `defer` inside a loop accumulating resources?

## 2. Errors

- Is every error wrapped with context via `fmt.Errorf("...: %w", err)`?
- Lower-case message, no trailing punctuation, no "failed to" noise?
- Sentinel errors compared with `errors.Is` / `errors.As`, never string matching?
- Any `panic`, `log.Fatal`, or `os.Exit` outside `main`?

## 3. Context & Concurrency

- Does every I/O-bound function take `ctx context.Context` as its first parameter and propagate it?
- Any `context.Context` stored in a struct field?
- Is concurrency bounded (`errgroup` + semaphore), and does every goroutine have an exit path?
- Is shared state guarded? Would `go test -race ./...` flag this?

## 4. API & Style

- Naming: initialisms (`URL`, `ID`, `HTTP`), no package stutter, short package names
- Accept interfaces, return concrete types; interfaces defined at the consumer
- Early returns instead of deep nesting; is the happy path leftmost?
- Is the zero value of each exported struct usable?
- Doc comment on every exported identifier, starting with its name?

## 5. HTTP & Feed Handling

- Shared `*http.Client` with a timeout; `http.NewRequestWithContext`
- `defer resp.Body.Close()`, `resp.StatusCode` checked, body read via `io.LimitReader`
- Server timeouts set; handlers thin and delegating to a service
- Untrusted feed content rendered through `html/template`, never `text/template`

## 6. Security

- Hard-coded secrets, tokens, or feed credentials
- Secrets or user data reaching log output
- SQL built by concatenation instead of parameters
- Unvalidated URLs from feed content (SSRF) or unbounded reads

## 7. Tests

- Is the changed behaviour covered by a table-driven test?
- Are error paths tested, not just the happy path?
- Any test reaching the live network instead of `httptest.Server` + `testdata/` fixtures?

## Output Format

For each finding: **severity** (blocker / should-fix / nit), file and line link, what is wrong,
why it matters, and the corrected snippet. End with the quality-gate commands still to run.

## Selected Code

${selection}
