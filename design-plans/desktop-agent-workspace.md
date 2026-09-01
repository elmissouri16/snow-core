# Recompose Snow Desktop as a dark, project-and-thread workspace

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current uncommitted desktop implementation and the two user-supplied T3 Code reference screenshots.

This plan supersedes this file's earlier “single canvas with no permanent navigation rail” direction. The user has now explicitly selected a two-pane desktop workspace as the visual target. The implementation must adopt that hierarchy without copying T3 Code branding, inventing unsupported product actions, or weakening Snow's existing RPC, permission, session-tree, provider, composer, and blocking-interaction behavior.

## Design language

- Audited surface: the single native Snow Desktop `Workspace` window in its empty/new-thread state and its active-conversation state.
- Design sources: the two user-supplied T3 Code screenshots are the binding rendered reference for hierarchy, density, placement, and dark presentation; `desktop/README.md` is authoritative for Snow capabilities; `desktop/src/workspace/view.rs` and `desktop/src/workspace.rs` are the current runtime/rendering owners; `desktop/src/appearance.rs`, `desktop/src/presentation_runtime.rs`, and `desktop/src/theme_palette.rs` govern adaptive appearance and Snow semantic colors.
- Documented decisions: preserve one external `snow --mode rpc` runtime; preserve the existing normalized event stream, serial prompt behavior, trusted blocking cards, settings/auth/resource/process/subagent surfaces, project session inventory, branches, attachments, slash completion, and provider/model/thinking/permission controls. Keep the current theme system and Snow's product name rather than cloning T3 Code labels or assets.
- Governing owners and consumers: `desktop/src/app.rs::run` owns window geometry; `desktop/src/workspace/view.rs::render_workspace` owns shell composition; `render_top_bar`, `render_transcript`, `render_empty_state`, `render_message`, and `render_composer` own the primary visual states; `Workspace` and the existing session command handlers in `desktop/src/workspace.rs` own state and RPC behavior; the GPUI theme consumed through `cx.theme()` owns adaptive colors.
- Explicit exceptions: Snow is a one-project-per-runtime coding-agent client, so the reference's “All projects” control becomes a current-project section rather than a fake multi-project database. T3 Code, Open/VS Code branding, and unsupported “Add action” behavior are not copied. Existing blocking permission and user-input cards retain input priority even when that differs from the sparse reference screenshots.

## Evidence chain

- Surface: `desktop/src/app.rs::run` creates one `Workspace`; `Workspace::render_workspace` currently renders a single vertical column containing a 50 px top bar, optional full-width panels, centered transcript, and bottom composer.
- Problem: the selected references establish a persistent left navigation rail, a distinct main canvas, a breadcrumb/action toolbar, a centered empty-state composer, a bottom-anchored active-state composer, sparse messages, subtle black/charcoal layers, and large rounded input containment. The current source has no sidebar, keeps the empty composer at the bottom, constrains the conversation to 760 px, presents assistant identity chrome on every message, and mixes telemetry/status pills into the top bar. Those deterministic source differences prevent the requested visual hierarchy.
- Design evidence: in both references the sidebar remains fixed while the main state changes; the main toolbar is one restrained horizontal strip; the empty state centers one project-specific question over the composer; the active state gives the transcript most of the canvas and anchors the same composer near the bottom; user messages are compact right-aligned bubbles; assistant text is unboxed and left aligned; controls are muted until selected.
- Owner: shell and state rendering are local to `desktop/src/workspace/view.rs`; only sidebar visibility and non-disruptive session inventory refresh require additions in `desktop/src/workspace.rs`; default/minimum window geometry is local to `desktop/src/app.rs`. No provider, RPC protocol, Go agent loop, or SDK owner needs to change.
- Scope and affected surfaces: native window geometry; persistent sidebar; current-project/session navigation; top toolbar; empty state; transcript geometry and message treatment; composer geometry/copy/control placement; overlay panels and popovers; responsive behavior at the documented minimum window; light/dark theme compatibility; existing keyboard and action handlers.
- Uncertainty: exact optical spacing and text truncation require validation in the native GPUI window. The target is the reference hierarchy and rhythm, not pixel-copying proprietary assets. If GPUI lacks a needed stock icon, use Snow-owned text or simple glyphs rather than adding a dependency.

## Design decision

Recompose the primary workspace into a persistent two-pane shell: a bounded project/thread sidebar and a flexible main conversation canvas. Use Snow's existing session inventory as the thread list and the current project path as the single project group. Keep the main toolbar and composer controls connected to existing actions. Render the empty state as a centered hero-plus-composer composition; once conversation content exists, render the transcript in the flexible middle and the composer at the bottom. Simplify message chrome so conversation content, not administrative metadata, owns the main canvas.

Match the references through hierarchy, spacing, typography, surface contrast, border restraint, and state placement. Preserve Snow identity through product copy, the Snow semantic palette, existing model/provider/permission controls, and all current command behavior. Do not add fake projects, hard-coded sessions, decorative actions with no behavior, a second runtime, or a second navigation state owner.

## Reuse

- `Workspace::sessions`, `SessionSummary`, `create_session`, `open_session`, and the existing `sessions_list` RPC projection for sidebar threads.
- `Workspace::project_label`, `ChatState::session_name`, `ChatState::session_id`, and active `SessionBranch` for project/session breadcrumbs.
- Existing `Button`, `Popover`, `Input`, `TextView`, `h_flex`, `v_flex`, and `ScrollHandle` primitives from `gpui-component`; no new dependency.
- Existing `cx.theme()` roles: `background` for the main canvas, `secondary`/`popover` for elevated or sidebar surfaces, `border` for restrained dividers, `foreground` and `muted_foreground` for hierarchy, `primary` for selected/send states, and semantic danger/warning/success for real runtime states.
- Existing composer pickers and actions for provider/model/thinking/permission, attachments, send/stop, slash commands, mentions, skills, and errors.
- Existing transient session/branch `Popover` for detailed branch management; the sidebar is the inventory/switcher, not a replacement branch editor.
- Exemplar: the user-supplied empty-state screenshot for centered hero/composer placement and the active-state screenshot for sparse transcript/bottom-composer placement.

The shell composition is new because the current root is a single vertical flex column and cannot express a persistent rail plus independent main canvas. The new shell belongs in `desktop/src/workspace/view.rs` and should remain local to `Workspace`; do not introduce a repository-wide navigation framework.

## Changes

1. `desktop/src/app.rs::run`
   - Change: increase the default window to approximately 1280×820 and the minimum to a layout-safe size around 900×600 so the two-pane shell opens with the intended proportions while remaining resizable.
   - Preserve: native window creation, appearance observation, one `Workspace`, and platform titlebar behavior.
   - Verify: initial launch shows a useful sidebar and main canvas without clipped controls; minimum size remains operable.

2. `desktop/src/workspace.rs::Workspace` and session completion handling
   - Change: add only the local presentation state needed to collapse/expand the sidebar. Refresh `sessions_list` silently when runtime readiness is established and after session create/open/delete, populate `Workspace::sessions`, and avoid automatically opening the old full-width sessions panel for a silent inventory refresh. Keep `/sessions` able to open the detailed inventory surface deliberately.
   - Preserve: request correlation, active-session protection, delete confirmation, session tree/history loading, provider switching, serial prompts, and command status/error ordering.
   - Verify: the sidebar receives real sessions, switching uses `session_open`, new thread uses `session_create`, deleted/created sessions refresh, background success does not overwrite errors, and `/sessions` still opens its detailed management view.

3. `desktop/src/workspace/view.rs::render_workspace`
   - Change: replace the root single column with an outer horizontal shell. Render a fixed/collapsible sidebar and a `flex_1/min_w(0)` main column. Keep top-level key contexts and action handlers on the shell. Render existing settings/auth/resource/process/subagent/session panels inside the main column below the toolbar so the sidebar does not disappear or duplicate their ownership.
   - Preserve: every existing keybinding/action listener, popover, blocking card, plan review/nudge, scroll behavior, and panel close/open semantics.
   - Verify: sidebar and toolbar remain stable while panels, transcript, and composer change; no panel overflows the minimum viewport; blocking interactions remain reachable.

4. New local sidebar renderer in `desktop/src/workspace/view.rs`
   - Change: render a 280–310 px dark navigation rail with Snow branding, a “New thread” action, one current-project group, real session rows, and compact bottom shortcuts for settings, subagents, processes, and refresh/connection state. Selected session receives a subtle raised/selected background; session titles truncate; timestamps/message counts remain secondary. Include a collapse button connected to local state.
   - Preserve: Snow naming, current project scope, active-session semantics, existing panel/action handlers, and adaptive theme colors.
   - Verify: empty inventory shows a restrained “No threads yet”; active session is distinguishable; clicking another session invokes existing switching; no T3/Open/VS Code branding or fake project behavior appears.

5. `desktop/src/workspace/view.rs::render_top_bar`
   - Change: reduce the top bar to a reference-like breadcrumb on the left (`project / session`) and a small set of real Snow actions on the right: new thread, initialize project when relevant, session/branch popover, and sidebar/layout controls. Move connection/usage/goal telemetry out of the dominant breadcrumb row into the sidebar footer or an existing detailed surface.
   - Preserve: session/branch popover behavior, project/session labels, disabled gates during blocking interactions, plan/default mode access, and all real actions.
   - Verify: the bar remains one line at narrow widths through truncation; controls perform real existing actions; no duplicated status owner is introduced.

6. Empty and active canvas composition in `render_workspace`, `render_transcript`, and `render_empty_state`
   - Change: when there are no conversation messages and no blocking surface, center the project-specific question “What should we build in {project}?” above the composer in the main canvas. When messages exist, give the transcript the flexible middle and render the composer at the bottom. Increase the main content/composer maximum width to fit the wider reference canvas while retaining readable message widths.
   - Preserve: restored history, active streaming, auto-scroll, plan cards, errors, tools, pending attachments, and transition from empty to active after optimistic prompt admission.
   - Verify: empty composer is vertically centered; first submitted message transitions to the active layout without duplicate composers; active composer remains bottom anchored; transcript scrolls independently.

7. `desktop/src/workspace/view.rs::render_message`
   - Change: render user messages as compact, right-aligned charcoal bubbles and assistant messages as sparse, unboxed left-aligned content without a repeated avatar/name header. Keep copy and tool/history affordances visually secondary and available.
   - Preserve: selectable text, Markdown, syntax highlighting, code-copy actions, historical blocks, tool results, streaming markers, and public/private content filtering.
   - Verify: a short user/assistant exchange matches the reference hierarchy; long Markdown/code and restored tool history remain readable and bounded.

8. `desktop/src/workspace/view.rs::render_composer` and input initialization in `desktop/src/workspace.rs`
   - Change: enlarge the rounded composer surface, put the multiline input in the upper area, keep provider/model/thinking/permission controls in a muted bottom row, and keep attachment plus send/stop controls on the right. Use the Snow-specific placeholder “Ask anything, @tag files/folders, $use skills, or / for commands”. Use one clear circular primary send control when admissible and an equally clear stop state during an active turn.
   - Preserve: current input entity, submission semantics, secondary-enter follow-up behavior, auto-grow, paste/image attachments, mention/skill/slash popups, picker state, permission gates, queued follow-ups, and error/activity presentation.
   - Verify: keyboard send/newline/stop behavior is unchanged; popups anchor above the composer; controls remain reachable at minimum width; empty and active placements share the same component.

9. Focused tests in `desktop/src/workspace_tests.rs` and existing pure helpers
   - Change: add structural/state tests for sidebar session inventory presentation state, silent inventory refresh, empty-versus-active canvas selection, and preservation of command/event ordering. Prefer pure helper assertions when GPUI elements cannot be introspected directly.
   - Preserve: all existing RPC integration and workspace behavior tests.
   - Verify: tests fail if background sessions reopen the old panel, if empty and active states select the wrong layout, or if sidebar state breaks session actions.

10. `desktop/README.md` and `desktop/PARITY.md` after implementation
    - Change: record the persistent current-project/session sidebar, responsive shell, centered empty state, active bottom composer, and validation evidence. Mark the older no-sidebar decision as superseded by the user-selected reference direction.
    - Preserve: architecture/security statements and honest limitations.
    - Verify: documentation describes only behavior proven by source and tests.

## Scope

- Inherit: empty/new-thread workspace, active/restored conversations, current-project sessions, all providers/models/themes, native light/dark modes, and existing transient panels rendered inside the main column.
- Verify: 900×600 minimum, default 1280×820, long project/session/model names, zero/many sessions, active streaming, permission/user-input cards, plan review, errors, attachments, slash/mention/skill suggestions, and light appearance despite the dark reference.
- Exclude: multi-project persistence, repository search backend, custom titlebar/window controls, editor integration, Git implementation beyond Snow's existing `/init`, protocol/schema changes, provider changes, TUI changes, and copied proprietary icons/assets.

## Validation

- Product: launch against the fake provider; verify the empty state, create a new thread, send a prompt, observe streaming, switch sessions, open branch management, attach an image, and open settings/process/subagent panels without losing shell navigation.
- Interface: compare native screenshots of empty and active states against the supplied references at default and minimum sizes; verify sidebar width/hierarchy, one-line toolbar, centered empty composer, bottom active composer, sparse assistant message, right user bubble, truncation, selected session, collapse behavior, popovers, blocking cards, and light/dark modes.
- System: confirm all colors derive from the existing GPUI/Snow theme, sessions derive from `sessions_list`, actions reuse existing handlers, and no duplicate runtime/session/navigation database was added.
- Repository: `cargo fmt --manifest-path desktop/Cargo.toml -- --check` → clean; `cargo check --manifest-path desktop/Cargo.toml --all-targets` → success; `cargo test --manifest-path desktop/Cargo.toml` → success; `SNOW_TEST_BINARY="$PWD/snow" cargo test --manifest-path desktop/Cargo.toml --test rpc_integration -- --ignored --test-threads=1` → all real-Snow tests pass; `go test ./...` → success if Go files are touched; `git diff --check` → clean; `./scripts/install-local.sh` → installed local Snow binary; documented debug launch → ready.

## Stop conditions

- Stop if the sidebar requires a second session store or changes append-only session/history semantics; use the existing bounded `sessions_list` projection.
- Stop if the visual work requires changing the Go agent loop, RPC protocol, provider adapters, permission policy, or turn seriality.
- Stop if a decorative reference action has no Snow behavior; omit or relabel it rather than shipping a dead control.
- Stop if the empty-state composition duplicates the composer entity or changes input/submission ownership; render the same composer once in the selected layout.
- Stop if permanent navigation or overlays make a trusted permission/user-input card unreachable; blocking interactions retain presentation and input priority.
- Stop if implementation introduces a new dependency or copies T3 Code/OpenAI/VS Code assets; use existing GPUI primitives, simple glyphs, and Snow branding.

## Design documentation

- After acceptance and native validation: update `desktop/README.md` with the two-pane workspace behavior and `desktop/PARITY.md` with source/test/native-state evidence for sidebar inventory, responsive composition, empty/active composer placement, and preserved session/prompt controls.
