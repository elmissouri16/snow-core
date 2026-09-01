use std::{
    env, fs,
    path::{Path, PathBuf},
    process::Command,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use serde_json::{Map, Value, json};
use snow_desktop::snow::{
    BranchCatalog, HistoryBlock, HistoryEntry, InteractionKind, ModelCatalog, PermissionDecision,
    PromptStatus, RuntimeConfig, RuntimeEvent, SessionInfo, SnowClient, SnowConnection,
    UserInputAnswer,
};

struct TempProject(PathBuf);

impl TempProject {
    fn new(label: &str) -> Self {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock after epoch")
            .as_nanos();
        let path = env::temp_dir().join(format!(
            "snow-desktop-{label}-{}-{nonce}",
            std::process::id()
        ));
        fs::create_dir_all(&path).expect("create temporary project");
        Self(path)
    }
}

impl Drop for TempProject {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn mock_config(project_root: &Path) -> RuntimeConfig {
    RuntimeConfig {
        executable: PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("tests/fixtures/mock_streaming_snow.sh"),
        project_root: project_root.to_owned(),
        provider: "fake".into(),
        model: None,
        permission: None,
        thinking: None,
        session_path: None,
        no_session: true,
        startup_timeout: Duration::from_secs(2),
        shutdown_timeout: Duration::from_secs(2),
        max_frame_bytes: 1024 * 1024,
    }
}

fn start_ready(config: RuntimeConfig, expected_version: Option<&str>) -> SnowConnection {
    let startup_timeout = config.startup_timeout;
    let connection = SnowClient::start(config).expect("start Snow RPC");
    loop {
        match connection
            .events
            .recv_timeout(startup_timeout)
            .expect("receive rpc_ready")
        {
            RuntimeEvent::Ready(ready) => {
                if let Some(expected) = expected_version {
                    assert_eq!(ready.snow_version, expected);
                }
                return connection;
            }
            RuntimeEvent::Diagnostic(_) => {}
            RuntimeEvent::Failed(error) => panic!("Snow startup failed: {error}"),
            other => panic!("event arrived before rpc_ready: {other:?}"),
        }
    }
}

fn start_ready_mock(project_root: &Path) -> SnowConnection {
    start_ready(mock_config(project_root), Some("mock-streaming"))
}

fn load_runtime_state(
    connection: &SnowConnection,
) -> (SessionInfo, ModelCatalog, Vec<HistoryEntry>, BranchCatalog) {
    let expected_generation = connection
        .client
        .load_runtime_state()
        .expect("request runtime state");
    let mut session = None;
    let mut catalog = None;
    let mut history = None;
    let mut history_pages = Vec::new();
    let mut branches = None;
    let deadline = std::time::Instant::now() + Duration::from_secs(2);
    while std::time::Instant::now() < deadline
        && (session.is_none() || catalog.is_none() || history.is_none() || branches.is_none())
    {
        match connection.events.recv_timeout(Duration::from_millis(250)) {
            Ok(RuntimeEvent::SessionLoaded { generation, info })
                if generation == expected_generation =>
            {
                session = Some(info)
            }
            Ok(RuntimeEvent::ModelsLoaded {
                generation,
                catalog: value,
            }) if generation == expected_generation => catalog = Some(value),
            Ok(RuntimeEvent::HistoryLoaded {
                generation,
                history: value,
            }) if generation == expected_generation => history = Some(value),
            Ok(RuntimeEvent::HistoryPageLoaded {
                generation,
                history: value,
                start,
                complete,
                ..
            }) if generation == expected_generation => {
                if start == 0 {
                    history_pages.clear();
                }
                history_pages.extend(value);
                if complete {
                    history = Some(std::mem::take(&mut history_pages));
                }
            }
            Ok(RuntimeEvent::BranchesLoaded {
                generation,
                catalog: value,
            }) if generation == expected_generation => branches = Some(value),
            Ok(RuntimeEvent::Failed(error)) => panic!("mock runtime failed: {error}"),
            Ok(_) | Err(flume::RecvTimeoutError::Timeout) => {}
            Err(flume::RecvTimeoutError::Disconnected) => {
                panic!("mock event channel closed while loading runtime state")
            }
        }
    }
    (
        session.expect("session_info response"),
        catalog.expect("models_list response"),
        history.expect("messages_page response"),
        branches.expect("branches_list response"),
    )
}

fn history_text(entry: &HistoryEntry) -> String {
    entry
        .blocks
        .iter()
        .filter_map(|block| match block {
            HistoryBlock::Text { text } | HistoryBlock::Plan { text, .. } => Some(text.as_str()),
            HistoryBlock::Image(_) | HistoryBlock::ToolCall(_) => None,
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn run_command(
    connection: &SnowConnection,
    command: &str,
    params: Option<Value>,
) -> Result<Option<Value>, String> {
    let mut fields = Map::new();
    if let Some(params) = params {
        fields.insert("params".into(), params);
    }
    let request_id = connection
        .client
        .command(command.into(), fields)
        .unwrap_or_else(|error| panic!("submit {command}: {error}"));
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .unwrap_or_else(|error| panic!("receive {command} response: {error}"))
        {
            RuntimeEvent::CommandCompleted {
                request_id: completed_id,
                command: completed_command,
                data,
            } if completed_id == request_id => {
                assert_eq!(completed_command, command);
                return Ok(data);
            }
            RuntimeEvent::RequestRejected {
                request_id: Some(rejected_id),
                error,
            } if rejected_id == request_id => return Err(error),
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    }
}

fn complete_fake_prompt(connection: &SnowConnection, prompt: &str) {
    let request_id = connection
        .client
        .prompt(prompt.into())
        .unwrap_or_else(|error| panic!("submit persistence prompt: {error}"));
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive persistence prompt completion")
        {
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == request_id => {
                assert_eq!(completed.status, PromptStatus::Completed);
                return;
            }
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    }
}

fn mock_pid(project_root: &Path) -> u32 {
    fs::read_to_string(project_root.join(".mock-snow.pid"))
        .expect("read mock PID")
        .trim()
        .parse()
        .expect("parse mock PID")
}

fn process_exists(pid: u32) -> bool {
    Command::new("kill")
        .args(["-0", &pid.to_string()])
        .output()
        .expect("run kill -0")
        .status
        .success()
}

#[test]
fn mock_provider_streams_text_before_completion() {
    let project = TempProject::new("streaming");
    let connection = start_ready_mock(&project.0);

    let request_id = connection
        .client
        .prompt("prove streaming".into())
        .expect("submit mock prompt");
    let mut text = String::new();
    let mut delta_count = 0;
    let mut completion_seen = false;
    let mut session_invalidated = false;
    let deadline = std::time::Instant::now() + Duration::from_secs(2);
    while std::time::Instant::now() < deadline {
        match connection.events.recv_timeout(Duration::from_millis(250)) {
            Ok(RuntimeEvent::TextDelta { text: delta }) => {
                delta_count += 1;
                text.push_str(&delta);
            }
            Ok(RuntimeEvent::SessionStateInvalidated) => session_invalidated = true,
            Ok(RuntimeEvent::PromptCompleted(completed)) if completed.request_id == request_id => {
                assert_eq!(completed.status, PromptStatus::Completed);
                completion_seen = true;
                break;
            }
            Ok(RuntimeEvent::Failed(error)) => panic!("mock runtime failed: {error}"),
            Ok(_) | Err(flume::RecvTimeoutError::Timeout) => continue,
            Err(flume::RecvTimeoutError::Disconnected) => {
                panic!("mock event channel closed before completion")
            }
        }
    }

    assert!(completion_seen, "matching prompt completion should arrive");
    assert!(
        session_invalidated,
        "session metadata invalidation should be observable"
    );
    assert_eq!(delta_count, 2, "both streaming chunks should be observable");
    assert_eq!(text, "streaming works");
    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn explicit_permission_and_thinking_overrides_are_forwarded() {
    let project = TempProject::new("launch-overrides");
    let mut config = mock_config(&project.0);
    config.permission = Some("ask".into());
    config.thinking = Some("high".into());
    let connection = start_ready(config, Some("mock-streaming"));

    let argv = fs::read_to_string(project.0.join(".mock-snow-argv")).expect("read mock argv");
    let argv: Vec<_> = argv.lines().collect();
    assert!(argv.windows(2).any(|args| args == ["--permission", "ask"]));
    assert!(argv.windows(2).any(|args| args == ["--thinking", "high"]));

    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mock_interactions_block_until_correlated_trusted_replies() {
    let project = TempProject::new("interactions");
    let connection = start_ready_mock(&project.0);
    let argv = fs::read_to_string(project.0.join(".mock-snow-argv")).expect("read mock argv");
    let argv: Vec<_> = argv.lines().collect();
    assert!(
        !argv
            .iter()
            .any(|arg| *arg == "--permission" || *arg == "--thinking"),
        "desktop must not override configured permission or thinking defaults"
    );
    for disabled in ["--no-plugins", "--no-mcp", "--no-skills", "--no-subagents"] {
        assert!(
            !argv.contains(&disabled),
            "desktop must honor configured {disabled} feature state"
        );
    }

    let prompt_id = connection
        .client
        .prompt("interactive milestone".into())
        .expect("submit interactive prompt");
    let permission = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive permission request")
        {
            RuntimeEvent::PermissionRequested(request) => break request,
            RuntimeEvent::TextDelta { text } => {
                panic!("prompt continued before permission reply: {text}")
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    };
    assert_eq!(permission.id, "perm-1");
    assert_eq!(permission.tool, "bash");
    assert_eq!(permission.args["command"], "printf trusted");
    assert!(
        matches!(
            connection.events.recv_timeout(Duration::from_millis(100)),
            Err(flume::RecvTimeoutError::Timeout)
        ),
        "fixture must remain blocked before the permission reply"
    );

    let permission_command_id = connection
        .client
        .permission_reply(permission.id, PermissionDecision::Allow)
        .expect("allow permission");
    let mut permission_confirmed = false;
    let user_input = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive permission acknowledgement and user input")
        {
            RuntimeEvent::InteractionResolved {
                command_id,
                request_id,
                command,
            } if command_id == permission_command_id => {
                assert_eq!(request_id, "perm-1");
                assert_eq!(command, "permission_reply");
                permission_confirmed = true;
            }
            RuntimeEvent::UserInputRequested(request) => break request,
            RuntimeEvent::TextDelta { text } => {
                panic!("prompt continued before user input reply: {text}")
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    };
    assert!(permission_confirmed);
    assert_eq!(user_input.id, "ask-1");
    assert_eq!(user_input.questions.len(), 2);
    assert!(
        matches!(
            connection.events.recv_timeout(Duration::from_millis(100)),
            Err(flume::RecvTimeoutError::Timeout)
        ),
        "fixture must remain blocked before the user input reply"
    );
    assert!(
        connection
            .client
            .user_input_reply(
                "stale-input".into(),
                vec![UserInputAnswer {
                    question_id: "language".into(),
                    answer: "Rust".into(),
                }],
            )
            .is_err(),
        "stale interaction IDs must be rejected locally"
    );
    assert!(
        connection
            .client
            .user_input_reply(
                user_input.id.clone(),
                vec![UserInputAnswer {
                    question_id: "language".into(),
                    answer: "Rust".into(),
                }],
            )
            .is_err(),
        "incomplete answer sets must be rejected locally"
    );
    assert!(
        connection
            .client
            .user_input_reply(
                user_input.id.clone(),
                vec![
                    UserInputAnswer {
                        question_id: "language".into(),
                        answer: "Rust".into(),
                    },
                    UserInputAnswer {
                        question_id: "language".into(),
                        answer: "Go".into(),
                    },
                ],
            )
            .is_err(),
        "duplicate answer IDs must be rejected locally"
    );

    let input_command_id = connection
        .client
        .user_input_reply(
            user_input.id,
            vec![
                UserInputAnswer {
                    question_id: "language".into(),
                    answer: "Rust".into(),
                },
                UserInputAnswer {
                    question_id: "reason".into(),
                    answer: "Safety".into(),
                },
            ],
        )
        .expect("reply to user input");
    let mut input_confirmed = false;
    let mut text = String::new();
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive interaction completion")
        {
            RuntimeEvent::InteractionResolved {
                command_id,
                request_id,
                command,
            } if command_id == input_command_id => {
                assert_eq!(request_id, "ask-1");
                assert_eq!(command, "user_input_reply");
                input_confirmed = true;
            }
            RuntimeEvent::TextDelta { text: delta } => text.push_str(&delta),
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == prompt_id => {
                assert_eq!(completed.status, PromptStatus::Completed);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }
    assert!(input_confirmed);
    assert_eq!(text, "continued after trusted replies");
    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mismatched_prompt_completion_fails_without_resolving_pending_interaction() {
    let project = TempProject::new("mismatched-completion");
    let connection = start_ready_mock(&project.0);

    connection
        .client
        .prompt("mismatched completion".into())
        .expect("submit prompt");
    let mut saw_permission = false;
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive fail-closed mismatch")
        {
            RuntimeEvent::PermissionRequested(request) => {
                assert_eq!(request.id, "mismatch-perm");
                saw_permission = true;
            }
            RuntimeEvent::Failed(error) => {
                assert!(error.contains("unknown correlation id"), "{error}");
                break;
            }
            RuntimeEvent::InteractionResolved { .. } => {
                panic!("an unknown completion must not resolve the pending interaction")
            }
            _ => {}
        }
    }
    assert!(saw_permission);
    let _ = connection.client.shutdown();
}

#[test]
fn mock_interaction_rejection_and_malformed_events_remain_correlated() {
    let project = TempProject::new("interaction-errors");
    let connection = start_ready_mock(&project.0);

    let prompt_id = connection
        .client
        .prompt("malformed interaction".into())
        .expect("submit malformed prompt");
    let request_id = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive malformed interaction")
        {
            RuntimeEvent::MalformedInteraction {
                kind,
                request_id,
                error,
            } => {
                assert_eq!(kind, InteractionKind::Permission);
                assert!(error.contains("invalid permission_request"));
                break request_id.expect("malformed event retains usable id");
            }
            RuntimeEvent::PermissionRequested(request) => {
                panic!("malformed request entered trusted UI state: {request:?}")
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    };
    let reject_id = connection
        .client
        .permission_reject(request_id)
        .expect("reject malformed permission");
    let mut rejection_confirmed = false;
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive malformed rejection completion")
        {
            RuntimeEvent::InteractionResolved {
                command_id,
                request_id,
                command,
            } if command_id == reject_id => {
                assert_eq!(request_id, "malformed-1");
                assert_eq!(command, "permission_reject");
                rejection_confirmed = true;
            }
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == prompt_id => {
                assert_eq!(completed.status, PromptStatus::Failed);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }
    assert!(rejection_confirmed);
    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mock_interaction_command_failure_reports_both_correlation_ids() {
    let project = TempProject::new("interaction-command-rejection");
    let connection = start_ready_mock(&project.0);
    connection
        .client
        .prompt("interactive milestone".into())
        .expect("submit interactive prompt");
    let permission = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .unwrap()
        {
            RuntimeEvent::PermissionRequested(request) => break request,
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    };
    connection
        .client
        .permission_reply(permission.id, PermissionDecision::Allow)
        .expect("allow permission");
    let user_input = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .unwrap()
        {
            RuntimeEvent::UserInputRequested(request) => break request,
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    };
    let command_id = connection
        .client
        .user_input_reject(user_input.id)
        .expect("reject user input");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .unwrap()
        {
            RuntimeEvent::InteractionRejected {
                command_id: rejected_command_id,
                request_id,
                command,
                error,
            } if rejected_command_id.as_deref() == Some(&command_id) => {
                assert_eq!(request_id.as_deref(), Some("ask-1"));
                assert_eq!(command, "user_input_reject");
                assert_eq!(error, "fixture rejection");
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }
    connection
        .client
        .shutdown()
        .expect("stop blocked mock Snow RPC");
}

#[test]
fn mock_thinking_change_is_correlated_and_restored() {
    let project = TempProject::new("thinking");
    let connection = start_ready_mock(&project.0);
    let (session, catalog, _, _) = load_runtime_state(&connection);
    assert_eq!(session.thinking, "off");
    assert_eq!(session.thinking_levels, ["off", "low", "high"]);
    assert!(catalog.models[0].supports_thinking);

    let request_id = connection
        .client
        .set_thinking("high".into())
        .expect("select high thinking");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive thinking change")
        {
            RuntimeEvent::ThinkingChanged {
                request_id: confirmed,
            } => {
                assert_eq!(confirmed, request_id);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let (updated, _, _, _) = load_runtime_state(&connection);
    assert_eq!(updated.thinking, "high");
    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mock_session_menu_actions_are_correlated_and_restored() {
    let project = TempProject::new("session-menu");
    let connection = start_ready_mock(&project.0);
    let (session, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(session.name, "Desktop proof");
    assert_eq!(branches.branches.len(), 1);
    assert!(branches.branches[0].active);

    let rename_id = connection
        .client
        .rename_session("API cleanup".into())
        .expect("rename session");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive session rename")
        {
            RuntimeEvent::SessionRenamed {
                request_id,
                session_id,
                name,
            } if request_id == rename_id => {
                assert_eq!(session_id, "mock-session");
                assert_eq!(name, "API cleanup");
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let fork_id = connection
        .client
        .fork_branch("main".into())
        .expect("fork active branch");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive branch fork")
        {
            RuntimeEvent::BranchForked { request_id, branch } if request_id == fork_id => {
                assert_eq!(branch.id, "experiment");
                assert!(branch.active);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }
    let (renamed, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(renamed.name, "API cleanup");
    assert_eq!(branches.branches.len(), 2);
    assert_eq!(
        branches
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| branch.id.as_str()),
        Some("experiment")
    );

    let select_id = connection
        .client
        .select_branch("main".into())
        .expect("select main branch");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive branch selection")
        {
            RuntimeEvent::BranchSelected { request_id } if request_id == select_id => break,
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }
    let (_, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(
        branches
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| branch.id.as_str()),
        Some("main")
    );

    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mock_atomic_model_thinking_change_confirms_or_rejects_by_request() {
    let project = TempProject::new("model-thinking");
    let connection = start_ready_mock(&project.0);
    let _ = load_runtime_state(&connection);

    let rejected_id = connection
        .client
        .set_model_thinking("mock-two".into(), "high".into())
        .expect("submit incompatible model and thinking");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive model rejection")
        {
            RuntimeEvent::RequestRejected { request_id, error } => {
                assert_eq!(request_id.as_deref(), Some(rejected_id.as_str()));
                assert_eq!(error, "unsupported thinking");
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let confirmed_id = connection
        .client
        .set_model_thinking("mock-two".into(), "off".into())
        .expect("submit compatible model and thinking");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive model confirmation")
        {
            RuntimeEvent::ModelChangeConfirmed { request_id } => {
                assert_eq!(request_id, confirmed_id);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let (updated, _, _, _) = load_runtime_state(&connection);
    assert_eq!(updated.model, "mock-two");
    assert_eq!(updated.thinking, "off");
    connection.client.shutdown().expect("stop mock Snow RPC");
}

#[test]
fn mock_provider_restart_reaps_each_child() {
    let project = TempProject::new("restart");
    let first = start_ready_mock(&project.0);
    let first_pid = mock_pid(&project.0);
    assert!(process_exists(first_pid), "first mock should be running");
    let (session, catalog, history, _) = load_runtime_state(&first);
    assert_eq!(session.session_id, "mock-session");
    assert_eq!(catalog.current, "mock-one");
    assert!(history.is_empty());

    first
        .client
        .set_model("mock-two".into())
        .expect("select mock-two");
    loop {
        match first
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive model change")
        {
            RuntimeEvent::ModelChanged(model) => {
                assert_eq!(model, "mock-two");
                break;
            }
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let prompt_id = first
        .client
        .prompt("persist this turn".into())
        .expect("submit continuity prompt");
    loop {
        match first
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive prompt completion")
        {
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == prompt_id => break,
            RuntimeEvent::Failed(error) => panic!("mock runtime failed: {error}"),
            _ => {}
        }
    }

    let (first_complete, first_finished) = flume::bounded(1);
    first.client.shutdown_in_background(first_complete);
    first_finished
        .recv_timeout(Duration::from_secs(4))
        .expect("first mock shutdown completed");
    assert!(
        !process_exists(first_pid),
        "old provider child must be reaped before replacement startup"
    );

    let discovered_session_path = PathBuf::from(&session.path);
    let mut replacement_config = mock_config(&project.0);
    replacement_config.no_session = false;
    replacement_config.session_path = Some(discovered_session_path.clone());
    let replacement = start_ready(replacement_config, Some("mock-streaming"));
    let replacement_pid = mock_pid(&project.0);
    assert_ne!(first_pid, replacement_pid);
    assert!(
        process_exists(replacement_pid),
        "replacement mock should be running"
    );
    let replacement_argv =
        fs::read_to_string(project.0.join(".mock-snow-argv")).expect("read replacement argv");
    let replacement_argv: Vec<_> = replacement_argv.lines().collect();
    assert!(
        replacement_argv.windows(2).any(|args| {
            args[0] == "--session" && Path::new(args[1]) == discovered_session_path
        })
    );
    assert!(!replacement_argv.contains(&"--no-session"));

    let (restored_session, restored_catalog, restored_history, _) =
        load_runtime_state(&replacement);
    assert_eq!(restored_session.session_id, session.session_id);
    assert_eq!(restored_catalog.current, "mock-two");
    assert_eq!(restored_history.len(), 2);
    assert_eq!(history_text(&restored_history[0]), "restored question");
    assert_eq!(history_text(&restored_history[1]), "restored answer");

    let (close_complete, close_finished) = flume::bounded(1);
    replacement.client.shutdown_in_background(close_complete);
    close_finished
        .recv_timeout(Duration::from_secs(4))
        .expect("close-during-replacement shutdown completed");
    assert!(
        !process_exists(replacement_pid),
        "replacement provider child must not survive close"
    );
}

#[test]
#[ignore = "requires SNOW_TEST_BINARY pointing to an existing Snow executable"]
fn fake_provider_completes_one_rpc_prompt() {
    let executable = env::var_os("SNOW_TEST_BINARY")
        .map(PathBuf::from)
        .expect("set SNOW_TEST_BINARY to the existing Snow executable");
    let project_root = env::current_dir().expect("current directory");
    let connection = SnowClient::start(RuntimeConfig {
        executable,
        project_root,
        provider: "fake".into(),
        model: None,
        permission: None,
        thinking: Some("off".into()),
        session_path: None,
        no_session: true,
        startup_timeout: Duration::from_secs(10),
        shutdown_timeout: Duration::from_secs(3),
        max_frame_bytes: 16 * 1024 * 1024,
    })
    .expect("start Snow RPC");

    let ready = loop {
        let event = connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive rpc_ready");
        match event {
            RuntimeEvent::Ready(ready) => break ready,
            RuntimeEvent::Diagnostic(_) => continue,
            RuntimeEvent::Failed(error) => panic!("Snow startup failed: {error}"),
            other => panic!("agent event arrived before rpc_ready: {other:?}"),
        }
    };
    assert_eq!(ready.protocol_version, "1");
    assert!(ready.capabilities.contains("prompt_completion"));
    assert!(ready.capabilities.contains("session_info"));

    let request_id = connection
        .client
        .prompt("integration smoke".into())
        .expect("submit prompt");
    let mut admitted = false;
    let mut turn_done = false;
    let mut completion = None;
    let deadline = std::time::Instant::now() + Duration::from_secs(10);

    while std::time::Instant::now() < deadline {
        let event = match connection.events.recv_timeout(Duration::from_millis(250)) {
            Ok(event) => event,
            Err(flume::RecvTimeoutError::Timeout) => continue,
            Err(flume::RecvTimeoutError::Disconnected) => {
                panic!("runtime event channel closed before prompt completion")
            }
        };
        match event {
            RuntimeEvent::PromptAdmitted {
                request_id: admitted_id,
            } if admitted_id == request_id => admitted = true,
            RuntimeEvent::TurnDone { .. } => turn_done = true,
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == request_id => {
                completion = Some(completed);
                break;
            }
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            RuntimeEvent::Exited {
                expected: false,
                status,
            } => panic!("Snow exited before prompt completion: {status:?}"),
            _ => {}
        }
    }

    let completion = completion.expect("matching prompt_completed frame");
    assert!(admitted, "prompt admission response was not observed");
    assert!(turn_done, "turn_done was not observed");
    assert_eq!(completion.status, PromptStatus::Completed);

    let (shutdown_complete, shutdown_finished) = flume::bounded(1);
    connection.client.shutdown_in_background(shutdown_complete);
    shutdown_finished
        .recv_timeout(Duration::from_secs(5))
        .expect("bounded background shutdown completed");
}

#[test]
#[ignore = "requires SNOW_TEST_BINARY pointing to an existing Snow executable"]
fn real_snow_restores_persistent_history_after_restart() {
    let executable = env::var_os("SNOW_TEST_BINARY")
        .map(PathBuf::from)
        .expect("set SNOW_TEST_BINARY to the existing Snow executable");
    let project = TempProject::new("real-session");
    let session_path = project.0.join("desktop-session.db");
    let config = RuntimeConfig {
        executable,
        project_root: project.0.clone(),
        provider: "fake".into(),
        model: None,
        permission: None,
        thinking: Some("off".into()),
        session_path: Some(session_path.clone()),
        no_session: false,
        startup_timeout: Duration::from_secs(10),
        shutdown_timeout: Duration::from_secs(3),
        max_frame_bytes: 16 * 1024 * 1024,
    };

    let first = start_ready(config.clone(), None);
    let (session, _, history, _) = load_runtime_state(&first);
    assert_eq!(PathBuf::from(&session.path), session_path);
    assert!(history.is_empty());
    let prompt_id = first
        .client
        .prompt("persistent desktop proof".into())
        .expect("submit persistent prompt");
    loop {
        match first
            .events
            .recv_timeout(Duration::from_secs(2))
            .expect("receive persistent prompt completion")
        {
            RuntimeEvent::PromptCompleted(completed) if completed.request_id == prompt_id => break,
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    }
    first.client.shutdown().expect("stop first Snow RPC");

    let replacement = start_ready(config, None);
    let (restored_session, _, restored_history, _) = load_runtime_state(&replacement);
    assert_eq!(restored_session.session_id, session.session_id);
    assert!(restored_history.iter().any(|message| {
        message.role == "user" && history_text(message) == "persistent desktop proof"
    }));
    replacement
        .client
        .shutdown()
        .expect("stop replacement Snow RPC");
}

#[test]
#[ignore = "requires SNOW_TEST_BINARY pointing to an existing Snow executable"]
fn real_snow_deletes_only_inactive_sessions() {
    let executable = env::var_os("SNOW_TEST_BINARY")
        .map(PathBuf::from)
        .expect("set SNOW_TEST_BINARY to the existing Snow executable");
    let project = TempProject::new("real-session-delete");
    let connection = start_ready(
        RuntimeConfig {
            executable,
            project_root: project.0.clone(),
            provider: "fake".into(),
            model: None,
            permission: None,
            thinking: Some("off".into()),
            session_path: None,
            no_session: false,
            startup_timeout: Duration::from_secs(10),
            shutdown_timeout: Duration::from_secs(3),
            max_frame_bytes: 16 * 1024 * 1024,
        },
        None,
    );
    let _ = load_runtime_state(&connection);

    let first_created = run_command(&connection, "session_create", Some(json!({})))
        .expect("create first managed real session")
        .expect("first session_create response data");
    let first_created_id = first_created["session_id"]
        .as_str()
        .expect("first created session id")
        .to_owned();
    assert_eq!(first_created["active"], true);
    complete_fake_prompt(&connection, "persist first managed session");

    let second_created = run_command(&connection, "session_create", Some(json!({})))
        .expect("create replacement real session")
        .expect("second session_create response data");
    let second_created_id = second_created["session_id"]
        .as_str()
        .expect("second created session id")
        .to_owned();
    assert_ne!(second_created_id, first_created_id);
    assert_eq!(second_created["active"], true);
    complete_fake_prompt(&connection, "persist second managed session");
    let (active_second, _, _, _) = load_runtime_state(&connection);
    assert_eq!(active_second.session_id, second_created_id);

    let opened = run_command(
        &connection,
        "session_open",
        Some(json!({"session_id": first_created_id})),
    )
    .expect("open first managed real session")
    .expect("session_open response data");
    assert_eq!(opened["session_id"], first_created_id);
    assert_eq!(opened["active"], true);

    let deleted = run_command(
        &connection,
        "session_delete",
        Some(json!({"session_id": second_created_id})),
    )
    .expect("delete inactive real session")
    .expect("session_delete response data");
    assert_eq!(deleted["session_id"], second_created_id);
    assert_eq!(deleted["deleted"], true);

    let sessions = run_command(&connection, "sessions_list", None)
        .expect("list real sessions after deletion")
        .expect("sessions_list response data");
    let sessions = sessions["sessions"]
        .as_array()
        .expect("sessions_list sessions array");
    assert!(
        sessions.iter().any(|session| {
            session["session_id"] == first_created_id && session["active"] == true
        })
    );
    assert!(
        !sessions
            .iter()
            .any(|session| session["session_id"] == second_created_id)
    );

    let active_delete_error = run_command(
        &connection,
        "session_delete",
        Some(json!({"session_id": first_created_id})),
    )
    .expect_err("active session deletion must fail");
    assert!(
        active_delete_error.contains("active session"),
        "unexpected active deletion error: {active_delete_error}"
    );
    run_command(
        &connection,
        "session_open",
        Some(json!({"session_id": second_created_id})),
    )
    .expect_err("deleted session must not reopen");

    let (active_first, _, _, _) = load_runtime_state(&connection);
    assert_eq!(active_first.session_id, first_created_id);
    connection.client.shutdown().expect("stop real Snow RPC");
}

#[test]
#[ignore = "requires SNOW_TEST_BINARY pointing to an existing Snow executable"]
fn real_snow_manages_current_session_branches() {
    let executable = env::var_os("SNOW_TEST_BINARY")
        .map(PathBuf::from)
        .expect("set SNOW_TEST_BINARY to the existing Snow executable");
    let project = TempProject::new("real-branches");
    let connection = start_ready(
        RuntimeConfig {
            executable,
            project_root: project.0.clone(),
            provider: "fake".into(),
            model: None,
            permission: None,
            thinking: Some("off".into()),
            session_path: Some(project.0.join("desktop-branches.db")),
            no_session: false,
            startup_timeout: Duration::from_secs(10),
            shutdown_timeout: Duration::from_secs(3),
            max_frame_bytes: 16 * 1024 * 1024,
        },
        None,
    );
    let (session, _, _, branches) = load_runtime_state(&connection);
    let source_branch = branches
        .branches
        .iter()
        .find(|branch| branch.active)
        .expect("active branch")
        .id
        .clone();

    let rename_id = connection
        .client
        .rename_session("Desktop branch proof".into())
        .expect("rename real session");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive real session rename")
        {
            RuntimeEvent::SessionRenamed {
                request_id,
                session_id,
                name,
            } if request_id == rename_id => {
                assert_eq!(session_id, session.session_id);
                assert_eq!(name, "Desktop branch proof");
                break;
            }
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    }

    let fork_id = connection
        .client
        .fork_branch(source_branch.clone())
        .expect("fork real branch");
    let forked_branch = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive real branch fork")
        {
            RuntimeEvent::BranchForked { request_id, branch } if request_id == fork_id => {
                break branch;
            }
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    };
    assert!(forked_branch.active);
    assert_ne!(forked_branch.id, source_branch);

    let (_, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(branches.branches.len(), 2);
    assert!(
        branches
            .branches
            .iter()
            .any(|branch| branch.id == forked_branch.id && branch.active)
    );

    let child_fork_id = connection
        .client
        .fork_branch(forked_branch.id.clone())
        .expect("fork child real branch");
    let child_branch = loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive real child branch fork")
        {
            RuntimeEvent::BranchForked { request_id, branch } if request_id == child_fork_id => {
                break branch;
            }
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    };
    assert!(child_branch.active);
    assert_eq!(child_branch.parent_branch_id, forked_branch.id);

    let renamed = run_command(
        &connection,
        "branch_rename",
        Some(json!({
            "branch_id": child_branch.id,
            "name": "Desktop child branch",
        })),
    )
    .expect("rename real child branch")
    .expect("branch_rename response data");
    assert_eq!(renamed["id"], child_branch.id);
    assert_eq!(renamed["name"], "Desktop child branch");

    let (_, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(branches.branches.len(), 3);
    assert!(branches.branches.iter().any(|branch| {
        branch.id == child_branch.id && branch.name == "Desktop child branch" && branch.active
    }));

    let non_leaf_error = run_command(
        &connection,
        "branch_delete",
        Some(json!({"branch_id": forked_branch.id})),
    )
    .expect_err("non-leaf branch deletion must fail");
    assert!(
        non_leaf_error.contains("children"),
        "unexpected non-leaf deletion error: {non_leaf_error}"
    );

    let select_id = connection
        .client
        .select_branch(source_branch.clone())
        .expect("select original real branch");
    loop {
        match connection
            .events
            .recv_timeout(Duration::from_secs(10))
            .expect("receive real branch selection")
        {
            RuntimeEvent::BranchSelected { request_id } if request_id == select_id => break,
            RuntimeEvent::Failed(error) => panic!("Snow runtime failed: {error}"),
            _ => {}
        }
    }

    assert!(
        run_command(
            &connection,
            "branch_delete",
            Some(json!({"branch_id": child_branch.id})),
        )
        .expect("delete leaf child branch")
        .is_none()
    );
    let (_, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(branches.branches.len(), 2);
    assert!(
        !branches
            .branches
            .iter()
            .any(|branch| branch.id == child_branch.id)
    );
    assert!(
        branches
            .branches
            .iter()
            .any(|branch| branch.id == forked_branch.id)
    );

    assert!(
        run_command(
            &connection,
            "branch_delete",
            Some(json!({"branch_id": forked_branch.id})),
        )
        .expect("delete newly-leaf parent branch")
        .is_none()
    );
    let (_, _, _, branches) = load_runtime_state(&connection);
    assert_eq!(branches.branches.len(), 1);
    assert!(
        branches
            .branches
            .iter()
            .any(|branch| branch.id == source_branch && branch.active)
    );

    connection.client.shutdown().expect("stop real Snow RPC");
}
