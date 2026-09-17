# Web frontend development

The web manager is served by Go. Its browser rendering is being migrated to
React, while the existing serial browser network/admission controller and the
RPC-backed worker boundary remain separate. This guide covers development and
asset ownership, not a second agent loop or a replacement runtime architecture.
For user operation, see [Using Snow](using-snow.md#try-the-local-web-manager-shell);
for security, see [Security model](security.md).

## Toolchain and files

The private package is `internal/web/frontend`. Its `package.json` pins React and
React DOM 19.3.0, TypeScript 7.0.2, Vite 8.3.0 and the React Vite plugin 6.1.1;
`package-lock.json` locks the full dependency tree. Use Node >=22.12.0 and npm
for frontend development. The frontend CI job pins Node 24.16.0. There is no
production Node server, Next.js dependency, or additional UI library, and these
browser dependencies do not enter core Go packages, the SDK or the TUI.

| Path | Responsibility |
|---|---|
| `internal/web/frontend/src/` | React views, typed models and panel controllers |
| `internal/web/frontend/src/main.tsx` | Page roots, native navigation cleanup/remount lifecycle, live-panel facade readiness |
| `internal/web/frontend/vite.config.ts` | Browser build and standalone workbench configuration |
| `internal/web/frontend/scripts/` | Typechecked build, runtime notices and read-only asset comparison |
| `internal/web/static/generated/` | Checked-in production bundle and runtime notices; generated, not hand-edited |
| `internal/web/static/` | Existing CSS, browser transport/admission and remaining legacy views |
| `internal/web/templates/` | Go document/navigation shell, bootstrap props and mount points |
| `internal/web/render.go` | Embedded assets, explicit static route allowlist and HTTP security headers/checks |

Ordinary source and release builds use Go 1.27rc3 and the checked-in embedded
assets; they do not invoke npm or require Node. Editing TypeScript alone does not
change the web UI served by a newly built Go binary unless the assets are rebuilt.

## Build and verify frontend changes

From the repository root:

```sh
cd internal/web/frontend
npm ci --ignore-scripts
npm run build
npm test
npm run check
```

- `npm ci --ignore-scripts` installs the locked tree without package lifecycle
  scripts. Keep package metadata and the lockfile together when updating versions.
- `npm run build` runs `tsc --noEmit`, validates installed runtime package metadata
  against the lockfile, and builds the production bundle. It explicitly replaces
  `../static/generated/`, emitting `app.js` and `THIRD-PARTY-NOTICES.txt`.
  `npm run typecheck` is also available as a standalone check.
- `npm test` runs the package's focused model and asset-verification tests. It
  does not run native browser acceptance or the whole Go suite.
- `npm run check` performs the same typechecked build in a temporary directory,
  compares the complete generated file tree by filename and bytes, then removes
  the temporary directory. Missing, extra or stale assets/notices fail the check;
  it never repairs the checked-in tree. Rebuild explicitly, review the diff and
  include generated files with source changes.

The Vite build emits one self-contained ES module targeting ES2022, with no
source maps or external CDN runtime. `envPrefix: []` disables automatic
client-environment exposure. The build script uses `envDir: false` to disable
`.env` loading (the current equivalent of the deprecated `envFile: false` option).
Do not introduce environment-derived credentials or bootstrap secrets into the
bundle. The only explicit process-environment definition is the fixed
production/development React mode selected by the command.

Runtime notices include full installed license texts for React, React DOM and
Scheduler, with package versions/license metadata checked against the lockfile.
Build-tool dependencies are not represented as runtime bundle dependencies.
Do not edit the generated notice text manually.

## Standalone component workbench

```sh
cd internal/web/frontend
npm ci --ignore-scripts
npm run dev
```

Open the `127.0.0.1` URL and port printed by Vite. This is a standalone workbench
with fictional, empty Organization data and the existing stylesheet stack. It
has no backend proxy: it is not a paired manager, cannot activate workers or
edit real registrations, and does not start a user runtime. It is useful for
component iteration, not live conversation or host-control acceptance.

Do not point this development server at real manager credentials, add a proxy,
or loosen production Host/Origin/CSP rules to make integration requests work.
The Go manager still serves same-origin embedded assets through an explicit
allowlist, rejects unexpected Host/Origin and cross-site requests, and retains
pairing, CSRF and permission/admission checks. Production CSP has no
`unsafe-inline` or `unsafe-eval` allowance. Vite's development/HMR server is not
part of that production security boundary.

For production-path verification, rebuild assets, then from the repository root
build Go and run the relevant fixtures. After a successfully verified feature
change, follow the repository's `./scripts/install-local.sh` workflow. Installing
or rebuilding does not update already running managers/workers; explicitly
restart them before exercising the new binary and embedded assets. Do not
silently stop or replace a user's runtime as part of a frontend check.

## Rendering ownership

The React rendering ports are wired into the production bundle, but source
integration alone does not establish whole-product parity. They cover:

- Activity, Organization, Home, the workspace catalog and cold/saved workspace,
  pairing/login, startup presentation and project-operation views.
- The Shell sidebar, workspace/session menus, Settings, browser inventory and
  host/API-key controls.
- Conversation menus, model/mode/usage controls, live status, composer text and
  actions, context attachments, questions/approvals, queueing and steering.
- Live and saved messages, Markdown, tool rows, images, transcript reader and
  width controls, Files/Changes inspection, reasoning, compaction, goals,
  managed processes and Versions/History controls.

The remaining classic scripts have separate, deliberate responsibilities:
`app.js` owns serial transport/admission and external document/form lifecycle;
`stream.js` owns the read-only snapshot connection; `menus.js` owns external
popup geometry and keyboard focus. Cost formatting now belongs to the typed
conversation model; the retired `costs.js` implementation is removed while its
stylesheet is retained. Retired classic scripts kept temporarily for unique
regression fixtures are excluded from the embedded production asset set and
remain unrouted. Go still owns
public projection, document shells, empty mount hosts and non-React outer form
metadata, with the first-party `SnowNavigation` controller replacing the bounded
`#workspace` ancestor through the existing server-rendered fragment path. These
are not competing legacy renderers to port wholesale into React. Shared
menu hosts must not rewrite JSX-owned launcher attributes or reconcile their
children. Merely bundling React does not transfer ownership of a view.

Page roots are marked with `data-react-page`; live panels use explicit mount
points and view facades. React owns descendants of a mounted root. Go and the
native navigator may replace its ancestor, but must not also render inside that
React-owned subtree. `main.tsx` unmounts roots before a committed native
replacement, remounts afterward and signals `snow:react-ready` for the classic
controller. Keep
bootstrap validation, detached-root disposal and safe failure presentation.

React rendering does not become admission authority. The existing serial
transport/controller contracts retain synchronous request reservation,
identity/revision checks, cancellation, consent consumption and uncertain-outcome
handling. Component scheduling or a disabled button is not a duplicate-send
fence. Preserve owner-scoped state, fresh public snapshots, explicit user actions
and no automatic mutation replay; do not add provider calls, runtime activation
or a parallel tool/turn loop in a component.

Clearly named, non-destructive actions execute on the explicit click rather than
adding a confirmation-only dialog or checkbox. Compaction captures the current
scope at click time, starts through the existing serial admission path, and shows
progress/errors inline with the composer's shared Stop. Thinking uses an anchored,
capability-driven level picker with direct selection; summary/verbosity remain in
secondary Response settings. Goal is a local composer-mode toggle: explicit Send
uses the existing editor's objective and native goal admission, never a second
input or a browser scheduler. Details and exact-target Resume stay secondary.
Saved-branch rename,
detached-conversation creation and reversible conversation archive also act
without a second confirmation. Required transport fields remain unchanged;
UI simplification is not removal of server checks or implicit consent grants.
Retain necessary input dialogs and confirmations for active-history replacement,
permanent deletion, interruption of work, permissions, trust and external access.

`SnowNavigation` is the only in-document workspace replacement owner. It accepts
same-origin GET destinations at `/` and the registration POST at `/projects/add`,
sends `X-Snow-Navigation: workspace`, follows the registration redirect, bounds
HTML to 8 MiB with fatal UTF-8 decoding, requires exactly one `#workspace`, and
rejects non-HTML/error/cross-origin responses. A newer navigation aborts its older
read. Before replacement it synchronously unmounts React roots and dispatches the
imperative cleanup lifecycle; afterward it pushes or replaces URL history,
remounts roots, and dispatches completion. Push navigation resets the document
to the destination top (or fragment); each outgoing history entry stores bounded
tab-local scroll coordinates, and Back/Forward refetches the URL before restoring
that entry instead of caching DOM snapshots. Ordinary SSR links and registration
forms retain browser `href`/`action` fallbacks when the module is unavailable;
React-owned links run their guarded callback before the bubble-phase generic
`data-snow-navigation` delegate.

Explicit workspace navigation retires the tab's background sidebar reads before
request dispatch. The server also preempts same-project sidebar catalog reads
and waits for their canceled I/O to finish before reading foreground history.
It does not increase the catalog-worker cap, cancel unrelated project reads,
retry failed operations or activate a runtime.

Composer native input is captured before target-level mention discovery can
repaint suggestion attributes, so a synchronous React update cannot restore the
previous draft. The context adapter replaces only its supplied text range and
collapses the caret after the insertion; it never treats a mention substring as
the complete draft.

## Verification and release integration

After regeneration, use the focused package tests and affected Go tests, then
the relevant native browser fixtures from the repository root. For example:

```sh
node scripts/tests/browser/react-pages/run.mjs
node scripts/tests/browser/stream-client/run.mjs
```

Native browser fixtures require Go and installed Chrome/Chromium; set
`SNOW_CHROME_BIN` for a nonstandard location. Additional manager access,
host-control, runtime-control, conversation and layout suites cover their
respective boundaries; see [architecture verification](../IMPLEMENTATION.md#testing-and-verification)
and the [web acceptance record](web-manager-implementation-plan.md#expanded-controls-acceptance-evidence).
Package tests or a reduced browser smoke run do not replace those gates.

Keep evidence attached to the artifact actually tested. The resumed integrated
migration run established these local baselines:

| Command under `scripts/tests/browser/` | Executed scope |
|---|---|
| `react-pages/run.mjs` | 202 assertions, 28 page/settings/lifecycle scenarios, including private-IP crypto fallback, push/reset, stored-scroll and hash traversal, rapid Back→Forward supersession, and failed-supersession URL/DOM repair |
| `harness-layout/run.mjs` | All 2,058 reports: seven widths, dark/light, normal and short heights |
| `conversation-workflow/run.mjs` | 1,736 assertions across 14 width/theme reports |
| `message-edit/run.mjs` | 1,208 assertions across four width/theme reports |
| `message-regenerate/run.mjs` | 1,748 assertions across four width/theme reports |
| `manager-runtime-controls/run.mjs` | 244 assertions across four real-manager reports |
| `live-stream/run.mjs` | 87 real-worker assertions, including cold history after explicit Close |
| `composer-context/run.mjs` | 1,044 assertions across 16 schedules: native mentions/caret/IME, files/skills, authenticated thumbnails and no-replay checks |
| `manager-execution/run.mjs` | New integrated rerun: 324 assertions across four real-manager Goal/Process reports; the former CSRF setup blocker is cleared, with the documented explicit fake-provider activation setup exception retained |
| `manager-workflows/run.mjs` | New integrated rerun: 432 assertions across four real-manager Queue/Activity/Organization/Versions/trust reports after fixing native document readiness in the fixture (BUG-207) |
| `workspace-actions/run.mjs` | 25 production-module native component assertions: scoped routing, modifier/disabled guards, popup ownership and no replay |
| `workspace-actions/grouped.mjs` | 26 production-module native component assertions: cache/revalidation, late-response rejection, scope/row bounds and read-only lifecycle |
| `workspace-actions/browser.mjs` | 108 exported-production-page assertions across 320/1280 widths and 740/240 heights, dark mode; no failed scenarios |

The standalone `node --test internal/web/message_image_browser.test.mjs` now
loads the generated React module rather than retired `messages.js`. Both tests
pass, including 130 native thumbnail assertions, 1280/320 sizing, exact
credentialed raster reads, one global image queue, scope rejection, timeout and
row/root cancellation, Blob revocation and standalone saved-history remounts.
It now also executes 52 parent-owned ColdWorkspace assertions at 320/1280:
actual production page composition, retained Markdown/tool/copy state, immunity
to standalone-facade disposal, held-read abort and Blob revocation on parent
retirement, exact session-scope rejection, and rejected/committed native
workspace requests. Repeated pagehide/pageshow restoration is driven through the production
listeners with synthetic lifecycle events; this is not proof of actual browser
bfcache eligibility. The fixture supplies bounded public SSR/bootstrap data and
raster responses; it does not run a manager, worker or provider.
The module tested by these image/workspace-action checks and the manager-execution run was
829,557 bytes, SHA-256
`0935eb843ca86955a57964a6b11d923b7690b7f756631ecff8047be80803c7c3`.
This does not retroactively identify earlier table entries with that artifact.

The earlier composer compaction relocation moved only its launcher into the
composer as an accessible icon, with a portal owned by the existing compaction
root. At that stage the dialog remained, and both entry points retained focus
return (BUG-209); the subsequent direct-action change below removes the dialog.
That module was 829,908 bytes, SHA-256
`c343f1cb1a5ef728c519756e4af3b9264cc97cd81bf667d20063c5116e377379`.
Its `harness-layout/run.mjs --runtime-only` matrix passes all 84 reports (seven
widths × dark/light × three heights × supported/unsupported fixtures), including
icon placement, unchanged draft/no mutation on open/cancel, consent and focus.
All 132 package tests, reproducible assets/notices and `go test ./internal/web`
also pass. The earlier full-layout and workflow runs are not reruns of this
newer module.

The direct-action baseline module was **820,981 bytes**, SHA-256
`c867d02e176896f4f0672b083f727be330faa7d960d286c55981e9d322ba8d0e`.
It removes compaction's dialog/checkbox, reasoning's extra review/consent stage,
confirmation for non-activating history actions, and the reversible conversation
archive disclosure. One compaction root still owns the composer icon via a
portal and renders status inline. Checks for that baseline passed:

- `manager-runtime-controls/run.mjs`: **348 assertions / four real-manager
  reports**, including composer/menu single-click compaction, repeated-click
  fencing, Stop, held progress, native no-op, exact scope and direct safe actions.
- `harness-layout/run.mjs --runtime-only`: **84 reports**, all supported/unsupported
  cases across seven widths, dark/light and three heights. The initial test
  assumptions corrected in BUG-210 are not passing evidence.
- `react-pages/run.mjs`: **217 assertions / 30 scenarios**, including one-click
  conversation archive with exact native form fields and retained workspace
  archive/security confirmations.
- All **132** frontend package tests, reproducible assets/notices,
  `go test ./internal/web ./cmd/snow`, and `go vet ./internal/web ./cmd/snow`.

These checks are not a full current-artifact layout, provider, repository/race,
CI or release gate.

The subsequent compaction-flicker fix (BUG-211) preserves the composer footprint
through reservation, native execution, stopping and completion. App projects
compaction as nonqueueable instead of showing ordinary Queue next, and keeps
redundant generic status screen-reader-only. Inline stopping/progress and visible
connection/unknown-outcome warnings remain; the editor and action nodes are not
remounted. Native frame sampling reports zero dock-height, scroll-normalized top
and action-row-height changes across all four runtime reports.

Compaction-flicker baseline module: **821,068 bytes**, SHA-256
`c4d487e835ebcf0e8f635df1f9b6c25963b35a7214970db214a8e4c6b44562f0`.
The changed classic presentation projection `static/app.js` is SHA-256
`526b2e4e91c72466044f63bb11f83108146808992a2eb1abcfe86d98e3182c5a`.
Checks for that baseline pass: runtime controls **352 assertions / four reports**;
manager workflows **432 / four** (ordinary queue delivery/review preserved);
runtime-only layout **84 reports**; **132** frontend tests; TypeScript,
reproducible assets, syntax/diff checks and `go test ./internal/web`.
The earlier image/page/full-layout gates are not reruns of these latest assets.

The earlier composer-launcher relocation module was **821,699 bytes**, SHA-256
`0f5e5bb84afadd7520c740ddaf42efb4af3aabe60da3aacba5b5b33aede195d6`;
`static/app.js` is SHA-256
`af8ed3709537499d20bcf06921bfce57930d12ef420ba6b06e3d6ff5846bf65f`.
That version moved Goal/reasoning dialog launchers into composer hosts alongside
Compact; it did not yet implement the Goal mode or direct Thinking dropdown.
Each retained its React root and external dialog with origin-specific focus.

That artifact's checks passed: `manager-runtime-controls/run.mjs` **352 assertions / four
reports**, including direct composer reasoning and the compaction geometry
regression; `manager-execution/run.mjs` **324 / four**, including native composer
Goal inspection, draft/selection preservation, Start/Stop/Resume and process
controls; runtime-only layout **84 reports**, including direct icon hit testing,
modal ownership and both origin-specific focus returns. All **132** frontend
tests, TypeScript, reproducible assets, JS syntax and `go test ./internal/web`
pass. Initial fixture-only failures and their verified corrections are recorded
in BUG-210 and BUG-212; prior full-layout/page/workflow counts remain historical.

### Composer Goal mode and Thinking picker

The Goal/Thinking redesign generated module was **830,034 bytes**, SHA-256
`c1ca0aeed1d6f17be42e5c92117067038ca501bb43322066942357aff233027f`;
`static/app.js` is SHA-256
`d5ab5410eed4b2e95828cd2e75cb3bc39f429499d67f18eea5c56f7b9f68ae4a`.

Goal now toggles local composition intent and uses the same editor. Explicit Send
performs a fresh, exact-scope preflight; only the inspection's single revision
advance is accepted, with captured facts and the draft unchanged. The confirmed
native receipt must correlate the new objective/budget before clearing that draft.
Existing goal status/Details and exact-target Resume remain separate. Thinking
is a native anchored level popover with immediate capability-derived selection;
response summary/verbosity remain in its secondary dialog. Neither feature adds
an agent loop, transport owner, automatic replay or alternate navigation replacement.

The accepted native matrices pass: `manager-execution/run.mjs` **332 assertions /
four reports**, `manager-runtime-controls/run.mjs` **396 / four**, and
`manager-workflows/run.mjs` **432 / four**. These cover Goal toggle/no-work,
unchanged editor/draft/selection, changed-draft preflight cancellation, exact
Start/Resume receipts, serial goal turns/whole-run Stop, direct Thinking payloads,
mode-specific levels, no-repeat mutations, compaction frame geometry and ordinary
queue/history workflows. Frontend **132/132**, Goal transport contracts **6/6**,
TypeScript, asset/notices reproducibility, JS syntax and uncached
`go test ./internal/web ./internal/plugindocs -count=1` pass. The final runtime-only
layout matrix passes **84 reports**, including supported/unsupported controls,
320–1512px widths, 240/360/740px heights, both themes, exact focus returns and
header/composer hit testing. Wrapped short-screen controls scroll inside the
composer; the ordinary live region does not scroll its header behind the topbar. Plugin guide resources
were synchronized and checked. One earlier intermittent Activity privacy-test
failure remains recorded separately in BUG-218; no Go source was changed.

### No-op compaction chat stability

The subsequent no-op fix produces **830,829 bytes**, SHA-256
`f93cab507508d5064d12b1853c25ca09375c5bdb1bfb9afd37fcc219c3fac5ae`;
`static/app.js` is SHA-256
`ee384e6f983bf24cde27c4394ac4feca15cf5f1a1f55627b7ad2c289b03f2413`.
Compaction feedback is bounded, dismissible and out of flow above the composer;
its wrapped text no longer changes transcript geometry. It reuses the scroll
owner's measured clearance and does not add a dialog or mutation path. The
ordinary chat Working indicator excludes compaction. Native admission, Stop,
receipt correlation, draft preservation and no-replay rules are unchanged.

The expanded native runtime-control matrix passes **404 assertions / four
reports**; the no-op sampler records zero transcript top/height, content-height
and scroll-offset changes, with retained message/editor nodes. Runtime-only
layout passes **84 reports**, including first feedback insertion and reachable
local dismissal on short screens. Frontend **132/132**, build/typecheck,
reproducible assets/notices, JS syntax and `go test ./internal/web` pass.
The test receipt reader now waits for `Network.loadingFinished` before reading
a body; background inventory reads are not attributed to local dismissal.
BUG-211, BUG-212 and BUG-219 retain reproduction and verification evidence.

The full layout baseline precedes the subsequent composer input-order fix;
focused rechecks of a changed area do not retroactively turn that baseline into
a full rerun. Package verification now selects all 132 tests, including the
previously omitted Shell menu ARIA regressions. The controller/source tests and
Go/race checks are separate evidence, not substitutes for native acceptance.

The named image/ColdWorkspace and workspace-action acceptance gaps are now
covered by the production bundle. Workspace routing and grouped inventories use
native component fixtures with public deferred transport; the exported-page
matrix uses real Go templates/assets plus mocked runtime DTOs. These do not
claim committed New/Stop/removal mutations or manager-backed cross-project
navigation. The grouped lifecycle fixture dispatches the real cleanup/remount
contract against empty replacement hosts, not a full navigation request.

Other retained historical entrypoints may still load retired JavaScript files;
their passes are not evidence for the corresponding React owner. Closing these
specific gaps does not establish whole-product parity, reusable CI, release
approval, physical-device acceptance, live-provider compatibility, or adoption
by an already running manager. Rebuild/install and explicitly restart that
manager separately.

The frontend CI job checks types, asset-verification tests, generated-asset and
license reproducibility, and native React pages. Its presence is not evidence
that a particular revision has passed CI. Preserve all existing CI/release gates;
frontend checks supplement rather than replace them. Follow the canonical
[release runbook](releases.md#next-release-runbook), including its reusable CI
gate and manual provider requirements, before publishing.

Release cross-builds consume checked-in assets with Go only. The archive keeps
its existing three files: `snow`, `README.md` and `LICENSE`. Packaging appends
Harness and generated runtime notices to the archive's copy of `LICENSE`,
without changing the repository license or adding a fourth archive file.
