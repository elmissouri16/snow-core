# Maintaining Snow

Start with the [agent working guide](../AGENTS.md) before changing code.
This index collects architecture, release procedures, and design history;
[the documentation index](README.md) covers using and integrating Snow.

## Maintainer guides

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

## Internals and design history

- [Lazy MCP connection plan](lazy-mcp-implementation-plan.md): connection lifecycle
  and catalog design. Current setup is documented in [MCP](mcp.md).
- [Retired plugin language research](plugin-js-python-research.md): historical
  record for removed authoring SDKs and examples.
- [Session storage internals](session-storage-internals.md) — SQLite driver,
  schema, migrations, append-only branches, projections, and durable child data.
- [ChatGPT authentication research](chatgpt-auth-research.md) — repository-only
  provider provenance and compatibility comparisons.
- [TUI responsiveness](tui-performance.md) — Bubble Tea rendering and
  performance implementation guidance.
- [Performance regression guard](performance.md) — deterministic allocation
  ceilings, local commands, CI policy, and benchmark review procedure.
- [Runtime performance measurements](runtime-fixes-performance.md) — checkpoint,
  terminal preview, and process capture before/after results and RAM tradeoffs.
- [Code audit and remediation record](code-audit.md) — repository-wide 2026 bug,
  security, lifecycle, and maintainability findings with closure evidence.
- [Codex Plan Mode and Goals research](codex-plan-mode-and-goals.md) — source
  research and design comparison; users should start with [Plan Mode](plan-mode.md)
  and [Goals](goals.md).
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
| Provider setup | [`providers.md`](providers.md) |
| Runtime configuration | [`configuration.md`](configuration.md) |
| Go embedding quickstart | [`sdk.md`](sdk.md) |
| Complete Go SDK behavior | [`sdk-reference.md`](sdk-reference.md) |
| Foreign-process control | [`rpc.md`](rpc.md) |
| JavaScript extension API | [`plugin-extensions.md`](plugin-extensions.md) and `pkg/plugin` |
| Branch-aware JS workflows, restrictions, lifecycle, and reload | [`plugin-workflows.md`](plugin-workflows.md) |
| JS fixture format and generated test guide | [`scaffold/fixtures.md`](../internal/plugin/javascript/scaffold/fixtures.md) |
| Go plugin contract | [`plugins.md`](plugins.md) and `pkg/plugin` |
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

## Plugin-builder skill snapshot

`internal/skills/bundled/snow-js-plugin/` bundles canonical plugin guides, declarations,
and examples for agents working outside the source checkout. Canonical sources
remain authoritative; do not edit the generated copies under its
`references/docs`, `references/api`, or `references/examples` directories.

After changing a bundled source, run:

```sh
python3 internal/skills/bundled/snow-js-plugin/scripts/sync_resources.py
python3 internal/skills/bundled/snow-js-plugin/scripts/sync_resources.py --check
python3 -m unittest scripts.tests.test_plugin_skill -v
```

The source/hash manifest and normal Python tests detect snapshot drift. Review
the handwritten `SKILL.md`, capability matrix, and example selector when supported
behavior changes. This directory is embedded in the binary; do not keep a second
`.agents/skills/` copy that shadows it. The built-in's enforced explicit-token
activation policy is documented in
[Agent Skills](skills.md#build-javascript-plugins-with-the-bundled-skill); it does
not add a mention-only frontmatter field to filesystem skills.

## Related documents

- [Documentation index](README.md)
- [Documentation style guide](style-guide.md)
