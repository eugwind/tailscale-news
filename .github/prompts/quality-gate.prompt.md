---
mode: agent
description: "Run the full Go quality gate and fix everything it reports"
---

# Quality Gate

Run the repository quality gate from the workspace root and fix every issue found.
Work through the commands in order — do not skip ahead while an earlier step fails.

```bash
gofmt -l .
go vet ./...
go build ./...
go test -race ./...
govulncheck ./...
```

## Rules

- `gofmt -l .` must print nothing; run `gofmt -w` on any listed file
- Fix `go vet` findings properly — never silence them with a comment or a rename
- Fix failing tests by correcting the code when the test is right; only change a test
  when the expectation itself is wrong, and say explicitly which you chose and why
- For `govulncheck` hits, prefer upgrading the module; if no fix exists, report the
  advisory and the affected call path instead of suppressing it
- If `govulncheck` is not installed, tell me the install command rather than skipping the step

## Report

Summarise per command: pass/fail, what was wrong, what you changed. List anything you
could not fix and why. Do not create a markdown report file.
