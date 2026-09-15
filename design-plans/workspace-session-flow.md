# One workspace, many sessions, one conversation surface

Written against: `db7b2185c86a6506006ff797dce3129e2de385f0`

Status: Implemented and verified in the working tree; not a published release.

Implementation evidence: `bugs.md` BUG-181 through BUG-183 and the current
workspace/session contract in `docs/web-manager-implementation-plan.md`. The
real-manager journey passes 87 assertions (including cross-workspace mounting and
one deliberate switch); frontend units pass 129 tests; grouped sidebar/workspace
actions have focused Node and native checks. Layout passes 147 mobile reports /
2,645 assertions and 294 desktop dark/light reports / 5,998 assertions. Focused
race, affected Go tests/vet, fixture units, syntax, resource and diff checks also
pass. Full conversation and permission/sidebar matrices passed during this change.

Implementation details retain the existing runtime's cleanup of unused unnamed
empty sessions. Displayed, instance-bound sidebar clicks delegate to Switch,
which refreshes and validates membership server-side; this avoids a redundant
foreground inventory read racing the background navigator. Explicit Start/Switch
preempts its own workspace's read-only inventory and waits for canceled I/O
teardown, never a hidden action retry. Saved-history pagination uses the same
synchronized HTMX workspace navigation so it preserves tab-only drafts and
expanded branches. Inventory/history errors remain distinct from a genuinely
empty new session.

The web implementation and several older plans are untracked working-tree files at this commit. Inspect the current files, not HEAD alone. Preserve unrelated work. This plan is one cohesive workflow change, not a series of independent visual patches. It supersedes the catalog-first navigation and indirect sidebar workflow launches in the older Harness plans only where explicitly described below. It does not replace their conversation, menu, safety, or responsive contracts.

## Design language

- Audited surface: Snow web manager's Add workspace → workspace → session workflow, including expanded desktop sidebar, collapsed rail, mobile drawer, cold/start state, saved history and live conversation.
- Design sources: The user's Snow sidebar screenshots and subsequent ChatGPT desktop reference; current composition in `docs/web-manager-implementation-plan.md`; actual templates and styles below. The ChatGPT image is supplied in the conversation, not stored as a repository asset.
- Documented decisions: Keep Snow branding, the existing 280px expanded sidebar / 56px collapsed rail, shared menus and conversation geometry. The user approved “a workspace is a folder; sessions are conversations inside it,” contextual New session, and one conversation area instead of catalog/activation detours.
- Governing owners and consumers: `pages.html` navigation, `projects.html` workspace presentation, `activation.html` start/resume form, `shell.js` sidebar presentation, `app.js` navigation/drafts/activation and `conversation.js` live workflow admission. `harness.css`, later-loaded `settings.css`, and `menus.js`/`menus.css` own their existing presentation layers.
- Explicit exceptions: None documented.
- Preserved runtime constraints: Runtime-free reads do not activate agents; live ownership mismatch, active-turn switching, queued work, unavailable folders, unknown outcomes, trust and restored permissions retain their real guards. These are behavioral constraints documented in the sources/tests, not permission to conceal required interface states.

## Evidence chain

- The user explicitly describes the current workflow as a maze, not merely unattractive. The accepted direction is one workspace containing multiple sessions with a stable conversation surface.
- The ChatGPT screenshot visibly groups indented conversation titles under several workspace/project rows simultaneously. It shows row-owned compose and overflow controls, a selected conversation, and a compact project popover. It does **not** prove click behavior, persistence, worker lifecycle, exact CSS dimensions, or that its project count is complete. The popover shows a folder/name, a count, a path and Edit project; do not manufacture a corresponding count in Snow.
- `internal/web/templates/pages.html`, `navigation`, renders `.session-tree` only inside the selected `.Project` and only when `.Sessions` or `.Live` exists. The project chevron is currently an aria-hidden span inside a navigation link, not an independent disclosure control.
- `internal/web/projects.go`, `projectData`, returns early for a live snapshot, and separately returns after reading saved messages. Neither path populates the catalog session list. This is why simply adjusting CSS cannot produce the persistent grouped navigator from the reference.
- `projects.html` separately renders a Conversations catalog, a Saved conversation/read-only surface with a return breadcrumb, and a runtime activation panel. Its registration UI says Projects/Add project while the sidebar says Workspaces/Add workspace. `activation.html` alternates conversation/session/project terminology for the same task.
- `shell.js` forwards New through another menu. A workspace-row New action in a different or cold workspace is currently just a session-free workspace GET. `conversation.js` owns guarded `switchTo`, including New via an empty session ID; it now exposes row-aware Rename without routing focus through the header.
- Important existing contract: `projects_navigation_test.go` requires a saved-session GET targeting a different live session to reject the mismatch without switching or reading a foreign session. `RuntimeManager.Open` does not silently switch an already-open workspace. Preserve these contracts; a simpler interface is not authorization to mutate on GET.
- Important inventory constraint: `RuntimeManager.Choices` refreshes models, sessions **and telemetry**. Do not call it automatically to fill a sidebar; opening a branch must not trigger model/provider discovery. The existing `refreshSessions` implementation already uses public `sessions_list` and bounds live session metadata to 100 entries.
- Runtime-free catalog reads use `Catalog.Sessions`/`Messages`, 25-entry pages and at most two short-lived catalog workers. These workers are not activated agent runtimes. Keep that distinction truthful.

## Design decision

Make the sidebar the primary workspace/session navigator and keep the center a conversation surface in every state. Registration selects a folder once. Session selection, empty draft, start, resume and live conversation occupy that same center. No ordinary path requires a separate full-page session catalog or an activation page below it.

Use **Workspace** and **Session** in this web workflow. Internal Go names, URLs and RPC contracts do not need renaming. A workspace registers an existing host folder; it does not create a directory. A session remains a durable Snow conversation, not another copy of the folder.

### Sidebar composition

```text
New session                         [current workspace context]

Workspaces                         [Add workspace]

▾ .pochi                    [New] [⋯]
    Fix login                     [⋯ when supported]
    Review current files
    Untitled session

▾ Testing                   [New] [⋯]
    Explore the API

▸ Another workspace         [New] [⋯]
```

1. Independent workspace disclosures. Multiple branches may remain open, as in the supplied reference. Expanding/collapsing does not select a workspace, change the center, or start work.
2. Workspace name opens that workspace. Keep its selection separate from its disclosure and row actions.
3. Session names open the corresponding conversation in the center. Keep the chosen session's filled selection treatment distinct from a hovered workspace row; do not permanently paint both parent and child as competing active pages. Preserve keyboard focus indication as a separate state.
4. Workspace-row New is always scoped to that workspace. Reuse `icon-chat-plus`; global New uses the currently selected workspace. Only when none is selected does global New open the existing workspace picker.
5. Reuse the existing row New/overflow slot. Do not add another permanent “+ New session” row inside every branch as well; the screenshot provides a simpler row-owned creation affordance. This refines the earlier text sketch without adding another creation path.
6. Put workspace path and supported workspace management actions in the existing overflow menu. Rename the existing “Project settings…” presentation to “Workspace settings…”. Keep removal's explicit retention confirmation. Do not add a second project editor or use a hover-only popover for actionable controls.
7. Reuse current-session Rename with its original row as focus-return target. Do not invent inactive-session rename/delete APIs or display empty ellipsis menus. Existing supported organization operations stay available through their owners; broad session-management feature additions are not this task.
8. Do not copy Images, Scheduled, Plugins, Explore, provider branding, fake tabs, or decorative status/counts. Activity, Organize and Settings remain reachable; they are not steps in creating or opening a session.

### Center and action semantics

| User action/state | Required presentation and outcome |
| --- | --- |
| Add workspace succeeds | Select the registered workspace and show an empty-session compose/start surface immediately. Do not send the user to a duplicate workspace/session list. Registration alone still starts no agent and creates no session database. |
| Open a workspace | Show its current live session if one exists. Otherwise show the last explicitly viewed session in that tab if still valid; fall back to the most recently updated available saved session, then the empty-session surface. All selection here is read-only. Do not invent a persisted “last active” session or load every workspace's history. |
| New session in a cold workspace | Show the empty-session draft and compact Start/Trust & start controls in the same composer seat. The explicit start POST creates the durable session; do not insert a fake saved row/ID before acknowledgement. No second folder picker. |
| New session in an already live workspace | Use the existing conversation owner's New/switch action with an empty session ID. Preserve idle, active-turn confirmation, queue and unknown-outcome guards. Never close/reopen a worker to evade them. |
| Select the current live session | Open its existing surface without switching or starting anything. Re-selecting it must not recreate it or clear the draft. |
| Select a cold saved session | Display its transcript in the normal conversation area, with Resume in the composer seat. No separate history page/breadcrumb/activation-panel detour. Browsing remains read-only; Resume remains an explicit POST and never auto-sends a draft. |
| Select a different session owned by the current live workspace | Treat the deliberate sidebar click as a request to the resident conversation switch owner, not as a mismatched saved-history GET. Revalidate membership and instance; use the existing stop/switch confirmation when required. Update selection and URL only after authoritative acknowledgement. Cancel/failure leaves the prior session selected and draft intact. |
| Select a session in another already-live workspace | First mount that workspace's current owner via a read-only workspace navigation, preserving the old workspace draft. Carry only the latest explicit click intent in tab memory; consume it once through the newly mounted conversation owner's guarded switch entry point after project/instance/membership validation. Do not synthesize this intent from a restored URL or retry it after failure. If the owner/instance changed or admission is unavailable, show the blocked reason and require a fresh explicit action in place. |
| First trust, busy/queued work, unavailable host, two-live-workspace limit, permission/recovery uncertainty | Show the actual required review/action in the current surface. Never hide a blocker inside optional Advanced details, grant Allow, replay work, discard a queue/draft, or close another workspace automatically. |

A cold workspace may contain files from an already-existing Snow session store. Adding its registration still lands in an explicitly new empty-session surface, rather than accidentally resuming an old session. A normal later workspace visit follows the selection rule above. Represent this distinction as bounded read-only new-draft navigation intent, not as a worker action on GET.

The first-start trust statement, skills choice and restored-policy warning remain visible when relevant. Technical explanatory prose can use the existing disclosure pattern. Start/Resume and the reason sending is unavailable must be immediately apparent. Preserve remembered skills as checkbox preselection, not a hidden grant; preserve separate CLI extension trust and tool permissions.

## Reuse

- `templates/icons.html`: existing `icon-folder`, `icon-chevron`, `icon-chat-plus`, `icon-folder-plus`, `icon-chat` and Snowflake. No new icon dependency or copied ChatGPT assets.
- `harness.css`: 280px sidebar, 56px rail, current brand arrangement, `--raised`, `--text`, `--muted`, existing indented session-tree geometry and conversation/composer sizing.
- `settings.css`: 32px-minimum workspace rows, 34px session anchors, 28px sibling actions, long-name truncation and pointer/focus action visibility. Keep one owner per rule rather than stacking conflicting overrides.
- `menus.js`/`menus.css`: one viewport-clamped portal, body-level lifetime, focus return, keyboard navigation and unchanged-content reconciliation. Exemplar: `shell.js` workspace menu and current-session Rename.
- `Catalog` and `workerCatalog`: safe inactive metadata/history, pinned identity, pagination and worker admission. `runtime_choices_refresh.go::refreshSessions`: live-owner public session inventory; extract/reuse its bounded implementation without model discovery.
- `conversation.js`: `canSwitch`, `switchTo`, instance/membership guards and `openRename`; `app.js`: action transport, runtime-open form handling, navigation lifecycle and existing draft owners. Extend these facades; do not add another action controller.
- Existing live and saved-message renderers, `SnowMessages`, `SnowScroll`, attention, inspection and menu owners. Moving saved history into the same composition must retain sanitized Markdown, image/tool projection, bounded history and correct follow/reader behavior.

A new narrow read-only sidebar session projection is required because the current selected-page DTO cannot represent several independently expanded workspace branches. It belongs in `internal/web`, over the existing catalog/runtime client boundaries. It is not a new agent/session store, workflow engine, provider discovery mechanism or application framework.

## Changes

1. **`internal/web/projects.go`, `render.go`, and a cohesive sidebar-session projection file under `internal/web/`**
   - Separate navigation metadata from the center's selected history/live state. Return a branch's public names/IDs even while the center is showing history; do not make every page load enumerate all projects' sessions.
   - Add a browser-authenticated, bounded read endpoint for one workspace branch (for example `GET /projects/{project}/sidebar-sessions` with validated offset), rendering a shared session-row partial. Reuse registry lookup/identity checks and existing catalog page validation. It must not activate, resume, switch, discover models or open writable session stores.
   - For an inactive workspace use `Catalog.Sessions`. For a live workspace use a narrow optional session-inventory capability on its existing runtime owner; reuse public `sessions_list` and current validation/gates from `refreshSessions`. Do not call combined `Choices` and do not weaken `projects_navigation_test.go` to read a foreign live database.
   - Preserve current truncation/availability flags. Unknown or failed inventory is not an empty workspace. Include only a verified current live row, deduplicated by workspace plus session ID. Do not represent a partial page's length as a total count.
   - Keep page reads explicit and lazy. Use existing 25-entry catalog pages and the existing 100-entry live metadata bound. Bound retained browser metadata as well: at most a 100-row window per loaded workspace, advancing older catalog pages within that branch rather than accumulating unbounded history. Show partial/older-page affordances honestly; never silently cap the user's reachable saved history. Unsupported live pagination remains visibly truncated rather than fabricated.
   - Preserve the current direct-link mismatch rejection. Any new-draft query must be bounded, reject duplicates, and never encode prompt text or grant execution authority.
   - Verify: cold/live/history center states all retain correct sidebar membership; expanding other workspaces is a read only; stale or busy reads leave scoped Retry rather than clearing good rows or changing selection.

2. **`internal/web/templates/pages.html`, reusable sidebar row partial, `static/shell.js`**
   - Replace the decorative project chevron with a real independent disclosure button. Use the existing 28px action sizing and chevron/folder glyphs; retain the name link and sibling New/menu controls as separate hit targets.
   - Render/reconcile loaded branches by stable workspace/session identity. Preserve multiple expansion states in bounded tab memory across workspace swaps; start with the selected workspace expanded, not all 100 registered workspaces automatically loaded.
   - Keep branch requests scoped to their own slots. They must not share the whole-workspace replacement cancellation group, steal focus into content, close unrelated menus, or replace the conversation. Reject responses for detached/replaced rows, changed identities/instances, superseded generations and collapsed/discarded loads.
   - Preserve BUG-180's latest-navigation-wins behavior, no-op DOM stability, search/filter/list scroll and original-launcher focus return. Reveal a restored selected row if expansion shifts it out of view; never scroll the conversation to do so.
   - Keep New's workspace target explicit. Global and row New use one intent/admission path. Do not infer a workspace from whichever live worker happened to update last.
   - Preserve normal browser link behavior: modified clicks, new tabs, direct URLs and history restores remain safe read-only navigation, not replayable mutation instructions. Expose pending selection without prematurely marking another session as active.

3. **`internal/web/templates/projects.html`, `activation.html`, relevant live/saved composition, `static/harness.css` and `settings.css`**
   - Replace the duplicate default central session catalog with the stable conversation/empty-session composition. The sidebar is the normal session navigator; keep bounded older-history navigation inside the selected session.
   - Reuse the existing read-only history renderer inside the conversation area, removing the separate “Saved conversation” shell and “Sessions in this project” detour from the normal path. Keep a concise accurate paused/read-only status until Resume succeeds.
   - Put Start/Trust & start/Resume and its necessary fields in the composer seat; preserve real form actions, CSRF, confirmation, skills, saved policy and error states. Use existing card/row/disclosure styles, not a second onboarding wizard or fabricated disabled composer.
   - Standardize this workflow's visible labels to Workspace/Session, including registration, folder-picker text, menus and status notices. Do not globally rename URLs, Go fields, protocol vocabulary or unrelated documentation concepts.
   - Preserve current desktop/mobile geometry, real utility actions, Snow branding, collapsed identity and short-rail scrolling. In a cold read-only view, do not display live-only model/status values as if an agent were connected.

4. **`internal/web/static/app.js`, `static/conversation.js`**
   - Extend the resident conversation facade for New/Select just as Rename already carries its actual launcher. Reuse the existing switch implementation, not simulated clicks through hidden intermediate menus or a parallel RPC sender.
   - Give live session membership its session-only read owner; do not bind sidebar switching to whether the model picker has been opened. Revalidate membership server-side/current-owner-side before mutation; cached row names are not authority.
   - Carry one bounded, cancelable navigation intent for cross-workspace explicit selection. Bind it to the intended project/session and applicable worker instance; clear on navigation replacement, user cancellation, disconnect, registration/instance changes or unknown outcome. No automatic retries or persisted action intents.
   - Adapt `homeDraft`/`syncHomeDraft`/`takeHomeDraft` into the workspace empty/start surface without duplicating draft state machines. Preserve the existing active-session draft map and one pending startup draft. Visiting another workspace must not overwrite that pending draft: retain it with its original workspace and an explicit review/discard path. Keep text out of URLs and browser storage.
   - Transfer a startup draft only to the verified acknowledgement for its intended New/Resume operation and an empty compatible composer. Preserve existing edit/reuse/unknown-outcome fences; never transfer into a different session merely because the workspace ID matches. Never auto-send.
   - Preserve page-wide failure handling and mounted live state after rejected reads. Keep every message/tool/permission/queue and runtime lifecycle behind its existing owner.

5. **Existing tests and fixtures**
   - Extend `projects_navigation_test.go`, `projects_test.go`, the relevant runtime/workflow tests and `harness_visual_fixture_test.go` for the new projection/center states. Add only focused tests needed for the new read capability and identity fences.
   - Extend `scripts/tests/browser/permission-policy/sidebar.mjs` and its existing runner/transport for multiple expanded groups, selected rows across live/history views, branch-scoped reads and preserved focus.
   - Extend `conversation-workflow` and native `live-stream` fixtures for create → first session → second session → return to first, cold resume, cross-workspace selection, busy/cancel and draft preservation. Extend `workspace-actions/run.mjs` for its shell-routing unit coverage; it is a Node VM test, not a native browser gate. Do not replace real HTMX/SSE transport with a simulated happy path; wait for HTMX settlement before exercising inserted controls.
   - Reconcile existing assertions that deliberately expected a central catalog or a decorative chevron. Do not delete identity/no-activation assertions to make the new path pass.

## Scope

- Inherit: All routes using the shared workspace/session sidebar and normal workspace start/resume center.
- Verify: Home draft entry, registration success/failure, workspace with zero/one/many sessions, several open branches, active/inactive/history states, selected session outside the first page, both themes, expanded/collapsed desktop and mobile drawer.
- Exclude: New project/session database models, multiple simultaneously active sessions inside one workspace, automatic background activation, new provider integration, arbitrary session rename/delete, fake task counts, a general session search feature, redesigning Activity/Organize/Settings, remote/LAN changes and new frontend dependencies.

## Validation

### Product acceptance

- A new user can add a folder, start a session, create a second session in the same workspace, and return to the first without revisiting workspace selection or a separate session-catalog page.
- Two workspace branches can remain expanded and show their own sessions while a third session is read in the center. Expansion alone makes no runtime mutation or model/provider discovery request.
- Session selection remains visible after cold history opens, activation, Rename, live switch, page replacement and a failed read. A canceled or rejected switch never falsely marks its target current.
- A saved session can be read and explicitly resumed in one conversation surface; startup drafts never send themselves or overwrite another session's draft.
- Existing direct saved links cannot silently switch a live workspace. Cross-workspace explicit click intent cannot survive a reload or act on a replacement runtime.
- Verify trust, remembered skills, restored Allow warning, queue review, stop/switch confirmation, unavailable folders, worker limits and unknown outcomes without extra catalog detours or weakened authority.

### Interface acceptance

- Inspect actual exported/rendered states, not just source. Compare the hierarchy to the supplied screenshot, not its uncertain pixel scale.
- Use 320/390 mobile, 768 breakpoint and 1280/1512 desktop; normal and 240px-short heights; dark/light; long names; 100 registered workspaces; paged and unavailable session inventories.
- A row's disclosure, name, New and overflow actions must not trigger one another. Keyboard/focus, hover, loading and selected presentation must remain distinct. Keep controls reachable in short windows and preserve the collapsed Snowflake/expand control.
- Verify no-op snapshots perform no sidebar reconciliation work; branch fetches preserve the current transcript, draft, reader position and open unrelated controls.

### Repository checks for the executor

Run applicable focused tests first, then:

```sh
go test ./internal/web ./cmd/snow
go vet ./internal/web ./cmd/snow
node --test internal/web/composer_context.test.mjs scripts/tests/*test.mjs
node scripts/tests/browser/permission-policy/run.mjs
node scripts/tests/browser/conversation-workflow/run.mjs
node scripts/tests/browser/live-stream/run.mjs
node scripts/tests/browser/workspace-actions/run.mjs
node scripts/tests/browser/harness-layout/run.mjs --smoke
node scripts/tests/browser/harness-layout/run.mjs --width 1280
python3 internal/plugindocs/scripts/sync_resources.py --check
git diff --check
```

Use the Modern Go Guidelines skill before Go edits, gofmt changed Go files, and obey the 1,000-line Go-file limit. Run the repository's broader gates if public protocols, SDKs or core session behavior become affected; such broadening should first trigger the stop condition below. Install with `./scripts/install-local.sh` only after successful implementation verification. Do not restart the user's manager/workers, commit or push without authorization.

## Stop conditions

- Stop if implementation requires a second turn loop, writable catalog access, relaxing leases/identity checks, provider discovery on expand, or a new core/public RPC command. Public `sessions_list` already exists; reuse the client boundary rather than importing internals.
- Stop if a supposedly existing capability is absent. Report the exact missing read/admission contract; do not fake session rows, totals, management actions or successful activation.
- Stop before discarding drafts, queued work, approvals or recovery state to make navigation feel simpler. Keep the applicable explicit review inside the same surface.
- Stop if the correct historical session cannot be bound without weakening the current different-live-session navigation test. Keep safe direct links and explicit owner-mediated switching distinct.
- Do not ship only a cosmetic hierarchy with the old catalog/activation maze still underneath and describe this plan as complete. Partial progress must be recorded as partial.

## Design documentation

After implementation and validation, update `docs/using-snow.md` and the Current composition section of `docs/web-manager-implementation-plan.md` with the workspace/session model, direct normal path, lazy bounded sidebar lists, explicit Start/Resume and preserved safety states. Update the affected browser README descriptions and `bugs.md` with verified results. Reconcile only the superseded navigation paragraphs in the Harness plans; retain their other requirements.

No product source, canonical documentation, tests, installation or running user processes were changed while writing this plan. The screenshots establish the selected design direction, not successful implementation or verification.
