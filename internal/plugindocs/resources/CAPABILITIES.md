# Choose a supported plugin design

This is a routing guide, not a replacement for exact declarations. Read
`api/snow.d.ts`, `docs/plugin-extensions.md`, and `docs/plugin-workflows.md` before
implementing the selected features. Paths here are relative to the embedded resources directory.

| User needs | Implement with | Read/example |
|---|---|---|
| A reusable slash action | `registerCommand`; optional alias/shortcut | `project-helper-v2`, `workspace-notes` |
| A capability the model can select | `registerTool`; bounded schema, result, effect and handler authority | `project-helper-v2`; API 1 `text-tools` only for a pure-algorithm example |
| File/search or shell-backed functionality | Declared host tools and serial `ctx.tools.call` | `project-context`, `todo-radar`, `git-review` are API 1 patterns; adapt with API 2 declarations |
| Instructions or prompt preprocessing | Pure `before_prompt` / `before_request` hooks | `prompt-recipes`, `agent-profiles` |
| Checks around tool calls | Pure `before_tool` / `after_tool` hooks within allowed result shape | `prompt-recipes`; hook table in extension guide |
| A branch-local profile or approval state | `workflow` + fresh `workflowKeys` | `agent-profiles`, `workflow-guard` |
| Enforce narrower tool access | Plugin-owned `tools.restrict` or atomic workflow update | `agent-profiles` |
| Block switching/compaction until user approval | Pure lifecycle `{block}` gates, explicit command to clear state | `workflow-guard` |
| React to progress or state changes | Bounded `snow.on` observer; `onReady` for initial display | `workspace-dashboard`; session-change observer in both workflow examples |
| Notes/settings/cache-like data | Scoped `storage`, typed manifest settings | `workspace-notes`, `project-helper-v2` |
| Status panels or a screen | Registered views + bounded component trees | `workspace-dashboard`, `ui-studio` |
| Ask the user for input | Supported dialogs, choices, and typed forms | `ui-studio`, `workspace-notes`; `docs/user-input.md` |
| A theme or better tool result display | Registered theme / tool renderer | `ui-studio`, `project-helper-v2` |
| Prompt/steer/follow-up/abort | Explicit command with agent controls | `session-pilot` |
| Select a configured model | `models.list` / `models.set`; no new provider protocol | `session-pilot` |
| Branches or explicit compaction | Session controls from a command | `session-pilot`; `docs/sessions.md` |
| Manage an existing goal workflow | Explicit goal controls | `session-pilot`; `docs/goals.md` |
| Parallel bounded reviewers/helpers | Subagent controls with explicit role/model/tool selection | `review-team`, `project-helper-v2`; `docs/subagents.md` |
| Iterative plugin development | TS build, Goja fixtures, one-plugin reload | `agent-profiles`, `workflow-guard`; workflow guide |
| Embed/control extension surfaces elsewhere | Existing SDK/RPC methods, not a second agent loop | `docs/sdk-reference.md`, `docs/rpc.md` |

## Authority and state choices

- Capabilities and handler `uses` declare access; they do not approve arbitrary
  tool effects. Nested tools still pass ordinary permission/Plan/role checks.
- Native mode and plugin profile are distinct. Restrictions only intersect;
  clearing one owner never clears another owner or native restrictions.
- A selected profile is guidance plus a restriction, not a model replacement.
- `storage` is scoped KV data. Workflow state follows Snow's append-only
  conversation branch ancestry. Neither means Git-branch-specific storage.
- State/restriction changes that must agree belong in one `workflow.update`.
  A later failed command does not undo completed updates or other side effects.
- Workflow writes require an explicit idle root command; they reject active
  automatic work and active child work. Reads do not grant mutation authority.
- Workflow hook snapshots are fresh, owned, bounded and root-only. Their union
  is limited to 64 distinct keys per plugin/phase, with per-handler subsets.
- Session navigation controls have their documented success-staging semantics;
  do not generalize these to all host operations being transactional.
- Session-change notification is post-commit. An observer error cannot undo it.
- A compaction veto also affects automatic compaction: an oversized turn can
  stop rather than continue. Document recovery; avoid permanent accidental vetoes.
- New runtime on reload loses JS globals but retains durable KV/workflow data.
  Only loaded enabled JavaScript packages reload; registration changes restart.

## Deliberate boundaries / unsupported requests

Do not invent a hook or registration API for the following:

- Node/browser compatibility, runtime `require`/imports, DOM, `fetch`, or native
  timers. Optional npm tooling is for authoring/bundling, not the Snow runtime.
- Raw arbitrary filesystem, network, process, or credential access. Use declared
  existing host tools, within operator permissions; otherwise explain the gap.
- New provider implementations, auth adapters, or arbitrary catalog injection.
- Raw terminal control, replacing the core editor/renderer, arbitrary frontend
  frameworks, or invented component/dialog types.
- Rewriting exact conversation history, provider-private data access, or custom
  compaction summarizers via the block-only lifecycle gate.
- Arbitrary background daemons, unbounded orchestration, authoritative mutation
  from observers/hooks/renderers, or keeping callback authority indefinitely.
- JS plugin OS sandboxing or enforced heap quotas.
- Remote plugin installation, hot enabling/disabling, reload-all, or changing
  already-open child tool snapshots through a reload.

For an unsupported request: name what is missing, propose the closest supported
composition, and ask before changing the intended behavior. A tool wrapper around
an external executable may be possible but needs explicit authority and deployment
requirements; it is not implicit Node compatibility or a security workaround.
