# Changelog

Notable user-visible changes to Snow are recorded here. Alpha release notes may
also include the generated GitHub comparison for the tagged commit.

## [Unreleased]

### Added

- Enable configured MCP servers and bounded model-directed subagents in
  explicitly activated Web Manager conversations. MCP and shell-capable child
  operations use the existing session permission broker and browser approval
  cards; plugins and browser-side MCP/subagent configuration remain disabled.
  Existing remembered Web workspace trust is revoked once so the expanded
  OS-privileged activation disclosure must be reviewed explicitly.

### Fixed

- Restore each Web Manager conversation's selected provider/model after an
  explicit reopen or manager restart without changing operator host defaults.

### Changed

- Disable the OpenCode Zen provider because its models are restricted to
  OpenCode clients. Fresh configurations now default to authenticated OpenCode
  Go; existing Zen selections fail explicitly instead of silently rerouting.

## [0.1.0-alpha.10] - 2026-09-17

This is Snow's largest alpha update since the initial launch. It adds an
opt-in, authenticated local Web Manager preview, expands the additive RPC v1
control surface for managed clients, and hardens append-only session continuity,
public history, goals, compaction, and skill-scoped execution. The terminal
interface remains available, and the public installer and three-member archive
shape are unchanged from alpha.9.

### Added

- Add `snow --mode web`, an authenticated local browser manager for registering
  host projects, passively browsing bounded saved-session history, and explicitly
  starting RPC-backed conversations. Merely starting or browsing the manager
  does not initialize an agent, provider, project configuration, plugin, MCP
  server, or session runtime.
- Add a responsive conversation workspace with sanitized Markdown and plan
  rendering, public tool activity, models and session-local reasoning, Default
  and Plan controls, usage/context indicators, questions and approvals, Stop,
  bounded attachments, `@` project-file references, and `$` installed-skill
  suggestions. Browsing or selecting a suggestion sends nothing and activates
  no skill; selection only inserts the reference into the draft.
- Add serial **Queue next**, native steering, **Edit & resend**, **Regenerate**,
  and owned manual compaction. Unsent queued work survives cancellation or
  failure for explicit review and is never replayed automatically.
- Add manager views for files and Git changes, Activity, workspace and
  conversation labels/pins/archive, branch versions, public-history preview,
  branch rename/fork, detached conversations, and two-minute single-use Restore.
  These history operations preserve exact append-only records and do not undo
  working-tree or command side effects.
- Add explicit browser Thread Goal runs with optional token budgets, exact
  session/branch/tip admission, whole-run Stop, and durable deferred state.
  Also add bounded managed-process list/log/Stop controls using opaque handles
  instead of exposing operating-system process IDs.
- Add reviewed host project creation and anonymous HTTPS clone operations.
  Destinations must be new, registration is a separate step, and neither
  operation automatically activates an agent runtime.
- Add durable, origin-scoped browser pairing, inventory and targeted revocation;
  remembered activation consent for an exact registered workspace; host provider
  status and future-worker defaults; and bounded manager organization controls.
- Add `--rpc-startup catalog` for runtime-free saved-session, public-history,
  public-tool-result, and user-image reads, plus `--rpc-startup control` for
  bounded host defaults, provider status, API-key mutation, inactive-session
  deletion, and project creation/clone without constructing an agent runtime.
- Expand capability-gated RPC v1 controls for model discovery and selection,
  session reasoning, Queue next, owned goals and compaction, managed steering,
  process management, message edit/regenerate, branch versions and restore,
  history controls, durable images, and explicit public-history projections.
- Add dependency-light `pkg/agentclient/rpc` and `pkg/agentclient/process`
  packages for bounded JSONL RPC over an owned connection and supervised Snow
  subprocesses without invoking a shell.

### Changed

- Persist provider-only goal, mode, plugin, and steering context beside its
  owning assistant response. Retry, resume, forks, hydration, cached prefixes,
  and compaction now retain exact provider continuity while normal history,
  RPC, SDK, TUI, catalog, Web Manager, logs, and summaries continue to omit it.
- Make public tool-result provenance explicit. Browser and other security-
  sensitive consumers can use bounded `tool_result`, `public_tool_result`, or
  `messages_page` data marked public at execution time instead of treating
  legacy `tool_output`, raw tool messages, arguments, images, thinking, provider
  continuity, or plugin metadata as a safe projection.
- Strengthen managed goal admission and ownership. A managed prompt cannot
  invoke goal tools unless an exact goal run was explicitly accepted; that run
  can inspect or update only its owning goal and cannot start in Plan Mode or
  consume pending/recovered queue work.
- Report mixed usage currencies as `cost_currency_conflict:true` without an
  invalid combined monetary value. Token and request totals remain available.
- Migrate the manager interface to a checked-in React 19.3, TypeScript 7, and
  Vite 8 bundle. Production remains a single Go binary with embedded assets and
  has no Node server, CDN, Next.js, or runtime npm dependency.
- Add Node 24 frontend CI, type checks, native Chromium fixtures, reproducible
  generated-asset checks, and frontend/harness third-party notices in release
  archives while preserving the archive's `snow`, `README.md`, and `LICENSE`
  member contract.

### Security and networking

- Web mode is a preview for a single user on a trusted private network. It
  automatically serves `127.0.0.1:7331` and, when available, the first private
  IPv4 address or IPv6 ULA. Localhost and LAN use independent exact Host/Origin,
  CSRF, cookie, pairing, throttling, expiry, and revocation boundaries.
- **Private-LAN traffic is unencrypted HTTP.** Pairing authenticates a browser
  but does not encrypt the pairing code, cookies, prompts, responses, tool
  output, or attachments. Never use Web mode on public Wi-Fi, forward its port,
  or expose it to the public Internet. There is no supported TLS, certificate,
  DNS-origin, trusted-proxy, custom-listener, or public-Internet deployment mode.
- Web browsing, project creation/clone, workers, tools, and subprocesses use the
  Snow host user's operating-system authority. Snow remains unsandboxed; Stop
  cannot reverse file writes, commands, network effects, or detached descendants.
- New Web Manager workers start in `ask`. Plugins, MCP servers, subagents, and
  debug capture are disabled in the fixed managed profile. Installed skills are
  disabled unless startup skill access is explicitly enabled; that saved project
  preference is preselected on later starts and exposes the normal skill catalog,
  so the model may activate any applicable enabled skill. Process and goal tools
  remain subject to their additional admission and permission checks.
- The browser never accepts provider credentials or performs OAuth. Runtime-free
  API-key control is only for trusted same-user local stdio/RPC clients and is
  not exposed through the HTTP manager.

### Compatibility and migration

- Alpha.9 had no Web Manager flags, so this release removes no alpha.9 Web CLI
  contract. Users of intermediate source previews that had `--web-listen`,
  `--web-tls-cert`, `--web-tls-key`, saved network profiles, or `snow web`
  subcommands must switch to root-command `snow --mode web` and the automatic
  localhost/private-LAN HTTP service on port 7331.
- Stop and restart the foreground manager after installing a new binary, then
  explicitly start fresh workers. Reloading a browser cannot replace a running
  manager or worker executable.
- The first upgrade from an older Web Manager preview revokes legacy unscoped
  browser sessions because they cannot prove a localhost or LAN origin. The
  pairing code remains available, but each origin must pair independently.
  Existing project registrations also require one explicit remembered-activation
  confirmation; removal, archive, restore, or identity replacement clears it.
- SQLite remains at schema version 12 and exact history remains append-only.
  Sessions can now include private `internal_context`, explicit public tool
  results, and unknown-tool-outcome metadata that alpha.9 does not interpret
  with the new semantics. Back up session databases with Snow stopped before
  upgrading; rollback should restore that backup to avoid losing provider
  continuity or the new public-history presentation.
- RPC remains schema version 1, eager startup remains the default, and wire
  changes are additive and capability-gated. Integrations must tolerate unknown
  capabilities, commands, event types, and optional fields; strict decoders
  that reject unknown members need updating. A prompt admission response is not
  terminal—`prompt_completed` remains authoritative.
- Web, catalog, and control startup profiles reject unrelated runtime flags.
  Web provider/model choices come from host defaults or explicit activation,
  not runtime flags passed to `snow --mode web`.
- Source builds continue to require Go 1.27rc3. Frontend development requires
  Node 22.12 or newer and npm; CI pins Node 24.16.0. Binary users and ordinary
  Go source builds use checked-in embedded assets and do not require Node.

### Fixed

- Keep localhost directly usable alongside private-LAN Web mode instead of
  redirecting the printed local URL to the LAN origin, and isolate LAN HTTP
  cookies from retired secure-cookie names.
- Generate browser request identifiers without relying on
  `crypto.randomUUID()`, which is unavailable on insecure private-IP origins;
  the fallback uses `crypto.getRandomValues()`.
- Correct release browser fixtures that mistook read-only Shell inventory GETs
  for mutations, stabilize worker-loss cleanup verification by retaining the
  fictional Git PID/PGID with a minimal terminal workload, order the plugin
  reload fixture after its own asynchronous notification, make the oversized Git
  patch fixture deterministic across Git versions, expire host-clone timeout
  coverage only after descendant admission, and serialize race-instrumented
  packages so hosted-runner contention cannot consume production catalog
  deadlines. Security boundaries and active-delivery admission remain unchanged.
- Fix compact mobile conversation-header geometry and short-height composer
  focus scrolling so header actions remain reachable.
- Require an advanced revision and exact native receipt before treating a
  browser goal as admitted; uncertain failures remain fenced and are not replayed.
- Preserve transcript/composer geometry when direct compaction is a no-op.
- Compact automatic goals correctly when a trusted mailbox update begins the
  next cycle, without weakening ordinary user/mailbox compaction boundaries.
- Preserve the enclosing request after a skill contributes its scoped output.
  Skill methods and stopping rules constrain only that contribution and do not
  authorize unrequested side effects or bypass permissions.
- Classify provider-confirmed RPC aborts as canceled, retain unknown-outcome
  provenance for interrupted tools, and report field-level permission-effect
  truncation instead of presenting incomplete review data as complete.

### Known alpha limitations

- Web Manager remains a bounded preview, not a remote deployment surface. At
  most two projects can be live; restart begins with no workers; reconnects
  perform fresh reads but never replay mutations or automatically resume work.
- Clone supports anonymous HTTPS only: no SSH, credentials, local/file clones,
  authenticated profiles, or disk/transfer quota. General Git writes, worktree
  forks, a browser file editor, PTY, browser OAuth, plugin/MCP/subagent controls,
  and automatic worker recovery remain out of scope.
- Browser acceptance uses local fake providers, Chrome/Chromium, mocked public
  DTOs where documented, and simulated viewports. It is not physical-device,
  public-network, detached-side-effect, or live-provider compatibility evidence.
- Open Web Manager fixture/presentation defects remain tracked: an Activity
  privacy fixture can intermittently match a numeric sentinel (BUG-218),
  reader-anchor assertions regress under intermediate React layouts (BUG-196),
  settings/layout coverage has a stale transport path (BUG-176), and redundant
  React telemetry reconciliation remains regressed (BUG-175).
- Compact terminal session/branch panels can hide management-action hints even
  though the keyboard actions still work (BUG-086).
- Binaries are not code-signed or notarized. Checksums prove integrity against
  the published GitHub release, not an independent signature.

### Validation

- The isolated release worktree passes repository-wide Go formatting, all Go
  tests, vet, 70 support-script tests, the benchmark regression guard, the
  full internal/SDK race suite,
  standalone SDK execution, an exact-version production build, and a
  credential-free fake-provider lifecycle smoke.
- Node 24.16 frontend installation, typechecking, all 127 package tests,
  reproducible generated assets/notices, and 202 native React-page assertions
  pass. Real-manager access, runtime, host, workflow, execution, streaming, and
  permission-execution suites pass. Production browser queue, history,
  edit/regenerate, composer, tool, and responsive suites also pass; two stale
  Shell-inventory fixture boundaries found during release verification were
  corrected and rerun successfully. The corrected real-worker cancellation
  fixture also passes 50 normal and 10 race-enabled consecutive repetitions.
- The complete mocked layout matrix remains red at 170 of 2,058 reports because
  of the already-disclosed BUG-176 transport drift and BUG-196 reader anchors.
  The supplemental permission-policy matrix retains 14 BUG-175 reconciliation
  failures. These fixture/presentation limitations do not replace the passing
  real HTTP/RPC/worker suites and remain listed above as known alpha issues.
- Secret-free live inference passes for OpenCode Go API-key authentication,
  ChatGPT/Codex OAuth, and the configured authenticated OpenAI-compatible
  endpoint. The two non-thinking models required an explicit `--thinking off`
  override instead of the saved `high` default. The optional `llm-studio`
  profile had no resolvable credential and was not counted as a live pass.
- The local environment did not provide `govulncheck`; the pinned reachable-code
  scan remains part of the required CI gate. Publication still requires
  successful CI and Documentation push workflows for the exact release commit
  before the immutable tag is created.

## [0.1.0-alpha.9] - 2026-09-11

This alpha refreshes the terminal interface, adds branch-local plugin workflows
and safe plugin reload, and makes plugin authoring references available without
a bundled skill. It also improves live model discovery and input reliability.

### Added

- Branch-local plugin workflow state, intersected tool restrictions, pure
  session/compaction gates, and session-change events shared by TUI, RPC, and
  SDK consumers. Agent Profiles and Workflow Guard demonstrate these APIs.
- Explicit reload of an already-loaded, enabled JavaScript plugin through
  `/plugins reload <id>`, the plugin inspector, RPC, or the Go SDK. Reload
  preserves persisted state and refuses busy sessions rather than cancelling work.
- Deferred, read-only `snow_plugin_docs` with embedded API declarations,
  fixtures, examples, and safe registered/loaded plugin inventory. References
  work outside a source checkout and do not authorize plugin execution.
- Persistent plugin and skill enablement controls, a searchable plugin
  inspector, TypeScript authoring scaffolds and fixtures, and a cat-pet example.
- Enhanced terminal keys, OSC 52 clipboard paste, automatic terminal-appearance
  updates, and terminal activity/progress indicators and attention alerts.
  Title/progress indicators default to enabled and notifications to `unfocused`;
  use `/settings` or `tui.terminal_title`, `tui.terminal_progress`, and
  `tui.notifications: "off"` to opt out (see [Configuration](docs/configuration.md)).

### Changed

- Migrate the terminal UI to Charm v2. Native selection, permission, process,
  and subagent panels use centered, bounded cards with resize-aware selection
  and drafts; composer completions remain attached to the input.
- Refresh stale model catalogs in the picker. OpenCode Zen discovers new free
  models using live catalog/pricing metadata; ChatGPT uses an updated Codex
  compatibility version and exposes GPT-6 Astra when available to the account.
- Reduce transcript/composer wrapping and layout work. Branch panels build the
  parent index once per card rather than once per row.

### Compatibility and migration

- The built-in `snow-js-plugin` skill is removed in favor of `snow_plugin_docs`.
  Old `skills.overrides.snow-js-plugin` policy has no effect unless a same-named
  filesystem skill is installed. Explicit tool allowlists must include
  `snow_plugin_docs` to use the new reference tool; `--no-skills` and
  `--no-plugins` do not disable its read-only references.
- Plugin and skill registration/enablement changes apply after restart. Plugin
  reload only replaces an already-loaded, enabled JavaScript package and does
  not install dependencies or build TypeScript. Existing API 1 plugins remain
  supported; custom session stores need the optional workflow interface for
  workflow operations, and custom registries need atomic replacement support
  for reload.
- The SQLite schema remains at version 12 and exact history stays append-only,
  but older binaries do not enforce the new workflow tool restrictions when
  resuming sessions. Back up session databases with Snow stopped before
  upgrading. To roll back, restore that pre-upgrade backup and compatible plugin
  packages; sharing a schema version does not make workflow policy backward
  compatible. See [Plugin workflows](docs/plugin-workflows.md).
- Source builds still require Go 1.27rc3; binary users do not need Go installed.

### Fixed

- Preserve modified Enter, selection, multiline history drafts, large paste,
  and clipboard request ownership; copying no longer quits or aborts Snow.
- Settle manual compaction accurately, ignore errors from completed turns,
  alert on failed automatic goal compaction, and avoid signal-shutdown deadlocks.
- Keep modal input from navigating the background transcript, and disable
  permission approval when required review context cannot fit in the card.
- Avoid plugin session-gate lock reentry, reject stale inactive-branch workflow
  writes, and enforce aggregate workflow snapshot budgets.
- Admit read-only plugin references in Plan Mode child roles without granting
  additional execution or mutation authority.

### Known alpha limitations

- Compact session/branch panels can hide rename, delete, and fork key hints;
  the shortcuts still work. Enlarge the terminal to see management hints.
- Plugins remain trusted local code running with the user's OS privileges,
  not a sandbox. Node/browser APIs and runtime npm loading are unavailable.
- Binaries are not code-signed or notarized. Checksums verify asset integrity
  against the published bundle, not an independent signature.

### Validation

- Local full Go tests, vet, support-script tests, benchmark regression guard,
  standalone SDK tests/execution, and embedded-reference synchronization pass
  in an isolated release worktree. Internal/SDK race checks and focused TUI
  race tests also passed during pre-release feature verification.
- Manual live inference passed for anonymous OpenCode Zen, OpenCode Go API-key
  authentication, ChatGPT/Codex OAuth, and an authenticated OpenAI-compatible
  endpoint. An additional local compatible profile could not complete model
  discovery; it is not counted as a successful live check. No credentials or
  provider-private response data are included in release evidence.
- Publication requires successful CI and Documentation push runs for the exact
  release commit, followed by the immutable-tag archive/checksum workflow.

## [0.1.0-alpha.8] - 2026-09-09

This alpha adds local JavaScript extensions across the terminal, CLI, RPC, and
Go SDK, with native plugin panels and workflow commands. It also includes the
goal, file-edit, and subagent lifecycle fixes made since alpha.7.

### Added

- Trusted local Goja packages with `snow-plugin.json`, permissioned tools,
  asynchronous workflow commands, lifecycle hooks, scoped SQLite state, typed
  settings, selected child tools, tool-result cards, and declarative UI themes.
- `snow plugin` registration, inspection, validation, execution, and JavaScript
  or TypeScript scaffolding. Explicit `--js-plugin` loading remains available.
- Workspace Notes, UI Studio, Prompt Recipes, Session Pilot, dashboard,
  project-context, TODO search, Git review, and review-team examples, with
  offline CLI/SDK/RPC smoke coverage and editor typings.

### Changed

- Plugin screens and input dialogs use centered native cards with visible
  focus, bounded scrolling, compact controls, and drafts preserved on resize.
- Closed plugin selections omit Other; confirmations default to No. Optional
  `choices_only` metadata lets RPC/SDK clients represent these constraints.
- README and GitHub Pages navigation now lead with installation and task guides,
  while implementation and maintainer references remain in the repository.

### Compatibility and migration

- External executable plugins, their JSON-RPC host, the `--plugin` flag, and
  public `PluginSpec` / `ExternalToolDefinition` APIs have been removed. Legacy
  `plugins` configuration stays inert. Migrate local scripts to JavaScript
  packages and `js_plugins`, or use MCP for external tool servers. Statically
  supplied Go plugins remain supported.
- Session databases upgrade automatically to schema 12 to store selected child
  plugin tools. Exact history remains append-only. Back up session databases
  with Snow stopped before upgrading if rollback is needed: older binaries
  reject upgraded databases, so rollback requires restoring the backup.
- Source builds still require Go 1.27rc3. Binary installations do not require Go.

### Fixed

- Enforce exhausted goal budgets before substantive tool work, count usage for
  goals created during a prompt, and stop repeated blocked-goal continuations.
- Revalidate file edits before replacement to detect concurrent saves. Keep
  accepted child follow-ups pending through execution or cancellation, and
  settle interrupted child state consistently across waits and shutdown.
- Keep plugin request context compatible with provider adapters, validate
  typed form values, preserve runtime cancellation ownership, and enforce
  explicit child tool selection without widening role permissions.
- Tie plugin dialogs to their own request lifetime; unrelated root events no
  longer dismiss them and cancellation no longer leaves a stale dialog.
- Discard failed plugin branch transitions, close command-owned children on
  failure, and suppress late command output from a previous branch.
- Prefer an exactly typed plugin alias over longer prefixes, keep selected
  panel actions visible, and remove duplicate or generic dialog feedback.

### Known alpha limitations

- JavaScript packages are trusted local code, not a security sandbox. Runtime
  time, call-stack, host-call, and output limits do not provide heap or OS
  isolation. Node APIs and npm module loading are unavailable at runtime;
  bundle supported JavaScript before loading it and restart Snow after edits.
- Headless dialogs require an explicit trusted input broker. Plugin commands
  and child workflows use ordinary provider credentials, permissions, and usage.
- Binaries are not yet code-signed or notarized. Release checksums establish
  asset integrity against the published bundle, not an independent signature.

### Validation

- Local full Go tests, internal/SDK race checks, vet, standalone SDK execution,
  all 56 support-script tests, benchmark guards, both plugin smoke packs, and
  formatting checks passed. The pinned vulnerability scan found no reachable
  vulnerabilities; four advisories in required modules were not identified as
  called by Snow.
- Live plugin request-hook prompts passed with OpenCode Go, OpenCode Zen,
  ChatGPT/Codex OAuth, and the configured OpenAI-compatible endpoint. The
  optional local `llm-studio` profile was not authenticated and was not counted
  as a passing provider smoke. No credentials are included in release evidence.

## [0.1.0-alpha.7] - 2026-09-05

This alpha improves cancellation and goal accounting, prevents admission
deadlocks, hardens Bash permission analysis, and reduces context, search,
terminal-preview, and process-capture overhead.

### Added

- Added Bash effect preflight that checks planned reads, writes, and execution
  against path policy before running supported commands, with protected-resource
  identity checks and explicit handling of ambiguous shell effects.
- Added highlighted file mentions and visible composer-only Ctrl+A selection.

### Compatibility

- Bash preflight hard denials also apply in `allow` mode, and prior remembered
  shell approvals are invalidated by the new analysis scope. Static analysis
  does not provide process containment.
- Core and SDK prompt methods now return caller cancellation/deadline errors;
  canceled active goals pause instead of silently continuing. Hosts should
  handle `context.Canceled` and `context.DeadlineExceeded`.
- Compaction usage uses existing append-only metadata without a schema migration.
  Previously unrecorded summary usage cannot be recovered, and older binaries
  omit the new summary metadata from totals.

### Fixed

- Prevent intermittent macOS failures when separate Snow processes create the
  shared keybindings update lock at the same time.
- Return caller cancellation and deadline errors from core and SDK prompts,
  pause the admitted active goal, and prevent automatic work from restarting
  after the caller cancels.
- Make prompt and subagent-manager admission waits cancelable, so Plan mode,
  compaction, branch/fork controls, and replacement prompts can stop automatic
  work without deadlocking on its manager tools.
- Include provider-reported compaction usage in session totals and automatic
  goal budgets, including failed summary attempts, without inflating
  conversational context usage.

- Make RPC event-delivery loss fail visibly and classify timed-out subagents
  as interrupted rather than successful.

### Performance

- Reduce repeated allocations in context pruning and checkpoint lookup, and
  reuse bounded per-search ignore rules while avoiding redundant grep copies.

- Build checkpoint section bodies incrementally, avoid copying safe terminal
  text, and decode only the prefix needed by truncated display labels.
- Reduce copying during bounded subprocess log capture using reusable buffer
  storage. A full default buffer reserves an additional 256 KiB; retained log
  content remains capped at 1 MiB. See the
  [measured results](docs/runtime-fixes-performance.md).

### Manual provider validation

- OpenCode Go, anonymous OpenCode Zen, and ChatGPT/Codex OAuth completed live
  response checks on 2026-09-05.
- The configured OpenAI-compatible endpoint returned HTTP 503 for both models
  it advertised, so a successful live compatible-provider response could not
  be verified. Local mocked compatibility tests passed; this service-side
  availability limitation remains recorded rather than counted as a pass.

## [0.1.0-alpha.6] - 2026-09-04

This alpha prevents silent headless-output truncation and hardens concurrent
configuration updates, OAuth shutdown, prior-session search caching, and
physical session-fork cleanup without changing public interfaces or persisted
formats.

### Fixed

- Made print and JSON modes return an explicit error if slow output causes their
  monitored event subscription to be evicted, instead of silently returning an
  incomplete response.
- Serialized typed and raw-section configuration mutations under one
  cross-process update lock, preventing concurrent plugin, MCP, Agent Skill,
  and ordinary settings writes from overwriting each other while preserving
  unknown JSON fields.
- Gave ChatGPT OAuth workers TUI-lifetime cancellation and ownership so shutdown
  cancels and joins them without breaking in-session cancellation completion.
- Excluded the active session database and its SQLite sidecars from the
  prior-session FTS corpus and cache identity, avoiding unnecessary index
  rebuilds after active-session writes while retaining historical invalidation.
- Removed temporary staging lock files after physical session forks while
  preserving the published destination session's live lease.

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
