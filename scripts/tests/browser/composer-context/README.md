# Composer context browser regression

Offline, loopback-only regression for attachments, `@` project-file mentions and `$`
installed-skill mentions. Tests use fresh **production Go templates and assets**,
not copied composer HTML or an alternate implementation.

```sh
node scripts/tests/browser/composer-context/run.mjs

# Also exercise the complete scenarios at 320×480 and 320×240 in both themes.
SNOW_COMPOSER_NARROW_HEIGHT=1 node scripts/tests/browser/composer-context/run.mjs

# Focus only on decoded draft/sent thumbnails and their lifecycle.
SNOW_COMPOSER_THUMBNAILS_ONLY=1 SNOW_COMPOSER_NARROW_HEIGHT=1 \
  node scripts/tests/browser/composer-context/run.mjs
```

Requires the repository Go toolchain, Node 22+ with built-in WebSocket, and
Chrome/Chromium. Set `SNOW_CHROME_BIN` to an explicit executable if needed. The
runner checks the same fixed executable paths as conversation-workflow; missing
requirements fail explicitly rather than silently skipping. No browser packages,
credentials, application manager, provider requests or internet access are needed.
The thumbnail schedule starts a temporary loopback-only HTTP asset fixture on an
OS-assigned port; it is closed in `finally`, including on browser failures.

## Fixture and matrix

The runner invokes `TestExportHarnessVisualFixtures` once per run and loads both
`workflow.html` and `workflow-queue.html`. The latter is the existing production
fixture with message editing, regeneration and queue capabilities. This covers
both historical **Use as new prompt** and **Edit & resend**, without manufacturing
capability-dependent markup in the test.

Each functional page retains **exactly 100 assertions** at 320×900 and 1280×900, dark and light:
**8 reports / 800 assertions**. Opting into 320×480 and 320×240 adds eight
reports: **16 reports / 1,600 assertions**. Missing assertions, explicit failures, missing
reports and bounded runner timeouts fail the command.

A separate fresh-page visual schedule adds **29 assertions per viewport/theme**:
**4 reports / 116 assertions** by default, or **8 reports / 232 assertions** with
the short-height matrix. The functional assertion count is not diluted or replaced
by these visual checks. They enforce an approximately 98px idle card (90–106px,
with smaller short seats permitted), one action row containing plus and paperclip,
no standalone mention/helper chrome, composer-aligned popup width with viewport
clamping, compact file/icon/chevron rows, short headers, 20- and 14-skill inventories,
<=60px rows, one-line long summaries with complete native titles, 240px viewport
fit, no horizontal overflow, and continued visibility of actionable status/errors.

A third independent schedule adds **32 decoded-thumbnail assertions per
viewport/theme**: **4 reports / 128 assertions** normally, or **8 reports / 256
assertions** with short heights. It does not reduce, replace or renumber any of
the original 100 functional assertions. Combined schedules therefore expect
**16 reports / 1,044 assertions** normally and **32 reports / 2,088 assertions**
with short heights. Thumbnail-only runs still enforce all 32 checks per report.

The 132-byte `fixture.png` is a valid 48×32 RGB checkerboard, also encoded exactly
in `helpers.js` for native `File` creation and payload assertions. The original
1×1 image fixture was not reliably decodable. Both draft and sent checks now
require the browser's real `HTMLImageElement.decode()`, `naturalWidth` and
`naturalHeight`, positive rendered rectangles, and the production renderer's
loaded state. No synthetic image load/error events, renderer URL bypasses,
inline styling, or manufactured image markup are used.

Thumbnail pages use the same exported production template/assets and shared
public DTO recorder, served by `thumbnail-fixture.mjs`. Its exact scoped image
route requires a fixture-only HttpOnly/SameSite cookie, serves the actual PNG
with `no-store`/`nosniff`, and rejects an unauthenticated startup probe with 401.
The runner independently verifies real authenticated HTTP image requests for
every thumbnail report and records those requests in the measurements JSON.
This is a browser image-loading/auth-shaped transport fixture, **not a test of
the real manager's pairing or image-authorization implementation**. Live sent
messages are injected only as server-shaped `{index, mime_type, url}` DTOs,
never as blob URLs or raw image data. Remount coverage uses a fresh live public
snapshot; it is not evidence of actual saved-history HTTP authorization.

Checks cover 28×28 draft image rectangles, 11px filenames, 10px/14px disclosure,
full privacy/vision title, 6px attached-composer gap, and non-collapsed disclosure
at short heights. They also exercise actual decoder failure with a truncated
PNG, local-only selection, remove/dispose/remount/session-switch/accept URL
revocation, retained pending-send previews, metadata-only sent messages,
text-preserving sent thumbnails, text-only Edit/Reuse restrictions, forbidden
sent blob URL fallback, no spontaneous replay and no persistent draft storage.

The CDP runner saves deterministic viewport PNGs and measured geometry outside its
temporary profile in **`dist/compact-composer/`**. Every visual viewport/theme has:

- `empty-WIDTHxHEIGHT-THEME.png`
- `files-WIDTHxHEIGHT-THEME.png` (`@` listing, no file bodies read)
- `skills-long-WIDTHxHEIGHT-THEME.png` (`$` listing with 20 paragraph-length summaries)
- `measurements-WIDTHxHEIGHT-THEME.json` (assertions, card/actions/popup geometry)
- `draft-image-WIDTHxHEIGHT-THEME.png` (decoded compact draft image and disclosure)
- `sent-image-WIDTHxHEIGHT-THEME.png` (decoded same-origin HTTP sent image)
- `thumbnail-measurements-WIDTHxHEIGHT-THEME.json` (32 assertions, image/text/notice
  geometry, natural image dimensions, and authenticated image request evidence)

This produces 20 PNGs by default, or 40 with short-height checks, including
1280×900 and 320×900 in dark/light. Screenshots wait for fonts, reduced motion,
and two animation frames; public fixture data and viewport scale are fixed.
No pixel hashes are enforced across platform/font rasterizers. Visual failures
retain evidence and the runner continues the matrix before exiting nonzero.
The temporary exported assets and isolated Chrome profile are still removed on
completion or failure; screenshots and JSON remain in `dist/compact-composer/`.

The suite directly loads `../conversation-workflow/fixture.js`, reusing its
production-shaped snapshot DTOs, in-memory fetch recorder, accelerated poll timer
and HTMX before/after-swap shim. Only these public transports are mocked. Local
files use real browser `File`, `File.arrayBuffer`, `DataTransfer`, clipboard/drop
events and native textarea selection. A narrowly scoped `File.arrayBuffer`
interceptor holds selected reads to exercise races and is restored in `finally`.
The browser's operating-system file chooser is not automated; tests assign the
native input's `FileList` and dispatch its change event. Inspection/skills/prompt
POSTs remain explicitly controlled; this suite does not replace server validation
or permission/RPC tests.

## Coverage

- Passive page load never discovers skills, inspects files, activates a worker or
  uploads. Native multiple-file input, add/remove, image paste and file drop are
  local until explicit Send. Ordinary clipboard text retains native behavior.
- PNG, JPEG, GIF, WebP and UTF-8 text admission; PDF, unsupported binary, invalid
  UTF-8 and oversized files produce explicit feedback. Failed reads remain failed
  chips and cannot be silently omitted from Send. Eight-attachment admission,
  ninth-file rejection, aggregate 2MiB image budget and aggregate 64KiB UTF-8
  text/label budget are exercised.
- Exact project URL, four URL-encoded fields, CSRF/instance binding and byte-exact
  ordered protocol attachment blocks. The separate `text` field is not duplicated
  in `content`. Pending admission disables duplicate Send; acceptance clears only
  captured context and unchanged text. Text-only and selected-skill sends preserve
  the legacy `/runtime/prompt` route.
- Attachments disable historical Edit/Reuse and supported Queue next; Ctrl+Enter
  cannot bypass queue restrictions. Capability-disabled legacy pages do not gain
  queue controls.
- The plus menu dynamically exposes file/skill actions without performing discovery;
  helpers open it before locating those rows. Folder selection uses the semantic
  folder icon and exact name, not the former slash decoration.
- Caret-local `@` parsing excludes email-like embedded tokens; bounded explicit
  `inspect/files` POSTs use path/offset, local name filtering does not re-fetch,
  folder choice drills down, and only file choice authorizes `inspect/file`.
  The selected full file is captured and the quoted `@"path" ` token replaces only
  the caret token. Truncated previews cannot be attached or silently sent.
- `$` discovery uses the exact runtime/CSRF/instance identity. Local skill-name
  filtering, disabled reasons, disabled-click handling, unavailable-runtime
  feedback, explicit identity-bound Retry without automatic request loops,
  active-descendant keyboard selection, exact `$name ` insertion,
  Escape, native Home/End and IME navigation/Enter are covered. Single matches
  never auto-insert/read/execute; skill insertion activates nothing until Send.
- Removed, disposed and changed-query file-read races; session/project draft
  independence; retained interrupted-read errors; newer text during admission;
  disposed-composer acknowledgements; abandoned listings; changed caret before
  selection; stale skill responses and foreign-instance inventories.
- Failed session switches retain completed attachments and exact text. Explicit
  outcome review preserves the old instance and rebinds working file/skill
  callbacks after controller rotation. Retained queue review never deadlocks
  local attachment removal or converts removal into a server mutation.
- Accepted attachments never reappear/replay after workspace reload. Lost
  admission responses keep exact text/context behind the existing uncertainty
  guard and cannot trigger an automatic retry.
- File popups remain within the viewport with a minimum 40px choice area, even
  at 240px height. Composer and context controls remain within viewport bounds, no horizontal
  overflow, no browser errors/unhandled rejections, and no persistent storage
  writes for prompts, attachments or context drafts.

## Maintenance

All suite-specific files live in this directory. Keep transport reuse confined to
the public conversation-workflow fixture. Do not copy or modify product code to
make this suite pass. Update exact assertion counts deliberately when adding or
removing checks. Syntax-only validation is available without Chrome:

```sh
node --check scripts/tests/browser/composer-context/run.mjs
for file in scripts/tests/browser/composer-context/*.js; do
  node --check "$file" || exit
done
```

## Verification status

The integrated expanded short-height run passes **2,088 assertions / 32 reports**:

- **1,600 functional assertions / 16 reports**, still exactly 100 per page.
- **232 compact-layout assertions / 8 reports**.
- **256 decoded-thumbnail assertions / 8 reports**.

An independent thumbnail-only short-height run also passes **256 assertions /
8 reports**. Both commands completed successfully with zero failed checks:

```sh
GOMAXPROCS=2 GOFLAGS='-p=2' \
  SNOW_COMPOSER_THUMBNAILS_ONLY=1 SNOW_COMPOSER_NARROW_HEIGHT=1 \
  node scripts/tests/browser/composer-context/run.mjs

GOMAXPROCS=2 GOFLAGS='-p=2' SNOW_COMPOSER_NARROW_HEIGHT=1 \
  node scripts/tests/browser/composer-context/run.mjs
```

Logs are retained at `dist/thumbnail-short-matrix.log` and
`dist/thumbnail-full-context.log`. All four viewport sizes (320×900, 1280×900,
320×480, 320×240) ran in both dark and light themes. `dist/compact-composer/`
contains **40 screenshots**, including 16 draft/sent image screenshots, plus
8 thumbnail measurement reports and 8 compact-layout measurement reports.

Fresh measurements confirm **28×28 draft previews**, **11px filename labels**,
and consistent **10px disclosure text / 14px line height**. The disclosure's
actual rendered height is **30px** at 320×900 and 320×480, **16px** at 1280×900,
and **14px** at 320×240, in both themes: the minimum readable line no longer
collapses. Sent images decode the 48×32 fixture into bounded **72×72** narrow
and **96×96** desktop thumbnails. Each thumbnail report independently records
an authenticated same-origin HTTP image request.

The earlier rejected-blob fallback and collapsed short-height disclosure
failures are **verified resolved by these unchanged regressions**. Invalid sent
blob metadata retains its visible unavailable fallback after enhancement; no
checks were weakened. Earlier failing runs and an intervening outer-command
timeout are not counted as passes. Current screenshots/measurements represent
the successful integrated rerun, not preserved earlier failure artifacts.

The expanded schedule has a bounded 480-second browser budget, plus a bounded
120-second Go fixture export. These are mocked browser integration checks,
not a real-provider upload, saved-history HTTP authorization, or real-manager
authorization smoke test.
