# Distinguish New session from Add workspace in the sidebar

Written against: `db7b2185c86a6506006ff797dce3129e2de385f0`

Status: Implemented and verified.

Verification: `go test ./internal/web` passed. Existing conversation workflow checks passed 1,736 assertions. Layout checks passed 5,932 assertions across 294 desktop reports at 1280px in both themes, and 2,612 assertions across 147 mobile smoke reports. The new icons were visually inspected in the isolated fixture in dark and light themes. Syntax/resource/diff checks passed. No navigation handler or runtime behavior was changed.
The relevant web files are untracked working-tree files at this commit. Inspect the current checkout, not HEAD alone, and preserve unrelated work.

## Evidence chain

- Surface: Snow web manager's desktop collapsed sidebar, shared by home, live conversation and manager routes through `templates/pages.html` → `navigation`.
- Problem: New session and Add workspace both render `icon-plus`. Their labels are hidden or absent in the collapsed rail, leaving two visually identical glyphs for different tasks.
- Design evidence: The user supplied side-by-side Harness and Snow screenshots and selected correcting this comparison. Harness distinguishes creation actions with chat-plus and folder-plus silhouettes; Snow shows two bare plus signs. This plan preserves Snow's own icon language rather than importing Harness branding.
- Owner: `internal/web/templates/pages.html` supplies the two actions; `internal/web/templates/icons.html` supplies SVGs; `internal/web/static/harness.css` resolves `.icon` to 20px and hides `.sidebar-new-session span` in the desktop collapsed branch.
- Runtime: `pages.html` loads `harness.css` after `app.css`. `static/shell.js` toggles `.sidebar-collapsed` at the desktop breakpoint. The sidebar always uses the same navigation template, so changing these two template calls also updates expanded/mobile presentations.
- Scope and affected surfaces: The global New session anchor and sidebar Add workspace anchor, in collapsed/expanded desktop and the mobile drawer.
- Uncertainty: The supplied screenshots are not stored as repository assets. Their decisive content is described above; exact screenshot scaling is not a sizing specification. Validate silhouette legibility at the existing rendered size.

## Design decision

Replace only these two creation-action glyphs with a chat-plus and a folder-plus. Do not change action order, control dimensions, colors, routes or event handling. The result must remain distinguishable without text.

## Reuse

- `icon-chat` and `icon-folder` in `internal/web/templates/icons.html`: existing silhouette geometry, `viewBox="0 0 24 24"`, `currentColor`, 1.6-width rounded strokes and decorative SVG treatment.
- Existing `.icon` sizing in `harness.css`: 20px for sidebar action icons. Retain existing button/anchor hit areas.
- Exemplar: `icon-model`, `icon-attach` and `icon-shield` are already simple named SVG template variants rather than an icon framework.
- Add `icon-chat-plus` and `icon-folder-plus` beside their base variants. The current templates cannot express a plus modifier, and changing the base icons would incorrectly mark ordinary conversations/folders as creation actions. No dependency, JavaScript icon registry or generic modifier API is warranted.

## Changes

1. `internal/web/templates/icons.html`
   - Change: Add `icon-chat-plus` and `icon-folder-plus`. Keep the existing chat/folder outlines and place a compact plus inside each outline, with clear separation from its edges. Use the base variant's 24×24 grid, current color and 1.6 rounded stroke convention.
   - Preserve: All existing icon definitions and their current consumers. Do not globally replace `icon-plus`, `icon-chat` or `icon-folder`.
   - Verify: Each variant reads as its intended object plus a creation mark at 20px, in both themes. Reject overlapping strokes or a silhouette that collapses into a generic plus at this size.
2. `internal/web/templates/pages.html`, `navigation` definition
   - Change: `.sidebar-new-session` uses `icon-chat-plus`; `.sidebar-heading .add-project-link` uses `icon-folder-plus`.
   - Preserve: Existing labels, hrefs, `data-shell-new-session`, disabled/admission states, drawer behavior and HTMX attributes. In particular, New session remains routed through the existing workflow owner when a live session is present; this is not permission to change the newly implemented home/start flow.
   - Verify: Collapsing hides the New session text but leaves the two creation actions visibly different. Expanded/mobile text labels remain unchanged.

## Scope

- Inherit: These two global sidebar actions in every route using `navigation`.
- Verify: Expanded desktop, collapsed desktop, mobile drawer, both themes; live and inactive workspace states.
- Exclude: Home picker Add workspace, per-project New conversation buttons, other plus icons, conversation menus, sidebar order, utility actions, brand/expand controls and general icon restyling. The separate brand plan owns collapsed brand presentation.
- Apply plans serially against the shared checkout; do not overwrite unrelated template changes.

## Validation

- Product: Both actions still invoke their original workflow/navigation; choosing either never silently grants trust, starts a provider turn or submits an existing draft.
- Interface: Inspect the real collapsed rail on desktop and the expanded/mobile variants. Check normal, hovered, focused and disabled states without changing existing state semantics. No rail widening or header movement should result from these glyph substitutions.
- System: Exactly two narrow SVG variants and two consumer substitutions; no new package, icon font or parallel navigation implementation.
- Repository: `go test ./internal/web` → existing template/HTTP tests pass.
- Repository: `node scripts/tests/browser/harness-layout/run.mjs --smoke` → existing layout/interaction smoke passes. This is not a full visual matrix or a pixel-perfect Harness comparison.
- Repository: `node scripts/tests/browser/conversation-workflow/run.mjs` → existing new-conversation workflow behavior remains intact.
- Repository: `git diff --check` → no whitespace errors. Remember tracked-only diffs omit untracked source.
- Reuse existing checks. Add a focused assertion only if current coverage cannot detect a meaningful regression; do not add a screenshot framework or snapshot every SVG path.
- After successful implementation verification, follow `AGENTS.md` and run `./scripts/install-local.sh`. Do not restart user manager/workers, commit or push without authorization.

## Stop conditions

- Stop if either creation action is no longer owned by the cited template, or changing it requires new navigation/workflow behavior.
- Stop if acceptable legibility requires widening the rail, changing global icon sizing or restyling unrelated icons; obtain a separate design decision instead.

## Design documentation

- After acceptance and validation: In the Current composition section of `docs/web-manager-implementation-plan.md`, record that New session and Add workspace use distinct chat-plus/folder-plus sidebar glyphs. Do not claim full Harness parity or performance improvements.
- This is a narrow follow-up to `design-plans/harness-conversation-port.md`, not a replacement for that port plan. That existing plan does not specify these two creation-icon variants.
