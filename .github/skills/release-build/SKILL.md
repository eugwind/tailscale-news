---
name: release-build
description: Produce a verified, reproducible static linux/amd64 binary — runs the full quality gate, stamps version metadata, strips debug info, and emits checksums. Use when cutting a release, tagging a version, or building an artifact for deployment.
---

# Release Build

The repeatable path from a clean working tree to a shippable static binary.

## Workflow

1. **Confirm the tree is clean.** Uncommitted changes make the build unreproducible —
   stop and report rather than building dirty.
2. **Run the quality gate.** Every step must pass before anything is built:

   ```bash
   gofmt -l .        # must print nothing
   go vet ./...
   go build ./...
   go test -race ./...
   govulncheck ./...
   ```

3. **Build.** Use the helper script, which handles flags, stamping, and checksums:

   ```bash
   .github/skills/release-build/scripts/build-release.sh [version]
   ```

   It builds with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, `-trimpath`,
   `-ldflags "-s -w -X main.version=... -X main.commit=... -X main.buildDate=..."`,
   and writes the binary plus a `SHA256SUMS` file to `dist/`.
4. **Verify the artifact.** Confirm it is a static ELF binary with no dynamic links,
   that `--version` reports the expected version and commit, and that it starts and
   shuts down cleanly on `SIGTERM`.
5. **Report.** Binary path, size, SHA-256, version, commit, Go toolchain version, and
   the result of each gate step.

## Rules

- `-race` and release builds are mutually exclusive — run the race tests first, then build.
- Never pass `-tags` or `-ldflags` that disable checks to make a build succeed.
- Never build from a dirty tree without saying so explicitly in the report.
- `dist/` is a build output — it belongs in `.gitignore`, never in a commit.
- A `govulncheck` finding blocks the release unless the user explicitly accepts the risk.
- Version metadata is injected at link time only; never hard-code a version in source.

## Completion Criteria

- All five gate commands passed.
- `dist/tailscale-news` exists, is statically linked, and reports the expected version.
- `dist/SHA256SUMS` matches the binary.
- Any skipped or waived step is named explicitly in the report.
