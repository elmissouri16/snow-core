# TUI-to-desktop parity evidence

This is the maintained parity ledger for Snow Desktop. It maps user-visible TUI
capabilities to the native GPUI/RPC implementation and its executable evidence.
A capability is **complete** only when the desktop can invoke the same canonical
runtime behavior and display a bounded, surface-safe result. Transport support
alone is not marked complete when the native presentation is unfinished.

Status values:

- **Complete** — implemented and covered by the listed focused or end-to-end
  evidence.
- **In progress** — RPC/protocol support exists, but a presentation, lifecycle,
  or focused-test gap from the final audit remains.
- **Shared runtime** — intentionally owned by Snow core/config rather than
  duplicated in either presentation surface.

Visual-layout rows cite source, controlled-state, and layout-projection evidence
unless they explicitly say otherwise. This ledger does not claim that native
rendered screenshot comparison was performed.

Keep this file synchronized with `internal/tui`, `desktop/src`, the RPC schemas,
and the required-capability list. The package gate currently requires **29** RPC
capabilities, including `context_report` and bounded `messages_page`; packaging
tests derive the Rust list and test omission of every capability.

## Conversation, rendering, and input

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Serial root prompt lifecycle | `SnowClient` prompt admission plus `turn_done`/`prompt_completed`; `ChatState` keeps one active root prompt | `workspace_tests::turn_done_does_not_finish_prompt`, `fake_completion_removes_empty_assistant_message`; real fake-provider integration | Complete |
| Streaming assistant text | Coalesced `text_delta` batches update one streaming message | `adjacent_streaming_events_are_coalesced`, `streamed_batch_updates_assistant_before_prompt_completion`, `deltas_append_to_one_assistant_message` | Complete |
| Persistent project/thread workspace | `render_workspace` composes a 296 px current-project/session sidebar and flexible main canvas; the sidebar collapses to a 58 px rail while panels remain in the main column | `app.rs::run` 1280×820 default/900×600 minimum; `render_sidebar`/`render_workspace` source and Rust compile coverage | Complete |
| Compact breadcrumb and real workspace actions | One-line project/session/active-branch breadcrumb retains Default/Plan, initialize, new-thread, sidebar, and session/branch handlers; no decorative reference actions or second navigation owner | `render_top_bar`, `toggle_sidebar`, and existing correlated session/branch handlers | Complete |
| Empty/active composer composition | An unblocked empty thread centers one project-specific hero and the shared composer; messages, panels, or blocking state switch to a scrolling transcript with the composer at the bottom | `canvas_layout_centers_only_an_unblocked_empty_thread`; `render_workspace`/`render_empty_state` source | Complete |
| Non-disruptive composer pickers | Provider/model/thinking controls and the mutually exclusive slash/mention/Agent Skill suggestion surface use bounded controlled `Popover`s anchored to their existing triggers; transient visibility does not add layout rows or resize the conversation/composer | `controlled_composer_picker_state_is_idempotent_exclusive_and_search_scoped`, `suggestion_priority_and_controlled_dismissal_are_stable`, `transient_composer_overlays_do_not_change_layout_projection`; `render_composer` controlled-`Popover` source/compile coverage | Complete |
| Sparse conversation hierarchy | User content is a compact right-aligned bubble; assistant Markdown is unboxed and left aligned while streaming, copy, code, and history affordances remain available | `render_message` source; Markdown/code compile coverage | Complete |
| Markdown, fenced-code highlighting, copy | GPUI Markdown transcript with code-block copy actions | Rust view implementation; `cargo test`/`cargo clippy` compile coverage | Complete |
| Copy/select a whole assistant response | Per-block copy exists; one-click whole-response copy remains to be added | View audit | In progress |
| Tool start/progress/end | Bounded contextual tool rows update by call ID | `tool_lifecycle_updates_one_record`; mock streaming integration | Complete |
| Root vs child activity | Agent-attributed events become bounded child activity and never root transcript deltas | client protocol tests and workspace status rendering | Complete |
| Abort/Stop | One correlated abort while active or blocked; repeat Stop disabled until completion | `abort_pending_disables_repeated_stop_requests`; integration interaction tests | Complete |
| Active-turn steer | Enter while busy sends typed `steer`; pending counts shown | workspace steer path, `active_input` capability | Complete |
| Queued follow-up | Option+Enter while busy sends typed `follow_up`; pending counts shown | workspace follow-up path, `pending_inputs` capability | Complete |
| Auto-growing multiline editor | GPUI input supports newline shortcut; exact TUI textarea behavior/height is not identical | view/manual behavior | In progress |
| Command completion | Searchable `/` catalog with aligned command/description rows, explicit keyboard selection, and local/RPC dispatch | `slash_selection_matches_navigates_wraps_and_dismisses`, `slash_selection_normalizes_after_filtering_shortens_results`, `desktop_slash_selection_never_navigates_hidden_rows`, and command parser tests | Complete |
| Exact shell-like aliases/subcommand completion | Primary commands and `/q` work; full TUI alias/subcommand suggestion fidelity is still being expanded | command audit | In progress |
| Unknown-command safety | Unknown slash commands fail locally and do not become prompts | command parser tests | Complete |
| RPC command-result presentation | Correlated command responses update bounded native panels/state/status and never become generic JSON conversation messages | `command_completions_do_not_enter_the_conversation`; semantic resource projection tests | Complete |

## Collaboration modes, plans, and goals

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Default/Plan mode switch | Header mode control and `/plan`/`/default` call canonical `set_mode` | command mutation tests; runtime refresh correlation tests | Complete |
| One-shot Plan prompt | `/plan <text>` sends an atomic prompt with `mode: plan` | `commands.rs::plan_with_text_is_an_atomic_mode_prompt` | Complete |
| Plan keyword nudge | Default-mode composer detects the bounded `plan` word and offers Plan mode/dismissal | view implementation; compile coverage | Complete |
| Plan-to-implementation boundary | Plan deltas are retained; completion offers implement here, clear context and implement, or stay in Plan mode | workspace/view implementation | Complete |
| Goals create/edit/replace/pause/resume/clear | Typed `/goal` commands and header goal status use canonical goal RPC | `goal_budget_is_validated_and_encoded`; RPC runtime command tests | Complete |
| Goal budget | Positive token budget parsed and encoded as an integer | `goal_budget_is_validated_and_encoded` | Complete |

## Permission, user input, trust, and initialization

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Ask-mode permission card | Trusted host-only card shows bounded tool/risk/reason/path/arguments | `trusted_interaction_previews_are_bounded`; mock interaction integration | Complete |
| Allow once/session/always and deny | Buttons and `/allow once|session|always`, `/deny` preserve exact decision scope | `permission_card_supports_all_four_decisions_and_waits_for_exact_ack`; `permission_commands_preserve_decision_scope` | Complete |
| Permission correlation/fail closed | Host command and Snow request IDs must both match; malformed/overflow requests reject and stop safely | interaction queue/correlation/malformed tests; integration rejection tests | Complete |
| Model-requested questions | Ordered questions, options/descriptions, Other drafts, previous/next, reject | user-input draft/navigation/order tests; mock integration | Complete |
| Permission mode get/set | Settings and `/permissions [ask|allow|deny]` use canonical RPC | command/settings RPC tests | Complete |
| Project trust get/set | `/trust` uses RPC `params.level` and canonical trust store | `commands.rs::trust_uses_the_rpc_level_field`; Go RPC tests | Complete |
| Project initialization | `/init` uses the prompt lifecycle and shared embedded prompt | protocol encoding test; Go app/RPC tests | Complete |

## Providers, models, authentication, and settings

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Provider selection/restart | Searchable picker uses Snow's bounded `auth_providers` inventory, shuts down/reaps old Snow, preserves the canonical session path, and restores state | provider replacement/state generation tests; child-reaping integration | Complete |
| Internal `fake` presentation boundary | Snow's credential-free `fake` provider remains available to the internal runtime and conformance tests, but its provider/model IDs are omitted from desktop selectors and replaced by neutral chooser labels; unknown non-test/custom providers remain visible | `provider_catalog::tests::fake_provider_is_filtered_whether_explicit_or_synthesized`, `provider_catalog::tests::unknown_non_test_provider_remains_visible_and_uses_raw_fallback`, `workspace_tests::fake_runtime_uses_neutral_model_presentation_until_a_real_provider_is_active` | Complete |
| Live model catalog/manual ID | Search by name/ID/provider; bounded manual IDs; correlated selection | picker/manual model tests; model integration tests | Complete |
| Thinking effort | Model-aware levels, Off fallback, atomic model+thinking change | thinking/model correlation tests and mock integration | Complete |
| Reasoning summary/text verbosity | Native settings controls call typed response-control RPC | settings RPC tests and panel compile coverage | Complete |
| Model privacy, limits, vision, price, upgrade metadata | Protocol decodes the complete safe model metadata; picker/settings presentation still needs the final rich metadata card | `protocol.rs::session_and_model_metadata_preserve_thinking_capabilities`; protocol rich-metadata tests | In progress |
| Provider login methods | `/login` panel lists provider methods, profiles, masked secrets, and browser/device progress | auth RPC tests; panel/client compile coverage | Complete |
| Device URL and user-code visibility | Progress renders URL and user code separately with independent copy actions | auth panel implementation | Complete |
| Logout/profile selection | `/logout` uses provider/profile selection and correlated Snow confirmation | auth app/RPC tests; command sensitive-data test | Complete |
| Canonical settings snapshot/update | `/settings` reads Snow state and updates model/thinking/response controls/permission/debug/concurrency | Go settings RPC tests; desktop settings panel | Complete |
| Restart-required reporting | Settings response displays Snow's restart-required signal | panel implementation and Go settings tests | Complete |
| Startup permission/thinking defaults | Desktop defers to Snow config; strict `SNOW_PERMISSION`/`SNOW_THINKING` are opt-in overrides | `rpc_integration::explicit_permission_and_thinking_overrides_are_forwarded`; process tests | Complete |

## Sessions, branches, history, and compaction

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Durable session and exact path continuity | Canonical `session_info.path` is pinned over provider replacement/relaunch | session path/restart tests; real persistent-history integration | Complete |
| Project session inventory | Persistent current-project sidebar and `/sessions` panel share the bounded `sessions_list` projection; fixed-height GPUI uniform lists render one visible range per frame instead of remeasuring variable rows or re-entering the workspace once per item; silent readiness/mutation refreshes do not force the panel open or erase an existing error | desktop compile coverage; `silent_session_inventory_refresh_updates_sidebar_without_opening_management_panel`, `silent_session_inventory_completion_preserves_existing_error`; Go inventory tests | Complete |
| New/open/delete session | Sidebar **New thread**/thread rows and the detailed panel call canonical `session_create`/`session_open`; panel deletion is confirmed and active sessions stay protected | real session create/open/delete integration; Go session command tests; desktop parser safety test | Complete |
| Safe resume semantics | Desktop rejects path-like/free-form resume identifiers and accepts one bounded RPC session ID | `commands.rs::resume_accepts_only_one_bounded_rpc_session_id` | Complete |
| Current branch list/switch/fork | Header control opens a bounded anchored card without resizing the conversation; switch/fork use correlated branch/session RPC | branch correlation tests; real branch integration; controlled Popover compile coverage | Complete |
| Session rename | Correlated normalized title, idle-only | `session_rename_uses_correlated_normalized_title`; mock integration | Complete |
| Branch rename/delete | Native branch popover controls call correlated rename/delete RPC, gate delete to eligible leaf branches, and require confirmation; focused workspace-level lifecycle coverage remains | real branch mutation integration and Go session RPC tests; focused native audit still required | In progress |
| Restored text/plan history | Surface-safe history restore replaces transcript only after generation-consistent state load | restored message/generation tests; real restart integration | Complete |
| Restored historical images/tool activity | Protocol validates/decodes bounded public image and tool display blocks; the workspace pairs tool results with matching calls, coalesces consecutive tool-only messages into one collapsed activity disclosure, and exposes lightweight rows with icon-only copy actions plus fixed-height internally scrolled details | `typed_history_preserves_safe_images_and_tool_cards`, `restored_tool_results_join_their_tool_call_card`, `consecutive_tool_messages_render_and_invalidate_as_one_activity_row`, `collapsed_tool_card_summaries_are_single_line_and_bounded`, rejection/bounds tests | Complete |
| Private continuity/thinking exclusion | Decoder exposes neither provider-private continuity nor private thinking | protocol history tests/security review | Complete |
| Compaction | `/compact` invokes canonical compaction and keeps exact session history append-only | Go RPC/app tests; desktop command dispatch | Complete |
| Context composition report | `/context` uses public `context_report` DTO and dedicated required capability | Go context-report RPC/schema tests; package omission tests | Complete |

## Images and multimodal prompts

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Attach image file | Content-sniffed PNG/JPEG/GIF/WebP with regular-file, byte, dimension, count, and aggregate bounds | attachment sniff/file/count/aggregate tests | Complete |
| Paste clipboard image | macOS clipboard or bounded Linux `wl-paste`/`xclip` helpers | clipboard preference/timeout/output tests | Complete |
| Review/remove pending images | Composer chips plus `/attachments` and `/detach` | attachment parser/collection tests | Complete |
| Multimodal RPC encoding | Typed image content blocks with bounded base64 serialization | `attachment_serializes_as_rpc_base64_content_block`; `multimodal_prompts` capability | Complete |
| Clear only after admission | Pending images survive local/send failure and clear on successful prompt admission | workspace lifecycle implementation; focused admission test should be added | In progress |

## Runtime resources and diagnostics

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Usage | `/usage` displays bounded token/cost data from canonical RPC | Go usage RPC tests; resource panel dispatch | Complete |
| Diagnostics status/on/off/clear/dump | `/debug` typed commands and settings debug toggle | command parser and Go diagnostics RPC tests | Complete |
| Live diagnostic tail/level controls | Command results exist, but TUI-equivalent live diagnostic pane/filter controls are not complete | final parity audit | In progress |
| Managed process list/logs/stop | `/processes` panel lists state, opens bounded logs, and stops by ID | Go managed-process RPC tests; desktop panel | Complete |
| Live process polling/detail richness | Manual refresh/mutation refresh exists; periodic state/log polling is still being added | final parity audit | In progress |
| Subagent fleet/list/details | `/agent [path]` panel displays bounded child state and fleet summary | Go subagent RPC tests; desktop panel | Complete |
| Interrupt/resume/close child | Native child actions call correlated RPC then refresh | Go subagent RPC tests; desktop completion dispatch | Complete |
| Subagent concurrency | `/agent concurrency [N]` reads/updates canonical settings without shadow config | `agent_concurrency_uses_settings_rpc_without_losing_fleet_filter`; Go settings tests | Complete |
| Per-role subagent model settings | RPC exposes investigator/builder/reviewer model settings; the native per-role controls are not complete | `subagent_models` capability and settings DTO tests | In progress |
| Live subagent polling/full detail | Event-driven status exists; periodic refresh and all TUI detail fields remain to finish | final parity audit | In progress |
| MCP server status | `/mcp` displays canonical bounded server status | Go MCP RPC tests; resource panel | Complete |
| Agent Skills list/clear | `/skills` and `/skills clear` use active-session canonical state; files/global config are not deleted | Go context/skills RPC tests; desktop command dispatch | Complete |
| Plugin execution/config | Desktop no longer disables plugins; execution/config stays in shared Snow runtime | process launch tests and Snow core tests | Shared runtime |
| Dedicated plugin manager | Neither parity requirement nor current desktop scope; no native manager | product scope | Shared runtime |

## Keyboard, themes, lifecycle, and packaging

| TUI capability | Desktop implementation | Evidence | Status |
| --- | --- | --- | --- |
| Send/newline/follow-up/Stop shortcuts | Enter, Option+Enter, Escape, picker arrows, and Stop are documented by `/keybindings` | desktop key action wiring; compile coverage | Complete |
| User-remappable semantic keybindings | Settings loads the complete supported semantic action inventory, edits/resets global and trusted-project layers through typed RPC, replaces the live GPUI keymap, and generates effective `/help` text | `keybindings::inventory_exactly_matches_tui_semantic_actions_and_defaults`, validation/collision/runtime-plan tests, `presentation_runtime::rpc_inventory_must_be_complete_and_unique` | Complete |
| Theme selection/system synchronization | Settings lists bounded Snow built-in/global/project themes and persists selection through Snow; adaptive semantic colors apply to GPUI, while desktop-owned System follows OS changes and Light/Dark stay pinned | `theme_palette` projection/adaptive-color tests, `presentation_runtime::semantic_palette_mapping_keeps_all_seven_roles_distinct`, `appearance::explicit_modes_do_not_follow_system_appearance`; window appearance observer | Complete |
| Process teardown | Close stdin, bounded wait, force-stop/reap fallback; provider replacement reaps old child | child-reaping integration and shutdown unit tests | Complete |
| Protocol bounds/correlation | 16 MiB frames, bounded requests/channels/stderr, generation/request correlation | protocol/client/state tests | Complete |
| Real Snow conformance | Fake provider prompt, persistent history after restart, and branch mutation use a built Snow binary | ignored `rpc_integration` real-process tests | Complete |
| Package compatibility | RPC v1 plus every required desktop capability, binary format/arch, reproducible manifest/checksum | `desktop/scripts/tests/test_packaging.py`, including one omission test per capability | Complete |
| Linux/macOS relocatable packages | Linux tar and macOS app ZIP; bundled Snow remains separate external executable | packaging unit tests and archive verifier | Complete |
| Official publishing/update channel | No publication, notarization, package-manager feed, or updater while release gates are disabled | release policy | In progress |

## Required verification before marking an item complete

Run from the repository root unless noted:

```sh
go test ./...
go vet ./...
go build -o ./snow ./cmd/snow

(cd desktop && cargo fmt --check)
(cd desktop && cargo check --all-targets)
(cd desktop && cargo test)
(cd desktop && cargo clippy --all-targets -- -D warnings)
(cd desktop && SNOW_TEST_BINARY="$PWD/../snow" \
  cargo test --test rpc_integration -- --ignored --test-threads=1)

python3 -m unittest discover \
  -s desktop/scripts/tests -p 'test_*.py' -v
```

For a parity change, add focused tests at the closest layer and update the
matching row here. Do not mark an item complete solely because a generic RPC
command can be sent.
