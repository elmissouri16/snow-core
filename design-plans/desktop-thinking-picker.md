# Add model-aware thinking control to the Snow Desktop composer

Written against: `10ad391e7c33a2248809cfe3e4cfd44f159d6063` plus the current uncommitted `desktop/` implementation and the user-supplied populated-window screenshot.

## Evidence chain

- Surface: the Snow Desktop composer footer rendered by `desktop/src/workspace/view.rs`, specifically the provider and model controls that govern the next request.
- Problem: Snow already supports model-specific thinking effort and exposes `set_thinking`, current effort, and supported levels over RPC, but the desktop drops those fields. The user explicitly selected a thinking adjustment control immediately to the right of the model picker, so the desktop currently omits an available request-level control from the task-local surface where provider and model are selected.
- Design evidence: `design-plans/desktop-agent-workspace.md` binds provider/model controls to the composer because they govern the next request. Thinking effort has the same scope. `pkg/protocol/model.go` defines `off|minimal|low|medium|high|xhigh|max|ultra` and requires callers to use catalog-advertised support instead of guessing. `pkg/protocol.RPCSessionInfo` already returns `thinking` and `thinking_levels`; `pkg/protocol.RPCRequest` and `internal/rpc/server.go` already support `set_thinking`. The rendered screenshot proves the requested placement: provider and model occupy the lower-left composer row with room for one adjacent compact control.
- Owner: `pkg/protocol` and `internal/agent` own valid effort values and model support; `internal/rpc` owns command validation; `desktop/src/snow/protocol.rs` and `desktop/src/snow/client.rs` own the desktop RPC projection; `desktop/src/workspace.rs` owns pending state/action gates; `desktop/src/workspace/view.rs` owns composer picker presentation.
- Scope and affected surfaces: RPC projection of existing fields/command, desktop current/pending thinking state, model-aware levels, one mutually exclusive composer picker placed to the right of the model picker, provider/model transitions, tests, and desktop behavior documentation.
- Uncertainty: some provider catalogs intentionally advertise only `off`. Native validation must cover a capable model, an off-only model, model switching between different level sets, provider switching, rejected changes, and a turn starting immediately after a confirmed change.

## Design decision

Add one compact `Thinking: <level> ▾` control immediately to the right of the model picker in the existing composer footer. Populate it only from Snow's advertised `thinking_levels`, preserve Snow's canonical order and validation, and use the existing `set_thinking` RPC command. Keep the control visible as `Thinking: Off` for off-only models but disable opening it when no alternative exists; this communicates the effective setting without pretending unsupported levels are selectable. Treat a thinking change like model selection: idle-only, explicitly pending until the correlated RPC response succeeds, mutually exclusive with the provider/model menus, and effective for subsequent prompts only.

## Reuse

- `protocol.ThinkingLevel`, `Model.SupportedThinkingLevels`, `Agent.SetThinking`, RPC `set_thinking`, and `RPCSessionInfo.Thinking/ThinkingLevels` as the only validity and capability owners.
- Existing desktop `RpcRequest`, `RuntimeEvent`, `SessionInfo`, and request/response loop rather than a new transport.
- Existing `ComposerPicker`, `ComposerPickerState`, anchored picker composition, `Button`, theme tokens, and composer action gates.
- Existing model catalog response, whose full model records already contain `supports_thinking`, `default_thinking`, and `thinking_levels`; extend the desktop projection instead of inventing model-name rules.
- Existing `RequestRejected` correlation and `last_error` presentation for failed changes.
- Exemplar: the direct model picker in `desktop/src/workspace.rs` and `desktop/src/workspace/view.rs`; thinking should match its placement, pending behavior, selected row, and chooser geometry.

No new UI dependency or provider-specific effort table is required.

## Changes

1. `desktop/src/snow/protocol.rs` — project Snow's existing thinking metadata and request
   - Change: add `SetThinking { id, thinking }` to `RpcRequest`, serialized as `type: "set_thinking"` with the canonical lowercase wire value.
   - Change: add a desktop `ThinkingLevel` representation that accepts the canonical protocol strings and preserves unknown future strings for additive compatibility at decode boundaries, while only known server-advertised values become selectable.
   - Change: add `thinking` and `thinking_levels` to `SessionInfo` with safe defaults, and add `supports_thinking`, `default_thinking`, and `thinking_levels` to `ModelInfo` with safe defaults so model transitions can be reconciled without model-name inference.
   - Preserve: unknown additive RPC fields remain ignored; absent metadata means off-only, never guessed support.
   - Verify: JSON tests cover exact `set_thinking` encoding, full current-session decoding, off-only defaults, all eight canonical levels, and ignored future model fields.

2. `desktop/src/snow/client.rs` — send and correlate thinking changes
   - Change: add `SnowClient::set_thinking` with the same ready/idle checks as `set_model`; return the generated request ID and send the new request through the existing bounded JSONL writer.
   - Change: on a successful response whose command is `set_thinking`, emit a correlated `RuntimeEvent::ThinkingChanged { request_id }`. Preserve the response ID before consuming response fields.
   - Change: continue routing a rejected `set_thinking` response through `RuntimeEvent::RequestRejected` with its request ID and error.
   - Preserve: serial request writes, frame limits, prompt admission, shutdown, and readiness semantics.
   - Verify: client tests prove the exact frame, rejection correlation, no active-prompt change, and one success event for one acknowledgement.

3. `desktop/src/workspace.rs` — own current, supported, and pending thinking state
   - Change: add `current_thinking`, ordered `thinking_levels`, and `thinking_change_pending { request_id, target }` to `ChatState`. Normalize empty session values to `off`; retain only canonical unique values in server order and ensure `off` is present first.
   - Change: apply `SessionLoaded` as the authoritative current effort/level set. A confirmed correlated `ThinkingChanged` commits the pending target; an unrelated acknowledgement does nothing; a correlated rejection clears pending state, retains the prior current value, and exposes the existing error row.
   - Change: add `can_switch_thinking`/`can_select_thinking` gates. Require ready, fully restored, no active prompt, no model change, no thinking change, and at least two advertised levels. Include `thinking_change_pending` in `can_send`, provider switching, and model switching so no prompt or transition observes an unconfirmed effort.
   - Change: add `begin_thinking_change` and completion/rejection transitions rather than optimistically changing the visible selected value.
   - Preserve: active prompt serialization, Send/Stop behavior, provider retry, and runtime-load gates.
   - Verify: focused tests cover off-only, every canonical level, duplicate/unknown filtering, pending action blocking, correlated success, unrelated success, correlated rejection, active-prompt blocking, and provider reconnect reset.

4. `desktop/src/workspace.rs` and `desktop/src/workspace/view.rs` — place the control to the right of the model picker
   - Change: add `ComposerPicker::Thinking` and render its compact button after `model-menu` in the same lower-left `h_flex`; label it `Thinking: Off`, `Thinking: Low`, `Thinking: Medium`, `Thinking: High`, `Thinking: X-high`, `Thinking: Max`, or `Thinking: Ultra` using presentation labels only while preserving canonical wire values.
   - Change: keep `ComposerPickerState` mutually exclusive: opening thinking closes provider/model; selecting or canceling closes thinking; opening provider/model closes thinking.
   - Change: reuse the existing anchored-above-composer chooser width, border, row, selected marker, and bounded height. Render only the current model's advertised levels and mark exactly one as `Selected`.
   - Change: keep `Thinking: Off` visible but disabled when `off` is the only advertised level. Disable all changes during prompts, provider/model transitions, or a pending thinking request.
   - Change: selecting the already active level only closes the picker; selecting another level calls `runtime.set_thinking`, records the correlated pending transition, and waits for acknowledgement.
   - Preserve: provider then model then thinking visual order, model/provider behavior, composer auto-grow, and Send/Stop placement.
   - Verify: at wide and 800 px minimum widths, provider/model/thinking controls remain on the left without colliding with Send; unusually long model names truncate before the thinking and Send controls rather than expanding the window.

5. `desktop/src/workspace.rs` — reconcile thinking support when the model changes
   - Change: use the target `ModelInfo` metadata to determine whether the current effort is supported. Never retain a non-off level for a target model that does not advertise it.
   - Change: extend RPC model selection to submit an explicit compatible effort with the model change: retain the current effort when the target advertises it; otherwise use the target's advertised default if valid, else `off`. The model and chosen effort must be admitted as one idle transition so a prompt cannot observe an unsupported intermediate combination.
   - Change: implement that atomicity through the existing core `App.SetProviderModelThinking` owner rather than sending two independently observable desktop commands. Extend the backward-compatible RPC `set_model` path to honor an optional `thinking` field when supplied; requests that omit it retain existing behavior for current RPC clients.
   - Change: after success, refresh/project the current model, effective effort, and supported levels consistently; do not update the UI from the model name alone.
   - Preserve: model selection without restart, provider identity, session tree, and compatibility with existing `set_model` requests.
   - Verify: switching High-capable → off-only commits model plus Off; off-only → High-capable retains Off unless the user chooses another level; switching between models supporting the active effort retains it; admission rejects prompts until the compound change finishes.

6. `internal/rpc/server.go`, `pkg/protocol` RPC tests, and `internal/rpc` tests — preserve protocol correctness for atomic model/effort selection
   - Change: when `set_model` includes non-empty `thinking`, parse it with `protocol.ParseThinkingLevel`, resolve the catalog model exactly as today, and call the existing app compound provider/model/thinking facade for the active provider. Keep the existing `App.SetModel` path when the field is omitted.
   - Change: return the same `set_model` response command and existing model-change event contract; do not add a protocol-version break.
   - Preserve: unknown-model policy, provider matching, idle admission, and authoritative model support validation.
   - Verify: RPC tests cover omitted-thinking compatibility, supported compound selection, unsupported rejection without partial mutation, active-turn rejection, and session_info reporting the committed combination.

7. `desktop/src/workspace_tests.rs` and desktop RPC integration tests — exercise the complete control path
   - Change: extend fixtures with realistic per-model level sets and session effort.
   - Change: test picker exclusivity across provider/model/thinking, selected-row closure, disabled off-only control, pending gates, failure rollback, provider replacement, and model/effort reconciliation.
   - Change: extend real-Snow RPC integration to select a supported thinking level on a capable fake model, confirm it through `session_info`, issue a prompt, and verify the process remains healthy; separately prove unsupported levels fail without mutating session state.
   - Preserve: existing 40 desktop unit tests and mock/real process lifecycle coverage.
   - Verify: no test infers capability from a model ID; all level sets come from fixture metadata or Snow's response.

8. `desktop/README.md` and canonical RPC documentation if needed — document the effective control
   - Change: document the thinking picker placement, model-advertised choices, off-only disabled state, idle/pending gating, and subsequent-prompt semantics.
   - Change: document the backward-compatible optional `thinking` field on `set_model` if the RPC command reference enumerates request shapes.
   - Preserve: reasoning summaries and text verbosity remain separate controls and are not conflated with thinking effort.
   - Verify: documentation uses canonical values and does not promise unsupported effort for every provider/model.

## Scope

- Inherit: all Snow Desktop providers and models that return current thinking metadata through `session_info` and model support metadata through `models_list`.
- Verify: Fake, OpenCode Go, ChatGPT, Compatible, capable/off-only catalogs, provider retry, model switch, prompt start, process restart, 800 px minimum width, and dark/light themes.
- Exclude: display of private thinking text, reasoning-summary controls, text-verbosity controls, plan-mode controls, thinking-budget token sliders, provider-specific invented presets, global configuration editing, and subagent effort selection.

## Validation

- Product: select a model that advertises multiple levels, choose High from the control immediately right of the model picker, submit a prompt, and confirm `session_info` and the next request use High; select an off-only model and confirm the control visibly reports Off without opening unsupported choices.
- Interface: inspect closed, open, selected, hover, pending, failed, active-prompt, provider-switch, model-switch, long-model-name, and minimum-width states; only one chooser may be open.
- System: confirm every option originated in Snow model metadata, `set_thinking` remains the direct effort command, and compound model/effort updates reuse the existing app/agent owner rather than a desktop-side approximation.
- Repository: `gofmt -w <changed-go-files> && go test ./internal/agent ./internal/app ./internal/rpc ./pkg/protocol` → all pass.
- Repository: `go test ./... && go vet ./...` → all pass.
- Repository: `cd desktop && cargo fmt --check && cargo check && cargo test && cargo clippy --all-targets --all-features -- -D warnings` → all pass.
- Repository: `cd desktop && SNOW_TEST_BINARY=../snow cargo test --test rpc_integration -- --ignored` → supported selection and rejection scenarios pass against real Snow.
- Repository: `git diff --check` → no whitespace errors.
- Repository: after the verified feature change, `./scripts/install-local.sh` → local Snow installation reflects the checkout.

## Stop conditions

- Stop if the active model does not provide authoritative `thinking_levels`; expose only Off rather than guessing from provider or model name.
- Stop if model plus effort cannot be committed without a prompt-visible unsupported intermediate state; use the existing compound app owner before enabling model changes in the desktop.
- Stop if an RPC rejection cannot be correlated to the exact pending target; do not optimistically display an unconfirmed effort.
- Stop if `set_thinking` would expose provider-private reasoning content; the control changes effort only and must not render hidden thinking.
- Stop if the third control causes Send to overflow at the 800 px minimum width; truncate the model label within the existing left control group rather than moving thinking away from the model picker.

## Design documentation

- After acceptance and verification: update `desktop/README.md` with the model-aware thinking picker contract.
- After acceptance and verification: update the canonical RPC command documentation for optional atomic `set_model.thinking` only if the implementation extends that request shape.
