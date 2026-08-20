# Changelog

Notable user-visible changes to Snow are recorded here. Alpha release notes may
also include the generated GitHub comparison for the tagged commit.

## [Unreleased]

### Fixed

- Made every Plan-to-Default transition durably clear active planning/audit
  skills—including Shift+Tab, `/default`, implementation handoffs, and SDK/RPC
  mode changes—while retaining `/skills clear` as optional recovery, and made
  Default mode explicit in provider context so stale transcript text cannot be
  mistaken for an active Plan-mode constraint.
- Removed blocking interactive-input tools from automatic Goal turns and added
  an execution-time gate so undeclared `ask_user` or `request_user_input` calls
  cannot suspend autonomous work.

## [0.1.0-alpha.1] - 2026-08-20

The first public alpha establishes the current streaming agent loop, TUI,
print/JSON/RPC surfaces, Go SDK, providers, permissioned coding tools, SQLite
sessions, compaction, goals, Plan Mode, MCP, plugins, Agent Skills, optional
subagents, and optional smolvm-backed Bash as the initial evaluation baseline.

### Added

- Automatic Linux and macOS CI, race detection, cross-build checks, private SDK
  conformance checks, and reachable-code vulnerability scanning.
- Tag-gated alpha release archives for Linux and macOS on amd64 and arm64, with
  SHA-256 checksums and a credential-free binary smoke test.
- A repository security reporting policy and an alpha release policy.

### Fixed

- Kept RPC stdin/stdout bounded and interruptible when inherited macOS pipe
  handles expose deadline methods but reject deadline operations.
- Upgraded the source and sandbox Go profile to 1.27rc3, including the standard
  library security fixes required for a clean reachable-code scan.

### Changed

- Canonicalized the Go module and SDK import path as
  `github.com/elmissouri16/snow-core`.
- Unified CLI, RPC, external-plugin, MCP, and Go SDK build-version metadata.
- Promoted the core runtime, Go SDK, and RPC protocol from pre-alpha to alpha;
  Python and JavaScript package publication remains deferred.

[Unreleased]: https://github.com/elmissouri16/snow-core/compare/v0.1.0-alpha.1...HEAD
[0.1.0-alpha.1]: https://github.com/elmissouri16/snow-core/releases/tag/v0.1.0-alpha.1
