# Keep Snow's identity visible in the collapsed rail

Written against: `db7b2185c86a6506006ff797dce3129e2de385f0`

Status: Implemented and verified, including the scoped short-height overflow correction documented below.

Verification: `go test ./internal/web` passed. Existing layout checks passed 5,932 assertions across 294 desktop reports at 1280px in both themes, including focused brand/nonoverlap and 240px rail checks; mobile smoke passed 2,612 assertions across 147 reports. Conversation workflow checks passed 1,736 assertions. Isolated browser inspection covered the rail in dark/light themes and measured Settings reachability at 1280×240. Syntax/resource/diff checks passed. No shell event handling, runtime behavior or user processes were changed.
The relevant web files are untracked working-tree files at this commit. Inspect the current checkout, not HEAD alone, and preserve unrelated work.

## Evidence chain

- Surface: The web manager's shared desktop collapsed sidebar, reached through `internal/web/templates/pages.html` → `navigation` → `brand`.
- Problem: Collapsing hides the entire brand anchor, including the Snowflake. Only the panel/expand button survives in the top brand row. The supplied Snow screenshot therefore starts with a generic panel icon, while the user's Harness reference retains its recognizable product mark.
- Design evidence: The user selected retaining the Snowflake in the collapsed rail after comparing these screenshots. `docs/web-manager-implementation-plan.md`, Current composition, also records preserving Snow's own name and Snowflake rather than copying provider branding. The new collapsed-state visibility requirement comes from this selected improvement, not a claim that the old document specified every brand state.
- Owner: `internal/web/static/harness.css`, desktop collapsed branch. Its combined `display: none` selector includes `.sidebar-collapsed .sidebar-brand-row .brand`.
- Runtime: `pages.html` loads `harness.css` after `app.css`; the `brand` template contains `.brand-mark` with `icon-snow`, `.brand-name` and `.brand-label`. `static/shell.js` applies `.sidebar-collapsed` and updates the existing expand/collapse button's label/state.
- Scope and affected surfaces: The shared collapsed rail at desktop widths, including home, live conversations and manager pages. Expanded desktop and the mobile drawer are preservation checks, not redesign targets.
- Uncertainty: The source proves why the mark disappears. The supplied comparison is not a pixel-scale specification; the executor must visually verify the selected two-control arrangement with the existing metrics.

## Design decision

Keep the existing home-linked Snowflake visible above the separate expand button. Hide only the wordmark and WORKSPACE badge in the collapsed brand anchor. Do not turn the logo into a sidebar toggle, replace it with Harness branding, or introduce a hover-only control.

Use a vertical brand-row arrangement only when collapsed. Keep the existing 27px Snowflake, 28px expand control and 8px row gap; allow the row to grow from its existing 60px minimum rather than squeeze these items into 60px. Those existing metrics require approximately 63px of content height, only 3px more than the present brand row. Both controls remain centered within the existing 56px rail with 6px side padding.

Implementation verification refinement: the existing toggle is 28px wide with a 28px minimum height, but its 20px SVG plus vertical padding renders at 30px high. The preserved controls therefore produce a measured 65px brand row, not the estimated 63px. At 1280×240, verification also reproduced pre-existing Settings clipping (Y=282 with reconstructed old CSS, Y=287 with the visible mark). A scoped vertical scroll on the collapsed rail fixes that without a new control arrangement or smaller targets; focused Settings is now fully inside the viewport at Y=198–234. See BUG-179. The rail uses no visible scrollbar to preserve its 56px composition; native scrolling and focus scrolling remain available.

This explicitly retains two separate functions: the Snowflake goes home; the panel button expands the sidebar. It avoids relocating controls into every route's header, duplicating controls, changing event delegation or widening the rail.

## Reuse

- `pages.html` → `brand`: reuse the existing home anchor, `.brand-mark`, wordmark and badge. Do not add a second logo.
- `icons.html` → `icon-snow`: reuse the existing Snowflake unchanged.
- `harness.css`: reuse the existing 56px collapsed rail, 6px side padding, 27px `.snow-icon`, 28px `.sidebar-collapse`, 60px brand-row height baseline and 8px brand-row gap.
- `shell.js` → `syncCollapse()` and the `[data-sidebar-collapse]` click branch: retain state, labeling, focus and the desktop/mobile boundary.
- Exemplar: The expanded brand row already contains the logo home link and a separate toggle with distinct behavior. The selected collapsed arrangement keeps that separation instead of inventing an overloaded logo button.
- No new primitive, script, token family or dependency is required.

## Changes

1. `internal/web/static/harness.css`, inside `@media (min-width: 768px)`
   - Change: Remove the entire `.brand` anchor from the collapsed hidden-selector list. Replace that selector with collapsed-brand-scoped `.brand-name` and `.brand-label` selectors; leave the other collapsed hidden consumers unchanged.
   - Change: Make `.sidebar-collapsed .sidebar-brand-row` a vertically stacked, centered flex container with `height: auto`, `min-height: 60px`, existing `gap: 8px` and no extra padding. Keep its nonshrinking ownership and the existing collapsed toggle margin reset.
   - Change: Ensure the collapsed brand anchor centers its mark without expanded wordmark spacing affecting alignment. Keep the anchor and expand button as distinct, nonoverlapping controls.
   - Preserve: 56px rail width; 6px side padding; existing SVG dimensions, colors, button hit area, focus indication and pointer behavior. Do not globally alter `.brand`, `.brand-name`, `.brand-label`, `.snow-icon` or `.sidebar-collapse`.
   - Verify: Snowflake and expand control remain visible together. New session and subsequent controls stay in their existing order, with only the small vertical accommodation required by the brand row.
2. `internal/web/templates/pages.html`, `internal/web/static/shell.js`
   - No implementation change expected. These are trace/verification owners, not permission to refactor markup or event handling.
   - Verify: Snowflake follows its existing home link. Expand/collapse still works repeatedly; `syncCollapse()` updates the state and the existing click branch's focus return targets a visible `.sidebar-collapse`.
3. `scripts/tests/browser/harness-layout/tests.js`
   - Reuse the existing desktop collapse/recollapse scenario near the 56px rail assertions.
   - If current assertions cannot detect loss or overlap of the brand, extend that scenario with a focused visibility/nonoverlap check for `.sidebar-brand-row .brand-mark` and `.sidebar-collapse` while collapsed, plus hidden wordmark/badge checks.
   - Preserve all existing navigation, search, drawer and workflow assertions. Do not add a separate harness or weaken the 56px rail assertion.

## Scope

- Inherit: Every desktop route using the common collapsed navigation template.
- Verify: Home, live conversation, inactive workspace and a management route; expanded desktop and mobile drawer; both themes; normal and short heights.
- Exclude: Sidebar creation-action icons (separate plan), utility actions, header geometry/content, tab navigation, persisted collapse preferences, logo redesign, home drafting, activation and runtime controls.
- Execute alongside the action-icon plan serially; respect the shared checkout and unrelated edits.

## Validation

- Product: Logo goes home using the existing navigation path; expand button only changes sidebar presentation. Neither action activates a worker, sends a prompt or changes trust/permissions.
- Interface: At collapsed desktop widths, verify the Snowflake is the first visual item and the separate expand button is present below it. Their hit areas do not overlap. Neither the name nor WORKSPACE badge leaks into the narrow rail. Test repeated collapse/expand, workspace navigation, and Search's existing expand/recollapse path.
- Interface: Verify that the small row-height increase does not clip bottom utilities in short windows. Expanded desktop and mobile keep their existing horizontal brand-row arrangement. Preserve focus indication; the supplied blue ring is not evidence that keyboard focus styling should be removed.
- System: Retain the same brand template, Snowflake SVG, toggle and event owner. No second header toggle, hover-only replacement logo, extra route-specific CSS or new library.
- Repository: `go test ./internal/web` → template/HTTP coverage remains passing.
- Repository: `node scripts/tests/browser/harness-layout/run.mjs --smoke` → existing geometry/interaction smoke and any narrowly extended brand assertion pass. Smoke alone does not establish complete visual equivalence.
- Repository: `git diff --check` → no whitespace errors. Inspect untracked sources explicitly; HEAD-only comparisons are insufficient.
- Use the existing browser fixtures to visually inspect the changed rail. Do not inspect or restart the user's live manager as a shortcut to fixture validation.
- After successful implementation verification, follow `AGENTS.md` and run `./scripts/install-local.sh`. Do not restart user manager/workers, commit or push without authorization.

## Stop conditions

- Stop if a later stylesheet or changed navigation owner invalidates the traced selectors.
- Stop if the selected arrangement requires shrinking existing controls, hiding expand, widening the rail or moving controls into route headers.
- Stop if rendered validation shows this arrangement crowds the rail or clips utilities. Report the failing viewport and request a revised arrangement rather than silently inventing new dimensions or removing features.

## Design documentation

- After acceptance and validation: Update the Current composition section of `docs/web-manager-implementation-plan.md` to state that collapsed desktop navigation retains the home-linked Snowflake above its separate expand control, while hiding the wordmark and WORKSPACE badge. Record any verified geometry change accurately; do not claim exact Harness parity.
- This is a narrow follow-up to `design-plans/harness-conversation-port.md`. That plan preserves Snow branding broadly but does not specify this collapsed brand arrangement; do not rewrite its unrelated conversation work.
