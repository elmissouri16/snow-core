# React shell integration (public contract)

`ReactShell` / `validateShellBootstrap` / `shell` / `sidebarSessions` /
`sessionActions` are exported by `./shell/index`.

Mount ONE ReactShell, never independently mount HostSettingsPanel or
BrowserInventory inside it. Suggested template layout:

```
#workspace (existing external grid + identity attributes)
  #shell-navigation-root (style="display:contents", EMPTY)
  #workspace-content (existing external page owner)
  #shell-react-root (style="display:contents", EMPTY, data-react-page="shell")
```

`<ReactShell bootstrap={validateShellBootstrap(props)}
 navigationHost={document.getElementById('shell-navigation-root')!}
 navigate={(href, source) => /* existing app navigation callback */}
 command={command => /* existing app global chrome owner */} />`

navigationHost is optional: absent, navigation renders inline. The navigation
portal belongs to the SAME React tree as Settings, not another root. Do not put
SSR children or nested data-react-page markers in either empty host. Do not put
raw pageState in props. Parent Go projects exactly these public fields:

```ts
interface ShellBootstrap {
 csrf: string; version: string; view: string;
 project: string; session: string;
 hostSettingsEnabled: boolean;
 pairingCode: string;
 projects: Array<{id: string; name: string; path: string; available: boolean;
   trustRemembered: boolean; skillsEnabled: boolean; pinned: boolean}>;
 sessions: Array<{session_id: string; name: string}>; // selected project only
 live: null | {project: string; session: string; instance: string; title: string;
   renameAvailable: boolean; renameDisabled: boolean; newDisabled: boolean};
}
```

Use empty strings/null/false explicitly. Limits: 100 projects, 100 session rows,
UUID project IDs, 128-byte project names, 4096-char path/title, 512-char CSRF,
256-char session/instance, 128-char version/pairing code, 32-char view.

Commands: `{type:'theme', theme:'light'|'dark'}`,
`{type:'collapse', collapsed:boolean}`, `{type:'navigation', open:boolean}`.
Parent owns global root theme/layout/body inert/role attributes; shell never
writes them. No callback: commands dispatch `snow:shell-command` CustomEvents.
For external heading collapse/mobile toggle handlers call `shell.collapse()` /
`shell.navigation(open)`, and stop the old competing shell handlers. Theme
bootstrap reads existing documentElement.dataset.theme (existing preference
owner remains authoritative). `shell.setTheme(theme)` updates local choice.

Register window.SnowShell=shell, SnowSidebarSessions=sidebarSessions,
SnowSessionActions=sessionActions BEFORE SnowReactReady. Remove legacy shell.js,
sidebar-sessions.js and session-actions.js. Existing shared SnowMenus is allowed
only as external host geometry/focus manager; JSX supplies .snow-menu-content.
Do not call SnowMenus.reconcile on shell menus.

App can keep READ-ONLY selectors. It must not mutate React children (sidebar
rows, titles, launcher attrs, tree visibility, Settings sections, input values).
`shell.syncCurrent(liveElement)` / `sidebarSessions.syncCurrent(liveElement)`
read the existing live identity/title/control presentation and commit React state.
Call on complete identity-checked snapshot; never intermediate switching state.
`shell.updateLive(publicLiveOrNull)` is preferred when projection is available.
`shell.preemptInventory()` MUST run synchronously before explicit Start/Resume
admission; this aborts background inventory without retry, preventing BUG-183.
`sidebarSessions.initialize()` reconnects the read-only live observer; it never
activates a worker. `invalidate(project)` revokes capability immediately and
refreshes only visible expanded inventory. `deleted(project,session)` removes
only the exact saved row. Legacy renderRow can only withdraw existing row authority (never grant it); appendMenu is a no-op:
all launchers/menu children now belong to JSX, not external DOM writers.

Session intent event contract unchanged: snow:session-select detail
{project,session,instance,trigger}; snow:session-new {project,trigger};
snow:session-deleted {project,session,instance}. Cold session links use navigate
only. New-session commands retain existing app owner, drafts, confirmation and
instance guard. The shell never starts/resumes/switches/sends itself. New and
select handlers stop bubbling, and links expose ordinary `href` values plus the
`data-snow-navigation` marker. The explicit callback delegates to the first-party
workspace navigator; disable any external capture listener that would duplicate
those commands.

Navigation callbacks may replace the `#workspace` ancestor only. They must never
process or mutate descendants inside shell hosts. Keep shell root and navigation portal stable
across purely live snapshot updates to preserve keyed nodes, focus and drafts.
On actual ancestor replacement unmount BEFORE replacing hosts. Tab-only branch,
query and scroll state is retained in memory, never browser storage; no query or
filter is added to URLs. Global browser/host settings remain native form posts.

## Global owner details (avoid competing writes)

`setNav` in app.js must no longer write nav.classList, nav.inert, nav role /
aria-modal, or backdrop.hidden: these are JSX-owned. React shell uses matchMedia
and owns those properties. Its `navigation` command callback should ONLY apply
external #workspace-content / .topbar inert, external mobile toggle aria-expanded,
and app's return-focus bookkeeping. It must NOT call shell.navigation recursively.
External mobile toggle calls shell.navigation(open). Likewise syncThemeChoices
must call shell.setTheme(theme), not rewrite React appearance buttons. Collapse
command applies documentElement.dataset.sidebarCollapsed and external #workspace
class plus any external heading-button attributes; the sidebar button is JSX.
Native Settings <dialog>.showModal owns modality; no second body inert manager.

The controller retains bounded sidebar query, independent expanded branches,
pinned filter, scroll and previously-owned focus in tab memory across real
ancestor swaps. No persistence, URL filter state or automatic Start is added.

Home picker continuity: the external `.workspace-picker > summary` click owner
must prevent its default details toggle and call `shell.openWorkspacePicker(summary)`.
That creates a JSX workspace menu (not a cloned SSR menu), preserving
`data-home-project`, `data-home-project-name`, `data-home-project-available` and
`.picker-add` for app's existing draft-first capture handler. Keep that capture
handler until its home state owner migrates; it selects locally, never navigates
or starts. This is separate from sidebar `.project-link` capture: move its
visited-session URL resolution into the supplied navigate callback so it cannot
duplicate/preempt React navigation. No imperative picker renderer remains.


## Managed menu launchers / navigation source metadata

ShellPopup passes `managedTrigger: true` to SnowMenus.open. SnowMenus MUST skip
all launcher aria-expanded / aria-controls writes on open AND close. JSX owns
those attributes from `menuLauncherARIA(view.menu, kind, project?, session?,
available?)`; exported `SHELL_MENU_ID` is the stable external panel ID. Session
availability is included, so withdrawn capability immediately projects a closed
launcher before its layout effect retires the affected popup and restores focus.

The external React Home picker summary must subscribe to shell state and spread
`menuLauncherARIA(snapshot.menu, 'workspace')`; it must not rely on SnowMenus
writing its ARIA. No mutation of React launcher attributes is permitted.

Native JSX navigation sources retain real `href` values and carry only the
`data-snow-navigation` marker for delegated ordinary-click handling. Explicit
React callbacks dispatch `snow:shell-navigate`, and the app calls
`SnowNavigation.visit` with the actual anchor as its source (not the popup
launcher), so fragment/history destinations remain correct. The generic
`data-snow-navigation` delegate runs in bubble phase so React's guarded callback
can prevent and stop the event first; a capture-phase delegate would bypass New,
selection and local-inspector admission. Modified clicks and native fallbacks
remain browser-owned. Existing session intent sources use the
same first-party navigation contract without adding competing listeners.
