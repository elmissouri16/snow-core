# Workspace React integration (workspace owner only)

Main imports `HomePage`, `WorkspaceCatalog`, `ColdWorkspace`, `Activation`,
`LoginPage`, `FolderPicker`, `DraftNotice`, `workspace`, `validateHomeProps`,
`validateCatalogProps`, `validateColdProps`, `validateLoginProps` from `./workspace`.
Register `window.SnowWorkspace = workspace` before React readiness. Main's page
switch uses `home`, `workspace-catalog`, `workspace-cold`, `login`; validators
accept explicit public DTOs only (see model.ts). `ProjectOperations` can also
mount inside Settings via exported component (csrf/enabled props).

DTOs (camelCase, no raw Page/live state):
- home: `{projects:[{id,name,path,available,trusted,skillsEnabled}],error}`
- workspace-catalog: same plus `{csrf,registryEnabled,projectOperationsEnabled}`
- login: `{csrf,error}` (NEVER pairing code). Preserve the outer Go version footer.
- workspace-cold: `{project,csrf,sessionID,sessionTitle,error,runtimeEnabled,
  hasHistory,nextURL,recoveryMessage,recoveryURL}`. Render only cold projects.
  `ColdWorkspace` accepts optional `history: ReactNode` from transcript owner;
  if omitted it emits EMPTY `#workspace-history-view` foreign mount. Inspector
  must remain a sibling owned by Go/inspection; place cold component in
  `.conversation-pane`; component includes cold heading but not inspector.
  Alternatively use exports `ColdHeader` and `ColdConversation` independently
  to preserve exact outer layout. Never wrap existing HTML/live DOM.

Bridge app -> React (synchronous commit, no network/admission):
`workspace.updateDraft({text,homeEnabled,workspaceEnabled,workspaceText,
name,pending,privacy,notice:{text,explanation,url,useVisible,useDisabled}|null})`
- App retains canonical homeDraft. Replace home/startup textarea value/disabled,
  home label, privacy and draft-notice DOM writes with this projection.
- Native delegated `input` events remain app-owned, identified by existing
  home-prompt/workspace-prompt IDs + data-draft-* attributes. Controlled React
  input updates local projection immediately; existing app input listener writes
  canonical homeDraft exactly once. App sync publishes canonical values.
- Existing Continue submit, home capture selection, Use/Discard handlers remain
  app-owned; emit updateDraft instead of writing React DOM. No duplicate HTTP.
- `workspace.updateFolder({path,parent,folders:[{name,path}],hasMore,busy,
  canSelect,error,status})` replaces folder render/disabled/error DOM writers.
  Existing app browse/folderRequest, folderState, offsets, openDialog/closeDialog,
  delegated data-folder-* actions remain owner. Folder rows have
  `data-folder-path`. `workspace.setProjectPath(path)` fills registration draft
  only. Never registers/selects an operation grant.
- `workspace.updateFlowError(text)` renders cold flow error; don't prepend DOM.
- `workspace.reset()` clears page-local projection but NOT app homeDraft.
- A live empty `data-react-page="startup-draft-notice"` mount can render
  `DraftNotice`; no runtime state bootstrap required. Folder picker included in
  catalog; standalone `FolderPicker` supported. Root effects announce
  `snow:workspace-mounted` for app to sync its canonical draft after mount.

Operations are self-contained React, preserve exact URLs/form bodies and
one-shot grants/revision receipts; no automatic mutations/navigation/activation.
Remove project-operations.js script + its SnowProjectOperations implementation
when integrated. React `projectOperations` facade provides legacy
`setParentPath(root,path)`, `refresh(root)`, `dispose(root)` for explicit callers.

`workspace.updateActivation({busy,error})` projects cold activation form status;
use this instead of modifying data-action-error or activation submit controls.
Cold textbox/trust/skills controls honor `busy`; the native POST/permission/admission
handler stays in app. Generic app action helpers can branch on data-runtime-open.
Folder append requests must publish the accumulated rows (the parent owns its
folderState/request sequencing); React never fetches for this picker.

Verification command (add these to parent package test list):
`node --experimental-strip-types --test src/workspace/*.test.ts` — nine focused
DTO/path/grant/receipt/once-only/detached-owner regressions. Operations intentionally
retain explicit read-only Refresh semantics; there are no polling timers or
automatic reads/mutations on mount. Existing browser fixtures remain integration
authority; no native parity claim is made by this source-only owner.

Live opening / cold inspector follow-up:
- Main renders exported `Opening` in a static `data-react-page="workspace-opening"`
  root beside (never inside) `#live-session`. `updateOpening({visible,minHeight})`
  controls `#workspace-opening`; height is finite, nonnegative, viewport-clamped,
  and capped by CSS `100dvh` on resize. This view performs no reads or admission.
- `updateInspector(open)` projects the cold header button's `aria-expanded`.
- `snow:workspace-mounted` only requests canonical draft projection from app;
  do not attach HTTP or runtime setup to this notification. `Opening` and
  `DraftNotice` do not dispatch it or add networking. Navigation links retain
  real `href` values and use `data-snow-navigation` for the single delegated
  ancestor navigation owner.


Home picker / registration transport follow-up:
- Home subscribes directly to shell.subscribe/getSnapshot and spreads
  menuLauncherARIA(shellView.menu, 'workspace') on its summary. React exclusively
  owns aria-expanded and aria-controls; SnowMenus managedTrigger must not mutate
  either. The shell helper supplies its stable SHELL_MENU_ID when open.
- #add-project-form MUST retain enhanced in-document submission when JavaScript
  is active. Its native method=post action=/projects/add remains the no-JS
  fallback, not parity for the ordinary draft-carry workflow.
- App's single delegated submit handler must intercept ONLY #add-project-form,
  capture FormData before disabling/changing controls, and call
  `SnowNavigation.submit` once from that form source with csrf/path/name fields.
  Do not use the JSON request helper: this endpoint returns HTML through a 303
  redirect, not a JSON receipt. Preserve unique `#workspace` selection,
  ancestor-only replacement, redirect URL/history, response bounds, and explicit
  unknown-outcome feedback. Never replay registration after a timeout or error.
- Backend projects.go:addProject redirects successful registration to
  /?view=projects&project=<id>&new=1 and failures to
  /?view=projects&notice=register_failed. Registration never activates a worker.
- Existing scripts/tests/browser/live-stream/tests.mjs registration assertions
  require window.homeLifetimeMarker and the startup draft to survive submission
  without full document reload. Native fallback alone fails that contract.
