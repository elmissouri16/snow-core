# Example selector

Paths below are relative to `references/`. Every example includes its real
`snow-plugin.json` and runnable `main.js`. Read both; declarations alone do not
explain the effect and ownership choices. Do not load all examples into context.

## API 2: start here for new plugins

| Example directory under `examples/plugins/` | What to reuse |
|---|---|
| `agent-profiles/` | Complete TypeScript factory/bundle, atomic branch state + tool restriction, fresh request guidance, footer refresh on readiness/session change, actual-Goja fixtures. Reviewer/architect/debugger all allow only read/grep/glob; off clears only this owner. |
| `workflow-guard/` | Complete TypeScript package + fixtures; manual on/off branch flag, pure session/compaction vetoes, footer refresh. Does not infer task completion. |
| `project-helper-v2/` | Settings, command, request hook, host-backed tool, custom result renderer, explicitly child-selectable tool. |
| `workspace-dashboard/` | Footer/sidebar/screen contributions and event-driven display. |
| `ui-studio/` | Supported component gallery, dialogs/forms, themes, notifications, shortcuts. |
| `workspace-notes/` | Scoped persistent storage, native dialogs, prompt-composer insertion. |
| `prompt-recipes/` | Explicit prompt transformations and opt-in request/tool hooks. |
| `session-pilot/` | Existing model selection, branch navigation, root agent controls, goals. |
| `review-team/` | Explicit command coordinating bounded reviewer children and root synthesis; cleanup and provider cost matter. |

Use `api/snow.d.ts` for exact API 2 signatures. Read
`examples/plugins/TRY.md` for the older API 2 pack's interactive walkthroughs;
read the workflow examples' own READMEs for new fixtures/build steps. These are
reference applications, not requirements to copy every capability into a plugin.

## API 1: compatibility and algorithm references only

| Example directory | What it demonstrates |
|---|---|
| `text-tools/` | Pure word-count tool, no host access. |
| `project-helper/` | Simple rooted file tool and event observation. |
| `project-context/` | Bounded project file discovery and document previews. |
| `todo-radar/` | Bounded configurable source search. |
| `git-review/` | Fixed Git status/diff commands, effect classification and permission implications. |

Do not transplant API 1 synchronous handlers or host-call syntax into an API 2
manifest unchanged. Use the API 2 types and `project-helper-v2` host-call pattern.
`examples/plugins/snow.d.ts` is the legacy declaration; the adjacent
`snow-v2.d.ts` is API 2. Prefer `api/snow.d.ts` to avoid that naming ambiguity.

## Tests and realistic validation

- Start fixture format from `api/fixtures.md`.
- Use `examples/plugins/agent-profiles/tests/plugin.json` for ordered workflow
  mutations, restoration, hook inputs, and rejected inputs.
- Use `examples/plugins/workflow-guard/tests/plugin.json` for lifecycle decisions
  and observation/ready UI updates.
- `examples/plugins/smoke.py` and `extensions_smoke.py` demonstrate broader
  mock-provider integration. They assume a Snow executable, Python, and in some
  cases Git. Read them before adapting; do not execute all examples merely to
  validate a new unrelated plugin.
- Build TS before fixture tests. Test the generated `main.js` that Snow loads.
- A fixture host is simulated; document manual permission/UI/cancellation checks.

## Resource layout and canonical links

The `docs/` and `examples/plugins/` directory layout preserves their normal
relative crosslinks within this bundle. Links from those canonical guides to
other repository internals may require the Snow source repository; those are
background references, not missing plugin APIs. The builder's required API,
manifest, workflow, UI, fixture, and example material is bundled here.
