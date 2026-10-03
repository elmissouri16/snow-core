# Remove empty height from the mobile conversation header

Written against: `7020c6a593895acca064e933ca9bd82e13db2231`

Status: Implemented and verified for the mobile-header scope. The complete conversation workflow passes 1,750 assertions. The targeted mobile runtime-layout subset passes 3,714 assertions across 36 width/theme/height reports. The broader layout smoke's header checks pass, while unrelated model/session fixture and reader-anchor failures remain tracked as reopened BUG-176 and BUG-196.

## Evidence chain

- Surface: authenticated Snow Web Manager → activated conversation at mobile widths (`templates/pages.html` → `/static/generated/app.js` → `frontend/src/conversation/Surfaces.tsx`, `Heading`).
- Problem: the user-provided mobile screenshot shows a single row containing the conversation title, runtime status, conversation menu, Close runtime and Files/changes controls at the top of a much taller header. The unused lower portion delays the transcript and consumes scarce mobile viewport height.
- Design evidence: `internal/web/static/harness.css` already assigns mobile `.workspace-heading` a 40px height with 4px × 16px padding. Its later mobile `.live-header` rule attempts a 30px minimum and compact 3px × 16px padding. Neither can override the more specific desktop `.workspace-heading.live-header` rule, which fixes both `height` and `min-height` at 76px. The rendered screenshot confirms that the desktop height wins on the mobile surface. `docs/web-manager-implementation-plan.md` and both browser suites currently describe/assert 76px at every width, so those contracts must be deliberately revised rather than bypassed.
- Owner: `internal/web/frontend/src/conversation/Surfaces.tsx` owns the header composition; `internal/web/static/harness.css` owns its responsive geometry; `scripts/tests/browser/{conversation-workflow,harness-layout}` own rendered geometry checks. `internal/web/templates/pages.html` proves that `harness.css` is loaded after `app.css` and that the generated React bundle reaches the page.
- Scope and affected surfaces: the live activated-conversation header at widths up to 767px, including idle, running and attention states in both themes. Desktop live headers and non-conversation workspace headers retain their existing geometry.
- Uncertainty: the screenshot is conversation evidence, not a repository asset or a pixel-scale specification. At 320px, long titles or increased text size may require a second row; the correction must remove unconditional empty height without clipping, overlapping, hiding or shrinking controls.

## Design decision

Keep the 76px live header on desktop. At the existing mobile breakpoint, make the exact `.workspace-heading.live-header` owner content-sized with the existing 40px mobile workspace-header floor, 4px × 16px padding and vertically centered contents. Retain wrapping so the header grows only when its title/status and controls genuinely need another row. This removes the unconditional desktop-height gap shown in the screenshot while preserving all current controls and the repository’s existing mobile spacing values.

## Reuse

- `internal/web/static/harness.css` mobile `.workspace-heading`: reuse its 40px floor and `4px 16px` padding.
- `internal/web/static/harness.css` `.workspace-heading.live-header`: retain its border, gap and desktop 76px geometry outside the mobile media query.
- `internal/web/static/harness.css` `.conversation-title`, `.live-controls`, `.icon-button` and `internal/web/static/menus.css` `.task-menu-trigger`: retain current title truncation, 28–32px controls, hit areas and spacing.
- Exemplar: `internal/web/static/scroll.css` already uses the exact `#live-session .live-header` owner for a short-height override instead of relying on a lower-specificity generic rule.
- No new component, token, breakpoint or responsive JavaScript is required.

## Changes

1. `internal/web/static/harness.css`
   - Change: replace the ineffective mobile `.live-header` geometry override with an exact `.workspace-heading.live-header` rule at `max-width: 767px`. Set `height: auto`, `min-height: 40px`, `padding: 4px 16px`, `align-items: center`, and retain `flex-wrap: wrap`.
   - Preserve: the desktop `.workspace-heading.live-header` 76px height; header border and stacking; title ellipsis; status text; conversation-actions, Close runtime and Files/changes controls; current control sizes; responsive breakpoint; and all runtime behavior.
   - Verify: at 360px and 390px with ordinary one-line content, the live header resolves to 40px and contains no blank second-row area. At 320px or with a long title, it remains at 40px when content fits and expands only if a second row is actually required. Nothing overlaps the transcript or mobile top bar.
2. `scripts/tests/browser/conversation-workflow/tests.js`
   - Change: replace the assertion that the live header is 76px at every supported width with an explicit responsive contract: 76px above the mobile breakpoint and 40px for the suite’s ordinary single-row mobile fixture. Add geometry checks that the title/status and each visible header control remain within the header bounds.
   - Preserve: the assertion that the live runtime owns exactly one workspace header and that Stop remains in the composer rather than moving into the header.
   - Verify: the existing seven-width conversation workflow catches a return to the unconditional mobile 76px height, as well as clipped or escaped controls.
3. `scripts/tests/browser/harness-layout/tests.js`
   - Change: update the live-header measurement from a universal 76px expectation to the same desktop/mobile contract using the existing `desktop` width classification. For mobile reports, also assert that an expanded height is justified by actual row wrapping rather than an empty fixed block.
   - Preserve: fixture measurement capture, header ownership, Files/changes and Close runtime checks, composer ownership and every non-header layout assertion.
   - Verify: chat, attention and other live fixtures pass at 320px, 360px and 390px in light and dark themes, including normal and short heights.
4. `docs/web-manager-implementation-plan.md`
   - Change: after implementation and visual acceptance, revise the Current composition statement that promises one 76px session header. Record a 76px desktop header and a content-sized mobile live header with a 40px single-row floor that grows only for real wrapping.
   - Preserve: the single-header architecture and the documented ownership of Close and Files/changes controls.
   - Verify: documentation no longer claims the superseded all-width 76px behavior.

## Scope

- Inherit: every activated conversation that renders `Heading` with both `workspace-heading` and `live-header`, regardless of runtime status or theme.
- Verify: 320px, 360px and 390px widths; dark and light themes; normal and short viewport heights; default and long conversation titles; Ready/running/permission states; open conversation menu; and inspector toggle availability.
- Exclude: the 48px mobile top bar, sidebar drawer, home/cold-start headings, desktop conversation header height, transcript/message spacing, composer position, action labels/icons, runtime admission and any attempt to merge or remove header controls.
- Preserve the existing uncommitted startup-output work in the shared checkout; this plan does not authorize modifying or reverting it.

## Validation

- Product: open an activated conversation on a phone-sized viewport. The transcript begins immediately after a compact one-row header; conversation actions, Close runtime and Files/changes still invoke their existing owners, and no agent/runtime behavior changes.
- Interface: inspect the real production fixture at 320px, 360px and 390px in both themes. Confirm ordinary content produces a 40px live header; any taller result corresponds to visible wrapping. Check title truncation, status legibility, control hit areas, focus rings, open menus, inspector opening/closing and short-height behavior.
- System: confirm the correction lives in the existing `harness.css` responsive owner with no inline styles, JavaScript measurements, duplicated header component or new breakpoint. Confirm `app.css` and unrelated workspace headings are unchanged.
- Repository: `go test ./internal/web` → production templates and web behavior remain valid.
- Repository: `node scripts/tests/browser/harness-layout/run.mjs --width 320 --theme dark` and `node scripts/tests/browser/harness-layout/run.mjs --width 390 --theme light` → narrow chat/live fixtures retain visible, bounded controls and compact header geometry.
- Repository: `node scripts/tests/browser/harness-layout/run.mjs --smoke` → broader responsive layout smoke passes.
- Repository: `node scripts/tests/browser/conversation-workflow/run.mjs` → responsive header ownership/geometry and conversation behavior pass across the complete workflow matrix.
- Repository: `git diff --check` → no whitespace errors.
- After successful implementation verification, follow `AGENTS.md` and run `./scripts/install-local.sh`; restart the foreground manager to load the updated CSS/assets.

## Stop conditions

- Stop if the visible mobile header is not the `Heading` composition and CSS ownership traced above.
- Stop if 40px cannot contain the normal one-row fixture without reducing existing hit areas, hiding controls or changing labels; preserve control usability and use content-driven wrapping rather than squeezing.
- Stop if fixing the gap requires runtime state changes, responsive JavaScript, a new breakpoint or changes to the desktop 76px header; obtain a separate design decision for that wider scope.

## Design documentation

- After acceptance and validation: update the Current composition section of `docs/web-manager-implementation-plan.md` to state that the single live session header is 76px on desktop and content-sized on mobile with a 40px single-row floor. Do not claim a universal 40px height, because long content may wrap.
