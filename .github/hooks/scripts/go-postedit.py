#!/usr/bin/env python3
"""PostToolUse: gofmt every Go file the agent just wrote, then vet its package.

Formatting is applied silently. Vet findings are returned to the model as
additional context so they get fixed in the same turn instead of at commit time.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys

EDIT_TOOLS = {
    "create_file",
    "replace_string_in_file",
    "multi_replace_string_in_file",
    "apply_patch",
    "insert_edit_into_file",
}

TIMEOUT = 60


def collect_paths(value: object, found: list[str]) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if key in ("filePath", "file_path", "path") and isinstance(item, str):
                found.append(item)
            else:
                collect_paths(item, found)
    elif isinstance(value, list):
        for item in value:
            collect_paths(item, found)


def run(args: list[str], cwd: str) -> tuple[int, str]:
    try:
        proc = subprocess.run(
            args, cwd=cwd, capture_output=True, text=True, timeout=TIMEOUT, check=False
        )
    except (OSError, subprocess.SubprocessError) as err:
        return 1, str(err)
    return proc.returncode, (proc.stdout + proc.stderr).strip()


def main() -> int:
    try:
        event = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        return 0

    if event.get("tool_name") not in EDIT_TOOLS:
        return 0

    cwd = event.get("cwd") or os.getcwd()

    paths: list[str] = []
    collect_paths(event.get("tool_input") or {}, paths)
    go_files = sorted(
        {p for p in paths if p.endswith(".go") and os.path.isfile(os.path.join(cwd, p) if not os.path.isabs(p) else p)}
    )
    if not go_files:
        return 0

    absolute = [p if os.path.isabs(p) else os.path.join(cwd, p) for p in go_files]

    messages: list[str] = []

    code, out = run(["gofmt", "-l", "-w", *absolute], cwd)
    if code != 0:
        messages.append(f"gofmt failed — the file does not parse:\n{out}")
    elif out:
        messages.append("gofmt reformatted: " + ", ".join(os.path.basename(p) for p in out.split("\n")))

    packages = sorted({os.path.dirname(p) or "." for p in absolute})
    code, out = run(["go", "vet", *packages], cwd)
    if code != 0 and out:
        messages.append(f"go vet reported problems — fix these before continuing:\n{out}")

    if messages:
        json.dump(
            {
                "systemMessage": "Go post-edit check",
                "hookSpecificOutput": {
                    "hookEventName": "PostToolUse",
                    "additionalContext": "\n\n".join(messages),
                },
            },
            sys.stdout,
        )

    return 0


if __name__ == "__main__":
    sys.exit(main())
