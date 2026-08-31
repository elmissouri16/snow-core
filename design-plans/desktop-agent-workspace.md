# Rebuild Snow Desktop as a minimal conversation-first agent

Written against: `10ad391e7c33a2248809cfe3e4cfd44f159d6063` plus the uncommitted desktop implementation rendered in the user-supplied macOS screenshot.

This plan supersedes the earlier three-region workspace plan in this file. That implementation was rendered and rejected because it produced a sparse dashboard rather than a focused coding-agent interface.

## Evidence chain

- Surface: the single Snow Desktop `Workspace` window in the empty/ready, populated, streaming, tool-running, failed, and stopped states; rendered by `desktop/src/workspace/view.rs` and shown in the user-supplied screenshot.
- Problem: the permanent 224 px workspace rail and 292 px Activity inspector spend a large portion of the window on duplicated provider/session/runtime metadata and an empty tool panel. The selected provider resembles a disabled form control. The center retains a large empty canvas while the composer is a nested form with two borders, permanently visible disabled actions, model arrows, and redundant helper text. The result does not satisfy the selected minimal Claude/Codex-style direction or the existing conversation-first intent.
- Design evidence: the supplied screenshot provides runtime proof of the hierarchy, density, contrast, selected-state, and empty-space problems; the user explicitly rejected the rendered result, selected “Minimal Claude/Codex-style,” and specified that both provider and model picking belong on the prompt input, matching the task-local control placement used by the intended agent pattern; `design-plans/desktop-agent-workspace.md` previously required the conversation to be dominant and controls to support the task rather than dominate it; `desktop/README.md` confirms Snow currently has one long-lived runtime/session surface rather than a session browser that would justify permanent navigation chrome.
- Owner: `desktop/src/workspace/view.rs` owns layout and presentation; `desktop/src/workspace.rs` owns `Workspace`, input setup, provider switching, runtime state, and existing interaction gates; `desktop/src/app.rs` owns window sizing; `desktop/README.md` owns desktop behavior documentation.
- Scope and affected surfaces: main shell, compact title bar, provider chooser presentation, empty and populated transcript, contextual tool activity, composer, errors, connection state, default/minimum window size, focused workspace tests, and desktop documentation.
- Uncertainty: native visual balance must be validated interactively from the ready, streaming, tool-running, failed, and narrow-window states. Do not treat compilation as visual acceptance.

## Design decision

Replace the current three-pane dashboard with one dominant conversation canvas. Snow has only one current project/session surface, so it should not render permanent thread navigation or an empty activity inspector. Use a restrained top bar only for identity, project, and connection; keep the readable transcript centered below it; reveal tool activity contextually above the composer only when activity exists; and place both provider and model pickers on one integrated auto-growing prompt surface, where they govern the next request.

The intended structure is:

```text
┌──────────────────────────────────────────────────────────────────┐
│ Snow · project                                      ● Connected │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│                 focused conversation transcript                  │
│                                                                  │
│                 contextual tool rows, only if any                │
│                 ┌────────────────────────────────┐               │
│                 │ auto-growing prompt            │               │
│                 │ provider · model          send │               │
│                 └────────────────────────────────┘               │
└──────────────────────────────────────────────────────────────────┘
```

Do not retain a narrow decorative sidebar merely to resemble another product. A sidebar becomes justified only when Snow Desktop exposes multiple navigable sessions or projects. Do not retain a permanent right rail when no activity exists.

## Reuse

- `desktop/src/workspace.rs`: preserve `ChatState`, `ConnectionState`, `ToolActivity`, `ToolState`, `PROVIDER_CHOICES`, `select_provider`, `cycle_model`, `submit`, `abort`, process lifecycle, runtime metadata loading, session restoration, and all action gates.
- `desktop/src/workspace/view.rs`: reuse `render_message`, `render_detail_row` concepts only where they remain relevant, the existing transcript `ScrollHandle`, `CONVERSATION_WIDTH`, and active theme tokens.
- `gpui_component::input::InputState::auto_grow(1, 6)`: use the dependency’s existing auto-growing plain-text mode rather than creating a custom editor.
- `gpui_component::input::Input::appearance(false)`: remove the input’s inner border/background so the composer has one visual boundary.
- Existing `Button`, `h_flex`, `v_flex`, `theme.background`, `theme.secondary`, `theme.foreground`, `theme.muted_foreground`, `theme.border`, `theme.success`, `theme.warning`, `theme.danger`, and `theme.info` owners.
- Existing provider buttons and `select_provider` behavior: move them into a small anchored chooser opened from a compact provider control in the composer footer; do not add provider settings or credential behavior.
- Exemplar: existing user/assistant message rendering in `desktop/src/workspace/view.rs`; retain its readable centered transcript rather than introducing another message system.

No new shared design primitive is required. The existing flex, button, input, border, theme, and state owners can express this correction.

## Changes

1. `desktop/src/workspace/view.rs` — remove the permanent dashboard rails
   - Change: delete `WORKSPACE_RAIL_WIDTH`, `ACTIVITY_RAIL_WIDTH`, `render_workspace_rail`, and `render_activity_inspector` from the rendered composition. Make `render_workspace` a full-width vertical stack containing only the compact top bar, transcript, contextual activity, and composer.
   - Change: keep the transcript centered at its existing readable maximum width while allowing the surrounding canvas to fill the window.
   - Preserve: background/theme ownership, transcript scrolling, automatic scroll-to-bottom, message rendering, and runtime-safe text boundaries.
   - Verify: the empty/ready screenshot has no blank left or right panels and the conversation/composer visually own the window.

2. `desktop/src/workspace/view.rs` — replace duplicated chrome with one compact status bar
   - Change: replace the 64 px session bar and rail header/status card with a single approximately 48–52 px row. Show “Snow” and the project label on the left and one connection dot/label on the right. Do not place provider or model controls in this row.
   - Change: remove repeated “Current project,” “Persistent local session,” provider, model, runtime-ready, and connected labels from the default canvas. Session mode and detailed status belong in error/status copy only when they materially differ from ready state.
   - Preserve: connection-state colors, project identity, session pinning, and failure presentation.
   - Verify: each runtime fact appears once in the ready state and the top bar does not compete with the prompt surface.

3. `desktop/src/workspace/view.rs` — make tool activity contextual
   - Change: render no activity UI when `state.tools` is empty.
   - Change: when tools exist, render a compact bounded activity strip directly above the composer inside the conversation-width column. Show the newest running or most recent tool first, preserve `ToolState` color semantics, show at most the latest three rows, and summarize any older entries as “+N earlier”.
   - Change: keep progress/preview text bounded and secondary; do not recreate runtime cards, counters, or a second dashboard.
   - Preserve: explicit `ToolState::{Running, Completed, Failed}`, tool names, status/preview text, and existing state updates.
   - Verify: empty/ready has no tool placeholder; running tools are visible without moving attention to a distant rail; completed and failed states remain distinguishable.

4. `desktop/src/workspace.rs` and `desktop/src/workspace/view.rs` — collapse composition and both request pickers into one integrated surface
   - Change: initialize the existing `InputState` with `auto_grow(1, 6)` and keep `Ask Snow…` as the placeholder. Preserve plain text and the existing bounded prompt submission path.
   - Change: render `Input::appearance(false)` inside one rounded composer boundary. Remove the nested inner field border/background.
   - Change: place two compact controls at the lower-left of the composer footer: active provider first, active model second. These controls govern the next prompt and must not also appear in the top bar or another permanent region.
   - Change: add only presentation-local, mutually exclusive provider/model chooser state to `Workspace`. Open each chooser upward from its composer control so it remains attached to the prompt at the bottom of the window; close it after selection or when the other chooser opens.
   - Change: the provider chooser renders the existing five `PROVIDER_CHOICES`, marks the active provider as selected rather than disabled-looking, preserves `can_switch_provider`, and retains the failed-provider retry path through `select_provider`.
   - Change: replace previous/next model arrows with a bounded scrollable model chooser populated from `state.models`. Add a direct model-selection method that sends the existing `runtime.set_model(model_id)` request and calls `state.begin_model_change`; do not change the RPC or model catalog. Mark the current model as selected and disable model changes through `can_switch_model` while the runtime is not ready.
   - Change: render Send while idle/ready and replace it with Stop only while an abortable prompt exists; do not render a disabled Stop button in the idle state.
   - Change: remove the permanent “Enter to send · Snow runs with permission deny” footer. Surface permission mode only where the runtime exposes a meaningful non-default status or security decision; do not repeat implementation configuration as decorative copy.
   - Change: keep the existing Enter-to-send event and allow the input component’s secondary Enter path to remain available for line breaks; verify this explicitly after enabling auto-grow.
   - Preserve: `select_provider`, provider restart/session pinning, `runtime.set_model`, `can_send`, `can_abort`, `can_switch_provider`, `can_switch_model`, `model_change_pending`, error presentation, submit/abort behavior, and input clearing only after accepted submission.
   - Verify: the composer has exactly one boundary, grows from one through six rows, contains both provider and model pickers, has one primary action, supports a multiline draft, and does not jump or clip at the minimum width. Opening either picker keeps it visually anchored to the composer and does not cover the Send/Stop action.

5. `desktop/src/workspace/view.rs` — reduce empty-state and typography noise
   - Change: remove the decorative “S” tile from the empty state because Snow identity already appears in the top bar. Keep one concise task-oriented heading and at most one short secondary sentence.
   - Change: use `theme.foreground` for actionable labels and primary metadata; reserve `theme.muted_foreground` for genuinely secondary copy. Do not globally brighten the theme or invent new colors.
   - Preserve: project-aware copy where it adds information and the existing theme system.
   - Verify: the empty state does not resemble a landing-page hero, provider controls are legible, and disabled controls remain distinguishable without making active content look disabled.

6. `desktop/src/app.rs` — size for the simplified shell
   - Change: after the permanent 516 px of rails are removed, reduce the native minimum width from 1080 px to approximately 760–800 px while retaining a comfortable default around 1080 × 760. Choose the exact minimum by rendering the longest built-in provider/model labels and the six-row composer without clipping.
   - Preserve: centered launch, native title bar, macOS lifecycle, and resizability.
   - Verify: default, minimum, and intermediate widths keep the transcript and composer usable with no hidden primary action.

7. `desktop/src/workspace_tests.rs` and `desktop/tests/rpc_integration.rs` — protect behavior through the visual rewrite
   - Change: add focused tests for mutually exclusive provider/model chooser state, direct model selection, provider selection/retry, and chooser closure after selection; extend input/event coverage for auto-grow Enter versus secondary Enter if this can be tested without a native display.
   - Change: retain all existing provider switching, prompt, model, abort, tool lifecycle, session restoration, and process-reaping tests unchanged unless a compile-only adaptation is required.
   - Preserve: runtime behavior as the authority; do not alter tests to excuse a lifecycle regression.
   - Verify: the visual rewrite produces no RPC, session, provider, prompt, model, or shutdown changes.

8. `desktop/README.md` and `design-plans/desktop-agent-workspace.md`
   - Change: after interactive visual acceptance, update the desktop composition documentation to describe the single conversation canvas, compact status-only top bar, contextual activity strip, and integrated auto-growing composer with both provider and model pickers. Remove documentation that promises a persistent workspace rail or Activity inspector.
   - Preserve: architecture, security boundary, provider behavior, deferred rich tool cards/Markdown/branch controls/session browser, and verification instructions.
   - Verify: the README describes the rendered UI and does not claim a session-navigation sidebar before the runtime exposes multiple sessions.

## Scope

- Inherit: empty, restored-history, populated, streaming, model-changing, tool-running, tool-failed, provider-switching, runtime-failed, aborting, and stopped states in the single Snow Desktop window.
- Verify: longest provider and model labels; long project names; long user/assistant text; long bounded tool previews; first launch; durable and ephemeral sessions; minimum native window size.
- Exclude: Markdown rendering, rich historical tool cards, file/diff/terminal panes, permission-ask UI, image input, branch/session browsing, multiple projects, plugin/MCP configuration, provider authentication, runtime protocol changes, and a new design system.

## Validation

- Product: launch with the fake provider, send a prompt, observe ready → submitted → completed, switch model, switch provider, force a provider failure/retry, and abort an active real or mocked streaming turn; all behavior must match the pre-redesign state.
- Interface: inspect native screenshots for starting, empty-ready, restored conversation, long transcript, streaming, tool-running, tool-failed, provider-chooser-open, model-chooser-open with a long catalog, model-changing, error, aborting, and stopped states at default width and the selected minimum. Confirm both pickers live on the prompt input, there are no permanent empty rails, no duplicated runtime facts, no nested composer border, no idle Stop button, and no clipping.
- System: confirm all presentation continues to consume `ChatState`, `RuntimeConfig`, `ToolActivity`, and active theme tokens; do not introduce a parallel runtime store, custom color palette, custom input editor, or fake session navigation.
- Repository: `cd desktop && cargo fmt --check && cargo check && cargo test && cargo clippy --all-targets --all-features -- -D warnings` → all checks pass.
- Integration: `cd desktop && SNOW_TEST_BINARY=../snow cargo test --test rpc_integration -- --ignored` → fake-provider completion and real-Snow durable-history restoration pass.
- Working tree: `git diff --check` → no whitespace errors.
- Review: perform a fresh visual review from screenshots before launching the final app; compilation and state tests are not sufficient evidence of visual quality.

## Stop conditions

- Stop if removing the rails would hide an existing runtime action rather than relocating it to the contextual activity, composer, status bar, or error state.
- Stop if provider or model selection cannot be expressed with the existing `Button`/flex/scroll/theme owners without adding a dependency; keep compact in-tree anchored choosers on the composer rather than adding a UI package or moving either picker to the top bar.
- Stop if auto-grow changes Enter/secondary-Enter semantics in a way that can submit an unintended multiline draft; resolve and test the input contract before shipping.
- Stop if contextual tool presentation loses running, completed, failed, preview, or error information; simplify layout, not observable lifecycle state.
- Stop if the implementation introduces unsupported session navigation merely to imitate Claude or Codex.

## Design documentation

- After acceptance and validation: update `desktop/README.md` to record the minimal one-canvas composition, provider and model chooser placement on the prompt input, contextual tool activity, auto-growing composer behavior, and the reason no permanent session/activity rail exists at this milestone.
