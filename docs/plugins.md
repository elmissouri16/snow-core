# Plugins

Snow's extensibility core has two adapters sharing one permissioned capability
registry:

- statically linked Go plugins implement `pkg/plugin.Plugin`;
- external runtimes implement Snow's JSON-RPC 2.0 JSONL protocol v2.

External runtimes are the supported JavaScript/Python plugin ABI. Snow keeps one
process alive per plugin package, so interpreter startup is paid once while
several related tools share the same runtime.

MCP and skills are intentionally separate. Use [MCP](mcp.md) for interoperable
tools, resources, and prompts; use Snow plugins for Snow-specific lifecycle,
private configuration, progress, and observation-only agent events. Skills are
an instructional resource layer with dedicated activation tools.

References:

- [Complete external protocol v2](plugin-protocol.md)
- [JavaScript/Python architecture research](plugin-js-python-research.md)
- [Tool routing](tool-routing.md)
- dependency-free examples under `examples/plugins/`

## Supervised agent-built plugins

Snow ships an immutable, progressively disclosed `$plugin-builder` Agent Skill
inside the binary. It teaches the root agent to decide between an existing tool,
one-off shell work, MCP, and a Snow-specific plugin; stage dependency-free
Python or JavaScript source; review the source diff, SHA-256 hashes, exact
command, and environment; and run the protocol checker. Its templates and
protocol notes are read through the normal `read_skill_resource` tool.

Start a prompt with `$plugin-builder` to activate it explicitly. The model may
also activate it when a reusable capability is missing. Generation still uses
ordinary `write`/`edit`/`bash` tools and their permissions. The workflow never
gives project trust, tool risk metadata, or skill activation the authority to
execute or persist generated code silently.

The generated declaration is registered disabled, validation is a separate
explicit executable operation, enabling is an explicit configuration mutation,
and the current Snow process must be restarted. Same-session plugin hot loading
is intentionally unsupported.

## Go API

A plugin has a stable manifest, registration phase, and idempotent close:

```go
p := myPlugin{}
s, err := snowsdk.Open(ctx, snowsdk.Options{
    NoSession: true,
    PermissionMode: "allow",
    GoPlugins: []plugin.Plugin{p},
})
```

`Plugin.Register` receives a scoped `plugin.Registrar`. Tool names are local to
the plugin and exposed to the model as `plugin_<plugin-id>_<tool-name>`.
Duplicate IDs, invalid identifiers, malformed schemas, and duplicate names are
rejected before the agent starts.

Tools declare a risk (`read`, `write`, `exec`, or `network`) and all calls pass
through Snow's central permission service. Tool contexts carry cancellation,
session/cwd/call identity, and a bounded progress callback.

Tools may set optional `protocol.ToolDiscovery` metadata. The default is direct
exposure; `mode: deferred` keeps the full schema in the registry and lets the
local router select it per prompt. Routing never bypasses execution permissions.

Go plugins may subscribe to versioned agent events. Handlers are
observation-only: they cannot mutate, veto, or reorder the loop, and panics are
isolated from the host. Delivery is best effort: shutdown stops new emissions
but does not wait for a blocked observer, preventing observer-driven deadlocks.

## External configuration

Global configuration and SDK/CLI options use the same `PluginSpec`:

```json
{
  "plugins": [
    {
      "id": "my-tools",
      "command": [
        "/absolute/path/to/python",
        "-u",
        "/absolute/path/to/plugin.py"
      ],
      "enabled": true,
      "timeout_ms": 120000,
      "max_output_bytes": 262144,
      "env": ["PATH=/usr/local/bin:/usr/bin:/bin"]
    }
  ]
}
```

`command` is argv. Snow never invokes `sh -c` or parses a shell string.

```sh
snow --plugin /absolute/path/to/executable
snow --plugin manifest.json
snow --no-plugins
```

The executable shorthand derives an ID and enables the plugin. A manifest is
required when an interpreter and script need separate argv entries.

Configuration can be inspected and changed without starting any plugin:

```sh
snow plugin list [--all] [--json]
snow plugin get my-tools [--json]
snow plugin add manifest.json [--project] [--replace] [--enable]
snow plugin enable my-tools [--project]
snow plugin disable my-tools [--project]
snow plugin remove my-tools [--project]
```

`plugin add` stages a declaration with `enabled: false` by default, regardless
of the source manifest. Use `--enable` only when registration and execution on
the next launch were reviewed together. `enable`, `disable`, and `remove` also
take effect on the next launch; no management command hot-loads code. A project
enable/disable requires an existing project declaration—Snow never copies a
global command, environment, or private config into the usually commit-visible
project file. Add and replace preserve unrelated configuration fields, while
list/get redact child environments, credential-shaped command/header arguments,
and private runtime configuration.

Global, trusted-project, and repeated explicit `--plugin` declarations merge by
ID in that order. A higher-precedence disabled declaration still suppresses a
lower enabled declaration. `plugin list --all` includes lower-precedence entries
with `shadowed: true`; ordinary list/get report only effective declarations.
Duplicate IDs inside one persisted scope are rejected.

Project `.snow/config.json` entries are loaded only after project trust has an
explicit `allow` decision (or always-trust policy). Denied or unresolved project
entries are not parsed by inspection commands and are reported as trust-blocked.
Disabled entries are not spawned. An explicit `--project` mutation does not
change project trust. Snow does not scan executable directories and has no
plugin marketplace.

### Environment behavior

Snow intentionally supplies the configured `env`; omitted `env` means an empty
child environment. Entries must be unique literal `NAME=VALUE` strings; blank,
whitespace-bearing, duplicate, or assignment-free names are rejected. This
reduces accidental credential inheritance but can affect interpreter shebangs,
subprocess lookup, locale, and certificate configuration.

When `command[0]` has no path separator, Go resolves it using Snow's launch
environment before applying the configured child `env`. The child's `PATH`
affects only the running plugin and subprocesses it starts; it does not select
the already resolved interpreter. Plugin `env` values are literal and do not
expand `${VAR}`.

Prefer an absolute interpreter path, especially a Python virtual environment's
interpreter. Otherwise provide a deliberately minimal `PATH`. Never put secrets
in a committed manifest; use a plugin-owned secure credential store or inject a
runtime-only `PluginSpec` from an embedding host.

## JavaScript and Python quickstart

From the repository root:

```sh
snow plugin check examples/plugins/javascript/manifest.json
snow plugin check examples/plugins/python/manifest.json

snow --plugin examples/plugins/javascript/manifest.json
snow --plugin examples/plugins/python/manifest.json
```

The checked-in examples use only Node/Python standard libraries. Their manifests
use runtime names plus a minimal POSIX `PATH` for readability; production
declarations should replace those values with explicit paths.

Stdout belongs exclusively to JSON-RPC frames. JavaScript plugins must log with
`console.error` or stderr; Python plugins must configure logging/printing for
stderr.

## Validate a runtime

```sh
snow plugin check manifest.json
snow plugin check manifest.json --json
snow plugin check manifest.json --timeout 20s --cwd /path/to/project
```

`plugin check` starts no provider or agent session. It:

- starts the configured process;
- validates the manifest ID/version/protocol and tool schemas;
- reports initialization time and effective cwd;
- lists effective plugin/tool capabilities, tools, risks, discovery modes, and
  subscribed events;
- includes informational negotiated limits;
- prints bounded diagnostics with best-effort redaction of common credential
  assignments/headers;
- verifies graceful shutdown.

Redaction is defense in depth, not a secret-handling API. Plugins must never emit
credentials to stderr, protocol logs, results, progress, or errors.

An explicit check runs even when the declaration has `enabled: false`; it does
not modify stored configuration.

## Protocol v2 summary

The wire transport is JSON-RPC 2.0 with one object per LF-terminated line.
Request IDs are strings and one host reader correlates concurrent responses.

Host requests:

- `initialize`: protocol/host version, cwd, session ID, capabilities, config;
- `tools/list`: authoritative tool descriptors;
- `tools/call`: original name, call ID, arguments, deadline, cancellation hint;
- `shutdown`: graceful close.

Plugin-to-host notifications:

- `notifications/progress`: bounded progress for a non-empty call ID;
- `notifications/log`: bounded diagnostics.

Host-to-plugin notifications:

- `notifications/event`: sanitized events listed in `supported_events`;
- `notifications/cancelled`: best-effort cancellation by request/call ID.

Tool descriptors may declare `risk` and per-tool capabilities. Omitted risk
fails closed to `exec`; invalid risks reject initialization. Result `details`
are preserved as private host metadata and are not sent to the model.

`supported_events` is an explicit subscription. Empty or omitted lists receive
no events. Delivery uses a bounded queue and is best effort, so plugins must not
treat events as a reliable transaction log.

Frames, inputs, results, progress, stderr, logs, event queues, deadlines, and
concurrent calls are bounded. EOF, crashes, malformed frames, JSON-RPC errors,
timeouts, and cancellation become isolated errors instead of panics. Processes
close in reverse load order.

See [External plugin protocol v2](plugin-protocol.md) for every field and frame.

## Security

Plugins execute with the user's OS privileges. Process separation provides a
crash/termination boundary, not a sandbox. Trust controls whether project input
is loaded; it does not confine an already loaded plugin.

Tool risk is plugin-declared metadata used by the central permission service;
capabilities are retained descriptor/discovery metadata and do not independently
authorize a call. Neither field stops the process from accessing files, network,
or subprocesses directly. Only trusted plugins should receive `read`, `write`,
or `network` classifications instead of the safe `exec` default.

Use containers, a VM, or an OS sandbox for untrusted code. Snow avoids implicit
environment inheritance, does not intentionally log credentials, applies
best-effort common-credential redaction to diagnostics, and bounds plugin I/O.

Snow's lifecycle draws inspiration from Pi's extension/progressive-disclosure
model and OpenCode's explicit permission-aware plugins, but does not promise API
compatibility with either.
