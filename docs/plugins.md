# Plugins

Snow's extensibility core has two adapters that share one capability registry:

- statically linked Go plugins implement `pkg/plugin.Plugin`;
- external runtimes implement JSON-RPC 2.0 over stdin/stdout (protocol v2).

MCP and skills are intentionally not plugin transports. MCP now adapts the
official Go SDK into the same registry (see [mcp.md](mcp.md)); skills are an
instructional resource layer with dedicated activation tools (see
[skills.md](skills.md)). JavaScript and Python may still use the external
JSON-RPC plugin protocol.

## Go API

A plugin has a stable manifest, a registration phase, and an idempotent close:

```go
p := myPlugin{}
s, err := snowsdk.Open(ctx, snowsdk.Options{
    NoSession: true,
    PermissionMode: "allow",
    GoPlugins: []plugin.Plugin{p},
})
```

`Plugin.Register` receives a scoped `plugin.Registrar`. Tool names are local to
the plugin and are exposed to the model as `plugin_<plugin-id>_<tool-name>`.
Duplicate IDs, invalid identifiers, malformed schemas, and duplicate names are
rejected before the agent starts. Tools declare a risk (`read`, `write`, `exec`,
or `network`) and all calls still pass through Snow's central permission
service. Tool contexts carry cancellation, session/cwd/call identity, and a
bounded progress callback.

Tools may also set optional `protocol.ToolDiscovery` metadata. The default is
direct exposure; `mode: deferred` keeps the full schema in the registry and
uses the local Bleve router to select it per prompt. This is independent of the
plugin transport and never bypasses the central execution permission gate. See
[tool-routing.md](tool-routing.md) for Go and JSON examples.

Plugins may subscribe to versioned agent events. Protocol v2 is observation-only:
event handlers cannot mutate, veto, or reorder the agent loop. Handler panics
are isolated from the host.

## External configuration

Global configuration and SDK/CLI options use the same explicit `PluginSpec`:

```json
{
  "plugins": [
    {
      "id": "my-tools",
      "command": ["/absolute/path/to/plugin", "serve"],
      "enabled": true,
      "timeout_ms": 120000,
      "max_output_bytes": 262144,
      "env": ["PATH=/usr/bin"]
    }
  ]
}
```

`command` is an argv array. Snow never invokes `sh -c` or parses a shell
command string. `snow --plugin /absolute/path/to/plugin` is shorthand for an
enabled spec; `--plugin manifest.json` loads a JSON spec. `--no-plugins`
disables config and statically supplied plugins.

Project `.snow/config.json` plugin entries are read only after the existing
trust store has an explicit `allow` decision (or the configured always-trust
policy). Denied, unresolved, or disabled entries are skipped with diagnostics.
There is no executable directory scan or marketplace.

## Protocol v2

The wire format is JSON-RPC 2.0 JSONL. stdout is reserved for frames; stderr is
captured as bounded diagnostics. Request IDs are strings and one reader
multiplexer correlates concurrent calls.

Host requests:

- `initialize`: protocol version, host version, cwd, session ID, capabilities,
  and plugin config;
- `tools/list`: refresh descriptors;
- `tools/call`: original tool name, call ID, JSON arguments, timeout and
  cancellation metadata;
- `shutdown`: graceful close.

Plugin responses/notifications:

- initialize returns a manifest, capabilities, tools, supported events, and
  limits;
- `notifications/progress` reports bounded call progress;
- `notifications/event` carries sanitized host observations;
- `notifications/log` is bounded diagnostics.

Frames, arguments, results, progress, stderr, and concurrent calls are bounded
by the spec. EOF, crashes, malformed frames, JSON-RPC errors, timeout, and
cancellation become isolated tool/startup errors rather than panics. External
processes are closed in reverse load order.

## Security

Plugins execute with the user's OS privileges. Trust is an input-loading gate,
not a sandbox: an already loaded plugin is not restricted by the trust store.
Use containers, a VM, or an OS sandbox for untrusted code. Snow does not inherit
the host environment by default, does not log credentials, enforces permissions
for plugin tools, and keeps plugin diagnostics bounded.

The registration/lifecycle separation is inspired by Pi's extension and
progressive-disclosure model and OpenCode's explicit plugin lifecycle and
permission-aware tools. Snow does not promise compatibility with either API.
