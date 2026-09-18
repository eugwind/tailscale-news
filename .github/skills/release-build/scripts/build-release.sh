#!/usr/bin/env bash
# Build a reproducible static linux/amd64 binary with stamped version metadata.
# Usage: build-release.sh [version]   (version defaults to `git describe`)
set -euo pipefail

BINARY="${BINARY_NAME:-tailscale-news}"
CMD_PATH="${CMD_PATH:-./cmd/${BINARY}}"
OUT_DIR="${OUT_DIR:-dist}"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

command -v go >/dev/null || { echo "go toolchain not found" >&2; exit 1; }
[[ -d "$CMD_PATH" ]] || { echo "no main package at $CMD_PATH" >&2; exit 1; }

version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if ! git diff --quiet HEAD -- 2>/dev/null; then
	echo "WARNING: working tree is dirty — this build is not reproducible" >&2
fi

echo "==> gofmt"
unformatted="$(gofmt -l . || true)"
if [[ -n "$unformatted" ]]; then
	echo "unformatted files:" >&2
	printf '%s\n' "$unformatted" >&2
	exit 1
fi

echo "==> go vet"
go vet ./...

echo "==> go test -race"
go test -race ./...

echo "==> govulncheck"
if command -v govulncheck >/dev/null; then
	govulncheck ./...
else
	echo "govulncheck not installed; install with:" >&2
	echo "  go install golang.org/x/vuln/cmd/govulncheck@latest" >&2
	exit 1
fi

echo "==> build ${BINARY} ${version} (${commit})"
mkdir -p "$OUT_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	-trimpath \
	-ldflags "-s -w -X main.version=${version} -X main.commit=${commit} -X main.buildDate=${build_date}" \
	-o "${OUT_DIR}/${BINARY}" \
	"$CMD_PATH"

echo "==> checksum"
( cd "$OUT_DIR" && sha256sum "$BINARY" >SHA256SUMS && cat SHA256SUMS )

echo "==> artifact"
file "${OUT_DIR}/${BINARY}" 2>/dev/null || true
ls -lh "${OUT_DIR}/${BINARY}"
echo "version=${version} commit=${commit} built=${build_date} toolchain=$(go version | awk '{print $3}')"
