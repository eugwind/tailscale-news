#!/usr/bin/env python3
"""PreToolUse guard: deny destructive shell commands and edits to hook scripts.

Reads the hook JSON event on stdin. Exit 2 blocks the tool call and shows the
stderr text to the model; exit 0 allows it.
"""

from __future__ import annotations

import json
import re
import sys

# (pattern, reason) — matched case-insensitively against the proposed command.
DENY: list[tuple[str, str]] = [
    (r"\brm\s+(-[a-z]*\s+)*-[a-z]*[rR][a-z]*f|\brm\s+(-[a-z]*\s+)*-[a-z]*f[a-z]*[rR]",
     "recursive force delete"),
    (r"\bgit\s+push\b.*(--force\b|-f\b)", "force push rewrites published history"),
    (r"\bgit\s+reset\s+--hard\b", "discards uncommitted work"),
    (r"\bgit\s+clean\b.*-[a-z]*f", "deletes untracked files, including new work"),
    (r"\bgit\s+(commit|push)\b.*--no-verify", "bypasses repository hooks"),
    (r"\bgit\s+checkout\s+--\s+\.", "discards all local modifications"),
    (r"\bgit\s+branch\s+-D\b", "force-deletes a branch"),
    (r"\bgo\s+clean\s+-modcache\b", "wipes the shared module cache"),
    (r"\bchmod\s+(-[a-zA-Z]+\s+)*777\b", "world-writable permissions"),
    (r"\bcurl\b[^|]*\|\s*(sudo\s+)?(ba)?sh", "pipes a remote script straight into a shell"),
    (r"\bwget\b[^|]*\|\s*(sudo\s+)?(ba)?sh", "pipes a remote script straight into a shell"),
    (r"\bsudo\b(?!\s+-n)", "interactive privilege escalation cannot be answered here"),
    (r"\b(DROP|TRUNCATE)\s+(TABLE|DATABASE|SCHEMA)\b", "destructive SQL"),
    (r">\s*/dev/sd[a-z]", "writes directly to a block device"),
]

# Warned, not blocked.
WARN: list[tuple[str, str]] = [
    (r"\bgo\s+test\b(?!.*-race)", "run `go test -race ./...` — this repo requires the race detector"),
    (r"\bgo\s+get\b\s+[^-]", "adding a dependency: state the reason, stdlib-first policy applies"),
    (r"\bgo\s+build\b.*-ldflags.*-X\s", "version stamping belongs in the release-build skill script"),
]

# Editing these would let the agent rewrite the code that polices it.
PROTECTED_PATH = re.compile(r"\.github/hooks/")

EDIT_TOOLS = {
    "create_file",
    "replace_string_in_file",
    "multi_replace_string_in_file",
    "apply_patch",
    "insert_edit_into_file",
    "create_and_run_task",
}


def collect_paths(value: object, found: list[str]) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if key in ("filePath", "file_path", "path", "uri") and isinstance(item, str):
                found.append(item)
            else:
                collect_paths(item, found)
    elif isinstance(value, list):
        for item in value:
            collect_paths(item, found)


def main() -> int:
    try:
        event = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        return 0  # Never block on a malformed event.

    tool = event.get("tool_name", "")
    tool_input = event.get("tool_input") or {}

    if tool in EDIT_TOOLS:
        paths: list[str] = []
        collect_paths(tool_input, paths)
        for path in paths:
            if PROTECTED_PATH.search(path.replace("\\", "/")):
                print(
                    f"Blocked: {path} is a hook policy file. Hooks must not be "
                    "modified by the agent that they govern — ask the user to edit it.",
                    file=sys.stderr,
                )
                return 2

    command = tool_input.get("command")
    if not isinstance(command, str) or not command.strip():
        return 0

    for pattern, reason in DENY:
        if re.search(pattern, command, re.IGNORECASE):
            print(
                f"Blocked by repository policy ({reason}):\n  {command}\n"
                "Propose this to the user and let them run it themselves.",
                file=sys.stderr,
            )
            return 2

    warnings = [reason for pattern, reason in WARN if re.search(pattern, command, re.IGNORECASE)]
    if warnings:
        json.dump({"systemMessage": "Policy note: " + "; ".join(warnings)}, sys.stdout)

    return 0


if __name__ == "__main__":
    sys.exit(main())
