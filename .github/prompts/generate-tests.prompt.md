---
mode: agent
description: "Generate table-driven Go tests for the selected package, type, or function"
---

# Generate Go Tests

Write tests for the selected Go code using only the standard `testing` package
(plus `github.com/google/go-cmp/cmp` for struct comparisons and
`net/http/httptest` for HTTP). No assertion or mocking frameworks.

## Requirements

- **Table-driven**: a `tests := []struct{ name string; ... }` slice with `t.Run(tt.name, ...)` subtests
- Test name pattern `TestFuncName_Scenario_Expected`; subtest names are short lower-case phrases
- Same package for internals; `package <pkg>_test` when exercising the public API only
- Call `t.Parallel()` in the test and in each subtest when there is no shared state
- Use `t.Cleanup()` for teardown, `t.Context()` for contexts, `t.TempDir()` for files
- Compare with `cmp.Diff(want, got)` and report `t.Errorf("Xxx() mismatch (-want +got):\n%s", diff)`
- Never call the live network — use `httptest.NewServer` and fixtures under `testdata/`

## Coverage to Include

- Happy path with representative input
- Boundary cases: empty slice, nil map, zero value, single element, duplicate entries
- Error paths: every `return err` branch, including wrapped-error identity via `errors.Is` / `errors.As`
- Context cancellation and deadline behaviour for any function taking a `context.Context`
- Concurrency safety where relevant — the suite must pass `go test -race ./...`

## Feed-Parsing Specifics

When the code parses RSS/Atom or any external payload, add a fixture file under
`testdata/` (a trimmed real-world sample) and a malformed variant, and assert the
parser returns a wrapped error rather than panicking.

## Deliverable

Create or update `<file>_test.go` next to the source file, then run
`go test -race ./...` and `gofmt -l .` and fix anything they report.

## Code Under Test

${selection}
