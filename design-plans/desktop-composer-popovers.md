# Make Snow Desktop composer pickers non-disruptive popovers

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current uncommitted Snow Desktop workspace redesign.

## Evidence chain

- Surface: the empty and active Snow Desktop composer when provider, model, thinking, slash-command, mention, or Agent Skill choices are visible.
- Problem: the user-supplied rendered screenshot proves that `render_active_picker` is inserted as a normal child above the composer card. Opening the provider picker pushes the composer downward and changes the empty-state composition instead of floating above it.
- Design evidence: the user explicitly required pickers to pop over without affecting layout. The existing session/branch `Popover` already establishes Snow's controlled overlay behavior, and the current `gpui-component` documentation confirms controlled `open`/`on_open_change`, trigger ownership, and `BottomLeft` anchoring.
- Owner: `desktop/src/workspace/view.rs::render_composer` owns current in-flow placement; `render_provider_picker`, `render_model_picker`, and `render_thinking_picker` own picker content; `render_command_suggestions`, `render_mention_suggestions`, and `render_skill_suggestions` own input suggestions; `desktop/src/workspace.rs::ComposerPickerState` owns mutual exclusion, search, keyboard navigation, and dismissal.
- Scope and affected surfaces: provider/model/thinking footer controls, input-driven slash/mention/skill suggestion surfaces, keyboard focus/dismissal, empty and active composer placement, narrow-window anchoring, tests, and desktop documentation.
- Uncertainty: none about the required behavior. Exact anchor fallback is delegated to `Popover`'s existing viewport positioning and must be validated at the minimum window size.

## Design decision

Remove every transient composer selection surface from normal flex flow. Wrap provider, model, and thinking triggers in controlled `Popover` instances anchored above their footer controls. Wrap the existing composer input trigger in one controlled suggestion popover that shows the currently applicable slash, mention, or skill choices. Preserve the existing picker content, search input entity, keyboard state, selection handlers, and mutual exclusion; change only ownership and placement so opening or closing a picker never changes transcript, hero, error, activity, or composer geometry.

## Reuse

- `gpui_component::popover::Popover` with controlled `open` and `on_open_change`.
- Existing `Corner::BottomLeft`/viewport-aware anchor vocabulary and the session-menu controlled-popover exemplar in `render_top_bar`.
- Existing `ComposerPicker`, `ComposerPickerState`, shared picker search input, `PickerSearch DesktopPicker` key context, and Escape/arrow/accept actions.
- Existing provider/model/thinking picker cards and slash/mention/skill suggestion cards; content styling is not redesigned.
- Existing `theme.popover`, `theme.border`, `PICKER_MAX_HEIGHT`, and bounded completion/search results.

No dependency or general overlay manager is required.

## Changes

1. `desktop/src/workspace.rs` — expose controlled picker open/close semantics
   - Change: replace trigger-only toggle assumptions with an idempotent `set_composer_picker_open(picker, open, window, cx)` path suitable for `Popover::on_open_change`; opening one picker closes the others, resets search, and focuses the shared picker search input; closing clears state without reopening another picker.
   - Change: add an idempotent close path for the input-suggestion popover that dismisses the active slash/mention/skill selection without modifying composer text or focus.
   - Preserve: arrow/page/top/bottom navigation, Enter acceptance, Escape dismissal, semantic keybindings, selection RPC behavior, pending gates, session-popover exclusivity, and picker query bounds.
   - Verify: repeated open-state callbacks cannot toggle a picker back open; provider/model/thinking remain mutually exclusive.

2. `desktop/src/workspace/view.rs` — move footer pickers into anchored overlay ownership
   - Change: remove `active_picker` from the outer composer `v_flex` child chain.
   - Change: wrap each provider/model/thinking footer button in a controlled `Popover` using the corresponding `ComposerPicker` state, `on_open_change`, and an above-composer/viewport-safe bottom anchor. Render the existing picker content as the popover child.
   - Change: keep wrapped-footer behavior at 900 px; the popover trigger occupies exactly the same row/wrapped-row space as the original button.
   - Preserve: trigger labels, button IDs where persistence/tests rely on them, disabled gates, selected rows, search field, widths/max heights, click handlers, and theme roles.
   - Verify: opening any footer picker leaves the composer card's bounds and empty-state centering unchanged; the popup stays inside the window and overlays rather than displacing content.

3. `desktop/src/workspace/view.rs` — make input-driven suggestions overlay the composer
   - Change: replace the three in-flow `when_some` suggestion children with one mutually exclusive suggestion popover anchored to the existing composer input trigger. Priority follows current parser behavior: slash command, mention, then skill; never display multiple suggestion cards simultaneously.
   - Change: render existing structured command rows and bounded mention/skill rows inside the popover without duplicating the composer input entity.
   - Preserve: typing-triggered visibility, selected row, Tab/Enter/Escape behavior, immediate versus editable slash commands, mention/skill insertion, completion limits, and input focus.
   - Verify: typing `/`, `@`, or `$` does not move the composer or transcript; dismissing the popup leaves the draft unchanged except for an accepted completion.

4. `desktop/src/workspace_tests.rs` and focused picker/component tests
   - Change: test controlled open/close idempotence, mutual exclusion, Escape, search reset/focus contract, input-suggestion priority, and a layout projection proving transient picker visibility is not part of composer height selection.
   - Change: retain minimum-width footer tests and add expanded/collapsed 900 px cases with each popover open.
   - Preserve: all provider/model/thinking selection, slash/mention/skill, submit/stop, and session-popover tests.
   - Verify: no test asserts in-flow picker placement after this change.

5. `desktop/README.md`, `desktop/PARITY.md`, and overlapping design records
   - Change: document that composer pickers and input suggestions are bounded overlays that do not resize the conversation. Mark the older inline-placement assumption in `design-plans/desktop-slash-command-picker.md` as superseded by this user-selected overlay requirement if reconciliation is necessary.
   - Preserve: content styling and behavioral documentation.
   - Verify: docs distinguish persistent errors/activity from transient selection overlays.

## Scope

- Inherit: empty/new-thread and active conversation states, normal/wrapped composer footer, expanded/collapsed sidebar, provider/model/thinking catalogs, slash commands, mentions, skills, keyboard and mouse input, light/dark themes.
- Verify: default 1280×820 and minimum 900×600; long provider/model labels; zero/eight results; error banner and contextual activity present; active streaming; blocking interaction; popover near left/right/bottom viewport edges.
- Exclude: redesigning picker contents, changing catalogs or RPC commands, changing composer dimensions, moving persistent error/activity cards into overlays, changing session/branch popover behavior, or introducing a new overlay dependency.

## Validation

- Product: open each footer picker in empty and active states; type `/`, `@`, and `$`; navigate and accept with keyboard; dismiss with Escape/click-away; confirm prompt submission, provider switching, model/thinking selection, and completion insertion remain correct.
- Interface: compare composer and transcript bounds before/during/after every popup at default and minimum sizes; bounds must be identical while popup cards remain fully reachable and above the composer.
- System: confirm one shared composer/input entity and one `ComposerPickerState`; no duplicate window, picker catalog, or selection path is introduced.
- Repository: `cargo fmt --manifest-path desktop/Cargo.toml -- --check` → clean; `cargo check --manifest-path desktop/Cargo.toml --all-targets` → success; `cargo test --manifest-path desktop/Cargo.toml` → success; `SNOW_TEST_BINARY="$PWD/snow" cargo test --manifest-path desktop/Cargo.toml --test rpc_integration -- --ignored --test-threads=1` → all real-Snow tests pass; `git diff --check` → clean; `./scripts/install-local.sh` → installed; documented fake-runtime desktop launch → ready.

## Stop conditions

- Stop if a popover requires a second composer input, duplicated picker state, lost keyboard focus, unbounded content, or changes to provider/model/thinking RPC semantics.
- Stop if viewport-safe placement cannot keep the popup reachable at 900×600; bound width/height or adjust the existing anchor rather than restoring in-flow layout.

## Design documentation

- After acceptance and native validation: update `desktop/README.md` and `desktop/PARITY.md` with non-disruptive composer popover ownership and verification evidence.
