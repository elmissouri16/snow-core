---
name: snow-js-plugin
description: "Explicit-invocation-only Snow JavaScript plugin builder. Use ONLY when the user explicitly mentions the exact $snow-js-plugin token. Never activate merely because a request involves JavaScript, TypeScript, plugins, or Snow. Once invoked, build and verify a working plugin for the user's requested behavior using current supported APIs and bundled examples."
compatibility: "Snow with Goja API 2, workflow APIs, plugin test, and single-plugin reload. TypeScript builds need optional development tooling, not a Node runtime in Snow."
---

# Snow JavaScript plugin builder

## Activation and purpose

**Explicit mention only.** Do not infer activation from a relevant task. The
bundled skill requires the exact `$snow-js-plugin` user token; ordinary model
`activate_skill` calls cannot activate it. No frontmatter extension is needed.
Do not add unsupported frontmatter such as `disable-model-invocation`. Once
explicitly activated, continue for that plugin assignment and its follow-ups.
On explicit exit, deactivate this skill; do not carry it into unrelated work.
This activation policy is not a security boundary for plugin effects; ordinary
permissions and project trust remain authoritative.

Your job is to **implement the plugin the user needs**, not merely explain how
plugins work. Deliver a runnable package, tests, and usage instructions. Do not
change Snow core or invent APIs to make an unsupported design appear possible.
If the requested behavior is unsupported, explain the exact boundary and offer
a supported alternative before building something materially different.

All paths below are **skill-directory-relative**. Use `read_skill_resource`
for bundled resources. The `builtin:` directory is a virtual embedded location,
not an OS path: do not pass it to Bash, read, or copy commands. Read resources with
`read_skill_resource` and write the needed content into the new plugin package.
The bundle ships inside Snow and needs no source checkout, installation, or
network download. Read relevant resources progressively, not the entire bundle.

## Start here

1. Read `references/CAPABILITIES.md` to map the need to a supported extension.
2. Read `references/docs/plugin-extensions.md` and the applicable parts of
   `references/docs/plugins.md` (manifest, validation, trust, permissions).
3. Read `references/api/snow.d.ts` for **exact signatures and payloads**. Use API 2
   for new plugins; API 1 examples are legacy-only references.
4. Select the closest example from `references/EXAMPLES.md`; read its manifest
   and source before generating code. Combine only the pieces the task needs.
5. For state, restrictions, lifecycle, or reload, read
   `references/docs/plugin-workflows.md`. For tests, read
   `references/api/fixtures.md` and a bundled `tests/plugin.json`.

`references/SOURCES.json` records source paths and content hashes. These are a
snapshot of current Snow, not a claim that every released binary supports every
API. Check `snow version` and `snow plugin --help` when available. If the target
binary lacks an API or command, report that incompatibility; do not silently
upgrade the user's installation. When working in a newer Snow checkout, verify
against its canonical docs, declarations, source, and tests before relying on
this snapshot. Do not automatically download replacements.

## Build workflow

### 1. Translate the request into a small design

Identify the user action, model-facing tools (if any), required data, state
scope, effects, UI, and success condition. Prefer explicit commands for user
workflows and controls; tools for model-selected operations; pure hooks for
request transformations or lifecycle vetoes; observers for non-authoritative
notifications.

Use the project's existing conventions. If no location is given, propose or
use a new project-local `plugins/<plugin-id>/` directory. Never overwrite an
existing package without inspecting it. Ask only about material ambiguities:
e.g. global versus project installation, external services, destructive effects,
or a materially different supported alternative. Do not ask the user to choose
implementation details you can resolve from the docs.

### 2. Produce the package

For small plugins prefer dependency-free API 2 JavaScript. Use TypeScript when
requested or when it helps a substantial implementation. Supported scaffolding:

```sh
snow plugin init my-extension
# OR
snow plugin init my-extension --typescript
```

Run scaffolding only into a new destination. If Snow is unavailable, create the
package directly from the bundled examples and report validation as blocked.
For TypeScript, use a synchronous registration factory plus a bundled IIFE
entry; copy matching declarations. Inspect generated build scripts before
running them. Installing build dependencies requires the normal permissions;
never install packages or run arbitrary package lifecycle scripts implicitly.

Minimum deliverables:

- `snow-plugin.json`: stable ID, API version, entry, smallest capability set.
- `main.js`: runnable Goja bundle or dependency-free JavaScript.
- `tests/plugin.json`: representative actual-Goja fixtures.
- `README.md`: commands, behavior, configuration, permissions, state semantics,
  build/test steps, load/reload steps, limitations, and recovery.
- For TypeScript: source, declarations, build configuration, and generated JS.

No `node_modules`, secrets, generated caches, or real-user state in the package.
Do not implement the user-specific feature in Snow's Go core.

### 3. Enforce the contract while coding

- Declare manifest capabilities; narrow each command/tool with `uses`.
  Nested tool names belong in both `host_tools` and handler `uses`.
- Registration is synchronous. Await host calls serially. Do not retain callback
  contexts for later work or assume cancellation rolls back completed effects.
- Use supported host APIs, not `require`, `process`, `fetch`, DOM APIs, runtime
  imports, `setTimeout`, implicit filesystem/network access, or Node-dependent
  packages. Bundled pure JavaScript libraries are possible, subject to limits.
- Bound user input, tool arguments, output, loops, and UI trees. Never concatenate
  untrusted strings into shell commands. Do not bypass permission denials.
- Hooks are pure and bounded. Use `workflowKeys` for fresh owned state; do not
  perform host I/O or use observer-updated globals as authoritative gate state.
- Use `storage` for appropriately scoped KV data and `workflow` for branch-local
  state. Combine state and tool-policy updates in one `workflow.update` when
  they must agree. Writes are immediate side effects, not command transactions.
- Workflow/policy writes are idle root-command-only. Children/observers/hooks/
  tools cannot write them. Restrictions intersect, never override other policy.
- Model-invoked tools cannot start root prompts or mutate sessions/goals. Keep
  those controls in explicit user commands. Native Plan Mode remains separate.
- UI is optional: check availability and provide a useful headless result.
  Read UI dialog types: displayed input/select/checkbox nodes are not forms that
  magically collect data. Use the actual supported dialog APIs.
- Subagents incur provider usage and share the filesystem. Use bounded tasks,
  appropriate roles, exact discovered models, selected child tools, and explicit
  cleanup/cancellation rules. Never grant shell/mutation merely for convenience.
- Never claim the runtime is an OS sandbox or has a Goja heap quota.

### 4. Test the behavior, not just parsing

Create fixtures exercising happy paths, bad input, important state transitions,
required host call order, and error paths. Test hooks with fresh/missing state;
test permission/capability-sensitive operations through explicit mocks. Include
UI unavailable behavior when UI is optional. Check plugin registration and
command/tool metadata where relevant.

```sh
snow plugin test ./plugins/my-extension \
  --fixtures ./plugins/my-extension/tests/plugin.json --json
```

This executes the real Goja runtime with a simulated host. It does **not** prove
real permissions, durable persistence, child lifecycle, terminal interaction,
or provider integration. Add a focused integration test where infrastructure
exists, or list concrete manual checks rather than pretending mocks prove them.
For TypeScript, run the package's typecheck and build before fixtures; test the
actual generated entry. Never claim a command passed unless it ran successfully.

### 5. Load only with the user's authority

Writing a plugin does not imply permission to enable it persistently or invoke
its real effects. Explain the manifest's authority before installation. Ask
before persistent registration unless the user already explicitly requested it.

One-launch loading (starts another Snow process, not the current session):

```sh
snow --js-plugin ./plugins/my-extension
```

Persistent registration, only when requested:

```sh
snow plugin add ./plugins/my-extension
snow plugin check my-extension
```

New/removed/enabled/disabled registrations require restart. For edits to one
already-loaded enabled JavaScript plugin, build first, then use the running
TUI's `/plugins reload my-extension`. Do not pretend Bash can send this slash
command to the current TUI. SDK/RPC reload is available only through an explicit
connected client. Reload must not cancel active work to get past a busy refusal.

Do not run an interactive Snow process as a non-interactive smoke test or start
real provider work just to prove registration. Fixtures need no provider.

### 6. Finish with evidence

Report the package location, what it does, exact invocation, capability/effect
summary, state scope, tests that actually passed, and remaining manual checks.
Distinguish **built**, **validated**, **registered**, and **loaded**. Do not claim
it is installed or running if you only wrote files. Never stage/commit unless
asked. If the request is unsupported or verification is blocked, say so plainly.

## Resources

- `references/CAPABILITIES.md` — supported design choices and hard boundaries.
- `references/EXAMPLES.md` — need-to-example map for every bundled plugin family.
- `references/api/snow.d.ts` — canonical API 2 declarations.
- `references/api/fixtures.md` — exact fixture format and mock-host behavior.
- `references/docs/` — canonical plugin and related host guides.
- `references/examples/plugins/` — real source, manifests, types, and fixtures.
- `scripts/sync_resources.py` — maintainer-only snapshot refresh/parity check;
  not required for building user plugins. It never installs or activates plugins.
