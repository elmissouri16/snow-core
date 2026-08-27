# Make model selection a clickable centered popup

Written against: `69f0e40190a07e23951005af8ab887dfc43d79cc`

## Design language

- Audited surface: Snow's interactive alternate-screen TUI, specifically the sticky provider/model header and the model-selection flow opened today by `/model` or the Model row in `/settings`.
- Design sources: the user-provided screenshot and explicit choices in this thread (click the header model, use a centered card, and type to filter immediately); `docs/using-snow.md` (single renderer-owned TUI frame, sticky provider/model header, picker controls, modal precedence); `internal/tui/theme.go` (semantic styles); `internal/tui/process_fleet_view.go` and `internal/tui/transcript_selection_ops.go` (rounded modal border and ANSI-aware popup composition exemplars).
- Documented decisions: Snow owns one fixed full-window Bubble Tea frame; the header continuously exposes the active provider/model; blocking permission and user-input requests preempt ordinary pickers; model discovery starts from cached catalogs and refreshes asynchronously; model changes and any required thinking effort are persisted together.
- Governing owners and consumers: `internal/tui/view.go`, `internal/tui/model_picker.go`, `internal/tui/transcript_layout.go`, `internal/tui/reducer.go`, and `internal/tui/stream_render.go`; `/model`, `/settings` Model, the sticky header, full-screen mode, and inline-renderer tests consume the same picker state.
- Explicit exceptions: None documented.

## Evidence chain

- Surface: interactive TUI → sticky header `provider/model` label → current `/model` picker → optional model-specific thinking picker.
- Problem: in the supplied rendered state, a 46-model catalog is presented as a narrow lower-left overlay that competes with the composer and exposes only a short list; its copy says `press / to search`, and opening it also requires entering `/model` or navigating through `/settings`. `internal/tui/model_picker.go` enforces a 4–10-model window in the normal renderer and only two models in the inline modal branch. The selected target is a larger centered popup opened from the model shown in the header, with search ready immediately.
- Design evidence: the user's selected interaction is **Click header model**, **Centered card**, and **Type to filter immediately**. `docs/using-snow.md` establishes the header as the persistent provider/model owner and the TUI as one renderer-owned frame. `styleCompletionSelected`, `styleHeader`, `styleHeaderDim`, `styleFooter`, `styleSep`, `colorAccent`, and the existing rounded borders already express Snow's active, muted, supporting, separator, and modal presentation.
- Owner: model state and selection behavior remain in `internal/tui/model_picker.go`; frame composition and the header remain in `internal/tui/view.go`; pointer routing remains in the existing Bubble Tea mouse path.
- Scope and affected surfaces: header rendering/hit testing, pointer routing, model modal layout, direct search, asynchronous catalog refresh while the modal is open, the nested thinking-effort step, TUI tests, user documentation, implementation notes, and changelog.
- Uncertainty: none in the requested interaction. Header clicking necessarily depends on application mouse mode (`tui.mouse: true`); `/model` and `/settings` remain the fallback when F6 switches to native terminal mouse mode.

## Design decision

Turn the visible provider/model segment in the sticky header into the model selector: while app-owned mouse mode is enabled, render that segment with Snow's existing accent-selected style plus a `▾`, and open the existing model-selection state on a left-button press inside the exact rendered cell bounds. In F6/native mouse mode, retain the current dim label without the chevron because Snow cannot receive the click. Do not add a keyboard shortcut; the user selected header click.

Render model selection as a true centered card over the unchanged renderer-owned frame rather than as another row block between transcript and composer. The card's outer size is at most 80 columns by 24 rows and never exceeds the managed frame minus the existing two-cell outer gutter on each side. Center it horizontally and vertically. On smaller frames, preserve—in order—the title, search row, at least one selected model row, and controls; remove selected-model description rows before clipping those required regions.

Keep the card at a stable size while selection and query change. Use a rounded `colorAccent` border, fixed title/search/list/detail/control regions, provider-group headings, the current-model marker, and the existing selected/muted styles. Size the list from its rendered row budget—including provider headings and scroll markers—instead of treating every model as one row.

Search is focused as soon as the card opens. Rune input filters the existing provider/ID/display-name/description haystack; Backspace removes one rune; Ctrl+U clears; arrows, Tab/Shift+Tab, PageUp/PageDown, and Home/End navigate. Because ordinary runes are search input, `j` and `k` no longer navigate this picker while it is open. Esc clears a non-empty query and restores the active model selection; Esc with an empty query closes. Enter preserves the current apply/persist behavior.

When the chosen model requires a thinking-effort choice, keep that second step in the same centered card shell. Esc returns to the filtered model list without losing its query or selection; Enter applies and persists the model and effort atomically through the existing functions. Standalone `/thinking` remains unchanged.

Opening by header click is idle-only, matching the safety of model selection through settings. A click during an active turn is consumed and reports `model: wait for the current turn to finish`; it must not mutate the running provider/model. Blocking permission and model-requested-input overlays retain visual and input precedence.

## Reuse

- `startModelPick`, `filteredModels`, `uniquePickerModels`, `movePicker`, `applyModel`, and `applyModelAndThinking` in `internal/tui/model_picker.go` / `internal/tui/auth.go`.
- `styleCompletionSelected`, `styleCompletion`, `styleHeader`, `styleHeaderDim`, `styleFooter`, `styleSep`, and `colorAccent` from `internal/tui/theme.go`.
- Rounded accent-border exemplar: `renderProcessFleetModal` in `internal/tui/process_fleet_view.go`.
- ANSI-aware frame layering exemplar: `overlayTranscriptSelectionContextMenu` in `internal/tui/transcript_selection_ops.go`.
- Modal-precedence exemplar: `renderOverlays` and the permission/user-input checks in `internal/tui/view.go` and `internal/tui/transcript_layout.go`.

A small generic frame-block compositor is required because the only existing true overlay routine is hard-wired to the transcript context menu. Extract its ANSI-width-aware cut/pad/reset loop into a package-local helper consumed by both the context menu and model card; do not duplicate that algorithm or create a general component framework.

## Changes

1. `internal/tui/view.go`
   - Change: split header composition into reusable rendered parts so the provider/model selector and its exact start/end cells come from the same truncation/layout calculation. In app mouse mode render `provider/model ▾` with `styleCompletionSelected`; in F6/native mode render the existing dim `provider/model` label without a hit target. Leave thinking, mode, goal, path, status, and thinking-flash behavior under their current responsive rules.
   - Change: build the ordinary full frame without model-picker rows, then place the model card over it at the calculated centered coordinates. Apply the same frame-sized result in normal and inline renderer branches. Set header status to `models` while this modal is visible.
   - Preserve: one terminal-height frame, sticky chrome, current context-menu ordering, and full-frame fleet ownership.
   - Verify: clicking anywhere outside the actually rendered provider/model segment does not open the picker, including when the header truncates on narrow terminals.

2. `internal/tui/model_picker.go` and new `internal/tui/model_picker_view.go`
   - Change: keep input/state transitions in `model_picker.go`; put the local responsive layout calculation and card renderer in a cohesive `model_picker_view.go` so the existing 733-line mixed picker file remains comfortably below the 1,000-line limit. Calculate the 80×24 maximum card, fixed regions, and rendered-row list budget. Render a stable rounded card with title/count/loading state, always-visible search row and match count, grouped model list, bounded selected-model context/thinking/description details, and controls.
   - Change: make search active on open and implement the direct-typing, navigation, Ctrl+U, Backspace, Enter, and two-stage Esc contract above. Update help copy to `type to filter · ↑/↓ navigate · PgUp/PgDn · Enter apply · Esc clear/close`; do not advertise `j/k` for this picker.
   - Change: render the model-triggered thinking-effort choice inside the same card shell while preserving the existing standalone thinking picker.
   - Preserve: deduplication, cached-first opening, provider grouping, current-model preselection, no-match state, privacy/training descriptions, model persistence, and model/effort rollback behavior.
   - Verify: long IDs and descriptions are width-bounded, provider headings and scroll markers count against the list region, and changing selection cannot resize or move the card.

3. `internal/tui/stream_render.go` and `internal/tui/reducer.go`
   - Change: route a left-button press on header row 0 through the shared header hit bounds before transcript selection/viewport handling. Require app-owned mouse mode, an initialized app, idle runtime, and no active/preempting modal before calling `startModelPick`.
   - Change: while the model card or its nested thinking step owns the frame, consume pointer events so clicks and wheel events cannot select or scroll the transcript behind it. Row clicking and outside-click dismissal are explicitly not part of this change.
   - Change: when `modelListMsg` refreshes catalogs, retain `modelQuery` and preserve selection by stable `provider + model ID`; if that model disappears from the filtered result, select the active model when present, otherwise the first match. Do not clear a query the user typed while refresh was in flight.
   - Preserve: stale generation rejection, cached-list interactivity, partial-provider-error status, and close-on-empty-catalog behavior.
   - Verify: a query entered before the async result remains visible and filters the refreshed list.

4. `internal/tui/transcript_layout.go` and the overlay section of `internal/tui/view.go`
   - Change: remove `pickModel` and the model-origin `pickThinking && thinkingReturnToModel` state from the generic bottom overlay and `inlineModalOverlay` height accounting so the centered flow never shrinks the transcript or replaces the composer tail. Keep keyboard dispatch to `handleModelPick` / `handleThinkingPick` at the existing modal priority.
   - Change: retain permission and user-input preemption. If either arrives while the model card is open, render and route only the blocking request; reveal the unchanged model state after the request resolves.
   - Preserve: `/model`, the `/settings` Model handoff/return path, session/fork/provider pickers, and command completion behavior.
   - Verify: opening and closing the model card leaves transcript viewport height, scroll position, editor draft, and total frame dimensions unchanged.

5. `internal/tui/transcript_selection_ops.go` or a cohesive new `internal/tui/frame_overlay.go`
   - Change: extract the existing ANSI-aware row replacement logic into `overlayFrameBlock(frame, block string, x, y, width int) string` (name may follow local convention). Keep bounds checks, x/ansi width calculations, padding, clipping, and style resets.
   - Change: make the transcript context menu call this helper with its existing geometry, then use the same helper for the centered model card.
   - Preserve: context-menu placement, selection reverse-video isolation, Unicode cell width, and exact managed-frame dimensions.
   - Verify: overlays at every edge neither wrap the terminal's final cell nor corrupt styled text before/after the block.

6. `internal/tui/model_picker_test.go` (new focused test owner), plus narrow updates to `internal/tui/features_test.go`, `internal/tui/usage_test.go`, and `internal/tui/state_lifecycle_test.go`
   - Change: add table-driven tests for header hit bounds at wide/narrow widths, outside clicks, app/native mouse state, busy-click rejection, and exact frame-size preservation.
   - Change: cover centered card geometry, accent rounded border, stable size, grouped row budgeting, current marker, long labels/descriptions, loading, no matches, small terminals, and both normal/inline renderer branches.
   - Change: cover direct typing without `/`, Unicode-safe Backspace, Ctrl+U, arrow/Tab/page/home/end navigation, Esc clear-then-close, selection/apply, and the nested thinking return/apply flow.
   - Change: cover async refresh preserving query and stable model identity, plus permission/user-input visual and keyboard preemption.
   - Preserve: existing persistence and catalog-deduplication assertions; move only model-picker-specific cases if necessary to keep every Go file below 1,000 lines.
   - Verify: tests assert stripped visible content and `lipgloss.Width`/height rather than brittle full ANSI snapshots.

7. `docs/using-snow.md`, `IMPLEMENTATION.md`, and `CHANGELOG.md`
   - Change: document that the accent `provider/model ▾` header segment opens a centered model card in app mouse mode, `/model` and `/settings` remain fallbacks, typing filters immediately, arrows/page keys navigate, Esc clears then closes, and model-picker `j/k` are text rather than navigation.
   - Change: record that model catalog refresh preserves active search and that the nested thinking step stays in the centered flow.
   - Preserve: F6/native selection behavior, provider discovery semantics, and the existing slash-command reference.
   - Verify: do not add a `models` keybinding action or claim header clicking works while mouse reporting is disabled.

## Scope

- Inherit: every provider catalog and every entry path that already calls `startModelPick` (`/model`, `/settings`, and the new header click) uses the same card, filtering, selection, and persistence flow.
- Verify: full-screen alternate-screen TUI, inline-renderer tests, custom themes, cached and asynchronously refreshed catalogs, model descriptions/privacy notices, models with and without selectable thinking levels, narrow terminals, and app/native mouse modes.
- Exclude: a new Alt+M shortcut, clickable model rows, outside-click dismissal, provider/login redesign, model sorting or ranking, catalog/network changes, standalone `/thinking` redesign, other pickers, SDK/RPC APIs, and removal of `/model`.

## Validation

- Product: launch `snow` with app mouse mode enabled, click the visible `provider/model ▾`, type part of a provider/model/display name without first pressing `/`, navigate with arrows and page keys, select, complete any thinking step, and confirm the persisted project selection after restart.
- Interface: inspect approximately 200×55, 100×30, and 60×16 terminals; cached loading and late refresh; 0, 1, 10, and 46+ matches; multiple providers; long IDs; long privacy descriptions; no-match search; nested thinking; permission/user-input preemption; busy turn; F6 native mode; default, light, dark, and high-contrast themes.
- System: confirm one renderer-owned frame remains constant in width/height, the transcript does not move when the popup opens, only one ANSI-aware block-overlay implementation exists, and no new keybinding/configuration contract was introduced.
- Repository: `gofmt -w <changed-go-files>` → no formatting diff.
- Repository: `go test ./internal/tui -count=1` → all model, mouse, overlay, lifecycle, and renderer tests pass.
- Repository: `go test ./...` → all packages pass.
- Repository: `go vet ./...` → no findings.
- Repository: `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v` → script tests pass.
- Repository: `python3 scripts/check_benchmarks.py` → performance guard passes.
- Repository: `./scripts/install-local.sh` → the verified checkout is installed for the manual TUI checks.

## Stop conditions

- Stop if the header hit box cannot be derived from the same rendered/truncated header layout; do not ship approximate fixed X coordinates.
- Stop if the extracted frame compositor changes ANSI/Unicode width, context-menu behavior, total frame dimensions, or writes through the terminal's final cell.
- Stop if a model change can race an active provider turn; keep header activation idle-only rather than widening runtime concurrency semantics.
- Stop and revisit the component boundary if centering requires a second renderer or bypasses the existing `Model.View` frame; the popup must remain part of the one Bubble Tea frame.

## Design documentation

- After acceptance and validation: update `docs/using-snow.md` with header-click, centered-card, direct-search, mouse-mode fallback, and model-picker key semantics; update `IMPLEMENTATION.md#tui-and-surfaces` with the one-frame centered overlay and cached-refresh preservation; record the user-visible change under `CHANGELOG.md` Unreleased.
