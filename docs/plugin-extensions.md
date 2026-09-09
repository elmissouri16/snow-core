# JavaScript extensions

JavaScript API 2 adds commands, TUI contributions, agent workflows, hooks, and
persistent state. Existing API 1 packages keep their synchronous behavior.
Both run trusted local code with the user's privileges. Installation and trust
rules are covered in [Plugins](plugins.md).

## On this page

- [Try the examples](#try-the-examples)
- [Create a plugin](#create-a-plugin)
- [Complete authoring examples](#complete-authoring-examples)
- [Host context](#host-context)
- [UI contributions](#ui-contributions)
- [Hooks](#hooks)
- [Storage, settings, and lifecycle](#storage-settings-and-lifecycle)
- [Selected child tools](#selected-child-tools)
- [SDK and RPC](#sdk-and-rpc)
- [Troubleshooting](#troubleshooting)
- [Local performance measurements](#local-performance-measurements)
- [Related documents](#related-documents)

## Try the examples

The [extension test pack](https://github.com/elmissouri16/snow-core/blob/main/examples/plugins/TRY.md)
walks through every category with ready-to-run packages:

| Package | Entry point | Exercises |
|---|---|---|
| `workspace-dashboard` | `/dashboard` | Sidebar, footer, lifecycle events |
| `ui-studio` | `/ui-studio`, `/plugin-theme ocean` | Components, forms, themes, header/composer regions, shortcuts |
| `workspace-notes` | `/note text`, `/notes` | Persistent state, dialogs, composer insertion |
| `prompt-recipes` | `/recipe review task`, `/recipe-mode concise` | Prompt, request, before-tool, and after-tool hooks |
| `session-pilot` | `/pilot` | Models, sessions/branches, agent control, goal control |
| `project-helper-v2` | `/source-scout question` | Custom tool cards and isolated selected child tools |
| `review-team` | `/review-team focus` | Three reviewers, progress, cleanup, root synthesis |

`source-scout` requires a configured `plugin_scout` role whose `tools` include
`read`, `grep`, `glob`, and `plugin_project-helper-v2_files`. The ordinary explorer
role does not grant plugin tools. See the test pack for the exact configuration.

From a checkout:

```sh
snow --js-plugin ./examples/plugins/workspace-dashboard
snow --subagents --js-plugin ./examples/plugins/review-team
snow --js-plugin ./examples/plugins/project-helper-v2
```

- `/dashboard` opens the workspace dashboard. On wide terminals it also appears
  beside the transcript; narrow terminals retain access through `/plugins`.
- `/review-team Review the changes in internal/agent` starts correctness,
  architecture, and testing reviewers with the `explorer` role, displays progress,
  closes the completed children, and asks the root agent to synthesize findings.
  Reviewers incur ordinary provider usage. Ctrl+C cancels the command and its own
  children; unrelated children remain active.
- `/project-helper-v2:draft` asks for a focus and fills the composer. Its `files`
  tool searches source files, provides a custom result card, and opts into child
  selection. A pure request hook contributes the configured source pattern.

To load a plugin on normal launches, register its directory:

```sh
snow plugin add ./examples/plugins/workspace-dashboard
snow plugin check workspace-dashboard
snow
```

`--js-plugin` applies to one launch. `snow plugin add` persists a path reference.
`--no-plugins` disables both JavaScript and Go plugins. Restart Snow after changing
code, settings, or registrations. There is no hot reload or remote marketplace.

## Create a plugin

```sh
snow plugin init my-extension
snow plugin add ./my-extension
snow plugin check my-extension
snow plugin run my-extension:hello -- world
```

Use `snow plugin init my-extension --typescript` for TypeScript source plus a
single-bundle build configuration. Install the development dependencies listed
in the generated README, then build `main.js`. Runtime loading never installs
npm packages. The generated `snow.d.ts` describes the complete API 2 surface.

A manifest selects the API and host capabilities:

```json
{
  "id": "my-extension",
  "name": "My extension",
  "version": "0.1.0",
  "api_version": 2,
  "entry": "main.js",
  "capabilities": ["commands", "ui", "storage"],
  "host_tools": ["glob"],
  "settings": [
    {"name": "label", "title": "Panel label", "type": "string", "default": "Workspace"}
  ]
}
```

Capabilities describe available host operations. Each command or tool narrows
its authority with `uses`; host tool names must appear in both `host_tools` and
that handler's `uses`. Read-only agent/model/session snapshots are available
without a control capability. Declarations never grant permission to execute a
Snow tool: its final arguments still pass schema, mode, preflight, invocation
policy, and the normal permission service.

```js
snow.registerView({name: "status", title: "Status", placement: "footer"});
snow.registerCommand({
  name: "hello", description: "Show a greeting", uses: ["ui", "storage"],
  async run(input, ctx) {
    await ctx.storage.set({key: "last-greeting", value: input});
    await ctx.ui.update({
      name: "status", content: {type: "text", text: input, tone: "accent"}
    });
    return "Greeting updated";
  }
});
```

Commands are invoked as `/my-extension:hello hello`. An optional `alias` supplies
an additional slash command; collisions with built-ins or another plugin fail
validation. `argumentHint` appears in help and completion. Optional `shortcut`
uses `alt+letter`; `/keybindings` exposes namespaced `plugin:id:command` actions
for overrides. Core keys and modal interaction retain precedence. Conflicting
default plugin shortcuts are not installed.

## Complete authoring examples

Each example is an independent API 2 package. Create the named directory and
save its two code blocks as `snow-plugin.json` and `main.js`. Run the shell
commands from the directory containing that package. Package paths select the
code to load; host file searches and project storage use Snow's working
directory, so launch Snow from the project you want to inspect.

### Expose a file search as a command and a tool

This package makes the same bounded host search available to you through
`/doc-files:find` and to the model as `plugin_doc-files_find`. Commands are
explicit actions; tools are selected by the model. Neither declaration grants
permission to bypass the underlying `glob` tool's checks.

Save as `doc-files/snow-plugin.json`:

```json
{
  "id": "doc-files",
  "name": "Documentation file search",
  "version": "0.1.0",
  "api_version": 2,
  "entry": "main.js",
  "capabilities": ["commands"],
  "host_tools": ["glob"]
}
```

Save as `doc-files/main.js`:

```javascript
async function findFiles(pattern, ctx) {
  if (typeof pattern !== "string" || !pattern.trim()) {
    throw new Error("Supply a non-empty glob pattern, such as **/*.md.");
  }
  const result = await ctx.tools.call("glob", {pattern: pattern.trim()});
  // Preserve host errors; an empty search and a denied search are different.
  return {content: result.content, isError: result.isError};
}

snow.registerCommand({
  name: "find",
  description: "Find project files by glob pattern",
  argumentHint: "[pattern]",
  uses: ["glob"],
  async run(input, ctx) {
    return await findFiles(input.trim() || "**/*.md", ctx);
  }
});

snow.registerTool({
  name: "find",
  description: "Find project files by glob pattern",
  parameters: {
    type: "object",
    properties: {pattern: {type: "string"}},
    required: ["pattern"],
    additionalProperties: false
  },
  uses: ["glob"],
  async execute(args, ctx) {
    ctx.progress("Finding matching files");
    return await findFiles(args.pattern, ctx);
  }
});
```

Register, validate, and invoke the command:

```sh
snow plugin add ./doc-files
snow plugin check doc-files
snow plugin run doc-files:find -- '**/*.md'
snow plugin run doc-files:find --json -- '**/*.go'
```

The first run returns the host search's text output for the current directory.
Quote patterns so your shell does not expand them before Snow receives them.
The JSON CLI envelope uses `is_error`; JavaScript results use `isError`.
A command reporting `isError: true` makes the CLI exit unsuccessfully, including
with `--json`. Host calls may also reject their promise; an uncaught rejection
fails the invocation. Catch an error only when you can return a useful,
accurate failure or recover from it.

For several host calls, await each call before starting the next. Do not use
`Promise.all` to launch multiple `ctx.tools.call` operations from one handler.
Keep the `ctx` local to that invocation. See
[Project Helper v2](https://github.com/elmissouri16/snow-core/tree/main/examples/plugins/project-helper-v2)
for a tool that adds `details` and a custom result renderer.

### Save project state and display it in a screen

This package saves one review focus per project and displays it in a screen.
It also works from the CLI: supply the focus as command input and receive text
without requiring a dialog. The stored value survives a normal Snow restart.

Save as `doc-focus/snow-plugin.json`:

```json
{
  "id": "doc-focus",
  "name": "Project review focus",
  "version": "0.1.0",
  "api_version": 2,
  "entry": "main.js",
  "capabilities": ["commands", "ui", "storage"]
}
```

Save as `doc-focus/main.js`:

```javascript
const location = {scope: "project", key: "review-focus"};
snow.registerView({
  name: "focus", title: "Review focus", placement: "screen"
});

async function showFocus(ctx) {
  const saved = await ctx.storage.get(location);
  const text = typeof saved === "string" ? saved : "No review focus saved.";
  await ctx.ui.update({name: "focus", content: {
    type: "column",
    children: [
      {type: "text", text, tone: "accent"},
      {type: "button", text: "Change focus", action: "set"}
    ]
  }});
  if (ctx.ui.available) await ctx.ui.open({name: "focus"});
  return text;
}

snow.registerCommand({
  name: "set", description: "Save a project review focus",
  argumentHint: "[focus]", uses: ["ui", "storage"],
  async run(input, ctx) {
    let text = input.trim();
    if (!text) {
      if (!ctx.ui.available) {
        throw new Error("Supply the focus as command input in headless mode.");
      }
      text = (await ctx.ui.input({title: "What should the review focus on?"}))
        .trim();
    }
    if (!text || text.length > 800) {
      throw new Error("Use a focus of 1–800 characters.");
    }
    await ctx.storage.set({...location, value: text});
    return await showFocus(ctx);
  }
});

snow.registerCommand({
  name: "show", description: "Show the saved review focus",
  uses: ["ui", "storage"],
  async run(_, ctx) { return await showFocus(ctx); }
});
```

Run these from the same project directory:

```sh
snow plugin add ./doc-focus
snow plugin check doc-focus
snow plugin run doc-focus:set -- cancellation and cleanup
snow plugin run doc-focus:show
```

Both commands return `cancellation and cleanup`; the second process reads it
from storage. In a new interactive Snow session, `/doc-focus:show` opens the
screen. Select **Change focus** with Tab and press Enter to open the input
dialog. The button's `action: "set"` resolves to this plugin's own command.
Cancelling the input rejects the pending call before the storage write.

`ui.available` detects a presentation surface. This example deliberately asks
for explicit input when headless; SDK/RPC hosts can separately attach a trusted
input broker. `ui.update` can maintain snapshots headlessly, but opening a
screen requires a UI. If a stored value seems missing, check the working
directory, plugin ID, scope, and `SNOW_HOME` before changing the code.

### Add request context with a pure hook

A hook can use configuration and local cached values. It cannot read storage,
call tools, or show dialogs. This package enables a request-only instruction
through an explicit command, making it easy to test and turn off.

Save as `doc-hooks/snow-plugin.json`:

```json
{
  "id": "doc-hooks",
  "name": "Optional evidence reminder",
  "version": "0.1.0",
  "api_version": 2,
  "entry": "main.js",
  "capabilities": ["commands", "hooks"]
}
```

Save as `doc-hooks/main.js`:

```javascript
let enabled = false;
snow.registerCommand({
  name: "mode", description: "Toggle the evidence reminder",
  argumentHint: "<on|off|status>", uses: [],
  run(input) {
    const value = input.trim() || "status";
    if (value === "on") enabled = true;
    else if (value === "off") enabled = false;
    else if (value !== "status") throw new Error("Use on, off, or status.");
    return enabled ? "Evidence reminder on." : "Evidence reminder off.";
  }
});
snow.registerHook("before_request", () => enabled ? {
  context: [{text: "Support code findings with file references and evidence."}]
} : {});
```

Register the package and start an interactive session:

```sh
snow plugin add ./doc-hooks
snow plugin check doc-hooks
snow
```

Run `/doc-hooks:mode on`, then send a normal prompt such as `Explain the main
entry point`. The hook adds its instruction to subsequent provider requests;
it does not rewrite your saved prompt or guarantee model compliance. Run
`/doc-hooks:mode off` to stop adding it. Restarting Snow resets this local flag.
Separate `snow plugin run` processes do not share JavaScript variables.

For persistent preferences, write storage in a command and initialize a local
cache with `onReady`. Keep host I/O out of the hook itself. For examples of all
four hook phases, inspect
[Prompt Recipes](https://github.com/elmissouri16/snow-core/tree/main/examples/plugins/prompt-recipes).

## Host context

Commands receive `(input, ctx)`, tools receive `(args, ctx)`, and readiness and
observers receive `(event, ctx)`. Host methods return promises. Always await tool
calls in sequence. Retaining a context does not extend its lifetime or transfer
its authority to another callback.

| Context | Operations | Control capability |
|---|---|---|
| `ctx.agent` | `state`, `pending`, `prompt`, `steer`, `followUp`, `abort` | `agent` for mutations |
| `ctx.models` | `list`, `set` | `agent` for changes |
| `ctx.session` | `messages`, `branches`, `rename`, `fork`, `selectBranch`, `renameBranch`, `deleteBranch`, `compact` | `session` for mutations |
| `ctx.goals` | `get`, `create`, `edit`, `pause`, `resume`, `clear` | `goals` for mutations |
| `ctx.subagents` | `models`, `spawn`, `list`, `get`, `messages`, `message`, `followUp`, `wait`, `interrupt`, `close`, `resume` | `subagents` for controls |
| `ctx.tools` | `call(name, arguments)` | declared tool in `host_tools` and `uses` |
| `ctx.storage` | `get`, `set`, `delete` | `storage` |
| `ctx.ui` | panels, screens, dialogs, editor, theme | `ui` |
| `ctx.sleep` | cancellable milliseconds, at most 60,000 per call | none |

For example, `await ctx.agent.prompt({text: "Review these findings"})` uses the
same agent loop as ordinary input. It rejects incompatible busy state. Use
`steer({text})` or `followUp({text})` for a running turn. A model-invoked plugin tool
cannot start root prompts or mutate sessions/goals; those controls belong in
explicit commands. Automatic goal turns cannot request interactive answers.

`models.set({provider, model, thinking})` selects an existing configured model;
plugins do not implement provider protocols. Subagent spawn arguments use the
normal snake_case protocol fields, including `fork_turns`, `reasoning_effort`,
and `plugin_tools`. Child controls require enabled subagents. `subagents.list`
returns the normal `{agents, running, queued, ...}` snapshot, including the root.

`session.messages({offset, limit})` returns newest-window messages in chronological
order, with at most 100 entries and bounded serialized output. Provider-private
continuity blocks are removed. Forking preserves the session tree. Branch creation and
selection are scheduled until its command succeeds; errors and `isError: true`
discard the scheduled transition and close children owned by that command.
Failure to apply a transition also closes those children. In-flight host work or active
children can prevent a transition. Replacing a session invalidates old contexts,
cancels commands, and clears contributed views.

## UI contributions

Register views with one of `header`, `footer`, `above_input`, `sidebar`, or
`screen`. Update a plugin-owned view with `ctx.ui.update({name, content})` and open
it with `ctx.ui.open({name})`. `ctx.ui.close()` closes that plugin's screen.
`/plugins` lists loaded extensions, capabilities, commands, diagnostics, views,
themes, and editable settings.

Component trees contain `text`, `markdown`, `row`, `column`, `list`, `table`,
`progress`, `button`, `input`, `select`, and `checkbox` nodes. Text is sanitized;
raw terminal escapes cannot control the renderer. Input/select/checkbox nodes
present values; use the dialog APIs to collect and validate interactive values.
Buttons dispatch only commands from their owning plugin, with optional string
`input`. Screens open in the same centered, bordered card as Snow's model picker,
with scrollable content and a separate action section. Buttons appear there in
tree order; the highlighted action stays visible while content scrolls.
Screens use Tab/Shift+Tab to select actions, Enter to activate, arrow or
page keys to scroll, Home/End to jump, and Escape to close. Permission and user-input dialogs take
precedence over plugin screens and shortcuts.

Headers and footers get at most two lines each, and above-input content gets six.
These contributions shrink or hide when the terminal has insufficient rows;
the composer, core status footer, and open dialogs take priority. Header and
footer content is inset from the terminal edge. Changes to view geometry take
effect before the next frame, including when an existing view grows or shrinks.
Sidebars appear at widths of 100 columns or more. Views remain accessible from
`/plugins` when hidden by a narrow or short terminal. Refreshes are coalesced and rendered
content is cached until its data, width, or theme changes.

Dialogs use Snow's existing user-input broker and centered question cards.
Text inputs, selections, confirmations, and forms share the native panel style;
the focused field or choice and its controls stay visible on small terminals.
Drafts survive resizing and moving between form questions.
Selections, confirmations, and enum/boolean fields offer only their listed
choices. Confirmations initially select **No**. Selects require 1–16 unique,
non-empty labels (at most 512 bytes each, without surrounding whitespace);
enum fields require 1–64 choices with the same label rules.
Cancelling a command dismisses its pending dialog. Unrelated agent turns do
not dismiss it, and old command completions cannot restore a previous branch's UI.

```js
const focus = await ctx.ui.input({title: "Review focus"});
const scope = await ctx.ui.select({title: "Scope", options: ["Current changes", "Whole project"]});
const confirmed = await ctx.ui.confirm({title: "Start review?"});
const values = await ctx.ui.form({title: "Preferences", fields: [
  {name: "compact", title: "Compact display", type: "boolean"}
]});
```

Forms support 1–3 fields with string, number, boolean, or enum values. In headless
mode, dialogs require a trusted SDK/RPC input broker and otherwise fail promptly.
`ui.available` reports an attached presentation surface. Headless `update` still
updates view snapshots; `notify` is harmless without a UI; screen/editor/theme
operations return an unavailable error. Editor methods are `editorGet()`,
`editorSet({text})`, and `editorInsert({text})`.

`registerTheme({name, colors})` requires `light` and `dark` hex colors for accent,
muted, foreground, warning, error, success, and separator. Apply it with
`ctx.ui.theme({name})` or select it in `/plugins`; the selection lasts for that run.
`registerToolRenderer(toolName, handler)` returns a bounded node tree from the
result's `content`, `isError`, and optional JSON `details`. Cards with details are
rendered once at completion and persisted for resume; a failed renderer falls
back to ordinary tool output. Renderers cannot call host APIs.

## Hooks

Register hooks during startup with `snow.registerHook(phase, handler, options)`.
Plugins run in ID order, with registration order inside each plugin. Hooks apply
to root work by default; `{includeSubagents: true}` opts into child work.

| Phase | Allowed result |
|---|---|
| `before_prompt` | `{text}` replacement or `{block}` reason; includes delivered queued inputs |
| `before_request` | `{context: [{text}]}` adds attributed request-only context |
| `before_tool` | `{arguments}` replacement object or `{block}` reason |
| `after_tool` | `{content}` replacement text blocks |

Request-hook fragments use a protocol-valid `plugin-<id>` source; the audit
record retains the original plugin ID. Empty context text is rejected.

Hooks and renderers have a short execution budget and cannot perform host I/O.
Use readiness/observer callbacks to maintain cached information. A failed hook
remains a gate rather than silently disappearing. A post-tool failure stops
continuation after preserving the actual tool outcome, so completed effects are
not represented as unexecuted. Hooks cannot rewrite tool names, alter error
status, edit historical messages, or inspect provider-private continuity data.
Changes are attributed in message audit fields or session metadata; audit and
presentation metadata are excluded from provider messages.

## Storage, settings, and lifecycle

`storage.get/set/delete` take `{key, scope}`; `set` also takes `value`. Scopes are
`global`, `project` (default, canonical working directory), or `session`. Keys are
isolated by plugin ID. SQLite state lives in `SNOW_HOME/plugin-state.db`, separate
from conversation trees. Ephemeral sessions use in-memory state. Values are
bounded to 64 KiB, with 1 MiB and 1,024 keys per plugin/scope. Missing keys return
null. A rejected quota write rolls back.

Typed manifest settings are exposed as `snow.config`. `/plugins` edits an
existing registration's settings; restart to apply. Plugins loaded solely with a
path flag must be registered before settings can be saved.

API 2 uses one VM owner with promise continuation jobs. Awaiting a host operation
releases the VM to service other callbacks. CPU slices are bounded to one second;
runaway CPU or stack overflow disables the runtime. Commands default to ten
minutes and can declare `timeoutMS` up to thirty minutes. Tools retain a two-minute
budget. At most 32 callbacks may be pending per runtime; commands permit 4,096
host calls, other callbacks 64. Cancellation stops accepted host work before the
invocation returns. These limits do not provide a Goja heap quota or an OS sandbox.

`onReady` runs once after surface setup, never during `plugin check`. API 2
observers receive the normalized event catalog and run in accepted order with
bounded queues. Subscription errors disable that observer, while hook errors
retain their gate. `onClose` remains synchronous. No Node globals, runtime module
loading, browser APIs, timers, or implicit network/filesystem access are supplied.

## Selected child tools

Children inherit no JavaScript tools by default. A tool must declare `child:true`,
then its full `plugin_id_tool` name must be explicitly selected in
`plugin_tools` and permitted by the role's `tools` allowlist. Every host tool it
uses must also be available to that role. Mutating built-ins require both existing
mutation switches. This implementation conservatively rejects explicit plugin
selection in Plan Mode. Recursive child-to-child plugin selection is unavailable.

Role configuration accepts exact canonical plugin tool names; wildcard entries
remain invalid.

Each child gets an independent runtime containing only selected tools, with no
commands, observers, UI, hooks, or root control APIs. Storage and declared host
tools remain available. Durable records persist selections and package/config
fingerprints; changed or missing packages fail restoration instead of silently
changing a child's authority.

## SDK and RPC

Go SDK methods are `Plugins`, `PluginCommands`, `PluginViews`,
`RunPluginCommand(ctx, id, input)`, `CancelPluginCommand(id)`, and `AttachPluginUI`.
See the generated typings and [SDK reference](https://github.com/elmissouri16/snow-core/blob/main/docs/sdk-reference.md) for host boundaries.
RPC exposes `plugins_list`, `plugin_commands`, `plugin_views`,
`plugin_command_run`, and `plugin_command_cancel`:

```json
{"id":"review","type":"plugin_command_run","params":{"command":"review-team:run","input":"Review current changes"}}
{"id":"cancel","type":"plugin_command_cancel","params":{"command":"review-team:run"}}
```

The command reader remains active while commands wait. Ordinary agent and child
events continue on the shared stream. `snow plugin run id:command -- input`
provides the same runner to the CLI; `--json` returns its content and error flag.

## Troubleshooting

### Find the failing stage

Start with the registration and package, then test the specific handler:

```sh
snow plugin list
snow plugin get doc-files --json
snow plugin check doc-files
snow plugin run doc-files:find --json -- '**/*.md'
```

Replace `doc-files` with your manifest ID and `find` with your command name.
`get` shows the registered path and manifest without executing the script.
`check` validates startup and registration with host I/O disabled; it does not
run commands, tools, `onReady`, or interactive views. A successful check cannot
prove those callbacks work. In the TUI, inspect `/plugins` for loaded commands,
capabilities, diagnostics, and views. Use `snow.log("info", "message")` for
bounded plugin diagnostics; `console.log` is not available.

After editing a package, restart Snow. Registration stores a path reference;
it does not copy the package or reload a running VM. For TypeScript, rebuild the
entry JavaScript before restarting. If the whole session fails to initialize,
`snow --no-plugins` starts without loading either JavaScript or Go plugins.
Disable the faulty registration with `snow plugin disable ID` before starting
normally again. Use `--project` when changing a project registration.

### Loading and registration

| Symptom or error | What to check or change |
|---|---|
| `unknown command "plugin"` or missing `init`/`run` | Check `command -v snow`, `snow --version`, and `snow plugin --help`. Use a build containing the extension API; an older binary may be first on `PATH`. |
| Plugin not found in allowed configuration scopes | Use the manifest ID, inspect `snow plugin list`, and register the intended directory. Check project trust and configuration scope. |
| Plugin loads with `--js-plugin` but disappears next launch | Register the directory with `snow plugin add`; a launch flag is temporary. |
| Registration exists but commands are missing | Check disabled state, `--no-plugins`, startup diagnostics, and whether you restarted after registration. |
| Entry must be a relative `.js` file | Point `entry` at a built file inside the package. Do not use an absolute path, `.ts` entry, or symlink. |
| `require`, `process`, `fetch`, or `setTimeout` is undefined | Remove Node/browser dependencies from runtime code. Bundle compatible code before loading; use declared host tools and `ctx.sleep` for supported operations. |
| Unsupported `api_version` or invalid manifest field | Match the installed API version and use the manifest fields shown in this guide. API 1 cannot use API 2 registrations. |
| `extensions must be registered during startup` | Move registrations to top-level code, outside commands, readiness callbacks, and observers. |
| Alias or shortcut does not activate | Try `/plugin-id:command` directly. Check collisions and `/keybindings`; shortcuts must use `alt+` and one lowercase letter. Core and modal keys take precedence. |

### Capabilities, host tools, and result handling

| Symptom or error | What to check or change |
|---|---|
| `manifest must declare ... capability` | Add the capability required by the registration, such as `commands`, `ui`, or `hooks`. |
| `undeclared command capability ...` | A command's `uses` must be backed by manifest capabilities or `host_tools`. |
| `undeclared capability for ...` | Add the operation's capability to that handler's `uses` and the manifest. See the host-context table above. |
| `undeclared host tool "glob"` | Declare `glob` in both manifest `host_tools` and the calling handler's `uses`. |
| Tool call denied despite declarations | Inspect the underlying tool's error, mode, allowed roots, schema, and permissions. Declarations do not grant execution approval. |
| `await the previous tool call before starting another` | Await each `ctx.tools.call` sequentially within a handler. |
| `hooks and renderers cannot perform host operations` | Move I/O to a command, tool, readiness callback, or observer; let the hook/renderer read cached plain data. |
| `plugin invocation is no longer active` | Stop retaining or sharing `ctx` across callbacks. Await host work before returning and stop using a cancelled context. |
| `result requires content array` or text-only result error | Return a string or `{content: [{type: "text", text: "..."}]}`. Keep structured JSON in optional `details`. |
| Command reports success after a failed host tool | Preserve the host result's `isError`; do not replace it with an unconditional success message. |

### UI, state, and workflow controls

| Symptom or error | What to check or change |
|---|---|
| View is not registered by this plugin | Register the exact view `name` during startup before updating it. |
| Sidebar or footer is missing | Open `/plugins`; narrow/short terminals hide contributions. Sidebars require at least 100 columns. |
| Headless dialog is unavailable | Supply command input or attach a trusted SDK/RPC input broker. A headless CLI has no interactive dialog surface. |
| Selection/form rejected | Selects allow 1–16 choices; forms allow 1–3 fields. Choice labels must be unique, non-empty, and trimmed. |
| Saved value is `null` after restart | Check scope, canonical project directory, plugin ID, `SNOW_HOME`, and whether the previous session was ephemeral. |
| Storage write rejected | Keep values below 64 KiB and the plugin/scope below 1 MiB and 1,024 keys. Handle a rejected write; it does not partially save the new value. |
| `agent busy` | Run a new prompt or branch transition when idle; use `steer` or `followUp` for input during a running turn. |
| `model is not in the configured catalog` | Select a provider/model returned by `ctx.models.list`; plugins cannot create provider implementations. |
| Root session/goal control fails from a tool | Put mutations in an explicit command. Model-invoked tools and selected child tools have narrower authority. |
| Command times out or runtime is disabled | Bound loops and output, await cancellable host work, and inspect diagnostics. Raising `timeoutMS` does not extend the one-second CPU slice. |
| Hook failure keeps blocking work | Fix or disable the plugin and restart. Failed hooks retain their gate; they are not silently removed. |

### Selected child tools

Check all four declarations together: the tool's `child: true`, the spawn's
`plugin_tools`, the role's exact plugin tool name in `tools`, and the host tools
allowed for that role. The exact name for the first example would be
`plugin_doc-files_find`, but that example does not opt into child use.

| Error | Resolution |
|---|---|
| `role does not allow ...` | Add the exact canonical plugin tool name to the intended role's allowlist; wildcards are invalid. |
| `child cannot use ... required by ...` | Grant the role the specific host tools the selected plugin tool needs. |
| `child plugin tool unavailable` | Confirm the package is loaded and the tool declares `child: true`; check spelling. |
| `host operation unavailable in child tool profile` | Keep child code limited to selected tools, storage, and supported sleep. Root controls and UI belong in a root command. |
| Child plugin changed or is no longer available | Restore the recorded package/configuration or create a new child with the current package; resume checks fingerprints. |

Enable subagents before invoking child controls. Explicit plugin selection is
rejected in Plan Mode, and children cannot recursively select plugin tools for
another child. Follow the complete role configuration in the
[extension test pack](https://github.com/elmissouri16/snow-core/blob/main/examples/plugins/TRY.md)
when trying `/source-scout`.

### Verify a fix

1. Rebuild bundled JavaScript if needed, then run `snow plugin check ID`.
2. Restart Snow and invoke the failing command with the same input and working
   directory. Check the result and diagnostics, including failures.
3. For state, read the value from a second process in the same project. For UI,
   also try a narrow terminal and cancellation. For hooks, send a real prompt;
   `plugin check` alone does not exercise them.
4. For child workflows, verify the role allowlist and close children after the
   workflow completes. Confirm cancellation affects only the workflow's work.

From a source checkout, the existing example smoke checks use isolated
configuration and fake/local providers:

```sh
python3 examples/plugins/smoke.py
python3 examples/plugins/extensions_smoke.py
```

Both scripts use `snow` from `PATH` by default. Set `SNOW_BIN` to an absolute
binary path to test a specific build. These checks exercise the example pack;
they do not validate arbitrary plugin behavior or replace a visual check of
your own UI.

## Local performance measurements

On the development Apple M3 Pro, a no-op API 2 command took approximately 30 µs
and allocated 53 KiB in `BenchmarkAsyncCommand`; API 1 no-op tools took about
6 µs. These are adapter-only measurements, excluding provider calls, tools,
startup, and rendering. They are not an end-to-end latency guarantee. A cached view lookup measured about 15 ns with zero allocations. The UI
caches declarative views and never invokes JavaScript from its frame renderer.
Use `go test ./internal/plugin/javascript ./internal/tui -run '^$' -bench
'BenchmarkAsyncCommand|BenchmarkRuntimeTool|BenchmarkPluginViewCached' -benchmem`
to measure the checkout on another machine.

## Related documents

- [Plugins](plugins.md): installation, trust, API 1, and Go plugins.
- [Subagents](subagents.md): roles and child workflow controls.
- [Security model](security.md): permissions and runtime boundaries.
