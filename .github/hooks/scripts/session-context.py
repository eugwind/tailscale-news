#!/usr/bin/env python3
"""SessionStart: inject the toolchain, module, and git state into the session.

Keeps the agent from guessing the Go version, the module path, or whether the
working tree is clean.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys

TIMEOUT = 20


def run(args: list[str], cwd: str) -> str | None:
    try:
        proc = subprocess.run(
            args, cwd=cwd, capture_output=True, text=True, timeout=TIMEOUT, check=False
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.strip()


def main() -> int:
    try:
        event = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        event = {}

    cwd = event.get("cwd") or os.getcwd()
    lines: list[str] = []

    go_version = run(["go", "version"], cwd)
    lines.append(f"Toolchain: {go_version}" if go_version else
                 "Toolchain: go not found on PATH — do not assume it is installed.")

    go_mod = os.path.join(cwd, "go.mod")
    if os.path.isfile(go_mod):
        module = run(["go", "list", "-m"], cwd)
        declared = None
        with open(go_mod, encoding="utf-8") as handle:
            for line in handle:
                if line.startswith("go "):
                    declared = line.strip()
                    break
        lines.append(f"Module: {module or 'unknown'} ({declared or 'no go directive'})")
        deps = run(["go", "list", "-m", "-f", "{{.Path}} {{.Version}}", "all"], cwd)
        direct = [d for d in (deps or "").splitlines()[1:] if d]
        lines.append(f"Dependencies: {len(direct)} module(s) in the graph — stdlib-first policy applies.")
    else:
        lines.append("Module: go.mod does not exist yet — the project is not initialised.")

    branch = run(["git", "branch", "--show-current"], cwd)
    dirty = run(["git", "status", "--porcelain"], cwd)
    if dirty is not None:
        state = f"{len(dirty.splitlines())} uncommitted change(s)" if dirty else "clean"
        unborn = run(["git", "rev-parse", "--verify", "HEAD"], cwd) is None
        history = ", no commits yet" if unborn else ""
        lines.append(f"Git: branch {branch or 'detached'}, working tree {state}{history}.")

    if not run(["command", "-v", "govulncheck"], cwd) and not any(
        os.path.exists(os.path.join(p, "govulncheck")) for p in os.environ.get("PATH", "").split(os.pathsep) if p
    ):
        lines.append(
            "govulncheck is not installed — the quality gate cannot complete. "
            "Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"
        )

    lines.append(
        "Quality gate before reporting done: gofmt -l . | go vet ./... | "
        "go build ./... | go test -race ./... | govulncheck ./..."
    )

    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": "Workspace state:\n- " + "\n- ".join(lines),
            }
        },
        sys.stdout,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
