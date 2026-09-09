# Plugins

Extend Snow with JavaScript tools, commands, TUI views, workflow controls, hooks,
and state, or supply compiled Go plugins from an embedding application. Both use
the shared tool registry, permissions, and agent event stream.

Start with [JavaScript extensions (API 2)](plugin-extensions.md) for UI and workflow
plugins, scaffolding, typings, and examples. The synchronous authoring reference
below documents API 1 compatibility.

## Install a JavaScript plugin

A local package contains `snow-plugin.json` and one bundled JavaScript entry
file. Register it by reference:

```sh
snow plugin add /absolute/path/to/my-plugin
snow plugin check my-plugin
snow plugin list
```

`add` enables the registration. Changes take effect on the next launch. `check`
initializes the script without provider startup and with host I/O disabled; it
does not execute registered tools. `list` and `get` never execute scripts.

```sh
snow plugin get my-plugin --json
snow plugin disable my-plugin
snow plugin enable my-plugin
snow plugin remove my-plugin
snow --js-plugin /absolute/path/to/my-plugin
```

Removal deletes only the registration. Use `--project` with add, enable,
disable, or remove to edit project configuration. This does not grant project
trust. Project packages must remain inside the canonical trusted project root.
Global/explicit packages may be elsewhere. Package paths and entry components
cannot be symlinks.

`--no-plugins` disables Go and JavaScript plugins before packages are read or
executed. There is no remote installer, automatic update, hot reload, or implicit
package discovery. The retired executable `plugins` config key remains ignored,
and the old `--plugin` flag remains unsupported.

## Write an API 1 JavaScript plugin

`snow-plugin.json`:

```json
{
  "id": "project-helper",
  "name": "Project Helper",
  "version": "0.1.0",
  "api_version": 1,
  "entry": "main.js",
  "host_tools": ["read"]
}
```

IDs use lowercase letters, digits, underscores, and hyphens, up to 64
characters. The entry must be a relative `.js` file inside the package. Unknown
manifest fields, unsupported API versions, duplicate host tools, and invalid
registrations fail startup. Enabled packages initialize before the agent starts;
startup failures roll back registrations and close acquired plugins.

`main.js`:

```js
snow.registerTool({
  name: "read_file",
  description: "Read a project file",
  parameters: {
    type: "object",
    properties: { path: { type: "string" } },
    required: ["path"]
  },
  uses: ["read"],
  execute(args, ctx) {
    if (typeof args.path !== "string") throw "path must be a string";
    ctx.progress("Reading project file");
    return ctx.callTool("read", { path: args.path });
  }
});
snow.on("turn_done", event => snow.log("info", "Turn completed"));
snow.onClose(() => {});
```

The tool becomes `plugin_project-helper_read_file`. Schemas must be JSON
objects; validate your arguments in the handler as shown. The available API is:

| API | Behavior |
|---|---|
| `snow.registerTool({name, description, parameters, uses, execute})` | Startup-only tool registration |
| `snow.on(eventType, handler)` | Startup-only observation subscription |
| `snow.onClose(handler)` | One synchronous shutdown callback |
| `snow.config` | A copied JSON configuration object |
| `snow.log(level, message)` | Bounded diagnostics; level is `info`, `warning`, or `error` |
| `ctx.sessionId`, `ctx.cwd`, `ctx.toolCallId` | Host-owned invocation metadata |
| `ctx.progress(message)` | Progress on the outer plugin tool call |
| `ctx.callTool(name, args)` | Synchronous permissioned built-in invocation |

Results are `{content: [{type: "text", text: "..."}], isError: false}`. Only text
blocks are supported; host-private result metadata is not exposed. JavaScript
exceptions fail the call. Cyclic, oversized, undefined, Promise, and thenable
results are rejected. Error messages are copied on the runtime worker so live
JavaScript exception objects do not escape into Snow.

Use bundled synchronous JavaScript. Compile TypeScript before distribution;
[example type declarations](https://github.com/elmissouri16/snow-core/blob/main/examples/plugins/snow.d.ts) provide editor
completion. There are no runtime imports, npm loading, Node/browser globals,
`fetch`, `process`, `console`, or timers. Source-map loading is disabled. See
[working examples](https://github.com/elmissouri16/snow-core/tree/main/examples/plugins).

The example collection includes Project Context (file/manifests overview),
TODO Radar (bounded marker searches), and Git Review (permissioned status and
diffs). Its `try.sh` launcher loads all three for one session. Project Context
and TODO Radar work in Plan mode; Git Review requires command permission.

## API 1 host operations and permissions

The supported built-ins are `read`, `write`, `edit`, `grep`, `glob`, `bash`, and
`webfetch`, using their existing Snow argument schemas. A tool's `uses` must be
a subset of manifest `host_tools`; omitted `uses` grants no host calls. The
built-in must also exist in the current session registry.

Snow derives plugin risk from `uses`: `bash` makes it `exec`, write/edit makes
it `write`, webfetch makes it `network`, and the remaining tools make it `read`.
Only the final category is available in Plan Mode. Authors cannot lower this
classification. Nested operations repeat the normal Plan, preflight, invocation
policy, and permission checks. An outer approval does not approve every inner
operation. Permission requests attribute the plugin, its tool, the parent call,
and the requested built-in. Remembered permissions are scoped to package bytes,
location, effective config, operation, and arguments.

Initialization, observers, and shutdown have no host I/O. Saved tool contexts
expire when their call returns. Plugins cannot invoke other plugins, MCP,
subagents, interactive questions, goals, or session controls through this API.
JavaScript plugins are not inherited by subagents.

File tools retain pinned-root and symlink protections. Webfetch retains public
HTTP(S) destination and redirect restrictions; it is not a general HTTP API
client. Bash retains shell preflight and protected-path policy, but approved
shell commands still run with the user's OS privileges.

## API 1 events, state, and limits

Each plugin has one Goja runtime and one worker per root session. Tools run
serially. Observers receive independent sanitized event copies through a bounded
queue, in accepted order; pending tool calls take priority. Observers cannot
mutate or veto events and may run after the corresponding agent operation.
Supported event names match the public plugin event constants, listed in the
[type declarations](https://github.com/elmissouri16/snow-core/blob/main/examples/plugins/snow.d.ts).

Observer exceptions disable that subscription. Queue overflow disables that
plugin's observers for the session without blocking Snow's event bus. Calls,
progress, and the final result retain the outer tool-call ID; nested operations
do not add synthetic provider-facing tool pairs.

JavaScript state is ephemeral. Reopening or resuming a session creates a fresh
runtime without replaying history. Shutdown stops admissions, cancels active
work, and drains observations within the shutdown budget. Interrupted runtimes
do not execute further callbacks.

| Resource | Limit |
|---|---:|
| Enabled JavaScript plugins | 32 per root session |
| Manifest / config | 64 KiB each per plugin |
| Entry script | 1 MiB |
| Registered tools / subscriptions | 64 / 128 per plugin |
| Observer queue | 256 events and 1 MiB per plugin |
| Initialization / shutdown | 1 second each |
| Observer callback | 100 ms |
| Tool execution, including nested approval waits | 120 seconds or the caller's earlier deadline |
| JavaScript call depth | 256 |
| Host calls per tool execution | 64 |
| Arguments / results / progress | Session tool-output limits |
| Logs | 256 entries per plugin, 2 KiB each |

Timeout, cancellation during execution, or stack overflow disables the runtime
for the session. Later calls return a disabled-plugin error. Ordinary tool
exceptions fail only their call. Built-ins retain tighter individual limits.

These deadlines are interruption requests, not hard process/resource limits:
Goja cannot interrupt native Go functions, including built-ins or compilation.
Host operations also receive contexts. Goja has no per-runtime heap quota, so
install only trusted plugins. A plugin can exhaust process memory. The exposed
API restricts host access but is not an OS sandbox.

Runtime failures and bounded logs appear in SDK `Diagnostics()`, RPC
`diagnostics`, and existing configuration diagnostic surfaces. Logging never
writes directly to JSON/RPC stdout. Logs can contain data deliberately supplied
by the plugin; do not log secrets.

## Configuration and SDK

The new `js_plugins` map works in global and trusted-project configuration:

```json
{
  "js_plugins": {
    "project-helper": {
      "path": "/absolute/path/to/project-helper",
      "disabled": false,
      "config": {}
    }
  }
}
```

The map key must match the manifest ID. Explicit SDK/CLI entries override
project entries, which override global entries by ID. Disabled entries also
shadow lower scopes. Global relative paths resolve from the configuration
directory; project paths resolve from the canonical project root; SDK paths
resolve from the session working directory.

In Go, supply `snowsdk.Options.JavaScriptPlugins`, a map from IDs to
`plugin.JavaScriptSpec{Path, Disabled, Config}`. Config is JSON object data.
JavaScript API version 1 is independent of the existing Go plugin protocol.
`NoPlugins` skips both plugin types. IDs cannot collide across Go and JS.

## Go plugins

Compiled embedding applications can still implement:

```go
type Plugin interface {
    Manifest() Manifest
    Register(context.Context, Registrar) error
    Close(context.Context) error
}
```

Pass implementations to `snowsdk.Options.GoPlugins`. The registrar supports
`RegisterTool` and `Subscribe`; tool names receive the same plugin namespace.
Go tool risk is `read`, `write`, `exec`, or `network`, defaulting to `exec`.
Go event handlers run inline and must return promptly. Output/progress are
bounded, failed registration is rolled back, and plugins close in reverse order.

Go plugins retain the embedding process's OS privileges; risk declarations do
not contain their code. There is no Go shared-object loader or external
executable-plugin transport. See [SDK setup](sdk.md) and [security](security.md).
