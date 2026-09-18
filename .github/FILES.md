# .github — Copilot Instruction & Prompt Files

> **These files are active on this repository right now.**
>
> When you open this repo in VS Code with GitHub Copilot enabled, these instruction and prompt files automatically apply to every chat request. They are not just examples — they are working artifacts.
>
> **Deep-dive explanation → [01-customization/README.md](../01-customization/README.md)**

---

## What's in here

| File | Type | What it does |
|------|------|-------------|
| [`copilot-instructions.md`](copilot-instructions.md) | Always-on instruction | Applied to every chat request in this workspace. Defines the Go 1.26 / linux-amd64 project conventions. |
| [`instructions/go-standards.instructions.md`](instructions/go-standards.instructions.md) | Scoped instruction (`**/*.go`) | Go coding standards: naming, errors, context, concurrency, security, testing. |

| [`AGENTS.md`](../AGENTS.md) | Always-on (multi-agent) | Same always-on scope, but recognized by Claude Code, Copilot and other agents. |
| [`prompts/code-review.prompt.md`](prompts/code-review.prompt.md) | Prompt file | Structured review of selected Go code against the repo standards. |
| [`prompts/generate-tests.prompt.md`](prompts/generate-tests.prompt.md) | Prompt file | Generates table-driven Go tests using the stdlib `testing` package. |
| [`prompts/add-feed-source.prompt.md`](prompts/add-feed-source.prompt.md) | Prompt file | Adds a new Tailscale news source end-to-end, with fixtures and tests. |
| [`prompts/new-package.prompt.md`](prompts/new-package.prompt.md) | Prompt file | Scaffolds a new `internal/` package with doc comments and tests. |
| [`prompts/quality-gate.prompt.md`](prompts/quality-gate.prompt.md) | Prompt file | Runs gofmt / vet / build / race tests / govulncheck and fixes findings. |
| [`agents/go-reviewer.agent.md`](agents/go-reviewer.agent.md) | Custom agent | Read-only Go review: races, error wrapping, context, security, test gaps. |
| [`agents/feed-integrator.agent.md`](agents/feed-integrator.agent.md) | Custom agent | Adds and maintains news sources, parsers, dedup keys, and fixtures. |
| [`agents/go-test-engineer.agent.md`](agents/go-test-engineer.agent.md) | Custom agent | Writes and repairs table-driven tests; diagnoses `go test -race` failures. |
| [`agents/dependency-auditor.agent.md`](agents/dependency-auditor.agent.md) | Custom agent | Read-only module hygiene and `govulncheck` auditing. |
| [`skills/feed-health-check/`](skills/feed-health-check/SKILL.md) | Agent skill | Probes every source for reachability/format/caching and refreshes `testdata` fixtures. Includes `scripts/check-feeds.sh` and a candidate source list. |
| [`skills/release-build/`](skills/release-build/SKILL.md) | Agent skill | Quality gate + reproducible static `linux/amd64` build with stamped version and checksums. Includes `scripts/build-release.sh`. |
| [`hooks/security-guard.json`](hooks/security-guard.json) | Hook (`PreToolUse`) | Blocks destructive commands and edits to hook files; warns on `go test` without `-race` and on `go get`. |
| [`hooks/go-postedit.json`](hooks/go-postedit.json) | Hook (`PostToolUse`) | Runs `gofmt -w` on edited `.go` files and feeds `go vet` findings back to the agent. |
| [`hooks/session-context.json`](hooks/session-context.json) | Hook (`SessionStart`) | Injects Go toolchain, module, git, and quality-gate state into every session. |

To use a prompt file: open Copilot Chat → click the **Attach** (paperclip) icon → **Prompt Files** → select from the list.

---

*Want to understand how these work? → [Module 01: Customization](../01-customization/README.md)*
