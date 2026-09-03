# Snow repository documentation

This is the complete documentation index for Snow maintainers and contributors.
It includes public user guides alongside architecture, release, performance,
research, audit, and implementation records that are intentionally not
published on the user documentation site.

If you want to install or use Snow, start with the curated
[GitHub Pages guide](https://elmissouri16.github.io/snow-core/) or the
[getting-started guide](getting-started.md). Use the ownership map below when
maintaining behavior or locating the authoritative repository reference.

> **Note:** Snow is alpha software. Source code and tests are the behavioral
> authority when an older research or roadmap document differs from a current
> feature guide.

## Start here

| I want to… | Read |
|---|---|
| Install Snow and run my first prompt | [Getting started](getting-started.md) |
| Learn the TUI, CLI modes, keys, and slash commands | [Using Snow](using-snow.md) |
| Configure providers, permissions, sessions, themes, and search | [Configuration](configuration.md) |
| Understand safety and privilege boundaries | [Security model](security.md) |
| Report a suspected vulnerability | [Security reporting](../SECURITY.md) |
| Prepare or verify an alpha release | [Release policy](releases.md) |
| Publish or maintain the documentation site | [Documentation site](pages.md) |
| Authenticate with ChatGPT/Codex | [ChatGPT authentication](chatgpt-auth.md) |

## Embed and automate

- [Go SDK](sdk.md) — options, lifecycle, methods, events, errors, concurrency,
  readiness, permissions, and a [standalone Go module](../examples/sdk).
- [JSONL RPC](rpc.md) — versioned framing, every command, responses/events,
  ordering, interactive input, goals, subagents, shutdown, and schemas.
- [Model-requested user input](user-input.md) — `ask_user` request/response
  schema across TUI, SDK, RPC, print, and JSON surfaces.
- [Sessions and branches](sessions.md) — storage, resume, branches, compaction,
  forks, retrieval, and public SDK options.

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
- [Lazy MCP connection plan](lazy-mcp-implementation-plan.md) — proposed
  metadata cache, connection state machine, idle shutdown, security rules,
  implementation phases, and verification.
- [Plugins](plugins.md) — statically linked Go plugins plus persistent
  language-neutral external runtimes.
- [External plugin protocol v2](plugin-protocol.md) — complete JSON-RPC JSONL
  framing, lifecycle, tools, risk, progress, events, errors, and shutdown.
- [External plugin runtime research](plugin-js-python-research.md) — benchmarked
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
- [Release policy](releases.md) — alpha versioning, CI gates, artifacts,
  checksums, and rollback.
- [Security reporting](../SECURITY.md) — private vulnerability disclosure and
  supported-release policy.
- [Documentation style guide](style-guide.md) — writing and formatting
  conventions for documentation contributors.
- [Documentation site](pages.md) — GitHub Pages enablement, staging, deployment,
  validation, and troubleshooting.
- [Session storage internals](session-storage-internals.md) — SQLite driver,
  schema, migrations, append-only branches, projections, and durable child data.
- [ChatGPT authentication research](chatgpt-auth-research.md) — repository-only
  provider provenance and compatibility comparisons.
- [TUI responsiveness](tui-performance.md) — Bubble Tea rendering and
  performance implementation guidance.
- [Performance regression guard](performance.md) — deterministic allocation
  ceilings, local commands, CI policy, and benchmark review procedure.
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
| Installation and first run | [`getting-started.md`](getting-started.md) |
| Project overview and contributor entry point | [`README.md`](../README.md) |
| TUI/CLI operation | [`using-snow.md`](using-snow.md) |
| Runtime configuration | [`configuration.md`](configuration.md) |
| Go embedding | [`sdk.md`](sdk.md) |
| Foreign-process control | [`rpc.md`](rpc.md) |
| External plugin ABI | [`plugin-protocol.md`](plugin-protocol.md) |
| ChatGPT/Codex authentication | [`chatgpt-auth.md`](chatgpt-auth.md) |
| ChatGPT adapter provenance | [`chatgpt-auth-research.md`](chatgpt-auth-research.md) |
| Lazy MCP implementation | [Connection plan](lazy-mcp-implementation-plan.md) |
| Safety model and privilege boundaries | [`security.md`](security.md) |
| Vulnerability disclosure | [`SECURITY.md`](../SECURITY.md) |
| Alpha versioning and distribution | [`releases.md`](releases.md) |
| User session workflows | [`sessions.md`](sessions.md) |
| SQLite session implementation | [`session-storage-internals.md`](session-storage-internals.md) |
| GitHub Pages publication | [`pages.md`](pages.md) |
| Performance allocation gates | [`performance.md`](performance.md) |
| Feature-specific behavior | The matching guide in this directory |
| Contributor workflow and must-load repository rules | [`AGENTS.md`](../AGENTS.md) |
| Package architecture, dependency direction, and roadmap | [`IMPLEMENTATION.md`](../IMPLEMENTATION.md) |
| Current implementation details | Source code and tests |

## Related documents

- [Getting started](getting-started.md) — public installation and first-run guide.
- [Project README](../README.md) — repository overview and contributor entry point.
- [Release policy](releases.md) — alpha release and distribution requirements.
- [Security reporting](../SECURITY.md) — private vulnerability disclosure.
- [Documentation style guide](style-guide.md) — conventions used across this
  directory.
- [Documentation site](pages.md) — GitHub Pages publishing and validation.
- [Architecture and roadmap](../IMPLEMENTATION.md) — design decisions and open
  risks.
- [Agent working guide](../AGENTS.md) — repository rules for contributors.
