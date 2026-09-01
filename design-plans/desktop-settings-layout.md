# Make Snow Desktop settings a dedicated workspace

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current dirty desktop implementation and the user-supplied native settings screenshot.

## Evidence chain

- Surface: the native Snow Desktop workspace when `settings_panel` is active.
- Previous problem: `desktop/src/workspace/view.rs::render_workspace` inserted settings between the conversation top bar and transcript, then continued to render the transcript and shared composer. Settings therefore felt like a temporary chat panel rather than an application page.
- Design evidence: the supplied screenshot shows a full-height settings workspace with its own category navigation and content canvas. The user's follow-up explicitly requires settings not to appear above, alongside, or behind the composer.
- Behavioral authority: existing settings values, RPC handlers, model selection, theme roles, inputs, appearance behavior, and semantic keybinding behavior remain unchanged.

## Design decision

When settings are visible, replace the ordinary conversation surface—including the project/thread sidebar, top bar, transcript, interaction cards, and composer—with a dedicated settings workspace.

The settings workspace uses:

- a fixed, full-height settings navigation rail;
- a full-height content canvas with a settings-local top bar;
- four focused sections: General, Capabilities, Appearance, and Keybindings;
- a bounded, centered, vertically scrollable content column;
- the existing semantic theme colors and existing control/RPC handlers;
- a settings-local model popover because the composer-owned popover trigger is not rendered on this page.

Closing settings restores the existing conversation workspace unchanged.

## Reuse

- `theme.background`, `theme.secondary`, `theme.border`, `theme.primary`, `theme.primary_foreground`, and `theme.muted_foreground`.
- Existing GPUI `v_flex`, `h_flex`, wrapping, spacing, scrolling, border, and rounded-corner primitives.
- Existing settings controls, IDs, input entities, enabled/disabled gates, selected styles, and RPC calls.
- The existing `ComposerPicker::Model` state and `render_model_picker` body, exposed through a settings-local `Popover` trigger.

## Changes

1. `desktop/src/workspace.rs`
   - Add bounded local `SettingsSection` presentation state and a separate desired-open flag, so settings can show a loading workspace immediately and late RPC responses cannot reopen it after **Done**.
   - Add helpers for opening a specific settings section and switching sections.
   - Close transient composer-picker state when changing sections or leaving settings; picker dismissal never returns focus to the hidden composer.
   - Route `/settings`, `/permissions`, and `/keybindings` to the appropriate dedicated settings section. Semantic Models switches to General before opening the settings-local picker.

2. `desktop/src/workspace/view.rs`
   - Branch the root render path: settings render as their own full-window surface; ordinary workspace/sidebar/composer render only when settings are absent.
   - Add the dedicated settings navigation rail, connection status, and back action.
   - Convert the settings panel from a 540 px chat insert into a full-height responsive content page.
   - Render only the selected settings category.
   - Keep every existing control and handler, including the keybinding editor and restart diagnostics.
   - Render the model picker in a settings-local popover.

3. `desktop/src/workspace_tests.rs`
   - Verify the stable settings-section order, default, labels, and non-empty descriptions.
   - Retain management-panel tests that verify blocking interactions suppress settings as before.

## Responsive behavior

- The desktop minimum remains `900×600`.
- The settings rail stays narrow and fixed; the content canvas owns the remaining width.
- The content column is capped at 860 px with horizontal gutters and independent vertical scrolling.
- Existing option groups keep `flex_wrap`, so controls wrap instead of overflowing at the minimum width.
- No composer is mounted while settings are visible, so settings cannot collide with or appear above the chat input.

## Validation

- Product: open Settings and confirm the normal sidebar, transcript, interaction cards, and composer are absent. Switch through all four categories, close settings, and confirm the conversation workspace returns.
- Behavior: change permission, model, thinking effort, verbosity, concurrency, capability toggles, theme, appearance, and a keybinding; each must use the existing handler and preserve current persistence behavior.
- Interface: inspect at `1280×820` and approximately `900×600`; the settings rail remains usable, content stays centered, option groups wrap, and scrolling reaches every control.
- Repository:
  - `cargo fmt --manifest-path desktop/Cargo.toml -- --check`
  - `cargo check --manifest-path desktop/Cargo.toml --all-targets`
  - `cargo test --manifest-path desktop/Cargo.toml`
  - `git diff --check`

## Scope

- Inherit all theme and appearance variants because only semantic theme roles are reused.
- Exclude settings protocol changes, new settings values, new dependencies, sidebar/session cleanup, and composer redesign.
- Preserve the existing rule that blocking interactions suppress management panels, including settings.
