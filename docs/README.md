# Snow documentation

This directory contains user guides, integration references, extension guides,
and maintainer design material for snow-core.

> Snow is pre-alpha. Source code and tests are the behavioral authority when an
> older research or roadmap document differs from a current feature guide.

## Start here

| I want to… | Read |
|---|---|
| Install Snow and run my first prompt | [Project README](../README.md#quick-start) |
| Learn the TUI, CLI modes, keys, and slash commands | [Using Snow](using-snow.md) |
| Configure providers, permissions, sessions, themes, and search | [Configuration](configuration.md) |
| Understand safety and privilege boundaries | [Security](security.md) |
| Authenticate with ChatGPT/Codex | [ChatGPT authentication](chatgpt-auth.md) |

## Embed and automate

- [Go SDK](sdk.md) — options, lifecycle, methods, events, errors, concurrency,
  readiness, permissions, and a [standalone Go module](../examples/sdk).
- [JSONL RPC](rpc.md) — versioned framing, every command, responses/events,
  ordering, interactive input, goals, subagents, shutdown, and schemas.
- [Python and JavaScript/TypeScript SDKs](language-sdks.md) — typed local clients,
  lifecycle, secure defaults, external-binary policy, and runnable
  [Python](../examples/rpc/python) and [JavaScript](../examples/rpc/javascript)
  examples.
- [Model-requested user input](user-input.md) — `ask_user` request/response
  schema across TUI, SDK, RPC, print, and JSON surfaces.
- [SQLite sessions](sessions.md) — session storage, branches, resume, and the
  public session-store APIs.

## Workflows

- [Plan Mode](plan-mode.md) — model-directed planning, proposed-plan events,
  mode persistence, permission boundaries, and implementation handoff.
- [Persistent Thread Goals](goals.md) — branch goals, budgets, automatic
  continuation, stopping, SDK/RPC controls, and privacy.
- [Subagents](subagents.md) — model tools, lifecycle, roles, permissions,
  persistence, and SDK/RPC/TUI surfaces.

## Extend Snow

- [Model Context Protocol](mcp.md) — stdio and Streamable HTTP configuration,
  management commands, capability bridging, permissions, and current limits.
- [Plugins](plugins.md) — statically linked Go plugins plus persistent
  JavaScript/Python/other external runtimes.
- [External plugin protocol v2](plugin-protocol.md) — complete JSON-RPC JSONL
  framing, lifecycle, tools, risk, progress, events, errors, and shutdown.
- [JavaScript/Python plugin research](plugin-js-python-research.md) — benchmarked
  architecture decision, alternatives, implementation sequence, and deferrals.
- [Agent Skills](skills.md) — `SKILL.md` discovery, trust, precedence,
  progressive disclosure, and resource confinement.
- [Tool routing](tool-routing.md) — deferred schemas, BM25 retrieval,
  `search_tools`, observability, and fallback behavior.

## Maintainers and design history

- [Architecture and roadmap](../IMPLEMENTATION.md) — package boundaries,
  interfaces, decisions, phased roadmap, verification, and open risks.
- [Agent working guide](../AGENTS.md) — repository-specific coding rules,
  security constraints, and verification commands.
- [TUI responsiveness](tui-performance.md) — Bubble Tea rendering and
  performance implementation guidance.
- [Code audit and remediation record](code-audit.md) — repository-wide 2026 bug,
  security, lifecycle, and maintainability findings with closure evidence.
- [Codex Plan Mode and Goals research](codex-plan-mode-and-goals.md) — source
  research and design comparison; users should start with the shorter Plan Mode
  and Goals guides above.
- [Subagent implementation plan](subagents-implementation-plan.md) — historical
  research and phased implementation record; current behavior is documented in
  [Subagents](subagents.md).

## Canonical ownership

To reduce drift, use these documents as the primary references:

| Subject | Canonical document |
|---|---|
| First run and project overview | [`README.md`](../README.md) |
| TUI/CLI operation | [`using-snow.md`](using-snow.md) |
| Runtime configuration | [`configuration.md`](configuration.md) |
| Go embedding | [`sdk.md`](sdk.md) |
| Python/JavaScript embedding | [`language-sdks.md`](language-sdks.md) |
| Foreign-process control | [`rpc.md`](rpc.md) |
| External plugin ABI | [`plugin-protocol.md`](plugin-protocol.md) |
| Safety model | [`security.md`](security.md) |
| Feature-specific behavior | The matching guide in this directory |
| Architecture and historical roadmap | [`IMPLEMENTATION.md`](../IMPLEMENTATION.md) |
| Current implementation details | Source code and tests |
