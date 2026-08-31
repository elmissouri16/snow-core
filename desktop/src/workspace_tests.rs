use std::{collections::HashSet, path::PathBuf, time::Duration};

use super::*;
use crate::snow::{BranchCatalog, ModelCatalog, RpcReady, SessionBranch, SessionInfo};

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
                },
                ModelInfo {
                    provider: "fake".into(),
                    id: "fake-2".into(),
                    display_name: "Fake Two".into(),
                    supports_thinking: false,
                    default_thinking: String::new(),
                    thinking_levels: Vec::new(),
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
fn provider_choices_have_unique_nonempty_ids() {
    let ids: HashSet<_> = PROVIDER_CHOICES.iter().map(|choice| choice.id).collect();
    assert_eq!(ids.len(), PROVIDER_CHOICES.len());
    assert!(PROVIDER_CHOICES.iter().all(|choice| {
        !choice.id.trim().is_empty() && !choice.label.trim().is_empty()
    }));
    assert!(ids.contains("fake"));
    assert!(ids.contains("opencode-zen"));
}

#[test]
fn discovered_session_path_is_reused_by_provider_replacement() {
    let mut config = RuntimeConfig {
        executable: PathBuf::from("snow"),
        project_root: PathBuf::from("/tmp/project"),
        provider: "fake".into(),
        model: None,
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
}

#[test]
fn provider_switch_retains_conversation_until_history_is_restored() {
    let mut state = ChatState::default();
    make_ready(&mut state);
    state.messages.push(ChatMessage {
        role: ChatRole::User,
        text: "old context".into(),
        streaming: false,
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

    apply_runtime_batch(&mut state, vec![ready()]);
    state.begin_runtime_load("replacement-load".into(), true);
    assert_eq!(state.messages.len(), 1);
    assert_eq!(state.tools.len(), 1);

    apply_runtime_batch(
        &mut state,
        vec![RuntimeEvent::HistoryLoaded {
            generation: "replacement-load".into(),
            history: vec![HistoryMessage {
                role: "user".into(),
                text: "restored context".into(),
            }],
        }],
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
    assert!(!state.can_select_model("unknown"));

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
fn restored_messages_receive_new_render_ids() {
    let mut state = ChatState::default();
    state.restore_history(vec![HistoryMessage {
        role: "assistant".into(),
        text: "first".into(),
    }]);
    let first_id = state.messages[0].render_id;
    state.restore_history(vec![HistoryMessage {
        role: "assistant".into(),
        text: "second".into(),
    }]);

    assert!(state.messages[0].render_id > first_id);
}

#[test]
fn authoritative_project_name_uses_session_cwd() {
    assert_eq!(project_name("/Users/example/snow-core").as_deref(), Some("snow-core"));
    assert_eq!(project_name(""), None);
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

    apply_runtime_batch(&mut state, batch);

    assert_eq!(state.messages[1].text, "streaming works");
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
fn composer_picker_toggles_exclusively_and_closes() {
    let mut picker = ComposerPickerState::default();
    assert_eq!(picker.active, None);

    picker.toggle(ComposerPicker::Provider);
    assert_eq!(picker.active, Some(ComposerPicker::Provider));

    picker.toggle(ComposerPicker::Model);
    assert_eq!(picker.active, Some(ComposerPicker::Model));

    picker.toggle(ComposerPicker::Thinking);
    assert_eq!(picker.active, Some(ComposerPicker::Thinking));

    picker.toggle(ComposerPicker::Thinking);
    assert_eq!(picker.active, None);

    picker.toggle(ComposerPicker::Provider);
    picker.close();
    assert_eq!(picker.active, None);
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
