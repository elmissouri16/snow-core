use std::{collections::HashSet, path::PathBuf, time::Duration};

use super::*;
use crate::snow::{
    BranchCatalog, ModelCatalog, RpcReady, SessionBranch, SessionInfo, SessionList, SessionSummary,
};

fn ready() -> RuntimeEvent {
    RuntimeEvent::Ready(RpcReady {
        protocol_version: "1".into(),
        snow_version: "test".into(),
        capabilities: HashSet::new(),
        max_input_bytes: 1024,
    })
}

fn make_ready(state: &mut ChatState) {
    state.apply(ready());
    state.begin_runtime_load("ready-load".into(), true);
    state.apply(RuntimeEvent::SessionLoaded {
        generation: "ready-load".into(),
        info: SessionInfo {
            session_id: "session-1".into(),
            name: "Desktop proof".into(),
            path: "/tmp/session.db".into(),
            provider: "fake".into(),
            model: "fake-1".into(),
            cwd: "/tmp/snow-core".into(),
            thinking: "off".into(),
            thinking_levels: vec!["off".into(), "low".into(), "high".into()],
            ..SessionInfo::default()
        },
    });
    state.apply(RuntimeEvent::HistoryLoaded {
        generation: "ready-load".into(),
        history: Vec::new(),
    });
    state.apply(RuntimeEvent::ModelsLoaded {
        generation: "ready-load".into(),
        catalog: ModelCatalog {
            provider: "fake".into(),
            current: "fake-1".into(),
            models: vec![
                ModelInfo {
                    provider: "fake".into(),
                    id: "fake-1".into(),
                    display_name: "Fake One".into(),
                    supports_thinking: true,
                    default_thinking: "medium".into(),
                    thinking_levels: vec!["low".into(), "medium".into(), "high".into()],
                    ..ModelInfo::default()
                },
                ModelInfo {
                    provider: "fake".into(),
                    id: "fake-2".into(),
                    display_name: "Fake Two".into(),
                    supports_thinking: false,
                    default_thinking: String::new(),
                    thinking_levels: Vec::new(),
                    ..ModelInfo::default()
                },
            ],
        },
    });
    state.apply(RuntimeEvent::BranchesLoaded {
        generation: "ready-load".into(),
        catalog: BranchCatalog {
            branches: vec![SessionBranch {
                id: "main".into(),
                name: "Main".into(),
                tip_id: "entry-1".into(),
                messages: 0,
                created_at: 1,
                updated_at: 1,
                active: true,
                ..SessionBranch::default()
            }],
        },
    });
}

#[test]
fn discovered_session_path_is_reused_by_provider_replacement() {
    let mut config = RuntimeConfig {
        executable: PathBuf::from("snow"),
        project_root: PathBuf::from("/tmp/project"),
        provider: "fake".into(),
        model: None,
        permission: None,
        thinking: None,
        session_path: None,
        no_session: false,
        startup_timeout: Duration::from_secs(1),
        shutdown_timeout: Duration::from_secs(1),
        max_frame_bytes: 1024,
    };
    apply_runtime_config_event(
        &mut config,
        &RuntimeEvent::SessionLoaded {
            generation: "config-load".into(),
            info: SessionInfo {
                session_id: "session-1".into(),
                name: String::new(),
                path: "/private/sessions/session-1.db".into(),
                provider: "fake".into(),
                model: "fake-1".into(),
                cwd: "/tmp/snow-core".into(),
                thinking: "off".into(),
                thinking_levels: vec!["off".into()],
                ..SessionInfo::default()
            },
        },
    );

    let replacement = replacement_provider_config(&config, "opencode-zen");
    assert_eq!(replacement.provider, "opencode-zen");
    assert_eq!(
        replacement.session_path,
        Some(PathBuf::from("/private/sessions/session-1.db"))
    );
    assert!(!replacement.no_session);
    assert_eq!(replacement.model, None);
    assert_eq!(replacement.thinking.as_deref(), Some("off"));
}

#[test]
fn runtime_state_discovers_the_effective_provider_without_a_cli_override() {
    let mut config = RuntimeConfig {
        executable: PathBuf::from("snow"),
        project_root: PathBuf::from("/tmp/project"),
        provider: String::new(),
        model: None,
        permission: None,
        thinking: None,
        session_path: None,
        no_session: true,
        startup_timeout: Duration::from_secs(1),
        shutdown_timeout: Duration::from_secs(1),
        max_frame_bytes: 1024,
    };

    apply_runtime_config_event(
        &mut config,
        &RuntimeEvent::ModelsLoaded {
            generation: "config-load".into(),
            catalog: ModelCatalog {
                provider: " openai ".into(),
                current: "gpt-5".into(),
                models: Vec::new(),
            },
        },
    );

    assert_eq!(config.provider, "openai");
    assert_eq!(config.model.as_deref(), Some("gpt-5"));
}

#[test]
fn provider_switch_retains_conversation_until_history_is_restored() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.messages.push(ChatMessage {
        role: ChatRole::User,
        text: "old context".into(),
        presentation_text: "old context".into(),
        streaming: false,
        history_blocks: Vec::new(),
        history_tool_results: Vec::new(),
        render_id: 999,
    });
    state.tools.push(ToolActivity {
        call_id: "t1".into(),
        name: "read".into(),
        status: "Completed".into(),
        state: ToolState::Completed,
    });

    assert!(state.can_switch_provider());
    state.begin_provider_switch("opencode-zen");

    assert!(matches!(state.connection, ConnectionState::Stopping));
    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.tools.len(), 1);
    assert!(!state.can_send());
    assert!(!state.can_switch_provider());
    assert_eq!(state.status_text, "Switching to opencode-zen…");

    apply_runtime_batch(&mut state, vec![ready()], &HashSet::new());
    state.begin_runtime_load("replacement-load".into(), true);
    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.tools.len(), 1);

    apply_runtime_batch(
        &mut state,
        vec![RuntimeEvent::HistoryLoaded {
            generation: "replacement-load".into(),
            history: vec![HistoryEntry {
                role: "user".into(),
                blocks: vec![HistoryBlock::Text {
                    text: "restored context".into(),
                }],
                tool_result: None,
            }],
        }],
        &HashSet::new(),
    );
    assert_eq!(state.messages[0].text, "restored context");
    assert!(state.tools.is_empty());
}

#[test]
fn active_prompt_prevents_provider_switch() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    assert!(!state.can_switch_provider());
}

#[test]
fn composer_stays_editable_while_a_new_session_runtime_loads() {
    let mut state = ChatState::default();
    assert!(!state.can_edit_composer());

    state.apply(ready());
    state.begin_runtime_load("new-session".into(), true);
    assert!(state.can_edit_composer());
    assert!(!state.can_send());

    make_ready(&mut state);
    state.begin_session_action(
        PendingSessionAction::Select {
            request_id: "session-change".into(),
        },
        "Opening thread…".into(),
    );
    assert!(state.can_edit_composer());
    assert!(!state.can_send());
}

#[test]
fn composer_accepts_steering_text_while_a_prompt_is_active() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("prompt-1".into(), "start".into());

    assert!(state.can_edit_composer());
    assert!(!state.can_send());
    assert!(state.can_abort());
}

#[test]
fn actions_wait_for_all_runtime_state_responses() {
    let mut state = ChatState::default();
    state.apply(ready());
    state.begin_runtime_load("partial-load".into(), true);
    state.apply(RuntimeEvent::SessionLoaded {
        generation: "partial-load".into(),
        info: SessionInfo {
            session_id: "session-1".into(),
            name: String::new(),
            path: "/tmp/session.db".into(),
            provider: "fake".into(),
            model: "fake-1".into(),
            cwd: "/tmp/snow-core".into(),
            thinking: "off".into(),
            thinking_levels: vec!["off".into(), "low".into(), "high".into()],
            ..SessionInfo::default()
        },
    });
    assert!(!state.can_send());

    state.apply(RuntimeEvent::ModelsLoaded {
        generation: "partial-load".into(),
        catalog: ModelCatalog {
            provider: "fake".into(),
            current: "fake-1".into(),
            models: Vec::new(),
        },
    });
    assert!(!state.can_send());

    state.apply(RuntimeEvent::HistoryLoaded {
        generation: "partial-load".into(),
        history: Vec::new(),
    });
    assert!(!state.can_send());

    state.apply(RuntimeEvent::BranchesLoaded {
        generation: "partial-load".into(),
        catalog: BranchCatalog {
            branches: Vec::new(),
        },
    });
    assert!(state.can_send());
}

#[test]
fn stale_runtime_generation_cannot_mix_restored_state() {
    let mut state = ChatState::default();
    state.apply(ready());
    state.begin_runtime_load("latest".into(), true);
    state.apply(RuntimeEvent::SessionLoaded {
        generation: "stale".into(),
        info: SessionInfo {
            session_id: "wrong-session".into(),
            name: "Wrong".into(),
            path: "/tmp/wrong.db".into(),
            cwd: "/tmp/wrong".into(),
            provider: "fake".into(),
            model: "wrong-model".into(),
            thinking: "off".into(),
            thinking_levels: vec!["off".into()],
            ..SessionInfo::default()
        },
    });
    state.apply(RuntimeEvent::BranchesLoaded {
        generation: "stale".into(),
        catalog: BranchCatalog {
            branches: vec![SessionBranch {
                id: "wrong".into(),
                tip_id: String::new(),
                active: true,
                ..SessionBranch::default()
            }],
        },
    });

    assert!(state.session_id.is_empty());
    assert!(state.branches.is_empty());
    assert!(!state.runtime_loaded);
}

#[test]
fn session_invalidation_is_deferred_until_the_prompt_boundary() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("prompt-1".into(), "hello".into());
    state.apply(RuntimeEvent::SessionStateInvalidated);
    assert!(state.session_metadata_stale);
    assert!(state.active_prompt.is_some());

    state.apply(RuntimeEvent::PromptCompleted(PromptCompleted {
        request_id: "prompt-1".into(),
        status: PromptStatus::Completed,
        error: None,
    }));
    assert!(state.session_metadata_stale);
    assert!(state.active_prompt.is_none());
}

#[test]
fn completed_plan_turn_becomes_reviewable() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("plan-1".into(), "plan this change".into());
    state.apply(RuntimeEvent::PlanDelta {
        text: "1. Inspect\n".into(),
    });
    state.apply(RuntimeEvent::PlanDelta {
        text: "2. Implement".into(),
    });

    assert_eq!(state.latest_plan, "1. Inspect\n2. Implement");
    assert_eq!(state.messages[1].text, state.latest_plan);
    assert!(state.plan_received_this_turn);
    assert!(!state.plan_review_ready);

    state.apply(RuntimeEvent::PromptCompleted(PromptCompleted {
        request_id: "plan-1".into(),
        status: PromptStatus::Completed,
        error: None,
    }));

    assert!(state.plan_review_ready);
    assert_eq!(state.latest_plan, "1. Inspect\n2. Implement");
}

#[test]
fn non_plan_turn_clears_stale_plan_review_state() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.latest_plan = "stale".into();
    state.plan_received_this_turn = true;
    state.plan_review_ready = true;

    state.begin_prompt("default-1".into(), "implement".into());

    assert!(state.latest_plan.is_empty());
    assert!(!state.plan_received_this_turn);
    assert!(!state.plan_review_ready);
}

#[test]
fn runtime_state_failure_allows_provider_retry() {
    let mut state = ChatState::default();
    state.apply(ready());
    state.begin_runtime_load("failed-load".into(), true);
    state.apply(RuntimeEvent::RuntimeStateFailed {
        generation: "failed-load".into(),
        command: "messages_list".into(),
        error: "history unavailable".into(),
    });

    assert!(matches!(state.connection, ConnectionState::Failed(_)));
    assert!(!state.can_send());
    assert!(state.can_switch_provider());
    assert_eq!(state.status_text, "Could not load messages_list");
}

#[test]
fn model_change_blocks_actions_until_confirmation() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    assert!(state.can_switch_model());
    assert!(!state.can_select_model("fake-1"));
    assert!(state.can_select_model("fake-2"));
    assert!(state.can_select_model("unknown")); // Valid manual model ID.

    state.begin_model_change("model-1".into(), "fake-2", "off".into());
    assert!(!state.can_send());
    assert!(!state.can_switch_provider());
    assert_eq!(state.status_text, "Selecting fake-2…");

    state.apply(RuntimeEvent::ModelChanged("unexpected".into()));
    state.apply(RuntimeEvent::ModelChangeConfirmed {
        request_id: "other".into(),
    });
    assert!(!state.can_send());
    assert_eq!(state.current_model, "fake-1");
    state.apply(RuntimeEvent::ModelChangeConfirmed {
        request_id: "model-1".into(),
    });
    assert_eq!(state.current_model, "fake-2");
    assert!(state.can_send());
    assert!(state.can_switch_provider());
}

#[test]
fn thinking_change_is_model_aware_and_correlated() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    assert_eq!(state.current_thinking, "off");
    assert_eq!(state.thinking_levels, ["off", "low", "high"]);
    assert!(state.can_select_thinking("high"));
    assert!(!state.can_select_thinking("ultra"));

    state.begin_thinking_change("thinking-1".into(), "high".into());
    assert!(!state.can_send());
    assert!(!state.can_switch_provider());

    state.apply(RuntimeEvent::ThinkingChanged {
        request_id: "other".into(),
    });
    assert_eq!(state.current_thinking, "off");
    assert!(!state.can_send());

    state.apply(RuntimeEvent::ThinkingChanged {
        request_id: "thinking-1".into(),
    });
    assert_eq!(state.current_thinking, "high");
    assert!(state.can_send());
}

#[test]
fn rejected_thinking_change_retains_previous_level() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_thinking_change("thinking-1".into(), "high".into());
    state.apply(RuntimeEvent::RequestRejected {
        request_id: Some("thinking-1".into()),
        error: "unsupported".into(),
    });

    assert_eq!(state.current_thinking, "off");
    assert!(state.can_send());
    assert_eq!(state.last_error.as_deref(), Some("unsupported"));
}

#[test]
fn branch_selection_is_correlated_and_blocks_actions() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_session_action(
        PendingSessionAction::Select {
            request_id: "branch-1".into(),
        },
        "Switching branch…".into(),
    );

    assert!(!state.can_send());
    assert!(!state.can_switch_provider());
    state.apply(RuntimeEvent::BranchSelected {
        request_id: "other".into(),
    });
    assert!(state.session_action_pending.is_some());

    state.reset_runtime_load();
    state.apply(RuntimeEvent::BranchSelected {
        request_id: "branch-1".into(),
    });
    assert!(state.session_action_pending.is_none());
    assert!(!state.can_send());
    assert_eq!(state.status_text, "Restoring branch…");
}

#[test]
fn session_rename_uses_correlated_normalized_title() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_session_action(
        PendingSessionAction::Rename {
            request_id: "rename-1".into(),
        },
        "Renaming session…".into(),
    );

    state.apply(RuntimeEvent::SessionRenamed {
        request_id: "other".into(),
        session_id: "session-1".into(),
        name: "Wrong".into(),
    });
    assert_eq!(state.session_name, "Desktop proof");
    state.apply(RuntimeEvent::SessionRenamed {
        request_id: "rename-1".into(),
        session_id: "session-1".into(),
        name: "API cleanup".into(),
    });
    assert_eq!(state.session_name, "API cleanup");
    assert!(state.can_send());
}

#[test]
fn rejected_session_action_clears_only_matching_request() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_session_action(
        PendingSessionAction::Fork {
            request_id: "fork-1".into(),
        },
        "Forking branch…".into(),
    );
    state.apply(RuntimeEvent::RequestRejected {
        request_id: Some("other".into()),
        error: "stale".into(),
    });
    assert!(state.session_action_pending.is_some());

    state.apply(RuntimeEvent::RequestRejected {
        request_id: Some("fork-1".into()),
        error: "session busy".into(),
    });
    assert!(state.session_action_pending.is_none());
    assert!(state.can_send());
    assert_eq!(state.last_error.as_deref(), Some("session busy"));
}

#[test]
fn model_change_falls_back_to_compatible_thinking() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.current_thinking = "high".into();
    state.begin_model_change("model-1".into(), "fake-2", "off".into());
    state.apply(RuntimeEvent::ModelChangeConfirmed {
        request_id: "model-1".into(),
    });

    assert_eq!(state.current_thinking, "off");
    assert_eq!(state.thinking_levels, ["off"]);
    assert!(!state.can_switch_thinking());
}

#[test]
fn paged_history_restores_incrementally_and_gates_sending_until_complete() {
    let mut state = ChatState::default();
    state.apply(ready());
    state.begin_runtime_load("paged-load".into(), true);
    state.apply(RuntimeEvent::SessionLoaded {
        generation: "paged-load".into(),
        info: SessionInfo {
            session_id: "session-1".into(),
            model: "fake-1".into(),
            thinking: "off".into(),
            thinking_levels: vec!["off".into()],
            ..SessionInfo::default()
        },
    });
    state.apply(RuntimeEvent::ModelsLoaded {
        generation: "paged-load".into(),
        catalog: ModelCatalog {
            provider: "fake".into(),
            current: "fake-1".into(),
            models: Vec::new(),
        },
    });
    state.apply(RuntimeEvent::BranchesLoaded {
        generation: "paged-load".into(),
        catalog: BranchCatalog::default(),
    });

    state.apply(RuntimeEvent::HistoryPageLoaded {
        generation: "paged-load".into(),
        history: vec![HistoryEntry {
            role: "assistant".into(),
            blocks: vec![HistoryBlock::ToolCall(HistoryToolCall {
                tool_call_id: "call-across-pages".into(),
                name: "read".into(),
                arguments_display: "README.md".into(),
            })],
            tool_result: None,
        }],
        start: 0,
        next_start: 1,
        total: 2,
        complete: false,
    });

    assert_eq!(state.messages.len(), 1);
    assert!(state.messages[0].history_tool_results.is_empty());
    assert!(!state.runtime_loaded);
    assert!(!state.can_send());

    state.apply(RuntimeEvent::HistoryPageLoaded {
        generation: "paged-load".into(),
        history: vec![HistoryEntry {
            role: "tool_result".into(),
            blocks: Vec::new(),
            tool_result: Some(HistoryToolResult {
                tool_call_id: "call-across-pages".into(),
                tool_name: "read".into(),
                is_error: false,
                display: HistoryToolDisplay {
                    output: "# Snow".into(),
                    ..HistoryToolDisplay::default()
                },
            }),
        }],
        start: 1,
        next_start: 2,
        total: 2,
        complete: true,
    });

    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.messages[0].history_tool_results.len(), 1);
    assert_eq!(
        state.messages[0].history_tool_results[0].display.output,
        "# Snow"
    );
    assert!(state.runtime_loaded);
    assert!(state.can_send());
}

#[test]
fn stale_or_out_of_order_history_pages_cannot_corrupt_restored_state() {
    let mut state = ChatState::default();
    state.apply(ready());
    state.begin_runtime_load("latest".into(), true);

    assert!(!state.accepts_history_page("stale", 0, 1, 1, true));
    state.apply(RuntimeEvent::HistoryPageLoaded {
        generation: "stale".into(),
        history: vec![HistoryEntry {
            role: "assistant".into(),
            blocks: vec![HistoryBlock::Text {
                text: "stale".into(),
            }],
            tool_result: None,
        }],
        start: 0,
        next_start: 1,
        total: 1,
        complete: true,
    });
    assert!(state.messages.is_empty());
    assert!(matches!(state.connection, ConnectionState::Ready { .. }));

    assert!(!state.accepts_history_page("latest", 1, 2, 2, true));
    state.apply(RuntimeEvent::HistoryPageLoaded {
        generation: "latest".into(),
        history: vec![HistoryEntry {
            role: "assistant".into(),
            blocks: vec![HistoryBlock::Text {
                text: "out of order".into(),
            }],
            tool_result: None,
        }],
        start: 1,
        next_start: 2,
        total: 2,
        complete: true,
    });
    assert!(state.messages.is_empty());
    assert!(matches!(state.connection, ConnectionState::Failed(_)));
    assert_eq!(state.status_text, "Could not restore session history");
}

#[test]
fn restored_tool_results_join_their_tool_call_card() {
    let mut state = ChatState::default();
    state.restore_history(vec![
        HistoryEntry {
            role: "assistant".into(),
            blocks: vec![HistoryBlock::ToolCall(HistoryToolCall {
                tool_call_id: "call-1".into(),
                name: "read".into(),
                arguments_display: "{\n  \"path\": \"README.md\"\n}".into(),
            })],
            tool_result: None,
        },
        HistoryEntry {
            role: "tool_result".into(),
            blocks: Vec::new(),
            tool_result: Some(HistoryToolResult {
                tool_call_id: "call-1".into(),
                tool_name: "read".into(),
                is_error: false,
                display: HistoryToolDisplay {
                    started: true,
                    output: "# Snow".into(),
                    duration_ms: 12,
                    ..HistoryToolDisplay::default()
                },
            }),
        },
    ]);

    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.messages[0].role, ChatRole::Assistant);
    assert!(matches!(
        state.messages[0].history_blocks.as_slice(),
        [HistoryBlock::ToolCall(tool)] if tool.tool_call_id == "call-1"
    ));
    assert_eq!(state.messages[0].history_tool_results.len(), 1);
    assert_eq!(
        state.messages[0].history_tool_results[0].display.output,
        "# Snow"
    );
}

#[test]
fn delayed_restored_tool_results_pair_with_indexed_calls() {
    const CALL_COUNT: usize = 2_000;
    let mut history = Vec::with_capacity(CALL_COUNT * 2);
    for index in 0..CALL_COUNT {
        history.push(HistoryEntry {
            role: "assistant".into(),
            blocks: vec![HistoryBlock::ToolCall(HistoryToolCall {
                tool_call_id: format!("call-{index}"),
                name: "read".into(),
                arguments_display: String::new(),
            })],
            tool_result: None,
        });
    }
    for index in 0..CALL_COUNT {
        history.push(HistoryEntry {
            role: "tool_result".into(),
            blocks: Vec::new(),
            tool_result: Some(HistoryToolResult {
                tool_call_id: format!("call-{index}"),
                tool_name: "read".into(),
                is_error: false,
                display: HistoryToolDisplay::default(),
            }),
        });
    }

    let mut state = ChatState::default();
    state.restore_history(history);

    assert_eq!(state.messages.len(), CALL_COUNT);
    assert!(
        state
            .messages
            .iter()
            .all(|message| message.history_tool_results.len() == 1)
    );
}

#[test]
fn hydrated_input_history_reads_only_the_latest_bounded_inputs() {
    let history = (0..250)
        .map(|index| HistoryEntry {
            role: "user".into(),
            blocks: vec![HistoryBlock::Text {
                text: format!("prompt-{index}"),
            }],
            tool_result: None,
        })
        .collect::<Vec<_>>();

    let inputs = hydrated_input_history(&history);

    assert_eq!(inputs.len(), INPUT_HISTORY_LIMIT);
    assert_eq!(inputs.front().map(String::as_str), Some("prompt-150"));
    assert_eq!(inputs.back().map(String::as_str), Some("prompt-249"));
}

#[test]
fn unmatched_restored_tool_results_still_use_the_assistant_card_surface() {
    let mut state = ChatState::default();
    state.restore_history(vec![HistoryEntry {
        role: "tool_result".into(),
        blocks: Vec::new(),
        tool_result: Some(HistoryToolResult {
            tool_call_id: "missing-call".into(),
            tool_name: "bash".into(),
            is_error: true,
            display: HistoryToolDisplay::default(),
        }),
    }]);

    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.messages[0].role, ChatRole::Assistant);
    assert_eq!(state.messages[0].history_tool_results.len(), 1);
}

#[test]
fn tool_card_code_fences_cannot_be_closed_by_their_content() {
    let rendered = markdown_code_block("text", "before\n```\nafter");
    assert!(rendered.starts_with("````text\n"));
    assert!(rendered.ends_with("\n````"));
    assert!(rendered.contains("before\n```\nafter"));
}

#[test]
fn restored_messages_receive_new_render_ids() {
    let mut state = ChatState::default();
    state.restore_history(vec![HistoryEntry {
        role: "assistant".into(),
        blocks: vec![HistoryBlock::Text {
            text: "first".into(),
        }],
        tool_result: None,
    }]);
    let first_id = state.messages[0].render_id;
    state.restore_history(vec![HistoryEntry {
        role: "assistant".into(),
        blocks: vec![HistoryBlock::Text {
            text: "second".into(),
        }],
        tool_result: None,
    }]);

    assert!(state.messages[0].render_id > first_id);
}

#[test]
fn authoritative_project_name_uses_session_cwd() {
    assert_eq!(
        project_name("/Users/example/snow-core").as_deref(),
        Some("snow-core")
    );
    assert_eq!(project_name(""), None);
}

#[test]
fn runtime_batch_bounds_received_events_even_when_deltas_coalesce() {
    let (sender, receiver) = flume::unbounded();
    for _ in 1..100 {
        sender
            .send(RuntimeEvent::TextDelta { text: "x".into() })
            .expect("test receiver remains connected");
    }

    let batch = receive_runtime_batch(
        &receiver,
        RuntimeEvent::TextDelta { text: "x".into() },
    );

    assert_eq!(batch.len(), 1);
    assert!(matches!(
        &batch[0],
        RuntimeEvent::TextDelta { text } if text.len() == MAX_RUNTIME_EVENTS_PER_BATCH
    ));
    assert_eq!(receiver.len(), 100 - MAX_RUNTIME_EVENTS_PER_BATCH);
}

#[test]
fn adjacent_streaming_events_are_coalesced() {
    let mut batch = Vec::new();
    push_coalesced(&mut batch, RuntimeEvent::TextDelta { text: "a".into() });
    push_coalesced(&mut batch, RuntimeEvent::TextDelta { text: "b".into() });
    push_coalesced(&mut batch, RuntimeEvent::Status("done".into()));
    assert_eq!(batch.len(), 2);
    assert!(matches!(
        &batch[0],
        RuntimeEvent::TextDelta { text } if text == "ab"
    ));
}

#[test]
fn streamed_batch_updates_assistant_before_prompt_completion() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    let mut batch = Vec::new();
    push_coalesced(
        &mut batch,
        RuntimeEvent::TextDelta {
            text: "streaming ".into(),
        },
    );
    push_coalesced(
        &mut batch,
        RuntimeEvent::TextDelta {
            text: "works".into(),
        },
    );

    apply_runtime_batch(&mut state, batch, &HashSet::new());

    assert_eq!(state.messages[1].text, "streaming works");
    assert_eq!(
        state.messages[1].presentation_text.as_ref(),
        "streaming works"
    );
    assert!(state.messages[1].streaming);
    assert!(state.active_prompt.is_some());
}

#[test]
fn state_moves_from_starting_to_ready() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    assert!(state.can_send());
    assert_eq!(state.composer_action(), ComposerAction::Send);
    assert_eq!(state.status_text, "Ready");
}

#[test]
fn presented_command_completion_status_respects_batch_order() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    let presented = HashSet::from(["command-1".to_owned()]);

    apply_runtime_batch(
        &mut state,
        vec![
            RuntimeEvent::CommandCompleted {
                request_id: "command-1".into(),
                command: "sessions".into(),
                data: None,
            },
            RuntimeEvent::Failed("later failure".into()),
        ],
        &presented,
    );

    assert_eq!(state.status_text, "Snow error");
    assert_eq!(state.last_error.as_deref(), Some("later failure"));
}

#[test]
fn command_completions_do_not_enter_the_conversation() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    let message_count = state.messages.len();
    let next_render_id = state.next_render_id;
    state.status_text = "Keep current status".into();
    state.last_error = Some("Keep current error".into());

    state.apply(RuntimeEvent::CommandCompleted {
        request_id: "auth-1".into(),
        command: "auth_providers".into(),
        data: Some(serde_json::json!({
            "providers": [{"provider_id": "fake", "status": {"authenticated": true}}]
        })),
    });
    state.apply(RuntimeEvent::CommandCompleted {
        request_id: "skills-1".into(),
        command: "skills".into(),
        data: Some(serde_json::json!({
            "skills": [{"name": "review", "description": "Review code"}]
        })),
    });

    assert_eq!(state.messages.len(), message_count);
    assert_eq!(state.next_render_id, next_render_id);
    assert_eq!(state.status_text, "Keep current status");
    assert_eq!(state.last_error.as_deref(), Some("Keep current error"));

    state.push_system_message("Human-readable desktop help");
    assert_eq!(state.messages.len(), message_count + 1);
    assert_eq!(
        state.messages.last().map(|message| message.text.as_str()),
        Some("Human-readable desktop help")
    );
}

#[test]
fn deltas_append_to_one_assistant_message() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::TextDelta { text: "one".into() });
    state.apply(RuntimeEvent::TextDelta {
        text: " two".into(),
    });
    assert_eq!(state.messages.len(), 2);
    assert_eq!(state.messages[1].text, "one two");
}

#[test]
fn fake_completion_removes_empty_assistant_message() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::PromptCompleted(PromptCompleted {
        request_id: "p1".into(),
        status: PromptStatus::Completed,
        error: None,
    }));
    assert_eq!(state.messages.len(), 1);
    assert!(state.active_prompt.is_none());
    assert!(state.can_send());
}

#[test]
fn turn_done_does_not_finish_prompt() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::TurnDone {
        turn_id: Some("turn-1".into()),
    });
    assert!(state.active_prompt.is_some());
    assert_eq!(state.status_text, "Finishing…");
}

#[test]
fn unrelated_completion_does_not_finish_prompt() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::PromptCompleted(PromptCompleted {
        request_id: "other".into(),
        status: PromptStatus::Completed,
        error: None,
    }));
    assert!(state.active_prompt.is_some());
}

#[test]
fn abort_pending_disables_repeated_stop_requests() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.active_prompt.as_mut().unwrap().abort_pending = true;
    assert!(!state.can_abort());
    assert_eq!(state.composer_action(), ComposerAction::Stop);
}

#[test]
fn desktop_slash_selection_never_navigates_hidden_rows() {
    let mut selection = SlashSelectionState::new(slash_completion_limits());
    selection.refresh("/", &slash_command_catalog());
    assert_eq!(selection.matches.len(), MAX_COMPOSER_COMPLETIONS);
    for _ in 0..MAX_COMPOSER_COMPLETIONS * 2 {
        assert_eq!(
            selection.handle_key(SlashKey::Down),
            SlashAction::SelectionChanged
        );
        assert!(selection.selected < MAX_COMPOSER_COMPLETIONS);
    }
}

#[test]
fn picker_highlight_starts_on_the_current_value() {
    let values = vec!["off".into(), "low".into(), "medium".into()];
    assert_eq!(picker_highlight_for_value(&values, "low"), 1);
    assert_eq!(picker_highlight_for_value(&values, "missing"), 0);
}

#[test]
fn sidebar_session_titles_hide_internal_ids_for_unnamed_threads() {
    assert_eq!(sidebar_session_title(""), "New thread");
    assert_eq!(sidebar_session_title("   "), "New thread");
    assert_eq!(
        sidebar_session_title("  Keep the user-facing title  "),
        "Keep the user-facing title"
    );
}

#[test]
fn process_poll_stops_only_for_an_applied_terminal_eof_response() {
    assert!(should_stop_process_poll(
        ProcessResponseDisposition::Applied,
        true
    ));
    assert!(!should_stop_process_poll(
        ProcessResponseDisposition::Applied,
        false
    ));
    assert!(!should_stop_process_poll(
        ProcessResponseDisposition::Stale,
        true
    ));
    assert!(!should_stop_process_poll(
        ProcessResponseDisposition::Invalid,
        true
    ));
}

#[test]
fn delayed_tool_updates_invalidate_the_exact_older_transcript_row() {
    let mut state = ChatState::default();
    state.messages = (0..7)
        .map(|index| ChatMessage {
            role: ChatRole::Assistant,
            text: format!("message {index}"),
            presentation_text: format!("message {index}").into(),
            streaming: false,
            history_blocks: if index == 1 {
                vec![HistoryBlock::ToolCall(HistoryToolCall {
                    tool_call_id: "old-call".into(),
                    name: "read".into(),
                    arguments_display: "Running".into(),
                })]
            } else {
                Vec::new()
            },
            history_tool_results: Vec::new(),
            render_id: index as u64 + 1,
        })
        .collect();
    let batch = vec![RuntimeEvent::ToolProgress {
        call_id: "old-call".into(),
        message: Some("halfway".into()),
    }];

    assert_eq!(tool_transcript_rows(&state, &batch), vec![1]);
    assert!(1 < state.messages.len().saturating_sub(4));
    apply_runtime_batch(&mut state, batch, &HashSet::new());
    assert!(matches!(
        &state.messages[1].history_blocks[0],
        HistoryBlock::ToolCall(tool) if tool.arguments_display.contains("halfway")
    ));
}

#[test]
fn consecutive_tool_messages_render_and_invalidate_as_one_activity_row() {
    let messages = (0..3)
        .map(|index| ChatMessage {
            role: ChatRole::Assistant,
            text: String::new(),
            presentation_text: "".into(),
            streaming: false,
            history_blocks: vec![HistoryBlock::ToolCall(HistoryToolCall {
                tool_call_id: format!("call-{index}"),
                name: "read".into(),
                arguments_display: "Running".into(),
            })],
            history_tool_results: Vec::new(),
            render_id: index as u64 + 1,
        })
        .collect::<Vec<_>>();

    assert_eq!(tool_activity_run_bounds(&messages, 0), Some((0, 2)));
    assert_eq!(tool_activity_run_bounds(&messages, 2), Some((0, 2)));
    let coalesced = coalesced_tool_activity_message(&messages, 0, 2);
    assert_eq!(
        coalesced
            .as_ref()
            .map(|message| message.history_blocks.len()),
        Some(3)
    );
    assert_eq!(
        coalesced.as_ref().map(|message| message.render_id),
        Some(messages[0].render_id)
    );

    let state = ChatState {
        messages,
        ..ChatState::default()
    };
    let batch = vec![RuntimeEvent::ToolProgress {
        call_id: "call-0".into(),
        message: Some("halfway".into()),
    }];
    assert_eq!(tool_transcript_rows(&state, &batch), vec![2]);
}

#[test]
fn transcript_following_respects_user_scroll_position_and_event_scope() {
    assert!(should_follow_transcript(
        5,
        ListOffset {
            item_ix: 5,
            offset_in_item: px(0.),
        },
        true,
        false,
    ));
    assert!(!should_follow_transcript(
        5,
        ListOffset {
            item_ix: 2,
            offset_in_item: px(0.),
        },
        true,
        false,
    ));
    assert!(!should_follow_transcript(
        5,
        ListOffset {
            item_ix: 5,
            offset_in_item: px(0.),
        },
        false,
        false,
    ));
    assert!(should_follow_transcript(
        5,
        ListOffset {
            item_ix: 2,
            offset_in_item: px(0.),
        },
        false,
        true,
    ));
}

#[test]
fn transcript_list_sync_preserves_scroll_until_history_is_replaced() {
    let list_state = ListState::new(3, ListAlignment::Bottom, px(64.));
    list_state.scroll_to(ListOffset {
        item_ix: 1,
        offset_in_item: px(7.),
    });

    assert!(!sync_transcript_list_items(&list_state, 3, false));
    let preserved = list_state.logical_scroll_top();
    assert_eq!(preserved.item_ix, 1);
    assert_eq!(preserved.offset_in_item, px(7.));

    assert!(sync_transcript_list_items(&list_state, 3, true));
    let reset = list_state.logical_scroll_top();
    assert_eq!(reset.item_ix, 3);
    assert_eq!(reset.offset_in_item, px(0.));

    assert!(sync_transcript_list_items(&list_state, 5, false));
    assert_eq!(list_state.item_count(), 5);
}

#[test]
fn transcript_content_events_are_marked_for_variable_row_remeasurement() {
    assert!(runtime_event_changes_transcript_content(
        &RuntimeEvent::TextDelta { text: "next".into() }
    ));
    assert!(runtime_event_changes_transcript_content(
        &RuntimeEvent::ToolProgress {
            call_id: "call-1".into(),
            message: Some("working".into()),
        }
    ));
    assert!(!runtime_event_changes_transcript_content(
        &RuntimeEvent::Status("Ready".into())
    ));
}

#[test]
fn collapsed_tool_card_summaries_are_single_line_and_bounded() {
    let summary = tool_card_summary(&format!("first\n  second\t{}", "x".repeat(240)));
    assert!(!summary.contains('\n'));
    assert!(!summary.contains('\t'));
    assert!(summary.ends_with('…'));
    assert!(summary.chars().count() <= TOOL_CARD_SUMMARY_CHARS + 1);
}

#[test]
fn tool_activity_uses_one_compact_transcript_label() {
    assert_eq!(tool_activity_label(4, 4, 0, 9_800), "Worked for 9.8s");
    assert_eq!(tool_activity_label(1, 0, 0, 0), "Working…");
    assert_eq!(
        tool_activity_label(3, 3, 1, 62_400),
        "Worked for 1m 2s · 1 failed"
    );
    assert_eq!(tool_activity_label(2, 2, 0, 0), "Used 2 tools");
}

#[test]
fn controlled_composer_picker_state_is_idempotent_exclusive_and_search_scoped() {
    let mut picker = ComposerPickerState::default();
    assert_eq!(picker.active, None);

    assert!(picker.set_open(ComposerPicker::Provider, true));
    assert_eq!(picker.active, Some(ComposerPicker::Provider));
    picker.search.set_query("configured");

    assert!(!picker.set_open(ComposerPicker::Provider, true));
    assert_eq!(picker.search.query, "configured");

    assert!(picker.set_open(ComposerPicker::Model, true));
    assert_eq!(picker.active, Some(ComposerPicker::Model));
    assert!(picker.search.query.is_empty());

    assert!(!picker.set_open(ComposerPicker::Provider, false));
    assert_eq!(picker.active, Some(ComposerPicker::Model));

    picker.search.set_query("model");
    assert!(!picker.set_open(ComposerPicker::Model, false));
    assert_eq!(picker.active, None);
    assert!(picker.search.query.is_empty());

    assert!(!picker.set_open(ComposerPicker::Model, false));
    assert_eq!(picker.active, None);
}

#[test]
fn primary_enter_accepts_the_highlighted_mention_before_skill_or_submission() {
    let matches = vec![
        "README.md".to_owned(),
        "desktop/src/workspace.rs".to_owned(),
    ];
    let suggestion = composer_suggestion_priority(false, true, true);

    assert_eq!(
        composer_enter_action(None, suggestion),
        ComposerEnterAction::AcceptMention
    );
    assert_eq!(
        selected_mention_completion(&matches, 1, false),
        Some("desktop/src/workspace.rs")
    );
    assert_eq!(
        composer_enter_action(None, composer_suggestion_priority(true, true, true),),
        ComposerEnterAction::AcceptSlash
    );
    assert_eq!(
        composer_enter_action(Some(ComposerPicker::Permission), None),
        ComposerEnterAction::ActivatePicker
    );
}

#[test]
fn highlighted_active_provider_activation_keeps_controlled_picker_state_open() {
    let mut picker = ComposerPickerState::default();
    assert!(picker.set_open(ComposerPicker::Provider, true));
    picker.search.set_query("opencode");
    let rows = ["opencode-go", "opencode-zen"];
    let highlighted_provider = rows[picker.search.highlighted];

    assert!(!picker.prepare_provider_activation(highlighted_provider, "opencode-go", false,));
    assert_eq!(picker.active, Some(ComposerPicker::Provider));
    assert_eq!(picker.search.query, "opencode");

    assert!(picker.prepare_provider_activation(highlighted_provider, "opencode-go", true,));
    assert_eq!(picker.active, None);
    assert!(picker.search.query.is_empty());
}

#[test]
fn suggestion_priority_and_controlled_dismissal_are_stable() {
    assert_eq!(
        composer_suggestion_priority(true, true, true),
        Some(ComposerSuggestion::Slash)
    );
    assert_eq!(
        composer_suggestion_priority(false, true, true),
        Some(ComposerSuggestion::Mention)
    );
    assert_eq!(
        composer_suggestion_priority(false, false, true),
        Some(ComposerSuggestion::Skill)
    );
    assert_eq!(composer_suggestion_priority(false, false, false), None);

    let mut picker = ComposerPickerState::default();
    picker.note_suggestion_input("$cave");
    assert!(picker.suggestions_allowed_for("$cave"));
    assert!(picker.dismiss_suggestions_for("$cave"));
    assert!(!picker.dismiss_suggestions_for("$cave"));
    assert!(!picker.suggestions_allowed_for("$cave"));

    picker.note_suggestion_input("$caveman");
    assert!(picker.suggestions_allowed_for("$caveman"));
    assert!(picker.set_open(ComposerPicker::Provider, true));
    assert!(!picker.suggestions_allowed_for("$caveman"));
}

#[test]
fn fake_runtime_uses_neutral_model_presentation_until_a_real_provider_is_active() {
    let models = vec![
        ModelInfo {
            provider: "fake".into(),
            id: "fake-1".into(),
            display_name: "Fake One".into(),
            ..ModelInfo::default()
        },
        ModelInfo {
            provider: "opencode-go".into(),
            id: "real-1".into(),
            display_name: "Real One".into(),
            ..ModelInfo::default()
        },
    ];

    assert_eq!(
        composer_model_label("fake", "fake-1", &models),
        "Choose model"
    );
    assert!(!can_open_model_picker("fake", true));
    assert_eq!(
        composer_model_label("opencode-go", "real-1", &models),
        "Real One"
    );
    assert!(can_open_model_picker("opencode-go", true));
    assert!(can_open_model_picker("private-compatible", true));
    assert!(!can_open_model_picker("opencode-go", false));
}

#[test]
fn unsupported_user_input_is_visible_and_keeps_stop_available() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::UnsupportedInteraction {
        kind: "user input".into(),
        request_id: Some("ask-1".into()),
    });
    assert!(state.can_abort());
    assert!(state.status_text.contains("unsupported user input"));
    assert!(state.last_error.as_deref().unwrap().contains("ask-1"));
}

#[test]
fn process_exit_clears_pending_thinking_and_allows_recovery() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_thinking_change("thinking-1".into(), "high".into());
    state.apply(RuntimeEvent::Exited {
        expected: false,
        status: Some(1),
    });

    assert!(state.thinking_change_pending.is_none());
    assert!(state.model_change_pending.is_none());
    assert!(state.can_switch_provider());
}

#[test]
fn process_exit_preserves_the_actionable_stderr_diagnostic() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.apply(RuntimeEvent::Diagnostic(
        "model does not advertise thinking level low".into(),
    ));
    state.apply(RuntimeEvent::Exited {
        expected: false,
        status: Some(1),
    });

    assert_eq!(
        state.last_error.as_deref(),
        Some("model does not advertise thinking level low")
    );
}

#[test]
fn process_exit_disables_composer() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_prompt("p1".into(), "hello".into());
    state.apply(RuntimeEvent::Exited {
        expected: false,
        status: Some(2),
    });
    assert!(!state.can_send());
    assert!(state.can_switch_provider());
    assert!(state.active_prompt.is_none());
    assert!(state.last_error.as_deref().unwrap().contains("status 2"));
}

#[test]
fn tool_lifecycle_updates_one_record() {
    let mut state = ChatState::default();
    state.apply(RuntimeEvent::ToolStarted {
        call_id: "t1".into(),
        name: "read".into(),
    });
    state.apply(RuntimeEvent::ToolProgress {
        call_id: "t1".into(),
        message: Some("Reading".into()),
    });
    assert_eq!(state.tools[0].state, ToolState::Running);
    state.apply(RuntimeEvent::ToolFinished {
        call_id: "t1".into(),
        name: "read".into(),
        is_error: false,
        preview: None,
    });
    assert_eq!(state.tools.len(), 1);
    assert_eq!(state.tools[0].status, "Completed");
    assert_eq!(state.tools[0].state, ToolState::Completed);
}

fn permission_request(id: &str) -> PermissionRequest {
    PermissionRequest {
        id: id.into(),
        agent: None,
        tool: "bash".into(),
        args: serde_json::json!({"command": "cargo test"}),
        paths: vec!["/tmp/snow-core".into()],
        risk: "exec".into(),
        reason: "Run the requested checks".into(),
    }
}

fn user_input_request(id: &str) -> UserInputRequest {
    UserInputRequest {
        id: id.into(),
        agent: None,
        tool_call_id: format!("tool-{id}"),
        questions: vec![
            UserInputQuestion {
                id: "language".into(),
                header: "Language".into(),
                question: "Which language?".into(),
                options: vec![
                    crate::snow::UserInputOption {
                        label: "Rust".into(),
                        description: "Use the Rust client".into(),
                    },
                    crate::snow::UserInputOption {
                        label: "Go".into(),
                        description: "Use the Go client".into(),
                    },
                ],
            },
            UserInputQuestion {
                id: "notes".into(),
                header: "Notes".into(),
                question: "Any implementation notes?".into(),
                options: Vec::new(),
            },
        ],
    }
}

fn begin_interactive_prompt(state: &mut ChatState) {
    make_ready(state);
    state.begin_prompt("prompt-interaction".into(), "work".into());
}

#[test]
fn permission_card_supports_all_four_decisions_and_waits_for_exact_ack() {
    for (index, decision) in [
        PermissionDecision::Allow,
        PermissionDecision::AllowSession,
        PermissionDecision::AllowAlways,
        PermissionDecision::Deny,
    ]
    .into_iter()
    .enumerate()
    {
        let mut state = ChatState::default();
        begin_interactive_prompt(&mut state);
        state.apply(RuntimeEvent::PermissionRequested(permission_request(
            "perm-1",
        )));
        assert!(!state.can_send());
        assert!(
            state.can_abort(),
            "Stop must remain available while blocked"
        );
        assert!(state.begin_permission_command("perm-1", decision, format!("command-{index}")));
        let Some(ActiveInteraction::Permission(interaction)) = &state.active_interaction else {
            panic!("permission must remain visible until the host acknowledges it");
        };
        assert_eq!(interaction.decision, Some(decision));
        assert!(interaction.pending.is_some());

        state.apply(RuntimeEvent::InteractionResolved {
            command_id: "stale-command".into(),
            request_id: "perm-1".into(),
            command: "permission_reply".into(),
        });
        assert!(state.active_interaction.is_some());
        state.apply(RuntimeEvent::InteractionResolved {
            command_id: format!("command-{index}"),
            request_id: "perm-1".into(),
            command: "permission_reply".into(),
        });
        assert!(state.active_interaction.is_none());
    }
}

#[test]
fn interaction_queue_suppresses_duplicates_and_fails_closed_on_overflow() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-1",
    )));
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-1",
    )));
    assert!(
        state.queued_interaction.is_none(),
        "duplicate must not consume queue"
    );

    state.apply(RuntimeEvent::UserInputRequested(user_input_request(
        "ask-1",
    )));
    assert!(matches!(
        state.queued_interaction,
        Some(InteractionRequest::UserInput(_))
    ));
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-overflow",
    )));
    assert_eq!(state.interaction_rejections.len(), 1);
    assert_eq!(state.interaction_rejections[0].request_id, "perm-overflow");
    assert_eq!(
        state.interaction_rejections[0].kind,
        InteractionKind::Permission
    );

    state.begin_permission_command("perm-1", PermissionDecision::Deny, "deny-1".into());
    state.apply(RuntimeEvent::InteractionResolved {
        command_id: "deny-1".into(),
        request_id: "perm-1".into(),
        command: "permission_reply".into(),
    });
    assert!(matches!(
        state.active_interaction,
        Some(ActiveInteraction::UserInput(_))
    ));
    assert!(state.queued_interaction.is_none());

    // A stale duplicate cannot reappear after the original leaves the visible slot.
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-1",
    )));
    assert!(state.queued_interaction.is_none());
}

#[test]
fn interaction_rejections_are_exactly_correlated_and_retryable() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-1",
    )));
    state.begin_permission_command("perm-1", PermissionDecision::Allow, "reply-1".into());

    for event in [
        RuntimeEvent::InteractionRejected {
            command_id: Some("other".into()),
            request_id: Some("perm-1".into()),
            command: "permission_reply".into(),
            error: "stale command".into(),
        },
        RuntimeEvent::InteractionRejected {
            command_id: Some("reply-1".into()),
            request_id: Some("other".into()),
            command: "permission_reply".into(),
            error: "stale request".into(),
        },
        RuntimeEvent::InteractionRejected {
            command_id: Some("reply-1".into()),
            request_id: Some("perm-1".into()),
            command: "permission_reject".into(),
            error: "stale command kind".into(),
        },
    ] {
        state.apply(event);
        assert!(
            state
                .active_interaction
                .as_ref()
                .and_then(ActiveInteraction::pending)
                .is_some()
        );
    }

    state.apply(RuntimeEvent::InteractionRejected {
        command_id: Some("reply-1".into()),
        request_id: Some("perm-1".into()),
        command: "permission_reply".into(),
        error: "host rejected reply".into(),
    });
    let Some(ActiveInteraction::Permission(interaction)) = &state.active_interaction else {
        panic!("a rejected host command must leave the trusted card available for retry");
    };
    assert!(interaction.pending.is_none());
    assert_eq!(interaction.decision, None);
    assert_eq!(state.last_error.as_deref(), Some("host rejected reply"));
}

#[test]
fn user_input_drafts_validate_and_answers_follow_request_order() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    state.apply(RuntimeEvent::UserInputRequested(user_input_request(
        "ask-1",
    )));

    state.select_user_input_option("not-an-option");
    assert!(state.user_input_answers().is_none());
    state.select_user_input_option("Rust");
    assert!(state.move_user_input_question(1));
    state.set_user_input_draft("   keep request order   ");
    let (request_id, answers) = state.user_input_answers().expect("valid answers");
    assert_eq!(request_id, "ask-1");
    assert_eq!(
        answers,
        vec![
            UserInputAnswer {
                question_id: "language".into(),
                answer: "Rust".into(),
            },
            UserInputAnswer {
                question_id: "notes".into(),
                answer: "keep request order".into(),
            },
        ]
    );

    state.set_user_input_draft("x".repeat(MAX_USER_INPUT_BYTES + 1));
    assert!(state.user_input_answers().is_none());
    assert!(
        state
            .current_user_input()
            .and_then(|interaction| interaction.validation_error.as_deref())
            .is_some_and(|error| error.contains("8 KiB"))
    );
}

#[test]
fn user_input_other_draft_survives_navigation() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    state.apply(RuntimeEvent::UserInputRequested(user_input_request(
        "ask-1",
    )));
    state.select_user_input_other();
    state.set_user_input_draft("Zig");
    assert!(state.move_user_input_question(1));
    state.set_user_input_draft("No extra notes");
    assert!(state.move_user_input_question(-1));

    let interaction = state.current_user_input().unwrap();
    assert!(interaction.draft().use_other);
    assert_eq!(interaction.draft().other, "Zig");
    assert!(state.move_user_input_question(1));
    assert_eq!(
        state.current_user_input().unwrap().draft().other,
        "No extra notes"
    );
}

#[test]
fn prompt_lifecycle_cleans_interactions_and_late_acks_are_ignored() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    state.apply(RuntimeEvent::PermissionRequested(permission_request(
        "perm-1",
    )));
    state.apply(RuntimeEvent::UserInputRequested(user_input_request(
        "ask-1",
    )));
    state.begin_permission_command("perm-1", PermissionDecision::Deny, "deny-1".into());
    state.apply(RuntimeEvent::PromptCompleted(PromptCompleted {
        request_id: "prompt-interaction".into(),
        status: PromptStatus::Canceled,
        error: None,
    }));
    assert!(state.active_interaction.is_none());
    assert!(state.queued_interaction.is_none());

    state.apply(RuntimeEvent::InteractionResolved {
        command_id: "deny-1".into(),
        request_id: "perm-1".into(),
        command: "permission_reply".into(),
    });
    assert!(state.active_interaction.is_none());
}

#[test]
fn malformed_interaction_is_declined_once_and_never_displayed() {
    let mut state = ChatState::default();
    begin_interactive_prompt(&mut state);
    let malformed = || RuntimeEvent::MalformedInteraction {
        kind: InteractionKind::UserInput,
        request_id: Some("bad-1".into()),
        error: "questions missing".into(),
    };
    state.apply(malformed());
    state.apply(malformed());
    assert!(state.active_interaction.is_none());
    assert_eq!(state.interaction_rejections.len(), 1);
    assert_eq!(state.interaction_rejections[0].request_id, "bad-1");
}

#[test]
fn search_picker_filters_required_fields_and_resets_highlight() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    assert_eq!(search_models(&state.models, "fake two"), vec![1]);
    assert_eq!(search_models(&state.models, "fake-1"), vec![0]);
    assert_eq!(search_models(&state.models, "missing"), Vec::<usize>::new());

    let mut search = SearchPickerState::default();
    search.move_highlight(1, 2);
    assert_eq!(search.highlighted, 1);
    search.set_query("fake");
    assert_eq!(search.highlighted, 0);
    search.move_highlight(-1, 2);
    assert_eq!(search.highlighted, 0);
    search.move_highlight(20, 2);
    assert_eq!(search.highlighted, 1);
    search.normalize_highlight(1);
    assert_eq!(search.highlighted, 0);
}

#[test]
fn manual_model_ids_are_trimmed_bounded_and_require_no_exact_catalog_match() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    assert_eq!(
        manual_model_id(&state.models, "  custom/model  ").as_deref(),
        Some("custom/model")
    );
    assert_eq!(manual_model_id(&state.models, ""), None);
    assert_eq!(manual_model_id(&state.models, " fake-1 "), None);
    assert_eq!(manual_model_id(&state.models, &"x".repeat(257)), None);

    state.models.clear();
    assert!(
        state.can_switch_model(),
        "empty discovery must still allow manual IDs"
    );
    assert!(state.can_select_model("custom/model"));
    assert!(!state.can_select_model("   "));
}

#[test]
fn manual_model_change_is_correlated_keeps_id_and_forces_thinking_off() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.models.clear();
    state.current_thinking = "high".into();
    state.begin_model_change("manual-1".into(), "private/model", "off".into());

    state.apply(RuntimeEvent::ModelChangeConfirmed {
        request_id: "stale".into(),
    });
    assert_eq!(state.current_model, "fake-1");
    state.apply(RuntimeEvent::ModelChangeConfirmed {
        request_id: "manual-1".into(),
    });
    assert_eq!(state.current_model, "private/model");
    assert_eq!(state.current_thinking, "off");
    assert_eq!(state.thinking_levels, ["off"]);

    state.begin_runtime_load("manual-refresh".into(), false);
    state.apply(RuntimeEvent::ModelsLoaded {
        generation: "manual-refresh".into(),
        catalog: ModelCatalog {
            provider: "fake".into(),
            current: String::new(),
            models: Vec::new(),
        },
    });
    assert_eq!(state.current_model, "private/model");
}

#[test]
fn trusted_interaction_previews_are_bounded() {
    assert_eq!(bounded_display("safe", 4), "safe");
    assert_eq!(bounded_display("permission", 4), "perm…");

    let paths = (0..10)
        .map(|index| format!("/{index}/{}", "x".repeat(300)))
        .collect::<Vec<_>>();
    let preview = bounded_paths(&paths);
    assert!(preview.contains("and 2 more"));
    assert!(!preview.contains(&"x".repeat(257)));
}

#[test]
fn canvas_layout_centers_only_an_unblocked_empty_thread() {
    assert_eq!(
        workspace_canvas_layout(0, false, false),
        WorkspaceCanvasLayout::CenteredEmpty
    );
    assert_eq!(
        workspace_canvas_layout(1, false, false),
        WorkspaceCanvasLayout::Active
    );
    assert_eq!(
        workspace_canvas_layout(0, true, false),
        WorkspaceCanvasLayout::Active
    );
    assert_eq!(
        workspace_canvas_layout(0, false, true),
        WorkspaceCanvasLayout::Active
    );
}

#[test]
fn settings_workspace_visibility_is_independent_from_loaded_data() {
    assert_eq!(
        settings_workspace_layout(false, false),
        SettingsWorkspaceLayout::Hidden
    );
    assert_eq!(
        settings_workspace_layout(false, true),
        SettingsWorkspaceLayout::Hidden,
        "late settings data must not reopen a closed workspace"
    );
    assert_eq!(
        settings_workspace_layout(true, false),
        SettingsWorkspaceLayout::Loading
    );
    assert_eq!(
        settings_workspace_layout(true, true),
        SettingsWorkspaceLayout::Ready
    );
}

#[test]
fn settings_sections_are_stable_and_human_readable() {
    assert_eq!(SettingsSection::default(), SettingsSection::General);
    assert_eq!(
        SettingsSection::ALL.map(SettingsSection::id),
        ["general", "capabilities", "appearance", "keybindings"]
    );
    assert_eq!(
        SettingsSection::ALL.map(SettingsSection::label),
        ["General", "Capabilities", "Appearance", "Keybindings"]
    );
    assert!(SettingsSection::ALL
        .into_iter()
        .all(|section| !section.description().trim().is_empty()));
}

#[test]
fn provider_picker_explains_empty_catalogs_and_searches() {
    assert_eq!(
        provider_picker_empty_message(0, 0, ""),
        Some("No providers available.")
    );
    assert_eq!(
        provider_picker_empty_message(3, 0, "missing"),
        Some("No providers match your search.")
    );
    assert_eq!(provider_picker_empty_message(3, 2, "open"), None);
}

#[test]
fn closing_a_picker_restores_focus_to_a_visible_workspace_target() {
    assert_eq!(
        picker_close_focus_target(false, true),
        PickerCloseFocusTarget::Composer
    );
    assert_eq!(
        picker_close_focus_target(true, true),
        PickerCloseFocusTarget::Settings
    );
    assert_eq!(
        picker_close_focus_target(false, false),
        PickerCloseFocusTarget::None
    );
}

#[test]
fn top_bar_compacts_before_the_minimum_window_can_overflow() {
    assert_eq!(
        workspace_top_bar_layout(900., false),
        WorkspaceTopBarLayout::Compact
    );
    assert_eq!(
        workspace_top_bar_layout(900., true),
        WorkspaceTopBarLayout::Compact
    );
    assert_eq!(
        workspace_top_bar_layout(1280., false),
        WorkspaceTopBarLayout::Full
    );
}

#[test]
fn blocking_interactions_suppress_every_management_panel() {
    let panel_states = [
        ManagementPanelState {
            sessions: true,
            ..ManagementPanelState::default()
        },
        ManagementPanelState {
            processes: true,
            ..ManagementPanelState::default()
        },
        ManagementPanelState {
            subagents: true,
            ..ManagementPanelState::default()
        },
        ManagementPanelState {
            resource: true,
            ..ManagementPanelState::default()
        },
        ManagementPanelState {
            auth: true,
            ..ManagementPanelState::default()
        },
        ManagementPanelState {
            settings: true,
            ..ManagementPanelState::default()
        },
    ];

    for requested in panel_states {
        assert_eq!(
            management_panels_for_interaction(true, requested),
            ManagementPanelState::default()
        );
        assert_eq!(
            management_panels_for_interaction(false, requested),
            requested
        );
    }

    let all_open = ManagementPanelState {
        sessions: true,
        processes: true,
        subagents: true,
        resource: true,
        auth: true,
        settings: true,
    };
    assert_eq!(
        management_panels_for_interaction(true, all_open),
        ManagementPanelState::default()
    );
}

#[test]
fn transient_composer_overlays_do_not_change_layout_projection() {
    let baseline = composer_layout_projection(900., false, true, true);
    assert_eq!(baseline.footer, ComposerFooterLayout::Wrapped);
    assert_eq!(baseline.persistent_rows, 2);

    let mut picker = ComposerPickerState::default();
    for active in [
        ComposerPicker::Provider,
        ComposerPicker::Model,
        ComposerPicker::Thinking,
        ComposerPicker::Permission,
    ] {
        assert!(picker.set_open(active, true));
        assert_eq!(
            composer_layout_projection(900., false, true, true),
            baseline
        );
        assert_eq!(
            composer_layout_projection(900., true, true, true).footer,
            ComposerFooterLayout::Wrapped
        );
    }

    for suggestion in [
        ComposerSuggestion::Slash,
        ComposerSuggestion::Mention,
        ComposerSuggestion::Skill,
    ] {
        assert!(matches!(
            suggestion,
            ComposerSuggestion::Slash | ComposerSuggestion::Mention | ComposerSuggestion::Skill
        ));
        assert_eq!(
            composer_layout_projection(900., false, true, true),
            baseline
        );
    }
}

#[test]
fn composer_footer_wraps_at_minimum_width_with_expanded_sidebar() {
    assert_eq!(
        composer_footer_layout(900., false),
        ComposerFooterLayout::Wrapped
    );
    assert_eq!(
        composer_footer_layout(900., true),
        ComposerFooterLayout::Wrapped
    );
    assert_eq!(
        composer_footer_layout(1280., false),
        ComposerFooterLayout::Inline
    );
}

#[test]
fn confirmed_session_rename_builds_only_a_correlated_silent_refresh_command() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.begin_session_action(
        PendingSessionAction::Rename {
            request_id: "rename-1".into(),
        },
        "Renaming session…".into(),
    );

    let unrelated = RuntimeEvent::SessionRenamed {
        request_id: "other".into(),
        session_id: "session-1".into(),
        name: "Wrong".into(),
    };
    assert!(
        session_inventory_command_after_confirmed_metadata_mutation(&state, &unrelated).is_none()
    );

    let wrong_session = RuntimeEvent::SessionRenamed {
        request_id: "rename-1".into(),
        session_id: "other-session".into(),
        name: "Wrong".into(),
    };
    assert!(
        session_inventory_command_after_confirmed_metadata_mutation(&state, &wrong_session)
            .is_none()
    );

    let confirmed = RuntimeEvent::SessionRenamed {
        request_id: "rename-1".into(),
        session_id: "session-1".into(),
        name: "Normalized title".into(),
    };
    let command = session_inventory_command_after_confirmed_metadata_mutation(&state, &confirmed)
        .expect("matching rename should refresh the sidebar inventory");
    assert_eq!(command.name, "sessions_list");
    assert!(command.fields.is_empty());
    assert!(!command.refresh_runtime);

    state.apply(confirmed);
    assert_eq!(state.session_name, "Normalized title");
    assert!(state.session_action_pending.is_none());
}

#[test]
fn silent_session_inventory_refresh_updates_sidebar_without_opening_management_panel() {
    let session = SessionSummary {
        session_id: "session-1".into(),
        name: "Desktop redesign".into(),
        updated_at: 42,
        messages: 7,
        active: true,
        ..SessionSummary::default()
    };
    let mut sessions = Vec::new();
    let mut sessions_panel_open = false;
    let mut session_menu_open = true;

    apply_session_inventory(
        &mut sessions,
        &mut sessions_panel_open,
        &mut session_menu_open,
        SessionList {
            sessions: vec![session.clone()],
        },
        true,
    );

    assert_eq!(sessions, [session.clone()]);
    assert!(!sessions_panel_open);
    assert!(session_menu_open);

    apply_session_inventory(
        &mut sessions,
        &mut sessions_panel_open,
        &mut session_menu_open,
        SessionList {
            sessions: vec![session],
        },
        false,
    );

    assert!(sessions_panel_open);
    assert!(!session_menu_open);
}

#[test]
fn silent_session_inventory_completion_preserves_existing_error() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.status_text = "Could not switch sessions".into();
    state.last_error = Some("session busy".into());

    apply_runtime_batch(
        &mut state,
        vec![RuntimeEvent::CommandCompleted {
            request_id: "inventory-1".into(),
            command: "sessions_list".into(),
            data: Some(serde_json::json!({"sessions": []})),
        }],
        &HashSet::new(),
    );

    assert_eq!(state.status_text, "Could not switch sessions");
    assert_eq!(state.last_error.as_deref(), Some("session busy"));
}

#[test]
fn auth_inventory_filters_only_the_normalized_internal_fake_provider() {
    let providers = ["fake", " fake ", "Fake", "private-compatible"]
        .into_iter()
        .map(|provider_id| AuthProvider {
            provider_id: provider_id.into(),
            ..AuthProvider::default()
        })
        .collect();

    let visible = user_visible_auth_providers(providers);
    assert_eq!(
        visible
            .iter()
            .map(|provider| provider.provider_id.as_str())
            .collect::<Vec<_>>(),
        ["Fake", "private-compatible"]
    );
}

#[test]
fn fake_runtime_identity_is_neutral_but_custom_runtime_ids_are_preserved() {
    assert_eq!(
        presented_provider_model("fake", "fake-1"),
        ("Choose provider", "Choose model")
    );
    assert_eq!(
        presented_provider_model(" fake ", "synthetic"),
        ("Choose provider", "Choose model")
    );
    assert_eq!(
        presented_provider_model("private-compatible", "private/model"),
        ("private-compatible", "private/model")
    );
}

#[test]
fn skill_suggestion_selection_is_owned_bounded_and_resettable() {
    let mut selection = SuggestionSelectionState::default();
    selection.move_selection(1, 3);
    assert_eq!(selection.selected, 1);
    selection.move_selection(20, 3);
    assert_eq!(selection.selected, 2);
    selection.move_selection(-20, 3);
    assert_eq!(selection.selected, 0);
    selection.selected = 7;
    selection.normalize(2);
    assert_eq!(selection.selected, 1);
    selection.reset();
    assert_eq!(selection.selected, 0);
}

#[test]
fn manual_model_keyboard_highlight_follows_discovered_rows() {
    assert!(!manual_model_row_highlighted(2, false, 2));
    assert!(!manual_model_row_highlighted(2, true, 1));
    assert!(manual_model_row_highlighted(2, true, 2));
}

#[test]
fn thinking_picker_highlight_is_bounded_by_advertised_levels() {
    let levels = ["off", "low", "high"];
    let mut selection = SearchPickerState::default();
    selection.move_highlight(1, levels.len());
    assert_eq!(levels[selection.highlighted], "low");
    selection.move_highlight(20, levels.len());
    assert_eq!(levels[selection.highlighted], "high");
    selection.move_highlight(-20, levels.len());
    assert_eq!(levels[selection.highlighted], "off");
}
