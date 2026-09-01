# Keep RPC command results out of the Snow Desktop conversation

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current uncommitted desktop implementation and the user-supplied macOS screenshot.

## Evidence chain

- Surface: `desktop/src/workspace.rs` runtime-event batching → `ChatState::apply` → `desktop/src/workspace/view.rs::render_transcript`, in the connected desktop state after startup inventory commands and after typed slash commands.
- Problem: successful desktop RPC commands are serialized as pretty JSON and appended to the conversation as system messages even though the same correlated completions are subsequently decoded by command-specific presentation owners. The supplied screenshot visibly shows raw `auth_providers` and `skills` payloads behind the session controls and composer.
- Design evidence: the user explicitly rejected the rendered result; `design-plans/desktop-agent-workspace.md` establishes a dominant conversation canvas rather than a debug console; `desktop/README.md` says runtime resources are exposed through desktop-native panels or typed desktop slash commands; `resource_panel_title` and `project_rpc_resource` already define structured native result presentation for supported resource commands.
- Owner: `desktop/src/workspace.rs` owns `ChatState`, event batching, pending command correlation, `apply_command_completion`, resource-panel projection, and compact status/error state. `desktop/src/workspace/view.rs` consumes `ChatState.messages` as the conversation transcript.
- Scope and affected surfaces: automatic startup inventory completions, user-triggered RPC slash commands, settings/auth/session/process/subagent/resource command completions, transcript restoration, compact command status, command rejection errors, and focused desktop state tests.
- Uncertainty: native validation must confirm that every currently dispatched command still exposes its intended panel, state mutation, status, or error after generic JSON transcript rendering is removed. Raw provider-private data must not be introduced into a replacement surface.

## Design decision

Treat `RuntimeEvent::CommandCompleted` as a correlated control-plane response, not a chat message. `ChatState` must not append command names or serialized response objects to `messages`. The existing `Workspace::apply_command_completion` path remains the sole presentation/state owner for successful desktop commands: it decodes bounded typed responses, opens the relevant native panel when that command has one, updates in-memory catalogs/settings for background refreshes, and leaves a concise status. Rejected or malformed commands continue to use `last_error`; local human-readable content deliberately produced by the desktop, such as `/help`, may continue to use `push_system_message`.

This removes the duplicate debug representation without changing RPC, command correlation, provider-facing context, persisted session history, or the normalized agent event stream.

## Reuse

- `Workspace::pending_commands` and request IDs for exact command correlation.
- `Workspace::apply_command_completion` for command-specific decoding and presentation.
- `ResourcePanel`, `project_rpc_resource`, and `resource_panel_title` for native resource cards.
- Existing dedicated owners for settings, auth, sessions, processes, subagents, themes, keybindings, and Agent Skill catalog refreshes.
- `ChatState::status_text` and `ChatState::last_error` for concise success/failure feedback outside the transcript.
- `ChatState::push_system_message` only for intentional human-readable local content such as `/help` and attachment summaries, not arbitrary RPC data.
- Exemplar: the `settings_get`/`settings_update` and `auth_providers` branches in `Workspace::apply_command_completion`, which decode bounded DTOs and update native surfaces instead of asking the renderer to interpret generic JSON.

No new component, protocol field, event type, or transcript role is required.

## Changes

1. `desktop/src/workspace.rs` — stop projecting command completions into chat messages
   - Change: in `ChatState::apply`, replace the `RuntimeEvent::CommandCompleted` branch that allocates a render ID and pushes a `ChatRole::System` message with status-only handling. Do not retain `command` response `data` in `ChatState.messages`.
   - Change: remove `format_command_result` and `MAX_COMMAND_RESULT_CHARS` if they become unused. Do not replace them with another generic serializer or generic command-result transcript card.
   - Preserve: request rejection handling, active prompt lifecycle, normalized assistant/user messages, restored history, local `/help` output, tool/history cards, runtime-safe text bounds, and the append-only Snow session owned by the Go runtime.
   - Verify: `auth_providers`, `skills`, `settings_get`, `themes_list`, `keybindings_get`, `sessions_list`, `processes_list`, and other startup/control completions add zero transcript messages.

2. `desktop/src/workspace.rs` — keep typed completion owners complete and explicit
   - Change: audit every command emitted by `run_rpc_command`, `run_rpc_command_with_input`, startup resource refreshes, process/subagent dispatch, and local slash-command dispatch against `apply_command_completion`. Each successful command must end in exactly one of these outcomes: update a dedicated native panel, update bounded desktop state/catalog data, trigger a correlated follow-up, or expose a concise `status_text` success. Unknown future commands must remain bounded and may update status, but must not dump arbitrary response data into the transcript.
   - Change: carry a presentation-origin bit from the correlated `PendingDesktopCommand` into `apply_command_completion` before removing the pending entry. Derive it from the existing `input_draft.is_some()` distinction: composer-invoked commands may open their result surface, while automatic inventory/polling refreshes update state silently. Preserve the draft itself for `finish_input_command`; do not create a second pending-command map.
   - Change: make `/skills` and `/skills clear` explicit: after their existing bounded catalog decode/update succeeds, pass the original response through `project_rpc_resource` and open `ResourcePanel` only when the correlated command originated in the composer. The automatic startup `skills` refresh must update `skill_catalog` without opening a panel. Keep the existing explicit owners for settings, auth, sessions, processes, and subagents; keep the default allowlisted resource projection for `/usage`, `/context`, `/mcp`, goals, diagnostics, permissions, trust, and similar commands in `resource_panel_title`.
   - Change: retain `project_rpc_resource` as the only generic structured projection for commands in `resource_panel_title`. Do not bypass its allowlisting by rendering `serde_json::Value` directly.
   - Preserve: `pending_commands` removal, `refresh_runtime`, input-draft completion, process/subagent request metadata, theme/keybinding application, auth refresh chaining, and all existing fail-closed parsing errors.
   - Verify: `/skills` opens the Agent Skills resource surface, the startup skills refresh stays silent, every other user-invoked resource command still opens its intended native surface, and rejected or malformed data remains visible through `last_error`.

3. `desktop/src/workspace_tests.rs` — lock the transcript boundary
   - Change: add a state test that starts from `make_ready`, records the message count, applies representative `RuntimeEvent::CommandCompleted` events with nested data for `auth_providers` and `skills`, and asserts that `messages` and render IDs do not change while status advances.
   - Change: add or retain a focused assertion that an intentional local system message still appears, so the correction does not remove `/help` or other authored desktop content by accident.
   - Change: if command-outcome coverage is currently only integration-level, add table-driven coverage around a dependency-light command classification/helper rather than constructing a second fake renderer. Do not duplicate `apply_command_completion` business logic in tests.
   - Preserve: existing prompt streaming, completion, history, and tool-lifecycle tests.
   - Verify: the new regression test fails on the current raw-JSON behavior and passes only when generic command data no longer enters the transcript.

4. `desktop/README.md` and `desktop/PARITY.md` — record the presentation boundary after acceptance
   - Change: state that RPC command results are decoded into native panels/state/status and are never rendered as generic JSON conversation messages.
   - Change: update the command-completion parity evidence to name the regression test that protects this boundary.
   - Preserve: the existing RPC capability and normalized-event claims; do not imply that every RPC command needs a bespoke permanent panel.
   - Verify: documentation distinguishes agent/session history from desktop control-plane responses.

## Scope

- Inherit: automatic startup catalog refreshes, every current desktop RPC slash command, settings/auth/session/process/subagent/resource panels, and future commands routed through the same `RuntimeEvent::CommandCompleted` path.
- Verify: provider switches, runtime-state reloads, user input/permission replies, session mutations, process/subagent polling, `/help`, `/skills`, `/usage`, `/context`, `/settings`, `/login`, and `/sessions`.
- Exclude: changing the Go RPC protocol, changing `pkg/protocol`, changing persisted Snow history, redesigning native resource panels, filtering provider assistant output, or hiding command failures.

## Validation

- Product: launch with `SNOW_PROVIDER=fake`; wait for runtime initialization; confirm the empty conversation remains empty and no `auth_providers`, `skills`, settings, theme, or keybinding JSON appears. Run `/skills`, `/usage`, `/context`, `/settings`, `/sessions`, and `/help`; confirm structured panels or intentional human-readable help appear without raw response objects.
- Interface: validate empty history, populated history, provider replacement, successful command, malformed command response, rejected command, and command completion while a prompt is idle. Confirm transcript scrolling and composer placement do not change merely because background commands complete.
- System: inspect `ChatState.messages` assignment sites and confirm only conversation/history, intentional local human-readable messages, tool/thinking projections, and prompt lifecycle content remain; confirm no parallel generic JSON renderer was added.
- Repository: `cargo fmt --manifest-path desktop/Cargo.toml -- --check` → formatting passes; `cargo test --manifest-path desktop/Cargo.toml` → desktop unit and RPC integration tests pass; `go test ./internal/rpc ./pkg/protocol` → the unchanged protocol and RPC command surface remain green.

## Stop conditions

- Stop if a current command has no safe typed/state/status owner and removing the transcript payload would make its user-requested result unavailable; add that result to the existing allowlisted resource projection or dedicated panel before deleting the generic fallback.
- Stop if any proposed replacement would retain or render arbitrary JSON, secrets, sensitive headers, provider-private continuity data, or unbounded paths; preserve the existing bounded typed projections instead.
- Stop if the only way to distinguish background refreshes from user-invoked commands requires a protocol change; first use the existing correlated `PendingDesktopCommand` metadata and input-draft ownership.
- Stop if the change alters agent prompt messages or persisted session history; this plan changes desktop presentation of control-plane events only.

## Design documentation

- After acceptance and native validation: update `desktop/README.md` with the control-plane/result boundary and `desktop/PARITY.md` with the regression-test evidence. No root documentation change is required unless another public surface currently promises raw command JSON in the transcript.
