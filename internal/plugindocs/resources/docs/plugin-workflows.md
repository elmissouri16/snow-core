# Plugin workflows and reload

Use JavaScript API 2 to build branch-aware profiles, unfinished-work guards, and
request guidance without adding native collaboration modes. This guide owns the
workflow, restriction, lifecycle, and reload contracts. Start with
[JavaScript extensions](plugin-extensions.md) for commands, UI, manifests, and
the existing host context.

## On this page

- [Try branch-aware profiles](#try-branch-aware-profiles)
- [Store workflow state](#store-workflow-state)
- [Restrict tools](#restrict-tools)
- [Read fresh workflow state in hooks](#read-fresh-workflow-state-in-hooks)
- [Gate lifecycle operations](#gate-lifecycle-operations)
- [Observe a session change](#observe-a-session-change)
- [Reload one plugin](#reload-one-plugin)
- [Author and test TypeScript](#author-and-test-typescript)
- [Limits and recovery](#limits-and-recovery)
- [Related documents](#related-documents)

## Try branch-aware profiles

From a checkout:

```sh
snow --js-plugin ./examples/plugins/agent-profiles
```

Select `/profile reviewer`, `/profile architect`, `/profile debugger`, or
`/profile off`. Enabled profiles contribute request guidance and allow only
`read`, `grep`, and `glob`. The debugger intentionally investigates source
without shell execution. The footer reflects the selected branch's profile.

The plugin commits the selection and its restriction together. Switching
branches, reopening a saved session, or reloading the plugin restores that
branch's values, not a JavaScript global left over from another branch.

`off` clears only this plugin's restriction. It never clears another plugin's
restriction, changes Default/Plan mode, or grants permission. The
[agent-profiles example](https://github.com/elmissouri16/snow-core/tree/main/examples/plugins/agent-profiles)
includes source, built JS, fixtures, and an edit/build/reload walkthrough.

For lifecycle gates, load
[workflow-guard](https://github.com/elmissouri16/snow-core/tree/main/examples/plugins/workflow-guard).
It defaults to off. `/workflow-guard on` blocks session changes and compaction
while the branch has unfinished work; `/workflow-guard off` is the explicit
recovery command.

## Store workflow state

Declare the `workflow` manifest capability and include `workflow` in a command's
`uses`. The host scopes all values to the calling plugin ID and active branch.

| API | Result |
|---|---|
| `ctx.workflow.get({key})` | Copied JSON value, or `null` if absent |
| `ctx.workflow.set({key, value})` | `{branchId, tipId}` receipt |
| `ctx.workflow.delete({key})` | `{branchId, tipId}` receipt |
| `ctx.workflow.update({set?, delete?, toolRestriction?})` | One atomic update receipt |

`set` is a JSON object mapping keys to values. `delete` is an array of keys.
`toolRestriction` replaces this plugin's restriction; `null` clears it, and
omitting the field preserves it. Including that field also requires
`tool_policy` in both the manifest and handler's `uses`.

```js
snow.registerCommand({
  name: "review", description: "Select the reviewer profile",
  uses: ["workflow", "tool_policy"],
  async run(_, ctx) {
    const receipt = await ctx.workflow.update({
      set: {profile: "reviewer"},
      delete: ["last-review-error"],
      toolRestriction: {allow: ["read", "grep", "glob"]}
    });
    return "Reviewer selected on branch " + receipt.branchId;
  }
});
```

Writes require an explicit root command and an idle root. Observers, readiness
callbacks, model-invoked tools, and child contexts cannot write workflow state.
Readiness and root observers may read it with the declared capability. A hook
must use the preloaded snapshot described below instead of calling `get`.

Updates commit **immediately**, not when the surrounding command finishes.
After a successful write, a later UI error, cancellation, or failed command
result does not undo it. Use one `update` when two values, or state and a
restriction, must not be half-applied. Separate calls are separate commits.

The store validates branch identity and the exact current tip while applying an
update. Invalid JSON/keys, conflicts, cancellation, or quota errors leave the
tip unchanged. Empty updates, duplicate deletes, and setting and deleting the
same key in one update are rejected. A restriction replacement or clear counts
as one mutation. Storing JSON `null` retains a key; deleting appends a tombstone.
Both read as `null`, but the stored-null key still consumes the key quota.

### Persistence and branch behavior

Workflow changes use versioned `plugin_workflow_v1` metadata entries in the
append-only session tree; no new database schema is needed. Projection follows
complete root-to-tip ancestry, including metadata older than the current
compaction checkpoint.

- Sibling branch writes do not affect each other.
- A historical fork sees only values recorded at or before the chosen entry.
- An independent session fork copies workflow changes from the selected path.
- Compaction preserves workflow state and exact history.
- Metadata is excluded from ordinary provider messages, compaction input, and
  the rendered transcript. Values reach a provider only if plugin code
  explicitly turns them into request guidance or another visible result.
- Ephemeral sessions keep this state in memory for that session's lifetime.

Existing `ctx.storage` is different: its global/project/session KV store remains
in `SNOW_HOME/plugin-state.db` (or ephemeral memory). It does not follow branch
ancestry and writing it does not advance the conversation tip. Use it for
preferences, not for a branch's selected workflow.

Memory and SQLite session stores implement the optional workflow store
interface. Existing custom stores remain compatible, but workflow operations
return unavailable unless that interface is implemented. A missing or malformed
restriction projection fails closed rather than silently widening tool access.

## Restrict tools

Declare `tool_policy` in the manifest and in the handler's `uses`, including for
catalog reads:

| API | Result |
|---|---|
| `ctx.tools.list()` | Bounded array of `{name, source, effect, allowed, reason?}` |
| `ctx.tools.restrict({allow?, deny?})` | Replaces this plugin's restriction; update receipt |
| `ctx.tools.clearRestriction()` | Clears only this plugin's restriction; update receipt |

These methods do not replace the existing `ctx.tools.call(name, args)` API.
Catalog entries contain public metadata, not executable handles or secrets.
`allowed` describes tool eligibility under current native/operator/plugin
policy; it is **not** a promise of permission approval for any arguments.

- Omitted `allow` imposes no extra allowlist.
- `allow: []` allows no tools.
- `deny` removes exact canonical tool names.
- Lists replace the previous lists; they do not merge with that plugin's old
  restriction. Use an explicit clear when removing the restriction entirely.
- Names cannot contain wildcards. Allowlisted names must exist when saved.
  If a later reload removes a named tool, it stays unavailable; the host does
  not broaden the allowlist to compensate.

The effective policy intersects the registered catalog, existing operator/role
constraints, native mode policy, and every active plugin's restriction. The
same restriction is applied to provider schemas, deferred discovery/routing,
model dispatch, nested plugin host-tool calls, and child admission. A tool
hidden from the model is also rejected if a stale tool call attempts dispatch.

Restriction changes require no active/queued child work. Root restrictions
also constrain descendant turns in addition to role and child-selection
boundaries. Children cannot change the root restriction. Clearing a profile
never widens a child's role or selected-plugin-tool catalog; subsequent turns
still intersect those fixed boundaries with the current root restrictions.

The host restores branch restrictions before the next request; no observer or
readiness callback has to reinstall them. A loaded plugin's runtime failure
retains its committed restriction. Clearing it, or disabling the registration
and restarting, is an explicit recovery action. Disabling removes only that
plugin's effect; its historical records remain available if it is re-enabled.

> **Warning:** Restrictions are tool policy, not an OS sandbox or a universal
> host-control gate. They do not constrain every non-tool plugin API, arbitrary
> Go code, or external process. Permission checks and Plan Mode remain separate
> and authoritative.

## Read fresh workflow state in hooks

`workflowKeys` selects the calling plugin's branch values for a hook:

```js
snow.registerHook("before_request", request => {
  if (request.workflow.profile !== "reviewer") return {};
  return {context: [{text: "Review inspected evidence; do not modify files."}]};
}, {workflowKeys: ["profile"]});
```

The manifest needs both `hooks` and `workflow`. Each root invocation receives
fresh copied values under `request.workflow`; missing requested keys are
explicitly `null`. No other plugin's values are included. The union of requested
keys across all handlers for one plugin and phase is limited to 64 distinct
keys; each handler receives only its own requested subset. Overlapping keys
count once toward the union. A serialized snapshot is limited to 128 KiB.
Projection or size failures remain
hook failures, not an empty successful snapshot.

Combining `workflowKeys` with `includeSubagents: true` is rejected. Existing
child opt-in for the original hook phases remains available without workflow
keys. Hooks still cannot call storage, filesystem, network, UI, or other host
APIs; the snapshot does not grant them I/O authority.

## Gate lifecycle operations

Two additional root-only phases accept only `{}` or `{block: "reason"}`. They
cannot redirect an operation, replace compaction input/output, or perform host
operations. Plugins run in ID order and hooks within a plugin in registration
order, using the normal bounded hook runtime.

| Phase | Additional request field | Boundary |
|---|---|---|
| `before_session_change` | `sessionChange` | Validated active-session/branch transition, before mutation |
| `before_compaction` | `compaction` | Safe compaction plan, before summary work or apply |

`sessionChange` contains camelCase fields `operation`, `oldSessionId`,
`newSessionId`, `oldBranchId`, and optional `newBranchId`, `fromEntryId`. The hook
reads the **old branch's** workflow snapshot. It runs before permission/store
rebinding, goal changes, process teardown, or branch mutation. Detached session
or worktree creation is not an active-session transition and does not run this
gate. Set the guard off before navigating away if it is intentionally blocking.

`compaction` contains `trigger`, `boundaryId`, `summarizedMessages`, and
`retainedMessages`, never provider-private conversation data. Both manual and
automatic root compaction use the gate after a nonempty safe plan exists;
no-op manual compaction remains a no-op. A block, exception, timeout, or invalid
result stops the operation. Automatic continuation ends through its existing
error path instead of repeatedly retrying oversized requests or using summary
fallback to bypass the gate.

## Observe a session change

After a successful active transition, the host publishes
`plugin_session_changed` after releasing transition locks and binding the new
generation's hosts. API 2 observers receive the normal envelope:

```js
snow.on("plugin_session_changed", async (event, ctx) => {
  const change = event.payload.plugin_session_changed;
  const profile = await ctx.workflow.get({key: "profile"});
  // Refresh existing UI with the newly active branch's value.
});
```

The nested payload contains snake_case fields `old_session_id`,
`new_session_id`, `old_branch_id`, `new_branch_id`, `reason`, and `generation`.
It contains identity, not plugin-owned workflow values. The same notification
is available on the SDK/RPC event stream. Observer failure cannot roll back an
already committed transition. JavaScript globals are not a substitute for the
fresh snapshot needed by an authoritative gate.

## Reload one plugin

After explicitly building edited TypeScript, use one of these surfaces:

| Surface | Operation |
|---|---|
| TUI | `/plugins reload <id>` or the loaded plugin inspector's Reload action |
| Go SDK | `Session.ReloadPlugin(ctx, id) (protocol.PluginReloadResult, error)` |
| RPC | `plugin_reload` with `params: {"id":"plugin-id"}` |

Only one **already-loaded, enabled JavaScript plugin** is replaced. It may be
API 1 or API 2. There is no reload-all, Go-plugin reload, remote installation,
or CLI command that silently targets a separate running process. Adding,
removing, enabling, and disabling registrations still requires restart.

Reload re-reads code and settings through the existing trusted configuration
scopes, pinned roots, and startup explicit overrides. Registration executes in
a detached candidate without a live host. Validation includes the rest of the
live catalog; only a successful commit replaces the target's tools, hooks,
commands, subscriptions, and contributed views. The manager and root registry
retain their identity, and unrelated Go/MCP/SDK registrations are preserved.
Package/config fingerprints continue to govern existing approval rules.

Reload refuses busy state rather than cancelling user work. Finish the root
operation, pause an active goal, and wait for automatic work, commands,
readiness, accepted host operations, session transitions, and child work to
settle. Close any still-open child retaining the target plugin's tools, even
when that child is terminal or on another branch. Shutdown and concurrent
management/reload also reject replacement. A custom registry/router without the
required atomic replacement/refresh support must be restarted instead. Adding
deferred tools to a session that started without a router also requires restart.

Explicit reload resets the target VM even when its package bytes are unchanged.
On commit, generation changes invalidate old callback/UI authority. Unrelated
views remain; surviving plugins are rebound but do not rerun readiness. The
replacement's `onReady` runs after commit if surface readiness has begun; if it
has not, normal surface startup performs readiness later. Existing KV/workflow
state survives. Already-applied theme colors stay until another theme is chosen.

### Interpret the result

A successful call returns `PluginReloadResult` (`PluginID`, `Applied`,
`Generation`, `Fingerprint`, optional `Diagnostics` in Go). JSON uses
`plugin_id`, `applied`, `generation`, `fingerprint`, and `diagnostics`; each
diagnostic has `phase` and bounded `message`.

- A preparation, validation, collision, stale-input, cancellation, or busy error
  **before commit** leaves the old plugin usable. The SDK returns an error;
  RPC returns `success: false` without an applied receipt.
- Once committed, cleanup/readiness failures return **`applied: true` with
  diagnostics**, not an error claiming that the replacement rolled back.
  Readiness may already have performed host operations. Inspect diagnostics,
  fix the plugin, and reload again when idle.

`snow plugin check` is useful before reload, but cannot prove callback behavior
or guarantee that a later live catalog will still be compatible.

## Author and test TypeScript

```sh
snow plugin init my-extension --typescript
cd my-extension
npm install --save-dev esbuild typescript
npm run check && npm run build
snow plugin test . --fixtures tests/plugin.json --json
```

The private scaffold provides a synchronous default factory in `src/main.ts`;
`src/entry.ts` invokes it with the existing typed `snow` global. Build produces
one neutral-platform IIFE in `main.js`. Plain global-style JavaScript packages
remain supported. `npm test` explicitly typechecks, builds, then invokes the
installed Snow CLI. Snow startup/reload never installs dependencies or builds.
Node, browser, and runtime module loading APIs are not supplied.

Fixtures run a fresh actual Goja runtime per case with ordered explicit mock
host calls. They support commands, tools, hooks, readiness, serial observer
callbacks, copied memory state, metadata assertions, and JSON Pointer result
selection. Unexpected host calls fail even if plugin code catches the error.
There are no real providers, processes, network operations, or session writes.

Read the generated `tests/README.md` or the canonical
[fixture format guide](https://github.com/elmissouri16/snow-core/blob/main/internal/plugin/javascript/scaffold/fixtures.md).
Fixture files are limited to 1 MiB, 128 cases, five seconds per case, and
60 seconds per suite. Mock tests verify plugin behavior against declared host
responses; they do not prove real permission checks, branch ancestry, tool
admission, or reload/lifecycle transactions. Snow's Go integration tests cover
those boundaries.

## Limits and recovery

| Resource | Limit |
|---|---|
| Workflow key | Valid UTF-8, 1–128 bytes, no NUL |
| Workflow value | 64 KiB encoded JSON |
| Live workflow state | 1 MiB and 1,024 keys per plugin/branch |
| Atomic update | 1–64 mutations, 128 KiB encoded update |
| Workflow/restriction history | 16 MiB or 16,384 records per session, across plugins/branches |
| Restriction lists | 512 exact names per list |
| Hook workflow snapshot | 64 distinct keys per plugin/phase across handlers, 128 KiB serialized snapshot |

Historical deletes and replaced values still consume the history budget. A
quota failure never truncates replay or rewrites old history. Reduce a pending
update to satisfy value/live-state limits; when the session history quota is
exhausted, use a fresh session rather than expecting deletes to reclaim history.

A restrictive profile still leaves its explicit recovery command available.
If the VM itself is disabled, fix/build/reload it while idle, or deliberately
disable the registration and restart. Reloading does not clear committed policy.
An active lifecycle guard can intentionally prevent navigating away; use its
off command before retrying.

## Related documents

- [JavaScript extensions](plugin-extensions.md) — commands, UI, original hooks
- [Plugins](plugins.md) — local registration, API 1, and Go plugins
- [Sessions and branches](sessions.md) — durable conversation workflows
- [Security model](security.md) — permissions and privilege boundaries
- [SDK reference](https://github.com/elmissouri16/snow-core/blob/main/docs/sdk-reference.md) · [JSONL RPC](https://github.com/elmissouri16/snow-core/blob/main/docs/rpc.md) — host integration
