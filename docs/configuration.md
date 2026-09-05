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
- [Provider retry](#provider-retry)
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
| `~/.snow/diagnostics/` | Explicit diagnostic dumps with generated names | Atomic mode-`0600` JSON files; created only on request or configured close-time dump |
| `<project>/.snow/config.json` | Restricted project extensions/preferences | Read only after project trust is allowed |
| `<project>/.snow/keybindings.yaml` | Project key overrides | Trusted project only |
| `<project>/.snow/themes/*.yaml` | Project custom themes | Trusted project only; wins by theme name |
| `<project>/.snow/search.yaml` | Project search overlay | Trusted project only; additive lists |

Environment overrides:

| Variable | Effect |
|---|---|
| `SNOW_HOME` | Replaces the global `~/.snow` directory for config, auth, trust, caches, oversized goal content, themes, keys, and search policy |
| `SNOW_SESSIONS_DIR` | Replaces the session database root, including durable Thread Goal state |
| `OPENCODE_API_KEY` | Fallback credential for `opencode-go` and optional credential for `opencode-zen` |
| `OPENAI_API_KEY` | Optional fallback Bearer credential for the legacy `openai-compatible` profile only |
| `XDG_DATA_HOME` | Included when discovering compatible OpenCode ChatGPT credentials |
| `SNOW_DEBUG` | File path for TUI debug logs, for example `SNOW_DEBUG=/tmp/snow.log`; intended for development |

`SNOW_DEBUG` is the pre-existing Bubble Tea development logger and is separate
from shared diagnostic capture. It does not enable `debug.enabled`, event
recording, or diagnostic dumps.

All Snow-managed `config.json` read-modify-write operations—including settings,
plugin, MCP, and skill changes—share a process-wide and cross-process lock before
atomic replacement. Concurrent Snow processes therefore apply changes to the
latest committed file instead of overwriting unrelated updates from stale
snapshots.

## Precedence

Runtime selection generally follows this order:

1. Explicit CLI flags or `snowsdk.Options`
2. Global `config.json`
3. Built-in defaults

The base system preamble has a more specific precedence: explicit SDK
`SystemPrompt`, trusted-project `system_prompt_file`, global
`system_prompt_file`, then Snow's embedded default preamble. Project
`AGENTS.md` files and runtime mode/skill guidance are appended separately.

Trusted project configuration is a separate, deliberately narrow overlay. It
may add plugins, MCP servers, and skill policy, select a confined project
system preamble, and override only the project TUI theme and compaction
preferences described below. It cannot select provider credentials, permission
mode, model, provider-retry policy, or global tool authority.

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

Every fresh interactive session starts in permission mode `ask`. An explicit
`--permission ask|allow|deny` overrides that baseline for the current launch.
`/permissions` and the TUI Settings permission row change only the active
session; that state and remembered rules are restored when the same session is
resumed, but are not inherited by a new session or project. Both `bash` and
`process_start` analyze POSIX source before this permission gate. Selected
protected effects are hard-denied; reusable approvals include the exact source,
working directory, launch-environment digest, analyzer/specification version,
policy, and inferred resources. Unknown effects allow only one-time approval.
Approved shell commands still execute as unrestricted host processes. For
upgrade compatibility, the removed `permission_mode` field is ignored in both
global and project configuration and cannot change the launch baseline. Delete
it when convenient; use `--permission` or the active-session TUI controls
instead.

Add custom protected files or directory trees in the **global** configuration:

```json
{
  "shell_protected_paths": ["/absolute/path/to/private-data"]
}
```

This additive policy accepts up to 128 absolute paths, each at most 4096 bytes.
It denies statically visible shell reads, writes, and deletes at each path and
below it. It does not change file-tool roots or provide OS containment. Built-in
protections stay active, project configuration cannot override these additions,
and changes take effect on the next launch.

The SDK intentionally defaults `PermissionMode` to `deny` when omitted. See
[SDK permissions](sdk.md#handle-permissions-and-input).

## Global config.json

A representative configuration:

```json
{
  "default_provider": "opencode-zen",
  "default_model": "big-pickle",
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
  "fixed_context_budget_percent": 25,
  "system_prompt_file": "system.md",
  "providers": {
    "opencode-go": {
      "base_url": "https://opencode.ai/zen/go/v1",
      "default_model": "kimi-k2.6"
    },
    "opencode-zen": {
      "base_url": "https://opencode.ai/zen/v1",
      "default_model": "big-pickle"
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
  "debug": {
    "enabled": false
  },
  "skills": {
    "disabled": false,
    "dirs": [],
    "include_claude": false,
    "overrides": {}
  },
  "retry": {
    "normal": {
      "max_attempts": 12,
      "max_elapsed_ms": 300000,
      "initial_delay_ms": 1000,
      "max_delay_ms": 30000,
      "jitter_percent": 20
    },
    "goal": {
      "max_attempts": 30,
      "max_elapsed_ms": 1800000,
      "initial_delay_ms": 2000,
      "max_delay_ms": 120000,
      "jitter_percent": 20
    }
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
| `default_provider` | `opencode-zen` | Global fallback provider ID for projects without a remembered selection; works anonymously |
| `default_model` | provider default | Global fallback model ID; provider-specific config may also declare a default |
| `default_project_trust` | `ask` | `ask`, `allow`, or `deny`; legacy `always`/`never` are aliases |
| `thinking` | `off` | Global fallback effort: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra` |
| `project_selections` | `{}` | Operator-owned absolute working-directory map populated by TUI or RPC model/thinking changes; each entry stores `provider`, `model`, and `thinking` |
| `reasoning_summary` | `auto` | `off`, `auto`, `concise`, or `detailed`; TUI and RPC settings persist this global preference |
| `text_verbosity` | `low` | `low`, `medium`, or `high`; TUI and RPC settings persist this global preference |
| `collaboration_mode` | `default` | `default` or `plan`; branch persistence may restore a saved mode |
| `plan_mode_reasoning_effort` | Plan preset | Optional explicit normalized thinking level |
| `tool_output_bytes` | `262144` | Bound for provider-facing tool results and previews |
| `bash_timeout_ms` | `120000` | Operator cap for foreground host shell execution |
| `processes.max_running` | `4` | Maximum concurrently running app-owned background process groups (`1..32`) |
| `processes.max_records` | `32` | Maximum running and terminal runtime records; must be at least `max_running` and at most `256` |
| `processes.retained_output_bytes` | `1048576` | Newest combined stdout/stderr bytes retained per process (`65536..16777216`) |
| `context_cap_bytes` | `102400` | Hard cap for loaded project instructions and maximum configured system-prompt file size |
| `fixed_context_budget_percent` | `25` (`10..50`) | Operator-owned admission budget for recurring system instructions, active skills, conditional guidance, and exposed tool schemas as a share of the selected model window; unknown windows use a 32,768-token fallback |
| `system_prompt_file` | unset | Markdown/text file replacing the embedded base preamble; relative paths resolve from the loaded config file's directory (normally the global config directory; `--config`/`ConfigPath` can override it) and `~` is supported |
| `debug.enabled` | `false` | Persisted opt-in for shared bounded diagnostic event capture across TUI, print/JSON, RPC, and SDK runtimes. The TUI **Debug diagnostics** setting, `/debug on|off`, and RPC `debug_enable`/`debug_disable` update this global field. |
| `updates.check_on_startup` | `false` | Opt in to an asynchronous metadata-only GitHub release check after interactive TUI startup. A newer eligible release opens an **Install update** / **Skip for now** confirmation; Snow never downloads an archive or installs automatically. Approved installs show foreground byte, percentage, verification, and installation progress. Headless and SDK startup never perform this check. |

Update policy is global/operator-owned; trusted project configuration cannot
override it. Startup checking defaults to disabled. `/settings` and
`settings_update` can configure the check preference, but starting an RPC
runtime does not itself check for or install updates. The removed
`updates.auto_update` field is ignored when reading older alpha configurations
and disappears on the next configuration rewrite.

`--debug` and `--no-debug` override `debug.enabled` for one CLI process without
rewriting configuration. `--debug-dump PATH` enables capture and writes a
final dump during normal shutdown; it conflicts with `--no-debug`. An omitted
or blank dump path creates a unique JSON file under
`$SNOW_HOME/diagnostics`; relative paths resolve from Snow's working directory.
The runtime can also create a dump through `/debug dump [PATH]`, RPC, or the Go
SDK. Capture remains disabled by default, and disabling it retains existing
records until they are explicitly cleared.

The fixed-context budget measures the final serialized system prompt and exposed
schemas; it does not count conversation messages or internal turn fragments.
Snow never trims tools or skill instructions to meet it. New skill activation
and model changes that would increase the runtime above the budget are rejected
with an actionable error. Routed or explicitly discovered schema expansion is
checked again before every provider continuation and cannot increase a request
past the budget. Existing resumed branches are grandfathered without
data loss and `/context` reports their over-budget state so the operator can
clear skills, reduce configured guidance, or select a larger-window model.
Project configuration cannot raise this global operator limit.

Model capabilities remain authoritative. An explicit CLI/SDK reasoning level
that is not advertised by the selected model is rejected. If refreshed backend
metadata withdraws a previously remembered project effort, startup resets that
project tuple to `off` and reports a configuration diagnostic. In the TUI,
selecting a model that does not support the current level likewise atomically
resets thinking to `off`, persists the provider/model/effort tuple for the
active working directory, and reports the adjustment instead of leaving the
next prompt in an invalid state.
A different project directory retains its own tuple across restarts. The global
`default_provider`, `default_model`, and `thinking` values remain fallbacks and
are not rewritten by `/model`, `/thinking`, the settings picker, or the
equivalent RPC controls. Explicit CLI/SDK options override the remembered
project tuple for that process.

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
`process_stop`, a session switch, or normal app shutdown. Session switching
stops and clears the active managed-process inventory.
`process_logs.max_bytes` remains capped
by `tool_output_bytes`; log waits, readiness checks, stop grace, and shutdown
also have fixed runtime bounds.

## Providers

Use [Providers](providers.md) for authentication and launch commands. The
`providers` object stores non-secret defaults for built-in providers and named
OpenAI-compatible profiles:

```json
{
  "default_provider": "opencode-zen",
  "default_model": "big-pickle",
  "providers": {
    "opencode-zen": {
      "default_model": "big-pickle"
    },
    "x-provider": {
      "type": "openai-compatible",
      "base_url": "https://gateway.example/v1",
      "default_model": "model-id",
      "stream_idle_timeout_ms": 600000
    }
  }
}
```

Each provider entry supports these fields:

| Field | Purpose |
|---|---|
| `type` | Set to `openai-compatible` for a named compatible profile; omit it for built-in providers |
| `base_url` | Override the provider endpoint; required for a named compatible profile |
| `default_model` | Select the provider's default model |
| `stream_idle_timeout_ms` | Bound silence between streamed bytes; `0` uses 10 minutes and `-1` disables the watchdog |

Positive `stream_idle_timeout_ms` values cannot exceed `86400000` (24 hours).
A compatible endpoint may be an API root or a full URL ending in `/responses`
or `/chat/completions`. If the endpoint cannot supply a usable model list, set
`default_model` or launch Snow with `--model MODEL`.

Create a named profile with the CLI instead of editing it by hand:

```sh
snow login openai-compatible \
  --name x-provider \
  --base-url https://gateway.example/v1

snow --provider x-provider
```

The name becomes the provider selector and credential key. Names use 1–64
lowercase letters, digits, or internal `.`, `_`, and `-` characters. The
reserved IDs are `opencode-go`, `opencode-zen`, `chatgpt`, and `fake`. Named
profiles keep endpoints, model defaults, timeouts, and credentials separate.

> **Warning:** Do not put API keys or OAuth tokens in `config.json`.
> Credentials belong in `~/.snow/auth.json`; use `snow login`, `--api-key`, or
> an environment variable.

`OPENCODE_API_KEY` applies to `opencode-go` and optional authenticated
`opencode-zen` access. `OPENAI_API_KEY` applies only to the unnamed
`openai-compatible` profile; named profiles do not inherit it. Compatible
profiles support Bearer authentication but not custom Azure headers or query
parameters.

## TUI

```json
{
  "tui": {
    "theme": "default",
    "mouse": true
  }
}
```

The four selectable built-in themes are Snow (`default`), Frost (`frost`),
Ember (`ember`), and Aurora (`aurora`). Every built-in adapts its complete
semantic palette to the terminal's reported light/dark background, including
Markdown rendered inside the transcript. Snow keeps terminal backgrounds
transparent. Legacy names (`dark`, `light`, `high-contrast`, `nord`, `dracula`,
and `gruvbox`) remain accepted for saved configurations and custom-theme
inheritance but are hidden from the Settings cycle. Any other valid name refers
to a custom theme file. Snow always uses Bubble Tea's
alternate-screen, app-owned transcript viewport so scrolling cannot expose
stale rendered headers or composer chrome. The default `mouse: true` keeps
wheel/trackpad gestures inside Snow's transcript viewport and provides
highlighted drag selection, edge auto-scroll, and OSC 52 copy. Apple Terminal
users can hold Fn while dragging for instant terminal-native selection without
disabling wheel handling. Right-click opens Snow's **Copy selection** context
menu without changing mouse mode. F6 toggles explicitly, and `mouse: false` starts
natively. In native mode wheel gestures may scroll terminal history;
PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down still scroll Snow.

## Provider retry

The global `retry` policy controls one centralized, cancellation-aware provider
recovery loop. Provider adapters classify failures and expose `Retry-After`, but
do not run separate transient retry schedules. Both the attempt and elapsed
limits must permit another request.

| Profile | Attempts | Elapsed window | Initial / maximum delay |
|---|---:|---:|---:|
| `normal` | 12 | 5 minutes | 1 second / 30 seconds |
| `goal` | 30 | 30 minutes | 2 seconds / 2 minutes |

Backoff is exponential with the configured `jitter_percent`. A valid provider
`Retry-After` is a minimum delay; when it exceeds the remaining elapsed window,
Snow stops rather than retrying earlier than requested. Successful provider
rounds reset the consecutive-failure episode.

Only structured network/transport, HTTP 408/425/5xx, stream truncation/idle,
overload, and temporary 429 failures are retried. Authentication, validation,
hard quota/payment, context, persistence, accounting, tool, and cancellation
failures are not. Context overflow retains its separate one-compaction recovery.

This policy is operator-owned global configuration. Project `.snow/config.json`
files cannot modify it. Root and child agents inherit the same effective policy;
`snowsdk.Options.Retry` may supply a runtime-only override. Bounds are 1–100
attempts, elapsed windows up to 24 hours, delays up to 1 hour, and jitter from
0–100 percent.

## Compaction

| Field | Range/default | Meaning |
|---|---|---|
| `retain_tokens` | `0..1000000`, default `0` | Token target retained after compaction; zero selects a model-aware target |
| `min_retained_turns` | `1..100`, default `2` | Minimum complete recent turns to preserve when a turn-prefix plan exists; also the recent-cycle floor for the oversized automatic-goal fallback |
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
long tool-driven turn, completed assistant-call/tool-result cycles may form safe
checkpoint boundaries when no exact retained prior turn would be consumed,
allowing old complete cycles to compact while the current and recent cycles
remain exact. Those boundaries remain eligible after a terminal assistant
response. If an assistant-originated automatic goal is itself the oversized
recent turn and no ordinary complete-turn prefix exists, Snow uses an explicit
progress fallback: it checkpoints the old prefix and retains
`min_retained_turns` newest complete goal cycles instead, plus the unresolved
current cycle while active. The separately injected goal objective remains
exact. This fallback works with an earlier conversation turn or checkpoint but
does not reinterpret an exact user-originated recent turn as a goal. A large
unresolved current batch still cannot cause unrelated history to be compacted.
Each trigger applies to ordinary, goal, Plan, and subagent turns; set either to `0`
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
- open identities: `1..4096`, and not below concurrency; closed identities remain durable but do not count;
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

`<project>/.snow/config.json` accepts only this restricted shape. Running the
TUI `/init` command creates a minimal `{}` file when this project config is
missing; it never overwrites an existing file or copies global settings and
credentials into the project.

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
  - vendor
  - dist
  - build
  - coverage
exclude:
  - "**/*.min.*"
  - "tmp/**"
```

Boolean project values override global values. `generated_dirs` and `exclude`
are additive and deduplicated. Per-call `hidden`, `include_ignored`, and
`exclude` options can alter soft policy, but `.git` and symlink entries remain
hard exclusions.

## Keybindings

Global: `$SNOW_HOME/keybindings.yaml`

Project: `<project>/.snow/keybindings.yaml` after trust

Use `/keybindings` in the TUI, or select **Keybindings** from `/settings`, to
edit these files interactively. The popup defaults to global scope; `S` toggles
to trusted-project overrides. Changes are validated, saved atomically, and
applied to the running TUI immediately. RPC clients use `keybindings_get` for
the same deterministic layered view and `keybindings_update` for the same
validated, locked, atomic update path. Resetting a global action removes its
override and restores the built-in default; resetting a project action removes
the project override so it inherits the global/default value. Manual YAML
editing remains supported.

```yaml
version: 1
bindings:
  submit: [enter]
  newline: [ctrl+j]
  follow_up: [alt+enter]
  toggle_mode: [shift+tab]
  thinking: [ctrl+t]
  models: [alt+m]
  agents: [alt+a]
  processes: [alt+p]
  picker_up: [up, k]
  picker_down: [down, j]
```

Supported actions:

```text
submit follow_up newline paste abort quit toggle_mode thinking models agents processes
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
of the four selectable built-ins or a supported legacy built-in listed above.
Custom names cannot replace current or legacy built-in names, exceed 64 runes,
contain control characters, or contain `/` or `\\`. Colors are optional
semantic overrides using `#RRGGBB` or ANSI `0..255`. Each light/dark value styles
both TUI chrome and Markdown while backgrounds remain terminal-owned. Project
themes replace same-named global themes. RPC clients use `themes_list` to read
the resolved catalog and selected name; `settings_get`/`settings_update` read
and change the same persisted `theme` setting used by the TUI.

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
