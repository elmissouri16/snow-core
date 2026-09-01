# Turn Snow Desktop slash completion into a structured command picker

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current uncommitted desktop implementation and the user-supplied macOS screenshot.

## Evidence chain

- Surface: the Snow Desktop composer after typing a slash-command first token, rendered by `desktop/src/workspace/view.rs::render_command_suggestions` from `SlashSelectionState` in `desktop/src/composer_support.rs`.
- Problem: the supplied screenshot shows a large list of visually identical text rows with selection indicated only by a small leading `›`. Source builds each row as one ghost-button string using format padding—`"{marker}{:<14}  {}"`—so command identity, description, and selected state have no structural presentation and depend on spaces rendered in the button’s proportional UI font.
- Design evidence: the user explicitly rejected the rendered UI as assembled rather than designed; the selected Snow Desktop direction is a minimal conversation-first native client; the adjacent provider and thinking pickers already use bounded cards, explicit row spacing, `theme.secondary` selected surfaces, primary labels, and muted supporting text; the model metadata rows already use a stable 128 px label column plus flexible value content.
- Owner: `desktop/src/workspace/view.rs` owns the command-picker card and rows. `desktop/src/composer_support.rs::SlashSelectionState` owns bounded matching, selected index, keyboard wrap, dismissal, insertion, and immediate-versus-editable execution. `desktop/src/commands.rs` owns command names, descriptions, and completion behavior.
- Scope and affected surfaces: `/` completion, prefix/fuzzy filtering, keyboard highlight, mouse selection, command descriptions, 800 px minimum width, light/dark themes, and long labels/descriptions. Mention and Agent Skill completion are sibling surfaces to verify but not redesign in this change.
- Uncertainty: native validation must confirm that the fixed command-label column and flexible description remain readable at the 800 px minimum window and that the active row remains obvious in every selectable Snow theme. No interaction behavior is ambiguous.

## Design decision

Keep the current bounded command catalog, filtering, keyboard semantics, completion cap, and composer-relative placement. Replace each space-padded ghost-button label with a real interactive row composed from two text elements: the command name in a stable leading column and the description in a flexible, muted column. Apply the existing `theme.secondary` selected-row surface to the row whose index equals `SlashSelectionState::selected`; do not encode selection into the command string. The complete row remains one click target and continues to call `select_command_completion`.

This corrects hierarchy, alignment, and active-state presentation without adding a command palette route, changing command behavior, or introducing a new design system.

## Reuse

- `SlashSelectionState::{visible, selected, matches}` as the only selection owner.
- `SlashCommand::{name, description, completion}` and `commands::catalog` as the only copy and behavior owners.
- Existing `MAX_COMPOSER_COMPLETIONS` and `PICKER_MAX_HEIGHT` bounds.
- Existing picker outer surface: `theme.popover`, `theme.border`, rounded corners, padding, and composer-width containment.
- Existing selected-row vocabulary from `render_thinking_picker`: rounded row, `theme.secondary`, primary label, and muted supporting text.
- Existing 128 px leading-label column from `render_model_metadata_card`, which is sufficient for the current bounded slash-command names and avoids another arbitrary alignment value.
- Existing `select_command_completion`, `handle_slash_key`, Enter/Tab behavior, focus restoration, and click listeners.
- Exemplar: `desktop/src/workspace/view.rs::render_thinking_picker` for selected-state styling and `render_model_metadata_card` for the label/value row structure.

No new dependency or cross-application primitive is required. Keep any row helper private to this picker unless a later audit proves that mention or Agent Skill suggestions require the identical contract.

## Changes

1. `desktop/src/workspace/view.rs` — replace formatted labels with semantic row composition
   - Change: in `render_command_suggestions`, remove the `marker` string and `format!("{marker}{:<14}  {}", ...)` label. Build each result as an `h_flex` row with a 128 px, non-shrinking command-name column and a `min_w(0)`, flexible description column.
   - Change: render the command name with the row’s primary foreground and medium weight; render the description with `theme.muted_foreground` and allow it to truncate cleanly at narrow widths rather than wrapping the picker into uneven row heights.
   - Change: keep the entire row interactive and keyed by the current index. On click, invoke the existing `select_command_completion` with the exact command name and return focus to the composer as today.
   - Preserve: command text, description text, match ordering, result cap, immediate/editable metadata, click action, and no-argument parsing.
   - Verify: command names form one stable visual column, descriptions form a second aligned column, and no layout depends on whitespace padding or a monospaced font.

2. `desktop/src/workspace/view.rs` — give keyboard selection a full-row native state
   - Change: when `index == self.slash_selection.selected`, apply the existing rounded `theme.secondary` selected surface to the complete row. Do not append `›`, `Selected`, or another character to the command name; the accessible command string and inserted value remain unchanged.
   - Change: retain a restrained default row with no filled background. Use the existing picker gap/padding vocabulary so eight results remain bounded by `PICKER_MAX_HEIGHT` without increasing the current panel footprint.
   - Change: keep the outer card attached immediately above the composer and preserve its border/background. This milestone restructures rows; it does not create a global command dialog or full-screen palette.
   - Preserve: Up/Down/Tab/BackTab/Enter/Escape dispatch, selected-index wrapping, dismissal, focus, and `MAX_COMPOSER_COMPLETIONS`.
   - Verify: the highlighted row is unmistakable in Snow, Frost, Ember, and Aurora themes and moves with keyboard navigation without changing text alignment.

3. `desktop/src/workspace_tests.rs` and `desktop/src/composer_support.rs` tests — protect the presentation inputs and interaction contract
   - Change: extend the existing `SlashSelectionState` tests to cover selection after prefix filtering, fuzzy filtering, wraparound, and refresh that shortens the result list. Assert that the selected index always names the same command the view should highlight.
   - Change: add focused coverage that mouse selection continues to insert the exact command token expected by `select_command_completion` and returns the composer to an editable state; if constructing `Workspace` would require a fake GPUI renderer, keep this as an existing action-helper test rather than adding a parallel view model.
   - Change: retain the current tests for immediate command execution versus editable command insertion on Enter/Tab.
   - Preserve: completion limits, catalog byte bounds, unknown-command safety, and command parser coverage.
   - Verify: tests fail if filtering leaves an out-of-range selected row or if the rendered row would receive a command name different from the selected state.

4. `desktop/README.md` and `desktop/PARITY.md` — record the native completion presentation after acceptance
   - Change: describe `/` completion as a bounded two-column native picker with an explicit keyboard-selected row.
   - Change: update command-completion parity evidence with the selection/filtering tests and native narrow-window validation.
   - Preserve: the documented command families and keyboard execution semantics.
   - Verify: documentation does not promise a global palette, command search outside the first token, or changed Enter behavior.

## Scope

- Inherit: every current and future command supplied by `commands::catalog` and projected through `slash_command_catalog`.
- Verify: exact prefix matches, stable fuzzy matches, eight visible results, fewer than eight results, long descriptions, Unicode text, mouse selection, keyboard navigation, immediate commands, editable commands, light/dark appearance, and all selectable themes.
- Exclude: changing command names/descriptions, adding command categories/icons, redesigning mention or Agent Skill suggestions, changing parser or dispatch semantics, adding a global keyboard shortcut, changing composer dimensions, or adding accessibility semantics not requested in this audit.

## Validation

- Product: launch with the fake provider, type `/`, `/s`, `/set`, and a three-character fuzzy query; move the selection with Up/Down and Tab/BackTab; execute an immediate command with Enter; insert an editable command; click first, middle, and last visible rows. Confirm the active surface moves with selection and the inserted/executed command is unchanged.
- Interface: validate the 800×560 minimum window and default 1080×760 window; one result and eight results; shortest and longest current command names; long descriptions; Snow, Frost, Ember, and Aurora in light/dark appearance. Confirm names stay aligned, descriptions truncate instead of colliding, and the picker does not grow beyond `PICKER_MAX_HEIGHT`.
- System: confirm `SlashSelectionState` and `commands::catalog` remain the only state/copy owners, no whitespace formatting remains in command-row presentation, no monospaced font is introduced to compensate for padding, and no shared row primitive is extracted without another proven consumer.
- Repository: `cargo fmt --manifest-path desktop/Cargo.toml -- --check` → formatting passes; `cargo test --manifest-path desktop/Cargo.toml composer_support` → completion-state tests pass; `cargo test --manifest-path desktop/Cargo.toml` → all desktop unit and integration tests pass; `cargo clippy --manifest-path desktop/Cargo.toml --all-targets -- -D warnings` → no new warnings.

## Stop conditions

- Stop if a structured interactive row cannot preserve the current whole-row click target and exact command insertion; do not split the name and description into separate actions.
- Stop if the 128 px existing label-column exemplar clips any current command name at supported UI scale; derive the minimum from the current bounded catalog before widening it, rather than truncating command identity.
- Stop if selected-row styling is not distinguishable in one of Snow’s selectable themes; correct the existing semantic selected-surface usage rather than adding a hard-coded color.
- Stop if the change alters immediate-versus-editable Enter/Tab behavior, command parsing, or unknown-command safety; those are owned by `SlashSelectionState` and `commands.rs`, not this presentation change.
- Stop if implementation requires a new UI dependency, a global command palette, or a second completion-state model.

## Design documentation

- After acceptance and native validation: update `desktop/README.md` with the structured picker presentation and `desktop/PARITY.md` with the corresponding interaction and narrow-window evidence. No root README or protocol documentation change is required.
