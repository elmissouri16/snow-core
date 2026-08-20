# Configuration

Snow separates runtime preferences, credentials, project trust, sessions, and
auxiliary TUI/search files. This document describes scope, precedence, paths,
and the supported global/project fields.

> **Note:** Global `config.json` is merged onto built-in defaults and validated
> at startup. Auxiliary YAML files are warn-and-fallback, as described in
> [Diagnostics and failure behavior](#diagnostics-and-failure-behavior).

## On this page

- [Paths](#paths)
- [Precedence](#precedence)
- [Global config.json](#global-configjson)
- [Providers](#providers)
- [TUI](#tui)
- [Compaction](#compaction)
- [Skills](#skills)
- [Subagents](#subagents)
- [Plugins and MCP](#plugins-and-mcp)
- [Trusted project configuration](#trusted-project-configuration)
- [Search policy](#search-policy)
- [Keybindings](#keybindings)
- [Themes](#themes)
- [Diagnostics and failure behavior](#diagnostics-and-failure-behavior)
- [Related documents](#related-documents)

## Paths

| Path | Purpose | Notes |
|---|---|---|
| `~/.snow/config.json` | Global runtime configuration | JSON; created/updated by TUI settings and management commands |
| `~/.snow/system.md` | Suggested custom system preamble | Optional; loaded only when selected by `system_prompt_file` |
| `~/.snow/auth.json` | Provider credentials | Atomic writes, mode `0600` |
| `~/.snow/trust.json` | Canonical project decisions | Atomic locked writes, mode `0600` |
| `~/.snow/sessions/` | Per-project SQLite sessions | Child histories live beside root databases under `.db.agents/` |
| `~/.snow/keybindings.yaml` | Global TUI key overrides | Strict versioned YAML, maximum 64 KiB |
| `~/.snow/themes/*.yaml` | Global custom themes | Up to 64 regular non-symlink files |
| `~/.snow/search.yaml` | Global grep/glob policy | Strict versioned YAML |
| `<project>/.snow/config.json` | Restricted project extensions/preferences | Read only after project trust is allowed |
| `<project>/.snow/keybindings.yaml` | Project key overrides | Trusted project only |
| `<project>/.snow/themes/*.yaml` | Project custom themes | Trusted project only; wins by theme name |
| `<project>/.snow/search.yaml` | Project search overlay | Trusted project only; additive lists |

Environment overrides:

| Variable | Effect |
|---|---|
| `SNOW_HOME` | Replaces the global `~/.snow` directory for config, auth, trust, caches, goals, themes, keys, and search policy |
| `SNOW_SESSIONS_DIR` | Replaces only the session database root |
| `OPENCODE_API_KEY` | Fallback credential for `opencode-go` |
| `OPENAI_API_KEY` | Optional fallback Bearer credential for the legacy `openai-compatible` profile only |
| `XDG_DATA_HOME` | Included when discovering compatible OpenCode ChatGPT credentials |
| `SNOW_DEBUG` | File path for TUI debug logs, for example `SNOW_DEBUG=/tmp/snow.log`; intended for development |

## Precedence

Runtime selection generally follows this order:

1. Explicit CLI flags or `snowsdk.Options`
2. Global `config.json`
3. Built-in defaults

The base system preamble has a more specific precedence: explicit SDK
`SystemPrompt`, trusted-project `system_prompt_file`, global
`system_prompt_file`, then the embedded `internal/context/system.md`. Project
`AGENTS.md` files and runtime mode/skill guidance are appended separately.

Trusted project configuration is a separate, deliberately narrow overlay. It
may add plugins, MCP servers, and skill policy, select a confined project
system preamble, and override only the project TUI theme and compaction
preferences described below. It cannot select provider credentials, permission
mode, model, or global tool authority.

Credentials use a separate order:

1. Explicit `--api-key` or `snowsdk.Options.APIKey`
2. Snow's auth store
3. A known environment fallback such as `OPENCODE_API_KEY` or `OPENAI_API_KEY`

This precedence is implemented once by Snow's provider-scoped auth service.
Provider modules register either the reusable API-key driver or their own OAuth
driver; the agent and subagents receive credential-free provider runtimes.
Model discovery and inference therefore use the same resolved provider
credential. OAuth endpoint, scope, claim, and token-exchange details remain
isolated in the provider's auth driver.

The SDK intentionally defaults `PermissionMode` to `deny` when omitted, even if
the global interactive default is `ask`. See
[SDK permissions](sdk.md#permissions-and-security).

## Global config.json

A representative configuration:

```json
{
  "default_provider": "opencode-go",
  "default_model": "kimi-k2.6",
  "permission_mode": "ask",
  "default_project_trust": "ask",
  "thinking": "off",
  "project_selections": {
    "/home/user/code/project-a": {
      "provider": "openai-compatible",
      "model": "model-id",
      "thinking": "off"
    }
  },
  "reasoning_summary": "auto",
  "text_verbosity": "low",
  "collaboration_mode": "default",
  "plan_mode_reasoning_effort": "medium",
  "tool_output_bytes": 262144,
  "bash_timeout_ms": 120000,
  "processes": {
    "max_running": 4,
    "max_records": 32,
    "retained_output_bytes": 1048576
  },
  "context_cap_bytes": 102400,
  "system_prompt_file": "system.md",
  "providers": {
    "opencode-go": {
      "base_url": "https://opencode.ai/zen/go/v1",
      "default_model": "kimi-k2.6"
    },
    "openai-compatible": {
      "base_url": "https://gateway.example/v1",
      "default_model": "model-id"
    },
    "x-provider": {
      "type": "openai-compatible",
      "base_url": "https://other-gateway.example/v1",
      "default_model": "other-model"
    },
    "chatgpt": {}
  },
  "tui": {
    "theme": "default",
    "mouse": true
  },
  "skills": {
    "disabled": false,
    "dirs": [],
    "include_claude": false,
    "overrides": {}
  },
  "subagents": {
    "enabled": false,
    "recursive": false,
    "max_concurrent_threads": 4,
    "max_agents_per_session": 32,
    "max_depth": 1,
    "min_wait_timeout_ms": 10000,
    "default_wait_timeout_ms": 30000,
    "max_wait_timeout_ms": 3600000,
    "task_timeout_ms": 1800000,
    "max_result_bytes": 65536,
    "durable": true,
    "allow_mutation": false,
    "expose_child_tool_events": true,
    "default_role": "general"
  },
  "compaction": {
    "retain_tokens": 0,
    "min_retained_turns": 2,
    "summary_max_tokens": 2000,
    "fallback": "local",
    "guidance": "",
    "auto_threshold_percent": 80,
    "tool_history_budget_percent": 20,
    "tool_result_inline_bytes": 16384,
    "artifact_max_bytes": 4194304,
    "historical_tool_result_threshold_bytes": 8192
  },
  "plugins": [],
  "mcp_servers": {}
}
```

You may omit defaults. Snow merges the file onto a built-in configuration and
fills required zero-value defaults before validation.

### Core fields

| JSON field | Values / default | Meaning |
|---|---|---|
| `default_provider` | `opencode-go` | Global fallback provider ID for projects without a remembered selection |
| `default_model` | provider default | Global fallback model ID; provider-specific config may also declare a default |
| `permission_mode` | `ask` | Interactive default: `ask`, `allow`, or `deny`; unknown nonempty values are startup errors |
| `default_project_trust` | `ask` | `ask`, `allow`, or `deny`; legacy `always`/`never` are aliases |
| `thinking` | `off` | Global fallback effort: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra` |
| `project_selections` | `{}` | Operator-owned absolute working-directory map populated by interactive model/thinking changes; each entry stores `provider`, `model`, and `thinking` |
| `reasoning_summary` | `auto` | `off`, `auto`, `concise`, or `detailed` |
| `text_verbosity` | `low` | `low`, `medium`, or `high` |
| `collaboration_mode` | `default` | `default` or `plan`; branch persistence may restore a saved mode |
| `plan_mode_reasoning_effort` | Plan preset | Optional explicit normalized thinking level |
| `tool_output_bytes` | `262144` | Bound for provider-facing tool results and previews |
| `bash_timeout_ms` | `120000` | Operator cap for foreground host shell execution |
| `processes.max_running` | `4` | Maximum concurrently running app-owned background process groups (`1..32`) |
| `processes.max_records` | `32` | Maximum running and terminal runtime records; must be at least `max_running` and at most `256` |
| `processes.retained_output_bytes` | `1048576` | Newest combined stdout/stderr bytes retained per process (`65536..16777216`) |
| `context_cap_bytes` | `102400` | Hard cap for loaded project instructions and maximum configured system-prompt file size |
| `system_prompt_file` | unset | Markdown/text file replacing the embedded base preamble; relative paths resolve from the loaded config file's directory (normally the global config directory; `--config`/`ConfigPath` can override it) and `~` is supported |

Model capabilities remain authoritative. An explicit reasoning level that is
not advertised by the selected model is rejected. In the TUI, selecting a model
that does not support the current level atomically resets thinking to `off`,
persists the provider/model/effort tuple for the active working directory, and
reports the adjustment instead of leaving the next prompt in an invalid state.
A different project directory retains its own tuple across restarts. The global
`default_provider`, `default_model`, and `thinking` values remain fallbacks and
are not rewritten by `/model`, `/thinking`, or the settings picker. Explicit
CLI/SDK options override the remembered project tuple for that process.

`project_selections` is stored in the operator-owned global config rather than
trusted project `.snow/config.json`; repository content therefore cannot choose
a provider or reasoning effort. Snow writes normalized absolute directory keys
and preserves unrelated project entries with a locked atomic read-modify-write,
so concurrent Snow instances in different folders do not replace each other's
selection. Global and project configuration files are limited to 4 MiB. The
remembered map is limited to 4,096 projects; updates to an existing entry remain
allowed at capacity, while adding another project fails explicitly. Snow never
silently prunes temporarily unavailable or removable project paths.

The global-only `processes` limits bound managed development servers and their
in-memory output tails. Project configuration cannot raise them. Individual
processes have no wall-clock deadline: they run until natural exit,
`process_stop`, or normal app shutdown. `process_logs.max_bytes` remains capped
by `tool_output_bytes`; log waits, readiness checks, stop grace, and shutdown
also have fixed runtime bounds.

## Providers

`providers` maps provider IDs to:

```json
{
  "base_url": "https://example.invalid/v1",
  "default_model": "model-id",
  "stream_idle_timeout_ms": 600000
}
```

`stream_idle_timeout_ms` bounds silence between bytes on a live streaming
response without imposing a total turn deadline. Omit it or set `0` for the
conservative 10-minute default; set `-1` to disable the watchdog. Positive
values above 86,400,000 ms (24 hours) are rejected. Any received bytes reset
the timer.

For `openai-compatible`, `base_url` is required and may be an API root such as
`https://gateway.example/v1` or a full URL ending in `/responses` or
`/chat/completions`. Snow tries the sibling `/models` endpoint, prefers
Responses/SSE, and automatically caches a Chat Completions/SSE fallback when
the Responses endpoint returns HTTP 404, 405, or 501. When neither
`default_model`/`--model` nor a valid discovered model is available, startup
fails with an actionable model-selection error. ID-only model records remain
tool-capable but do not guess vision, reasoning, verbosity, limits, or pricing.
Because standard compatible `/models` responses do not reliably advertise
reasoning efforts, switching from a reasoning-capable model to an ID-only model
in the TUI resets the current effort to `off`; use `/thinking` only when the
selected model advertises additional levels.

The compatible provider's Bearer key is optional. Inside the TUI,
`/login openai-compatible` captures a profile name, endpoint, and optional
masked key. A blank profile name updates the legacy `openai-compatible` entry.
Any other profile is stored as another `providers` map entry with
`"type": "openai-compatible"`; its name must be 1-64 lowercase letters, digits,
or internal `.`, `_`, and `-` characters and must not collide with a built-in
provider. The profile name is also its provider/model selector and `auth.json`
credential key.

The top-level `snow login openai-compatible` remains key-only for the legacy
profile. `snow login openai-compatible --name x-provider --base-url URL`
creates or updates a named profile, after which `snow login x-provider`
addresses it directly. `--api-key` binds to the selected active profile.
`OPENAI_API_KEY` is a fallback only for the legacy profile; named profiles do
not silently share it. Keyless gateways receive no `Authorization` header.

> **Warning:** Do not put API keys or OAuth tokens in `config.json`. Credentials
> belong in `~/.snow/auth.json`.

Named profiles support independent endpoints, default models, stream timeouts,
and Bearer keys. Compatible profiles still do not accept custom/Azure headers
or query parameters. ChatGPT/Codex retains its dedicated backend and OAuth flow.

## TUI

```json
{
  "tui": {
    "theme": "default",
    "mouse": true
  }
}
```

Built-in themes are `default`, `dark`, `light`, `high-contrast`, `nord`,
`dracula`, and `gruvbox`. `default` and `high-contrast` adapt to the terminal's
reported light/dark background; `dark`, `nord`, `dracula`, and `gruvbox` target
dark terminal backgrounds, while `light` targets a light background. Any other
valid name refers to a custom theme file. Snow always uses Bubble Tea's
alternate-screen, app-owned transcript viewport so scrolling cannot expose
stale rendered headers or composer chrome. The default `mouse: true` keeps
wheel/trackpad gestures inside Snow's transcript viewport and provides
highlighted drag selection, edge auto-scroll, and OSC 52 copy. Apple Terminal
users can hold Fn while dragging for instant terminal-native selection without
disabling wheel handling. Right-click opens Snow's **Copy selection** context
menu without changing mouse mode. F6 toggles explicitly, and `mouse: false` starts
natively. In native mode wheel gestures may scroll terminal history;
PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down still scroll Snow.

## Compaction

| Field | Range/default | Meaning |
|---|---|---|
| `retain_tokens` | `0..1000000`, default `0` | Token target retained after compaction; zero selects a model-aware target |
| `min_retained_turns` | `1..100`, default `2` | Minimum complete recent turns to preserve |
| `summary_max_tokens` | `128..32768`, default `2000` | Maximum provider summary output |
| `fallback` | `local` | `local` uses deterministic fallback; `error` fails when provider summary generation or quality validation fails |
| `guidance` | maximum 16 KiB | Additive operator instructions appended to the fixed summary contract |
| `auto_threshold_percent` | `0` or `50..99`, default `80` | Prune and auto-compact all turn types at this context pressure; zero also disables overflow repair |
| `tool_history_budget_percent` | `0` or `5..50`, default `20` | Auto-compact when safely compactable completed tool calls/results exceed this share of the model window; zero disables this independent trigger |
| `tool_result_inline_bytes` | `1024..1048576`, default `16384` | Plain-text result size retained inline before spilling the full retained result |
| `artifact_max_bytes` | inline threshold..64 MiB, default `4194304` | Maximum private spill artifact size |
| `historical_tool_result_threshold_bytes` | `1024..1048576`, default `8192` | Old plain-text result size that triggers ordinary request/summarizer projection pruning |

`auto_threshold_percent` defaults to `80`. At safe boundaries between complete
provider/tool cycles, Snow compacts older complete turns when provider-reported
usage plus significant newly appended context reaches that percentage of the
model context window. `tool_history_budget_percent` independently triggers the
same safe whole-turn compaction when tool calls and bounded tool-result
projections in the eligible old prefix exceed 20% of the model window. Minimum-
retained recent work never counts toward that aggregate trigger. In a single
long active turn, completed assistant-call/tool-result cycles may form safe
checkpoint boundaries when no exact retained prior turn would be consumed,
allowing old complete cycles to compact while the current and recent cycles
remain exact. A large unresolved current batch still
cannot cause unrelated history to be compacted. Each trigger applies to
ordinary, goal, Plan, and subagent turns; set either to `0`
to disable it. Disabling `auto_threshold_percent` also disables one-shot
provider context-overflow repair. For upgrade compatibility, an existing file
that explicitly disables `auto_threshold_percent` (or the legacy goal key) and
omits `tool_history_budget_percent` keeps both automatic triggers disabled; set
the new key explicitly to opt in. The legacy `goal_auto_threshold_percent` key
is accepted only when the new key is absent. Full conversation history remains
append-only.

Compaction preserves the complete append-only conversation history. Checks run
only at safe complete-cycle boundaries, never between an assistant call and its
serial tool-result batch. A pairing validator fails closed if a candidate cut
would separate a tool call and result. The old prefix becomes a durable,
structured working-state checkpoint covering objectives, decisions, files,
verification, failures, agent updates, retrieval references, pending work, and
active-batch state. Snow always merges deterministic objective, prior-checkpoint,
command/result, and failure evidence from the exact compacted prefix, so
provider prose cannot erase an old constraint or a recorded failed check.
Raw provider tool-protocol markup is rejected in favor of the bounded local
fallback and is sanitized from carried prior-checkpoint text. Complete
provider-private continuity disappears from model context
only with its owning old turns; opaque state is never truncated alone.

If compacted history contains tools, Snow also saves one private transcript of
exact text, arguments, result metadata, and image metadata (excluding image
payloads, private reasoning, and provider continuity) and inserts its verified
artifact reference deterministically in the checkpoint. Image payloads remain
in append-only session history. Retrieval manifests are capped at 24 verified
references across repeated compactions. If transcript persistence fails or
exceeds `artifact_max_bytes`, compaction still preserves durable history and
emits a lifecycle warning instead of advertising unavailable retrieval.
Oversized new plain-text results continue to spill individually under
`$SNOW_HOME/artifacts` with bounded previews. `artifact_read` and
`artifact_grep` retrieve bounded fragments without adding that directory to
ordinary file-tool roots. If a provider explicitly reports that a request
exceeds its context window, Snow attempts one automatic compaction and one
retry; it never loops. Project configuration cannot change automatic or
artifact thresholds. Project `guidance` is additive; it cannot remove the
host's factual checkpoint contract.

## Skills

```json
{
  "skills": {
    "disabled": false,
    "dirs": ["/trusted/additional/skills"],
    "include_claude": false,
    "overrides": {
      "pdf": true,
      "unsafe-example": false
    }
  }
}
```

Standard user/project discovery remains enabled unless `disabled` is true.
Per-name overrides can re-enable or suppress a discovered skill without
changing its files. See [Agent Skills](skills.md).

## Subagents

The complete subagent schema includes execution, identity, depth, wait, task,
result, durability, mutation, event, default-model, default-role, and role-map
controls. `subagents.default_provider` and `subagents.default_model`
automatically select a provider/model pair for children. A role's
`provider`/`model` overrides those defaults, and a `spawn_agent` selection
overrides both. If omitted, the child inherits the parent selection. Key bounds
are:

- concurrency: `1..256` child agents; root does not consume a slot;
- identities: `1..4096`, and not below concurrency;
- depth: `1..8`;
- wait timeouts: minimum is nonnegative, default is at least the minimum,
  maximum is at least the default and no more than 24 hours;
- task timeout: positive and at most 24 hours;
- result: `1024..65536` bytes.

Enabling subagents does not enable recursion or mutation. File mutation
requires both `subagents.allow_mutation=true` and a selected role with
`allow_mutation=true`, while the parent tool allowlist remains an upper bound.
See [Subagents](subagents.md) for role examples and the full safety model.

## Plugins and MCP

- `plugins` is an array of public `plugin.PluginSpec` declarations.
- `mcp_servers` maps stable names to public `mcp.ServerSpec` declarations.
  `lifecycle` is `eager` by default, `lazy`, or `lazy-keep-alive`;
  `idle_timeout_ms` is a positive `lazy` session override whose zero value uses
  ten minutes. `cache_bootstrap` is `auto` by default or `explicit` for strict
  startup with no MCP transport work on a missing, expired, or mismatched cache.
  Automatic lazy cache misses bootstrap once, while valid tool, resource, and
  prompt catalogs start disconnected. `lazy-keep-alive` retains its session
  after first activation. Resource subscriptions keep their session connected
  until unsubscribe or shutdown; automatic catalogs with no activation
  descriptor remain eager, while explicit catalogs require `snow mcp cache
  refresh <name>` to discover changes.

Plugin declarations merge by ID with `global < trusted project < explicit
--plugin` precedence; a disabled higher layer suppresses an enabled lower
layer. Manage persisted declarations with
`snow plugin list|get|add|enable|disable|remove`. `add` defaults to disabled,
mutations preserve unknown configuration fields, and all changes require a
restart. Inspection and mutation do not start a plugin; `snow plugin check`
does.

> **Warning:** These processes run with the user's OS privileges. External
> plugins receive their literal configured `env` and otherwise start with an
> empty environment; plugin env values do not expand `${VAR}`. Snow resolves a
> bare `command[0]` using its own launch environment before assigning the child
> env, so prefer absolute interpreter paths and never commit credentials. MCP
> has separate environment/header expansion rules.

See [Plugins](plugins.md) and [MCP](mcp.md) for schemas and management
commands.

## Trusted project configuration

`<project>/.snow/config.json` accepts only this restricted shape:

```json
{
  "plugins": [],
  "mcp_servers": {},
  "skills": {
    "disabled": false,
    "overrides": {
      "project-skill": true
    }
  },
  "tui": {
    "theme": "project-theme"
  },
  "compaction": {
    "min_retained_turns": 4,
    "guidance": "Preserve release verification commands."
  },
  "system_prompt_file": ".snow/system.md"
}
```

Snow resolves trust on a canonical project path before reading this file.
Interactive TUI launches prompt for every undecided project. Headless surfaces
never prompt and treat `ask` as deny. An exact project decision can override an
inherited parent decision. Runtime `/trust` changes apply on the next launch
because loaded extensions cannot be safely hot-unloaded.

Project input loading is pinned to the authorized canonical root. A project
`system_prompt_file` replaces the global/embedded base preamble only after
trust is allowed; relative paths resolve from the project root. The file must
stay under that root, cannot contain symlink components, and is bounded by
`context_cap_bytes`. Project `AGENTS.md` and runtime guidance are still
appended. Project auxiliary paths must likewise stay under the root and cannot
contain symlink components.

## Search policy

Global: `$SNOW_HOME/search.yaml`

Project: `<project>/.snow/search.yaml` after trust

```yaml
version: 1
respect_gitignore: true
respect_ignore: true
hidden: false
generated_dirs:
  - node_modules
  - vendor
  - dist
  - build
  - coverage
exclude:
  - "**/*.min.js"
  - "tmp/**"
```

Boolean project values override global values. `generated_dirs` and `exclude`
are additive and deduplicated. Per-call `hidden`, `include_ignored`, and
`exclude` options can alter soft policy, but `.git` and symlink entries remain
hard exclusions.

## Keybindings

Global: `$SNOW_HOME/keybindings.yaml`

Project: `<project>/.snow/keybindings.yaml` after trust

```yaml
version: 1
bindings:
  submit: [enter]
  newline: [ctrl+j]
  follow_up: [alt+enter]
  toggle_mode: [shift+tab]
  thinking: [ctrl+t]
  agents: [alt+a]
  processes: [alt+p]
  picker_up: [up, k]
  picker_down: [down, j]
```

Supported actions:

```text
submit follow_up newline paste abort quit toggle_mode thinking agents processes
page_up page_down top bottom line_up line_down
picker_up picker_down picker_previous picker_next
picker_page_up picker_page_down picker_top picker_bottom
accept close branch_fork branch_rename branch_delete confirm
```

Keys may be named keys such as `enter`, `esc`, `tab`, `shift+tab`, arrows,
`home`, `end`, `pgup`, and `pgdown`; one-rune keys; or supported `ctrl+`/`alt+`
combinations. Snow rejects collisions inside the same interaction context.
`branch_rename` is reused by both the branch tree and session picker (default
`r`). Emergency `ctrl+c` and modal `esc` bindings are always retained.

## Themes

Global: `$SNOW_HOME/themes/*.yaml`

Project: `<project>/.snow/themes/*.yaml` after trust

```yaml
version: 1
name: glacier
extends: default
colors:
  accent: {light: "#005FAF", dark: "#7DCFFF"}
  muted: {light: "244", dark: "245"}
  foreground: {light: "#202124", dark: "#E8EAED"}
  warning: {light: "#8A4B00", dark: "#FFB86C"}
  error: {light: "#B00020", dark: "#FF6B6B"}
  success: {light: "#137333", dark: "#50FA7B"}
  separator: {light: "250", dark: "238"}
```

`extends` is optional and defaults to `default`; when supplied, it must name one
of the built-ins listed above. Custom names cannot replace built-in names,
exceed 64 runes, contain control characters, or contain `/` or `\\`. Colors are
optional semantic overrides using `#RRGGBB` or ANSI `0..255`. Project themes replace
same-named global themes.

## Diagnostics and failure behavior

Global `config.json` validation errors fail startup. Auxiliary YAML is
warn-and-fallback: invalid files produce diagnostics, valid scopes continue to
load, and the runtime remains usable. Print/RPC modes write configuration
warnings to stderr; the TUI displays them; SDK callers use
`Session.Diagnostics()`.

Auxiliary files are strictly decoded, single-document, regular non-symlink
files bounded to 64 KiB. Theme discovery is capped at 64 files per scope.

## Related documents

- [Using Snow](using-snow.md)
- [Security model](security.md)
- [Sessions](sessions.md)
- [SDK](sdk.md)
