# Changelog

Notable user-visible changes to Snow are recorded here. Alpha release notes may
also include the generated GitHub comparison for the tagged commit.

## [Unreleased]

## [0.1.0-alpha.5] - 2026-09-04

This alpha shortens release publication while preserving exact-commit CI,
documentation, platform, archive, checksum, and smoke-test gates.

### Changed

- Replaced the duplicate post-tag CI suite with fail-closed provenance checks
  for the exact successful `main` CI and Documentation runs.
- Focused native macOS CI on cross-platform Go, SDK, and installer coverage
  while keeping platform-neutral checks on Linux.
- Made the Documentation workflow the canonical rendered-site validator for
  pull requests and `main` deployments, without granting pull requests deploy
  access.
- Simplified release monitoring to bounded exact-commit status queries and
  concise evidence reports.

### Fixed

- Stabilized the updater installation success-path test under host load by
  using the production-equivalent binary version-check timeout.

## [0.1.0-alpha.4] - 2026-09-04

This alpha fixes native installation of published release archives, removes
background archive downloads and automatic installation, and keeps every
approved download visible through completion.

### Added

- Added a foreground installation card with live archive byte counts,
  percentage, progress bar, checksum/archive verification status, and final
  installation status.

### Changed

- Removed automatic update installation. Opt-in startup checks now fetch only
  release metadata and require an explicit **Install update** or **Skip for
  now** decision before any archive download or executable modification;
  legacy `auto_update` configuration is ignored.

### Fixed

- Fixed native self-update rejecting valid large release archives because the
  gzip checksum trailer had not been consumed when strict trailing-data
  validation ran.

## [0.1.0-alpha.3] - 2026-09-03

This alpha adds opt-in release checks and native self-updates to the interactive
TUI while keeping every headless and SDK startup path free of implicit update
traffic or executable mutation.

### Added

- Added `/settings` controls for startup checks, automatic installation, manual
  checks, and explicit updates, with both persisted preferences disabled by
  default and dependency-safe settings/RPC updates.
- Added prerelease-aware GitHub release discovery, strict version and archive
  validation, bounded downloads, checksum verification, staged binary checks,
  pinned executable identity checks, and atomic replacement for supported
  macOS/Linux amd64 and arm64 release builds.
- Added a post-install **Restart now** or **Later** prompt that shuts down
  gracefully and preserves the active durable session across re-execution.

### Changed

- Update status now shows the current and latest versions explicitly and labels
  the installed version as latest when no newer release is available.

## [0.1.0-alpha.2] - 2026-09-03

This alpha adds verified release installation and a curated documentation
site, removes the unsupported language-specific SDK surface, and fixes
OpenCode request affinity.

### Added

- Added a checksum-verifying one-line curl installer for the latest macOS/Linux
  amd64 or arm64 GitHub release, with version pinning, a configurable install
  directory, and idempotent shell PATH setup.
- Added a responsive GitHub Pages documentation site generated from concise,
  task-oriented guides, with balanced provider setup, pinned deployment
  actions, and link/staging validation.

### Removed

- Removed the checked-in Python and JavaScript RPC client SDKs and their
  SDK-backed examples, tests, package metadata, and documentation. The
  language-neutral JSONL RPC protocol remains supported.
- Removed the Python and JavaScript plugin-authoring SDKs, embedded SDK
  snapshots, offline vendoring command, and bundled plugin-builder skill. Raw,
  dependency-free JavaScript and Python external protocol-v2 examples remain
  supported.
- Removed language-specific raw-plugin runtime tests and the remaining Python,
  JavaScript, Node.js, and package-ecosystem references from Go source and test
  files.

### Changed

- Simplified the public installer invocation to the conventional
  `curl -fsSL … | sh` form while retaining release checksum, archive, and
  binary-version verification inside the installer.
- Refocused the GitHub Pages site on external users with an ordered first-run
  guide, equal setup paths for every provider, task-based navigation, concise
  capability guides, and an explicit public-document allowlist. Exhaustive SDK,
  protocol, maintainer, audit, research, release-process, and implementation
  references remain available in the repository.
- Generalized fixture commands, generated-directory classification, provider
  JSON formatting, application-level textual response detection, and the macOS
  clipboard implementation so Go runtime paths remain language-neutral.

### Fixed

- Fixed low-contrast headings, links, quotes, tables, and code in printed or
  PDF versions of the public documentation.
- Added OpenCode's required stable `X-Opencode-Session` conversation-affinity
  header to OpenCode Go and OpenCode Zen inference requests without forwarding
  it to model catalogs or unrelated compatible endpoints.

## [0.1.0-alpha.1] - 2026-09-02

The first public alpha establishes the current streaming agent loop, TUI,
print/JSON/RPC surfaces, Go SDK, providers, permissioned coding tools, SQLite
sessions, compaction, goals, Plan Mode, MCP, plugins, Agent Skills, optional
subagents, and managed development processes as the initial evaluation baseline.

### Added

- Added a Linux CI performance-regression guard with reviewed `B/op` and
  `allocs/op` ceilings plus broad `ns/op` catastrophe limits for long-session
  hydration, context projection, event
  delivery, provider request construction, and SSE ingestion.
- Added Codex-style `close_agent` and `resume_agent` lifecycle controls across
  model tools, RPC, and SDKs. Closing a terminal child releases the open-agent
  slot while preserving its stable path, transcript, result, and usage;
  follow-up automatically resumes a closed identity when capacity permits.
- Added a model-callable `deactivate_skill` tool that removes one named active
  skill, or all active skills on an explicit `*` request, before the next model
  continuation and durably preserves that lifecycle transition across resume.
- Added first-class `process_start`, `process_status`, `process_logs`,
  `process_stop`, and `process_list` tools for session-scoped development
  servers. Starts/stops use `exec` permission; inspection uses `read` permission;
  global count/record/output limits default to 4/32/1 MiB; optional loopback
  TCP/HTTP and log readiness is bounded; session switching and normal Snow
  shutdown stop and reap managed process groups without persisting or
  reattaching PIDs. Session switches also clear the old runtime inventory
  instead of requiring users to stop each process manually. The TUI now
  exposes `/processes [id|name]`, an auto-refreshing fleet-style inspector with
  a selectable process list and escaped, scrollable combined stdout/stderr.
  `Alt+P` opens the process fleet and `Alt+A` opens the subagent fleet, including
  during active turns; both bindings are configurable.
- Added automatic Linux and macOS CI, race detection, cross-build checks,
  private SDK conformance checks, and reachable-code vulnerability scanning.
- Added tag-gated alpha release archives for Linux and macOS on amd64 and arm64,
  with SHA-256 checksums and a credential-free binary smoke test.
- Added a repository security reporting policy and an alpha release policy.

### Changed

- Reworked TUI model selection into a centered, searchable card. `Alt+M` now
  opens it directly, and with app mouse mode enabled the accented
  `provider/model ▾` header control does the same. Typing filters immediately,
  catalog refreshes preserve the active query and selection, and model-specific
  thinking effort stays in the same modal flow. Standalone `/thinking` and the
  adjacent thinking header control now open a centered fixed-frame effort card;
  the mode control toggles Default/Plan in app mouse mode. `/model` and Settings
  remain available in native mouse mode.
- Moved `/settings` into the shared centered, frame-preserving card treatment.
  Its complete option list stays keyboard-navigable through a selection-following
  window on short terminals, status and save errors remain inside the fixed card,
  nested model selection returns to Settings, and blocking host requests still
  take visual and input precedence.
- Moved `/help` from transcript output into a centered, fixed-frame card. The
  complete command registry, composer directives, active keybindings, and mouse
  guidance remain scrollable on short terminals without moving the transcript.
- Moved the complete TUI `/login` flow into the same centered-card treatment:
  provider selection, OpenAI-compatible profile and endpoint fields, masked API
  key capture, ChatGPT account/method selection and OAuth progress, and model
  discovery after compatible-provider setup. `/logout` uses matching provider
  and progress cards, serializes credential deletion against new auth actions,
  and field validation remains visible in-place. Esc now moves back one login
  step, preserving non-secret field values and returning child cards to their
  previous selection list; Esc only cancels at the root provider/direct-login
  step, while discarded masked keys are never restored. Short cards retain
  required device codes/errors, and compatible endpoint paths are not echoed
  into the post-submit progress card or completion transcript. Single-line auth
  fields strip terminal/layout controls, and delayed clipboard results are
  scoped to the field generation that requested them. Slash-command and login
  transitions invalidate pending composer text or image paste results before
  reusing the editor.
- Changed interactive permission policy so every fresh session starts in
  `ask`, unless `--permission` explicitly overrides that launch. TUI
  `/permissions` and Settings changes now persist only with the active session
  for resume and no longer write a global default inherited by new projects.
  The removed `permission_mode` field is ignored for upgrade compatibility and
  cannot alter that baseline; use the launch flag or active-session controls
  instead.
- Removed unreachable private helpers and the unused container-registry
  dependency tree, and corrected the Python plugin module's exported names.
- Reduced CPU and transient memory for long-session reopen, compacted in-memory
  context assembly, TUI hydration, OpenAI-compatible request/SSE handling,
  event delivery, artifact pruning, and subagent forks without changing
  provider wire payloads, session history, event order, or transcript limits.
  Schema-v11 sessions now maintain a rebuildable hydration projection so the
  TUI fetches old message blobs only for its bounded visible suffix and focused
  tool-call lookbehind.
- Changed managed-process startup guidance to treat a stable log marker as
  sufficient readiness evidence. Snow now prefers log readiness and does not
  add an HTTP or TCP probe merely to reconfirm a process that announced it is
  ready; network probes remain available when network health is explicitly
  required or no reliable log marker exists.
- Changed the `Ctrl+T` thinking shortcut to cycle directly through the active
  model's supported efforts instead of opening a picker or adding transcript
  entries. The header/footer briefly highlights each change, while `/thinking`
  still supports explicit selection.
- Canonicalized the Go module and SDK import path as
  `github.com/elmissouri16/snow-core`.
- Unified CLI, RPC, external-plugin, MCP, and Go SDK build-version metadata.
- Promoted the core runtime, Go SDK, and RPC protocol from pre-alpha to alpha;
  Python and JavaScript package publication remains deferred.

### Removed

- Removed the optional smolvm Bash sandbox end to end: runtime routing, CLI/TUI
  controls, configuration, SDK/RPC contracts, language-client methods, schemas,
  implementation packages, and current documentation. Bash now always executes
  on the host under the existing permission and timeout controls; old
  `sandboxes.json` state is left untouched but is no longer read.

### Fixed

- Made the TUI slash-command palette retain the complete command registry so
  Up/Down navigation scrolls beyond the first visible page instead of wrapping
  after `/login`.
- Restored Left/Right value changes in Settings, blocked transcript paging behind
  thinking cards, and returned model-catalog failures to the Settings card
  instead of leaving the nested flow without a visible modal.
- Prevented restored subagent status snapshots from appearing as fresh
  lifecycle rows at the end of resumed TUI and print-mode transcripts. Snapshot
  events still initialize the fleet inspector and remain observable to SDK,
  JSON, RPC, and plugin consumers with `snapshot: true`.
- Corrected model-specific thinking discovery across providers. ChatGPT efforts
  now come from authenticated backend records (or the same-account cache),
  intersected with valid Responses inference efforts; Codex's catalog-only
  `ultra` host preset is no longer sent as `reasoning.effort`. OpenCode Go no
  longer invents `low`/`medium`/`high` from a generic reasoning flag or
  parameter. Without explicit per-model effort values, providers expose only
  Snow's local `off`; stale remembered selections reset to `off`.
- Made every Plan-to-Default transition durably clear active planning/audit
  skills—including Shift+Tab, `/default`, implementation handoffs, and SDK/RPC
  mode changes—while retaining `/skills clear` as optional recovery, and made
  Default mode explicit in provider context so stale transcript text cannot be
  mistaken for an active Plan-mode constraint.
- Removed blocking interactive-input tools from automatic Goal turns and added
  an execution-time gate so undeclared `ask_user` or `request_user_input` calls
  cannot suspend autonomous work.
- Kept RPC stdin/stdout bounded and interruptible when inherited macOS pipe
  handles expose deadline methods but reject deadline operations.
- Upgraded the source Go profile to 1.27rc3, including the standard library
  security fixes required for a clean reachable-code scan.

[Unreleased]: https://github.com/elmissouri16/snow-core/compare/v0.1.0-alpha.2...HEAD
[0.1.0-alpha.2]: https://github.com/elmissouri16/snow-core/compare/v0.1.0-alpha.1...v0.1.0-alpha.2
[0.1.0-alpha.1]: https://github.com/elmissouri16/snow-core/releases/tag/v0.1.0-alpha.1
