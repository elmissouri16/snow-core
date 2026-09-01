use std::{
    collections::HashMap,
    io::{BufReader, Read, Write},
    process::Child,
    sync::{
        Arc, Condvar, Mutex,
        atomic::{AtomicBool, AtomicUsize, Ordering},
    },
    thread::{self, JoinHandle},
    time::{Duration, Instant},
};

use flume::{Receiver, Sender};
use serde_json::{Map, Value};
use uuid::Uuid;

use super::{
    SnowError,
    process::{RuntimeConfig, read_bounded_frame, spawn},
    protocol::{
        AgentEvent, BranchCatalog, BranchForkParams, BranchSelectParams, DecodedHistoryPage,
        HistoryEntry, InteractionKind, KeybindingsUpdateParams, MAX_SUBAGENT_IDENTITY_BYTES,
        MAX_SUBAGENT_MESSAGE_CURSOR_BYTES, MAX_SUBAGENT_MESSAGE_PAGE_BYTES,
        MAX_SUBAGENT_MESSAGES_PER_PAGE, MIN_SUBAGENT_MESSAGE_PAGE_BYTES, MalformedInteraction,
        MessagesPageParams, ModelCatalog, PermissionDecision, PermissionRejectParams,
        PermissionReplyParams, PermissionRequest, PromptCompleted, PromptStatus,
        REQUIRED_CAPABILITIES, RPC_PROTOCOL_VERSION, RpcFrame, RpcReady, RpcRequest, RpcResponse,
        SessionBranch, SessionInfo, SessionRenameParams, SessionRenameResult, SubagentMessagesPage,
        SubagentMessagesParams, ThemeSettingsUpdateParams, UserInputAnswer, UserInputRejectParams,
        UserInputReplyParams, UserInputRequest, decode_frame, decode_history_entries,
        decode_history_page, decode_keybindings, decode_settings, decode_subagent_messages_page,
        decode_theme_catalog, encode_request, validate_keybindings_update,
        validate_theme_selection,
    },
};

const COMMAND_QUEUE_CAPACITY: usize = 32;
const EVENT_QUEUE_CAPACITY: usize = 256;
const EVENT_SEND_TIMEOUT: Duration = Duration::from_secs(1);
const HISTORY_PAGE_LIMIT: usize = 32;
const HISTORY_PAGE_MAX_BYTES: usize = 2 * 1024 * 1024;

#[derive(Debug, Clone)]
pub enum RuntimeEvent {
    Ready(RpcReady),
    ModelsLoaded {
        generation: String,
        catalog: ModelCatalog,
    },
    SessionLoaded {
        generation: String,
        info: SessionInfo,
    },
    HistoryLoaded {
        generation: String,
        history: Vec<HistoryEntry>,
    },
    HistoryPageLoaded {
        generation: String,
        history: Vec<HistoryEntry>,
        start: usize,
        next_start: usize,
        total: usize,
        complete: bool,
    },
    BranchesLoaded {
        generation: String,
        catalog: BranchCatalog,
    },
    SessionStateInvalidated,
    RuntimeStateFailed {
        generation: String,
        command: String,
        error: String,
    },
    ModelChanged(String),
    ModelChangeConfirmed {
        request_id: String,
    },
    ThinkingChanged {
        request_id: String,
    },
    BranchSelected {
        request_id: String,
    },
    BranchForked {
        request_id: String,
        branch: SessionBranch,
    },
    SessionRenamed {
        request_id: String,
        session_id: String,
        name: String,
    },
    PromptAdmitted {
        request_id: String,
    },
    RequestRejected {
        request_id: Option<String>,
        error: String,
    },
    CommandCompleted {
        request_id: String,
        command: String,
        data: Option<Value>,
    },
    ChildActivity {
        path: String,
        kind: String,
        detail: Option<String>,
    },
    TextDelta {
        text: String,
    },
    PlanDelta {
        text: String,
    },
    ThinkingDelta {
        text: String,
    },
    ToolStarted {
        call_id: String,
        name: String,
    },
    ToolProgress {
        call_id: String,
        message: Option<String>,
    },
    ToolFinished {
        call_id: String,
        name: String,
        is_error: bool,
        preview: Option<String>,
    },
    TurnDone {
        turn_id: Option<String>,
    },
    UnsupportedInteraction {
        kind: String,
        request_id: Option<String>,
    },
    PermissionRequested(PermissionRequest),
    UserInputRequested(UserInputRequest),
    InteractionResolved {
        command_id: String,
        request_id: String,
        command: String,
    },
    InteractionRejected {
        command_id: Option<String>,
        request_id: Option<String>,
        command: String,
        error: String,
    },
    MalformedInteraction {
        kind: InteractionKind,
        request_id: Option<String>,
        error: String,
    },
    PromptCompleted(PromptCompleted),
    Status(String),
    Diagnostic(String),
    Failed(String),
    Exited {
        expected: bool,
        status: Option<i32>,
    },
}

pub struct SnowConnection {
    pub client: SnowClient,
    pub events: Receiver<RuntimeEvent>,
}

pub struct SnowClient {
    shared: Arc<Shared>,
    workers: Mutex<Vec<JoinHandle<()>>>,
}

#[derive(Debug)]
struct PendingInteractionCommand {
    kind: InteractionKind,
    request_id: String,
    command: &'static str,
}

#[derive(Debug, Clone)]
struct PendingInteraction {
    resolving: bool,
    question_ids: Option<Vec<String>>,
}

#[derive(Debug, Clone)]
struct PendingSubagentMessages {
    target: String,
    max_bytes: usize,
}

/// Correlates a child-history request with the caller's selected-detail generation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SubagentMessagesRequest {
    pub request_id: String,
    pub generation: u64,
}

#[derive(Debug, Default)]
struct PendingHistoryLoad {
    next_start: usize,
    total: Option<usize>,
    superseded: bool,
}

fn begin_pending_history_load(
    pending_history: &mut HashMap<String, PendingHistoryLoad>,
    generation: String,
) {
    for pending in pending_history.values_mut() {
        pending.superseded = true;
    }
    pending_history.insert(generation, PendingHistoryLoad::default());
}

struct Shared {
    command_tx: Sender<WriterCommand>,
    event_tx: Sender<RuntimeEvent>,
    child: Arc<Mutex<Child>>,
    ready: AtomicBool,
    shutting_down: AtomicBool,
    max_input_bytes: AtomicUsize,
    active_prompt: Mutex<Option<PendingPrompt>>,
    pending_interactions: Mutex<HashMap<(InteractionKind, String), PendingInteraction>>,
    interaction_commands: Mutex<HashMap<String, PendingInteractionCommand>>,
    pending_commands: Mutex<HashMap<String, String>>,
    pending_history: Mutex<HashMap<String, PendingHistoryLoad>>,
    pending_subagent_messages: Mutex<HashMap<String, PendingSubagentMessages>>,
    termination_watchdog_started: AtomicBool,
    shutdown_timeout: Duration,
}

#[derive(Clone)]
pub(crate) struct ShutdownTracker {
    shared: Arc<Shared>,
    state: Arc<(Mutex<bool>, Condvar)>,
}

impl ShutdownTracker {
    fn new(shared: Arc<Shared>) -> Self {
        Self {
            shared,
            state: Arc::new((Mutex::new(false), Condvar::new())),
        }
    }

    fn finish(&self) {
        let (finished, notification) = &*self.state;
        *finished
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = true;
        notification.notify_all();
    }

    pub(crate) fn wait_and_force(&self) {
        let deadline = Instant::now() + self.shared.shutdown_timeout + EVENT_SEND_TIMEOUT;
        let (finished, notification) = &*self.state;
        let mut finished = finished
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        while !*finished {
            let now = Instant::now();
            if now >= deadline {
                break;
            }
            let remaining = deadline.saturating_duration_since(now);
            let (next, _) = notification
                .wait_timeout(finished, remaining)
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            finished = next;
        }
        if *finished {
            return;
        }
        drop(finished);
        force_stop_child(&self.shared);
        self.finish();
    }
}

struct PendingPrompt {
    id: String,
    abort_pending: bool,
}

impl PendingPrompt {
    fn begin_abort(&mut self) -> Result<(), SnowError> {
        if self.abort_pending {
            return Err(SnowError::AbortAlreadyRequested);
        }
        self.abort_pending = true;
        Ok(())
    }
}

enum WriterCommand {
    Frame(Vec<u8>),
    Shutdown,
}

impl SnowClient {
    pub fn start(config: RuntimeConfig) -> Result<SnowConnection, SnowError> {
        let process = spawn(&config)?;
        let (command_tx, command_rx) = flume::bounded(COMMAND_QUEUE_CAPACITY);
        let (event_tx, event_rx) = flume::bounded(EVENT_QUEUE_CAPACITY);
        let child = Arc::new(Mutex::new(process.child));
        let shared = Arc::new(Shared {
            command_tx,
            event_tx,
            child,
            ready: AtomicBool::new(false),
            shutting_down: AtomicBool::new(false),
            max_input_bytes: AtomicUsize::new(config.max_frame_bytes),
            active_prompt: Mutex::new(None),
            pending_interactions: Mutex::new(HashMap::new()),
            interaction_commands: Mutex::new(HashMap::new()),
            pending_commands: Mutex::new(HashMap::new()),
            pending_history: Mutex::new(HashMap::new()),
            pending_subagent_messages: Mutex::new(HashMap::new()),
            termination_watchdog_started: AtomicBool::new(false),
            shutdown_timeout: config.shutdown_timeout,
        });

        let workers = vec![
            spawn_writer(process.stdin, command_rx, Arc::clone(&shared)),
            spawn_stdout_reader(process.stdout, config.max_frame_bytes, Arc::clone(&shared)),
            spawn_stderr_reader(process.stderr, Arc::clone(&shared)),
            spawn_supervisor(Arc::clone(&shared)),
            spawn_startup_timer(config.startup_timeout, Arc::clone(&shared)),
        ];

        Ok(SnowConnection {
            client: Self {
                shared,
                workers: Mutex::new(workers),
            },
            events: event_rx,
        })
    }

    pub fn prompt(&self, message: String) -> Result<String, SnowError> {
        self.prompt_with_mode(message, None)
    }

    pub fn prompt_with_mode(
        &self,
        message: String,
        mode: Option<String>,
    ) -> Result<String, SnowError> {
        self.prompt_content(message, Vec::new(), mode)
    }

    pub fn prompt_content(
        &self,
        message: String,
        content: Vec<Value>,
        mode: Option<String>,
    ) -> Result<String, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if self.shared.shutting_down.load(Ordering::Acquire) {
            return Err(SnowError::ChannelClosed);
        }
        let message = message.trim().to_owned();
        if message.is_empty() && content.is_empty() {
            return Err(SnowError::Protocol("prompt must not be empty".into()));
        }
        if content.iter().any(|block| {
            block.get("type").and_then(Value::as_str) != Some("image")
                || block.get("mime_type").and_then(Value::as_str).is_none()
                || block.get("data").and_then(Value::as_str).is_none()
        }) {
            return Err(SnowError::Protocol(
                "prompt content must contain only encoded image blocks".into(),
            ));
        }
        let mode = mode
            .map(|mode| mode.trim().to_owned())
            .filter(|mode| !mode.is_empty());
        if mode
            .as_deref()
            .is_some_and(|mode| !matches!(mode, "default" | "plan"))
        {
            return Err(SnowError::Protocol(
                "prompt mode must be default or plan".into(),
            ));
        }

        let id = Uuid::new_v4().to_string();
        {
            let mut active = self
                .shared
                .active_prompt
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if active.is_some() {
                return Err(SnowError::PromptAlreadyRunning);
            }
            *active = Some(PendingPrompt {
                id: id.clone(),
                abort_pending: false,
            });
        }

        let request = if content.is_empty() {
            match mode {
                Some(mode) => RpcRequest::PromptWithMode {
                    id: id.clone(),
                    message,
                    mode,
                },
                None => RpcRequest::Prompt {
                    id: id.clone(),
                    message,
                },
            }
        } else {
            RpcRequest::PromptContent {
                id: id.clone(),
                message,
                content,
                mode,
            }
        };
        let result = self.send_request(request);
        if result.is_err() {
            clear_matching_active_prompt(&self.shared, &id);
        }
        result.map(|()| id)
    }

    pub fn project_init(&self) -> Result<String, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if self.shared.shutting_down.load(Ordering::Acquire) {
            return Err(SnowError::ChannelClosed);
        }
        let id = Uuid::new_v4().to_string();
        {
            let mut active = self
                .shared
                .active_prompt
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if active.is_some() {
                return Err(SnowError::PromptAlreadyRunning);
            }
            *active = Some(PendingPrompt {
                id: id.clone(),
                abort_pending: false,
            });
        }
        if let Err(error) = self.send_request(RpcRequest::ProjectInit { id: id.clone() }) {
            clear_matching_active_prompt(&self.shared, &id);
            return Err(error);
        }
        Ok(id)
    }

    pub fn command(
        &self,
        command: String,
        fields: Map<String, Value>,
    ) -> Result<String, SnowError> {
        let id = Uuid::new_v4().to_string();
        let request = RpcRequest::raw(id.clone(), command.clone(), fields)?;
        self.send_correlated_command(id, command, request)
    }

    pub fn load_themes(&self) -> Result<String, SnowError> {
        let id = Uuid::new_v4().to_string();
        self.send_correlated_command(
            id.clone(),
            "themes_list".into(),
            RpcRequest::ThemesList { id },
        )
    }

    pub fn load_keybindings(&self) -> Result<String, SnowError> {
        let id = Uuid::new_v4().to_string();
        self.send_correlated_command(
            id.clone(),
            "keybindings_get".into(),
            RpcRequest::KeybindingsGet { id },
        )
    }

    pub fn update_keybindings(
        &self,
        params: KeybindingsUpdateParams,
        project_allowed: bool,
    ) -> Result<String, SnowError> {
        validate_keybindings_update(&params, project_allowed)?;
        let id = Uuid::new_v4().to_string();
        self.send_correlated_command(
            id.clone(),
            "keybindings_update".into(),
            RpcRequest::KeybindingsUpdate { id, params },
        )
    }

    pub fn load_settings(&self) -> Result<String, SnowError> {
        let id = Uuid::new_v4().to_string();
        self.send_correlated_command(
            id.clone(),
            "settings_get".into(),
            RpcRequest::SettingsGet { id },
        )
    }

    pub fn update_theme(&self, theme: String) -> Result<String, SnowError> {
        validate_theme_selection(&theme)?;
        let id = Uuid::new_v4().to_string();
        self.send_correlated_command(
            id.clone(),
            "settings_update".into(),
            RpcRequest::SettingsThemeUpdate {
                id,
                params: ThemeSettingsUpdateParams { theme },
            },
        )
    }

    fn send_correlated_command(
        &self,
        id: String,
        command: String,
        request: RpcRequest,
    ) -> Result<String, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if self.shared.shutting_down.load(Ordering::Acquire) {
            return Err(SnowError::ChannelClosed);
        }
        self.shared
            .pending_commands
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .insert(id.clone(), command);
        if let Err(error) = self.send_request(request) {
            self.shared
                .pending_commands
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .remove(&id);
            return Err(error);
        }
        Ok(id)
    }

    pub fn load_subagent_messages(
        &self,
        target: String,
        request_generation: u64,
        cursor: Option<String>,
        limit: usize,
        max_bytes: usize,
    ) -> Result<SubagentMessagesRequest, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if self.shared.shutting_down.load(Ordering::Acquire) {
            return Err(SnowError::ChannelClosed);
        }
        let target = target.trim().to_owned();
        if target.is_empty() || target.len() > MAX_SUBAGENT_IDENTITY_BYTES {
            return Err(SnowError::Protocol(format!(
                "subagent_messages target must contain 1..={MAX_SUBAGENT_IDENTITY_BYTES} bytes"
            )));
        }
        if let Some(cursor) = cursor.as_deref()
            && (cursor.is_empty() || cursor.len() > MAX_SUBAGENT_MESSAGE_CURSOR_BYTES)
        {
            return Err(SnowError::Protocol(format!(
                "subagent_messages cursor must contain 1..={MAX_SUBAGENT_MESSAGE_CURSOR_BYTES} bytes"
            )));
        }
        if !(1..=MAX_SUBAGENT_MESSAGES_PER_PAGE).contains(&limit) {
            return Err(SnowError::Protocol(format!(
                "subagent_messages limit must be between 1 and {MAX_SUBAGENT_MESSAGES_PER_PAGE}"
            )));
        }
        if !(MIN_SUBAGENT_MESSAGE_PAGE_BYTES..=MAX_SUBAGENT_MESSAGE_PAGE_BYTES).contains(&max_bytes)
        {
            return Err(SnowError::Protocol(format!(
                "subagent_messages max_bytes must be between {MIN_SUBAGENT_MESSAGE_PAGE_BYTES} and {MAX_SUBAGENT_MESSAGE_PAGE_BYTES}"
            )));
        }

        let id = Uuid::new_v4().to_string();
        self.shared
            .pending_subagent_messages
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .insert(
                id.clone(),
                PendingSubagentMessages {
                    target: target.clone(),
                    max_bytes,
                },
            );
        let request = RpcRequest::SubagentMessages {
            id: id.clone(),
            params: SubagentMessagesParams {
                target,
                cursor,
                limit,
                max_bytes,
            },
        };
        if let Err(error) = self.send_request(request) {
            self.shared
                .pending_subagent_messages
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .remove(&id);
            return Err(error);
        }
        Ok(SubagentMessagesRequest {
            request_id: id,
            generation: request_generation,
        })
    }

    pub fn load_runtime_state(&self) -> Result<String, SnowError> {
        let generation = Uuid::new_v4().to_string();
        let mut pending_history = self
            .shared
            .pending_history
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        begin_pending_history_load(&mut pending_history, generation.clone());
        drop(pending_history);
        if let Err(error) = self.send_runtime_requests(
            &generation,
            &[
                "session_info",
                "messages_page",
                "models_list",
                "branches_list",
            ],
        ) {
            self.shared
                .pending_history
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .remove(&generation);
            return Err(error);
        }
        Ok(generation)
    }

    pub fn load_session_metadata(&self) -> Result<String, SnowError> {
        let generation = Uuid::new_v4().to_string();
        self.send_runtime_requests(&generation, &["session_info", "branches_list"])?;
        Ok(generation)
    }

    fn send_runtime_requests(&self, generation: &str, commands: &[&str]) -> Result<(), SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        for command in commands {
            let id = runtime_request_id(generation, command);
            let request = match *command {
                "session_info" => RpcRequest::SessionInfo { id },
                "messages_list" => RpcRequest::MessagesList { id },
                "messages_page" => RpcRequest::MessagesPage {
                    id,
                    params: MessagesPageParams {
                        cursor: None,
                        limit: HISTORY_PAGE_LIMIT,
                        max_bytes: HISTORY_PAGE_MAX_BYTES,
                    },
                },
                "models_list" => RpcRequest::ModelsList { id },
                "branches_list" => RpcRequest::BranchesList { id },
                _ => {
                    return Err(SnowError::Protocol(format!(
                        "unsupported runtime state request {command}"
                    )));
                }
            };
            self.send_request(request)?;
        }
        Ok(())
    }

    pub fn set_model(&self, model: String) -> Result<String, SnowError> {
        self.set_model_with_thinking(model, None)
    }

    pub fn set_model_thinking(&self, model: String, thinking: String) -> Result<String, SnowError> {
        self.set_model_with_thinking(model, Some(thinking))
    }

    fn set_model_with_thinking(
        &self,
        model: String,
        thinking: Option<String>,
    ) -> Result<String, SnowError> {
        self.require_idle()?;
        let id = Uuid::new_v4().to_string();
        self.send_request(RpcRequest::SetModel {
            id: id.clone(),
            model,
            thinking,
        })?;
        Ok(id)
    }

    pub fn set_thinking(&self, thinking: String) -> Result<String, SnowError> {
        self.require_idle()?;
        let id = Uuid::new_v4().to_string();
        self.send_request(RpcRequest::SetThinking {
            id: id.clone(),
            thinking,
        })?;
        Ok(id)
    }

    pub fn select_branch(&self, branch_id: String) -> Result<String, SnowError> {
        self.require_idle()?;
        let id = Uuid::new_v4().to_string();
        self.send_request(RpcRequest::BranchSelect {
            id: id.clone(),
            params: BranchSelectParams { branch_id },
        })?;
        Ok(id)
    }

    pub fn fork_branch(&self, source_branch_id: String) -> Result<String, SnowError> {
        self.require_idle()?;
        let id = Uuid::new_v4().to_string();
        self.send_request(RpcRequest::BranchFork {
            id: id.clone(),
            params: BranchForkParams { source_branch_id },
        })?;
        Ok(id)
    }

    pub fn rename_session(&self, name: String) -> Result<String, SnowError> {
        self.require_idle()?;
        let id = Uuid::new_v4().to_string();
        self.send_request(RpcRequest::SessionRename {
            id: id.clone(),
            params: SessionRenameParams { name },
        })?;
        Ok(id)
    }

    pub fn permission_reply(
        &self,
        request_id: String,
        decision: PermissionDecision,
    ) -> Result<String, SnowError> {
        self.send_interaction_request(
            InteractionKind::Permission,
            request_id.clone(),
            "permission_reply",
            RpcRequest::PermissionReply {
                id: String::new(),
                params: PermissionReplyParams {
                    request_id,
                    decision,
                },
            },
        )
    }

    pub fn permission_reject(&self, request_id: String) -> Result<String, SnowError> {
        self.send_interaction_request(
            InteractionKind::Permission,
            request_id.clone(),
            "permission_reject",
            RpcRequest::PermissionReject {
                id: String::new(),
                params: PermissionRejectParams { request_id },
            },
        )
    }

    pub fn user_input_reply(
        &self,
        request_id: String,
        answers: Vec<UserInputAnswer>,
    ) -> Result<String, SnowError> {
        if answers.is_empty() {
            return Err(SnowError::Protocol(
                "user input reply must contain at least one answer".into(),
            ));
        }
        let mut normalized = Vec::with_capacity(answers.len());
        for answer in answers {
            let question_id = answer.question_id.trim().to_owned();
            let value = answer.answer.trim().to_owned();
            if question_id.is_empty() || value.is_empty() {
                return Err(SnowError::Protocol(
                    "user input answer ids and values must be non-empty".into(),
                ));
            }
            if value.len() > 8 * 1024 {
                return Err(SnowError::Protocol(
                    "user input answer exceeds the 8 KiB limit".into(),
                ));
            }
            normalized.push(UserInputAnswer {
                question_id,
                answer: value,
            });
        }
        let question_ids = self
            .shared
            .pending_interactions
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .get(&(InteractionKind::UserInput, request_id.clone()))
            .and_then(|interaction| interaction.question_ids.clone())
            .ok_or_else(|| {
                SnowError::Protocol(format!(
                    "user_input_reply does not match a complete pending user input request {request_id}"
                ))
            })?;
        if normalized.len() != question_ids.len() {
            return Err(SnowError::Protocol(
                "user input reply must answer every pending question exactly once".into(),
            ));
        }
        let mut ordered = Vec::with_capacity(question_ids.len());
        for question_id in question_ids {
            let mut matches = normalized
                .iter()
                .filter(|answer| answer.question_id == question_id);
            let Some(answer) = matches.next() else {
                return Err(SnowError::Protocol(format!(
                    "user input reply is missing question {question_id}"
                )));
            };
            if matches.next().is_some() {
                return Err(SnowError::Protocol(format!(
                    "user input reply contains duplicate question {question_id}"
                )));
            }
            ordered.push(answer.clone());
        }
        self.send_interaction_request(
            InteractionKind::UserInput,
            request_id.clone(),
            "user_input_reply",
            RpcRequest::UserInputReply {
                id: String::new(),
                params: UserInputReplyParams {
                    request_id,
                    answers: ordered,
                },
            },
        )
    }

    pub fn user_input_reject(&self, request_id: String) -> Result<String, SnowError> {
        self.send_interaction_request(
            InteractionKind::UserInput,
            request_id.clone(),
            "user_input_reject",
            RpcRequest::UserInputReject {
                id: String::new(),
                params: UserInputRejectParams { request_id },
            },
        )
    }

    fn send_interaction_request(
        &self,
        kind: InteractionKind,
        request_id: String,
        command: &'static str,
        mut request: RpcRequest,
    ) -> Result<String, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if request_id.trim().is_empty() {
            return Err(SnowError::Protocol(format!(
                "{command} request id must be non-empty"
            )));
        }
        if !command.ends_with("_reject")
            && self
                .shared
                .active_prompt
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .is_none()
        {
            return Err(SnowError::NoActivePrompt);
        }
        let interaction_key = (kind, request_id.clone());
        {
            let mut interactions = self
                .shared
                .pending_interactions
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            let Some(interaction) = interactions.get_mut(&interaction_key) else {
                return Err(SnowError::Protocol(format!(
                    "{command} does not match a pending {} request {request_id}",
                    kind.label()
                )));
            };
            if interaction.resolving {
                return Err(SnowError::Protocol(format!(
                    "{} request {request_id} already has a reply in flight",
                    kind.label()
                )));
            }
            interaction.resolving = true;
        }
        let command_id = Uuid::new_v4().to_string();
        match &mut request {
            RpcRequest::PermissionReply { id, .. }
            | RpcRequest::PermissionReject { id, .. }
            | RpcRequest::UserInputReply { id, .. }
            | RpcRequest::UserInputReject { id, .. } => id.clone_from(&command_id),
            _ => {
                mark_interaction_retryable(&self.shared, kind, &request_id);
                return Err(SnowError::Protocol(
                    "internal interaction request mismatch".into(),
                ));
            }
        }
        self.shared
            .interaction_commands
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .insert(
                command_id.clone(),
                PendingInteractionCommand {
                    kind,
                    request_id: request_id.clone(),
                    command,
                },
            );
        if let Err(error) = self.send_request(request) {
            self.shared
                .interaction_commands
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .remove(&command_id);
            mark_interaction_retryable(&self.shared, kind, &request_id);
            return Err(error);
        }
        Ok(command_id)
    }

    fn require_idle(&self) -> Result<(), SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        if self
            .shared
            .active_prompt
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .is_some()
        {
            return Err(SnowError::PromptAlreadyRunning);
        }
        Ok(())
    }

    pub fn abort(&self) -> Result<String, SnowError> {
        if !self.shared.ready.load(Ordering::Acquire) {
            return Err(SnowError::NotReady);
        }
        let mut active = self
            .shared
            .active_prompt
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let Some(active) = active.as_mut() else {
            return Err(SnowError::NoActivePrompt);
        };
        active.begin_abort()?;

        let id = Uuid::new_v4().to_string();
        if let Err(error) = self.send_request(RpcRequest::Abort { id: id.clone() }) {
            active.abort_pending = false;
            return Err(error);
        }
        Ok(id)
    }

    pub fn shutdown(&self) -> Result<(), SnowError> {
        initiate_shutdown(&self.shared);
        finish_shutdown(Arc::clone(&self.shared), self.take_workers())
    }

    pub fn shutdown_in_background(&self, completion: Sender<()>) {
        let _ = self.shutdown_in_background_tracked(completion);
    }

    pub(crate) fn shutdown_in_background_tracked(&self, completion: Sender<()>) -> ShutdownTracker {
        initiate_shutdown(&self.shared);
        let tracker = ShutdownTracker::new(Arc::clone(&self.shared));
        let workers = self.take_workers();
        if workers.is_empty() {
            tracker.finish();
            let _ = completion.try_send(());
            return tracker;
        }
        let tracker_from_thread = tracker.clone();
        let completion_from_thread = completion.clone();
        if thread::Builder::new()
            .name("snow-rpc-background-shutdown".into())
            .spawn(move || {
                let _ = finish_shutdown(Arc::clone(&tracker_from_thread.shared), workers);
                tracker_from_thread.finish();
                let _ = completion_from_thread.try_send(());
            })
            .is_err()
        {
            force_stop_child(&tracker.shared);
            tracker.finish();
            let _ = completion.try_send(());
        }
        tracker
    }

    fn take_workers(&self) -> Vec<JoinHandle<()>> {
        self.workers
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .drain(..)
            .collect()
    }

    fn send_request(&self, request: RpcRequest) -> Result<(), SnowError> {
        queue_request(&self.shared, request)
    }
}

impl Drop for SnowClient {
    fn drop(&mut self) {
        let (completion, _ignored) = flume::bounded(1);
        self.shutdown_in_background(completion);
    }
}

fn finish_shutdown(shared: Arc<Shared>, workers: Vec<JoinHandle<()>>) -> Result<(), SnowError> {
    let deadline = Instant::now() + shared.shutdown_timeout;
    let mut result = Ok(());

    loop {
        let exited = {
            let mut child = shared
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            match child.try_wait() {
                Ok(status) => status.is_some(),
                Err(error) => {
                    result = Err(SnowError::io(error));
                    true
                }
            }
        };
        if exited {
            break;
        }
        if Instant::now() >= deadline {
            force_stop_child(&shared);
            result = Err(SnowError::ShutdownTimeout);
            break;
        }
        thread::sleep(Duration::from_millis(25));
    }

    let current = thread::current().id();
    for worker in workers {
        if worker.thread().id() != current {
            let _ = worker.join();
        }
    }
    result
}

fn force_stop_child(shared: &Shared) {
    let mut child = shared
        .child
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let _ = child.kill();
    let _ = child.wait();
}

fn spawn_writer(
    mut stdin: std::process::ChildStdin,
    commands: Receiver<WriterCommand>,
    shared: Arc<Shared>,
) -> JoinHandle<()> {
    thread::Builder::new()
        .name("snow-rpc-writer".into())
        .spawn(move || {
            while !shared.shutting_down.load(Ordering::Acquire) {
                let command = match commands.recv_timeout(Duration::from_millis(50)) {
                    Ok(command) => command,
                    Err(flume::RecvTimeoutError::Timeout) => continue,
                    Err(flume::RecvTimeoutError::Disconnected) => break,
                };
                match command {
                    WriterCommand::Frame(frame) => {
                        if shared.shutting_down.load(Ordering::Acquire) {
                            break;
                        }
                        if let Err(error) = stdin.write_all(&frame).and_then(|()| stdin.flush()) {
                            fail_runtime(
                                &shared,
                                SnowError::Protocol(format!("Snow stdin write failed: {error}")),
                            );
                            break;
                        }
                    }
                    WriterCommand::Shutdown => break,
                }
            }
        })
        .expect("failed to spawn Snow RPC writer")
}

fn spawn_stdout_reader(
    stdout: std::process::ChildStdout,
    frame_limit: usize,
    shared: Arc<Shared>,
) -> JoinHandle<()> {
    thread::Builder::new()
        .name("snow-rpc-reader".into())
        .spawn(move || {
            let mut reader = BufReader::new(stdout);
            let mut first_frame = true;
            loop {
                let bytes = match read_bounded_frame(&mut reader, frame_limit) {
                    Ok(Some(bytes)) => bytes,
                    Ok(None) => return,
                    Err(error) => {
                        fail_runtime(&shared, error);
                        return;
                    }
                };
                if bytes.is_empty() {
                    continue;
                }
                let frame = match decode_frame(&bytes) {
                    Ok(frame) => frame,
                    Err(error) => {
                        fail_runtime(&shared, error);
                        return;
                    }
                };
                if first_frame {
                    first_frame = false;
                    let RpcFrame::Ready(ready) = frame else {
                        let kind = frame_kind(&frame);
                        fail_runtime(&shared, SnowError::InvalidFirstFrame(kind));
                        return;
                    };
                    if let Err(error) = accept_ready(&shared, ready) {
                        fail_runtime(&shared, error);
                        return;
                    }
                    continue;
                }
                if let Err(error) = dispatch_frame(&shared, frame) {
                    fail_runtime(&shared, error);
                    return;
                }
            }
        })
        .expect("failed to spawn Snow RPC reader")
}

fn spawn_stderr_reader(
    mut stderr: std::process::ChildStderr,
    shared: Arc<Shared>,
) -> JoinHandle<()> {
    thread::Builder::new()
        .name("snow-rpc-stderr".into())
        .spawn(move || {
            let mut buffer = [0_u8; 4096];
            loop {
                match stderr.read(&mut buffer) {
                    Ok(0) => return,
                    Ok(read) => {
                        let message = String::from_utf8_lossy(&buffer[..read]).trim().to_owned();
                        if !message.is_empty() {
                            emit_event(&shared, RuntimeEvent::Diagnostic(message));
                        }
                    }
                    Err(error) => {
                        if !shared.shutting_down.load(Ordering::Acquire) {
                            emit_event(
                                &shared,
                                RuntimeEvent::Diagnostic(format!(
                                    "Snow stderr read failed: {error}"
                                )),
                            );
                        }
                        return;
                    }
                }
            }
        })
        .expect("failed to spawn Snow stderr reader")
}

fn spawn_supervisor(shared: Arc<Shared>) -> JoinHandle<()> {
    thread::Builder::new()
        .name("snow-rpc-supervisor".into())
        .spawn(move || {
            loop {
                let status = {
                    let mut child = shared
                        .child
                        .lock()
                        .unwrap_or_else(|poisoned| poisoned.into_inner());
                    child.try_wait()
                };
                match status {
                    Ok(Some(status)) => {
                        let expected = shared.shutting_down.swap(true, Ordering::AcqRel);
                        shared.ready.store(false, Ordering::Release);
                        force_clear_active_prompt(&shared);
                        let _ = shared.command_tx.try_send(WriterCommand::Shutdown);
                        emit_event(
                            &shared,
                            RuntimeEvent::Exited {
                                expected,
                                status: status.code(),
                            },
                        );
                        return;
                    }
                    Ok(None) => thread::sleep(Duration::from_millis(25)),
                    Err(error) => {
                        fail_runtime(&shared, SnowError::io(error));
                        return;
                    }
                }
            }
        })
        .expect("failed to spawn Snow supervisor")
}

fn spawn_startup_timer(timeout: Duration, shared: Arc<Shared>) -> JoinHandle<()> {
    thread::Builder::new()
        .name("snow-rpc-startup-timeout".into())
        .spawn(move || {
            let deadline = Instant::now() + timeout;
            while Instant::now() < deadline {
                if shared.ready.load(Ordering::Acquire)
                    || shared.shutting_down.load(Ordering::Acquire)
                {
                    return;
                }
                thread::sleep(Duration::from_millis(25));
            }
            if !shared.ready.load(Ordering::Acquire) {
                fail_runtime(&shared, SnowError::StartupTimeout);
            }
        })
        .expect("failed to spawn Snow startup timer")
}

fn validate_ready(ready: &RpcReady) -> Result<(), SnowError> {
    if ready.protocol_version != RPC_PROTOCOL_VERSION {
        return Err(SnowError::UnsupportedProtocol(
            ready.protocol_version.clone(),
        ));
    }
    for capability in REQUIRED_CAPABILITIES {
        if !ready.capabilities.contains(capability) {
            return Err(SnowError::MissingCapability(capability.into()));
        }
    }
    Ok(())
}

fn accept_ready(shared: &Shared, ready: RpcReady) -> Result<(), SnowError> {
    validate_ready(&ready)?;
    let local_limit = shared.max_input_bytes.load(Ordering::Acquire);
    shared
        .max_input_bytes
        .store(local_limit.min(ready.max_input_bytes), Ordering::Release);
    shared.ready.store(true, Ordering::Release);
    emit_event(shared, RuntimeEvent::Ready(ready));
    Ok(())
}

fn runtime_request_id(generation: &str, command: &str) -> String {
    format!("runtime:{generation}:{command}")
}

fn runtime_response_generation(
    request_id: Option<&str>,
    command: &str,
) -> Result<String, SnowError> {
    let request_id = request_id
        .ok_or_else(|| SnowError::Protocol(format!("{command} response is missing request id")))?;
    let prefix = "runtime:";
    let suffix = format!(":{command}");
    request_id
        .strip_prefix(prefix)
        .and_then(|value| value.strip_suffix(&suffix))
        .filter(|generation| !generation.is_empty())
        .map(str::to_owned)
        .ok_or_else(|| {
            SnowError::Protocol(format!(
                "{command} response has an invalid runtime request id"
            ))
        })
}

struct HistoryPageAdvance {
    entries: Vec<HistoryEntry>,
    start: usize,
    next_start: usize,
    total: usize,
    next_cursor: Option<String>,
}

impl HistoryPageAdvance {
    fn complete(&self) -> bool {
        self.next_cursor.is_none()
    }
}

fn advance_history_page(
    shared: &Shared,
    generation: &str,
    page: DecodedHistoryPage,
) -> Result<Option<HistoryPageAdvance>, SnowError> {
    let mut loads = shared
        .pending_history
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    let result = (|| {
        let superseded = loads
            .get(generation)
            .ok_or_else(|| {
                SnowError::Protocol(format!(
                    "messages_page response has unknown runtime generation {generation}"
                ))
            })?
            .superseded;
        if superseded {
            loads.remove(generation);
            return Ok(None);
        }
        let pending = loads
            .get_mut(generation)
            .expect("active history load exists after supersession check");
        if page.start != pending.next_start {
            return Err(SnowError::Protocol(format!(
                "messages_page starts at {}, expected {}",
                page.start, pending.next_start
            )));
        }
        match pending.total {
            Some(total) if total != page.total => {
                return Err(SnowError::Protocol(format!(
                    "messages_page total changed from {total} to {}",
                    page.total
                )));
            }
            None => pending.total = Some(page.total),
            _ => {}
        }
        pending.next_start = pending
            .next_start
            .checked_add(page.wire_count)
            .ok_or_else(|| SnowError::Protocol("messages_page progress overflow".into()))?;
        if !page.has_more && pending.next_start != page.total {
            return Err(SnowError::Protocol(format!(
                "terminal messages_page loaded {} of {} messages",
                pending.next_start, page.total
            )));
        }
        let advance = HistoryPageAdvance {
            entries: page.entries,
            start: page.start,
            next_start: pending.next_start,
            total: page.total,
            next_cursor: page.next_cursor,
        };
        if advance.complete() {
            loads.remove(generation);
        }
        Ok(Some(advance))
    })();
    if result.is_err() {
        loads.remove(generation);
    }
    result
}

fn queue_request(shared: &Shared, request: RpcRequest) -> Result<(), SnowError> {
    let frame = encode_request(&request, shared.max_input_bytes.load(Ordering::Acquire))?;
    shared
        .command_tx
        .try_send(WriterCommand::Frame(frame))
        .map_err(|error| match error {
            flume::TrySendError::Full(_) => SnowError::CommandQueueFull,
            flume::TrySendError::Disconnected(_) => SnowError::ChannelClosed,
        })
}

fn correlate_command_response(
    pending_commands: &Mutex<HashMap<String, String>>,
    mut response: RpcResponse,
) -> Result<RpcResponse, Box<RuntimeEvent>> {
    let Some(request_id) = response.id.as_deref() else {
        return Ok(response);
    };
    let Some(expected_command) = pending_commands
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .remove(request_id)
    else {
        return Ok(response);
    };

    let request_id = response
        .id
        .take()
        .expect("correlated response has a request id");
    if response.command.as_deref() != Some(expected_command.as_str()) {
        let actual_command = response.command.as_deref().unwrap_or("<missing>");
        return Err(Box::new(RuntimeEvent::RequestRejected {
            request_id: Some(request_id),
            error: format!(
                "RPC response command mismatch: expected {expected_command}, received {actual_command}"
            ),
        }));
    }
    if !response.success {
        return Err(Box::new(RuntimeEvent::RequestRejected {
            request_id: Some(request_id),
            error: response
                .error
                .unwrap_or_else(|| format!("{expected_command} request failed")),
        }));
    }

    let data = match sanitize_presentation_response(&expected_command, response.data) {
        Ok(data) => data,
        Err(error) => {
            return Err(Box::new(RuntimeEvent::RequestRejected {
                request_id: Some(request_id),
                error: error.to_string(),
            }));
        }
    };
    Err(Box::new(RuntimeEvent::CommandCompleted {
        request_id,
        command: expected_command,
        data,
    }))
}

fn sanitize_presentation_response(
    command: &str,
    data: Option<Value>,
) -> Result<Option<Value>, SnowError> {
    let Some(data) = data else {
        if matches!(
            command,
            "themes_list"
                | "keybindings_get"
                | "keybindings_update"
                | "settings_get"
                | "settings_update"
        ) {
            return Err(SnowError::Protocol(format!(
                "{command} response is missing data"
            )));
        }
        return Ok(None);
    };
    let sanitized = match command {
        "themes_list" => serde_json::to_value(decode_theme_catalog(data)?),
        "keybindings_get" | "keybindings_update" => serde_json::to_value(decode_keybindings(data)?),
        "settings_get" | "settings_update" => serde_json::to_value(decode_settings(data)?),
        _ => return Ok(Some(data)),
    }
    .map_err(|_| SnowError::Protocol(format!("could not normalize {command} response data")))?;
    Ok(Some(sanitized))
}

fn subagent_page_matches_target(page: &SubagentMessagesPage, target: &str) -> bool {
    page.agent.path == target || page.agent.thread_id == target
}

fn validate_subagent_response_command(
    pending: &Mutex<HashMap<String, PendingSubagentMessages>>,
    response: &RpcResponse,
) -> Result<(), SnowError> {
    let Some(request_id) = response.id.as_deref() else {
        return Ok(());
    };
    let mut pending = pending
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    if !pending.contains_key(request_id) {
        return Ok(());
    }
    if response.command.as_deref() == Some("subagent_messages") {
        return Ok(());
    }
    pending.remove(request_id);
    Err(SnowError::Protocol(format!(
        "subagent_messages response command mismatch for correlation id {request_id}"
    )))
}

fn dispatch_frame(shared: &Shared, frame: RpcFrame) -> Result<(), SnowError> {
    match frame {
        RpcFrame::Ready(_) => Err(SnowError::Protocol(
            "received a second rpc_ready frame".into(),
        )),
        RpcFrame::Response(response) => {
            validate_subagent_response_command(&shared.pending_subagent_messages, &response)?;
            let response = match correlate_command_response(&shared.pending_commands, response) {
                Ok(response) => response,
                Err(event) => {
                    emit_event(shared, *event);
                    return Ok(());
                }
            };
            if matches!(response.command.as_deref(), Some("prompt" | "project_init")) {
                let request_id = response.id.ok_or_else(|| {
                    SnowError::Protocol("prompt response is missing its correlation id".into())
                })?;
                if !active_prompt_matches(shared, &request_id) {
                    return Err(SnowError::Protocol(format!(
                        "prompt response has unknown correlation id {request_id}"
                    )));
                }
                if response.success {
                    emit_event(shared, RuntimeEvent::PromptAdmitted { request_id });
                } else {
                    let error = response
                        .error
                        .unwrap_or_else(|| "prompt admission failed".into());
                    clear_matching_active_prompt(shared, &request_id);
                    emit_event(
                        shared,
                        RuntimeEvent::RequestRejected {
                            request_id: Some(request_id),
                            error,
                        },
                    );
                }
            } else if !response.success {
                let command = response.command.unwrap_or_else(|| "request".into());
                let error = response.error.unwrap_or_else(|| "request failed".into());
                if command == "subagent_messages" {
                    let request_id = response.id.ok_or_else(|| {
                        SnowError::Protocol(
                            "subagent_messages rejection is missing request id".into(),
                        )
                    })?;
                    let pending = shared
                        .pending_subagent_messages
                        .lock()
                        .unwrap_or_else(|poisoned| poisoned.into_inner())
                        .remove(&request_id);
                    if pending.is_none() {
                        return Err(SnowError::Protocol(format!(
                            "subagent_messages rejection has unknown correlation id {request_id}"
                        )));
                    }
                    emit_event(
                        shared,
                        RuntimeEvent::RequestRejected {
                            request_id: Some(request_id),
                            error,
                        },
                    );
                } else {
                    let pending = response
                        .id
                        .as_deref()
                        .and_then(|id| take_interaction_command(shared, id));
                    if is_interaction_command(&command) || pending.is_some() {
                        let command_id = response.id;
                        if let Some(pending) = pending.as_ref() {
                            mark_interaction_retryable(shared, pending.kind, &pending.request_id);
                        }
                        emit_event(
                            shared,
                            RuntimeEvent::InteractionRejected {
                                command_id,
                                request_id: pending.map(|pending| pending.request_id),
                                command,
                                error,
                            },
                        );
                    } else if matches!(
                        command.as_str(),
                        "models_list"
                            | "session_info"
                            | "messages_list"
                            | "messages_page"
                            | "branches_list"
                    ) {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), &command)?;
                        if command == "messages_page" {
                            shared
                                .pending_history
                                .lock()
                                .unwrap_or_else(|poisoned| poisoned.into_inner())
                                .remove(&generation);
                        }
                        emit_event(
                            shared,
                            RuntimeEvent::RuntimeStateFailed {
                                generation,
                                command,
                                error,
                            },
                        );
                    } else {
                        if command == "abort" {
                            clear_abort_pending(shared);
                        }
                        emit_event(
                            shared,
                            RuntimeEvent::RequestRejected {
                                request_id: response.id,
                                error,
                            },
                        );
                    }
                }
            } else {
                let data = response.data;
                match response.command.as_deref() {
                    Some(command) if is_interaction_command(command) => {
                        let command_id = response.id.ok_or_else(|| {
                            SnowError::Protocol(format!("{command} response is missing request id"))
                        })?;
                        if let Some(pending) = take_interaction_command(shared, &command_id) {
                            if pending.command != command {
                                mark_interaction_retryable(
                                    shared,
                                    pending.kind,
                                    &pending.request_id,
                                );
                                emit_event(
                                    shared,
                                    RuntimeEvent::InteractionRejected {
                                        command_id: Some(command_id),
                                        request_id: Some(pending.request_id),
                                        command: command.into(),
                                        error: format!(
                                            "interaction response command mismatch: expected {}",
                                            pending.command
                                        ),
                                    },
                                );
                            } else {
                                resolve_pending_interaction(shared, &pending);
                                emit_event(
                                    shared,
                                    RuntimeEvent::InteractionResolved {
                                        command_id,
                                        request_id: pending.request_id,
                                        command: command.into(),
                                    },
                                );
                            }
                        } else {
                            emit_event(
                                shared,
                                RuntimeEvent::InteractionRejected {
                                    command_id: Some(command_id),
                                    request_id: None,
                                    command: command.into(),
                                    error: "interaction response has an unknown correlation id"
                                        .into(),
                                },
                            );
                        }
                    }
                    Some("subagent_messages") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol(
                                "subagent_messages response is missing request id".into(),
                            )
                        })?;
                        let pending = shared
                            .pending_subagent_messages
                            .lock()
                            .unwrap_or_else(|poisoned| poisoned.into_inner())
                            .remove(&request_id)
                            .ok_or_else(|| {
                                SnowError::Protocol(format!(
                                    "subagent_messages response has unknown correlation id {request_id}"
                                ))
                            })?;
                        let data = data.ok_or_else(|| {
                            SnowError::Protocol("subagent_messages response is missing data".into())
                        })?;
                        let page = decode_subagent_messages_page(data.clone(), pending.max_bytes)?;
                        if !subagent_page_matches_target(&page, &pending.target) {
                            return Err(SnowError::Protocol(format!(
                                "subagent_messages response identity does not match target {:?}",
                                pending.target
                            )));
                        }
                        emit_event(
                            shared,
                            RuntimeEvent::CommandCompleted {
                                request_id,
                                command: "subagent_messages".into(),
                                data: Some(data),
                            },
                        );
                    }
                    Some("models_list") => {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), "models_list")?;
                        let catalog: ModelCatalog =
                            serde_json::from_value(data.ok_or_else(|| {
                                SnowError::Protocol("models_list response is missing data".into())
                            })?)
                            .map_err(|error| {
                                SnowError::Protocol(format!(
                                    "invalid models_list response data: {error}"
                                ))
                            })?;
                        emit_event(
                            shared,
                            RuntimeEvent::ModelsLoaded {
                                generation,
                                catalog,
                            },
                        );
                    }
                    Some("session_info") => {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), "session_info")?;
                        let info: SessionInfo = serde_json::from_value(data.ok_or_else(|| {
                            SnowError::Protocol("session_info response is missing data".into())
                        })?)
                        .map_err(|error| {
                            SnowError::Protocol(format!(
                                "invalid session_info response data: {error}"
                            ))
                        })?;
                        emit_event(shared, RuntimeEvent::SessionLoaded { generation, info });
                    }
                    Some("messages_page") => {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), "messages_page")?;
                        let page = decode_history_page(data.ok_or_else(|| {
                            SnowError::Protocol("messages_page response is missing data".into())
                        })?)?;
                        let Some(advance) = advance_history_page(shared, &generation, page)? else {
                            return Ok(());
                        };
                        if let Some(cursor) = advance.next_cursor.as_ref() {
                            let request = RpcRequest::MessagesPage {
                                id: runtime_request_id(&generation, "messages_page"),
                                params: MessagesPageParams {
                                    cursor: Some(cursor.clone()),
                                    limit: HISTORY_PAGE_LIMIT,
                                    max_bytes: HISTORY_PAGE_MAX_BYTES,
                                },
                            };
                            if let Err(error) = queue_request(shared, request) {
                                shared
                                    .pending_history
                                    .lock()
                                    .unwrap_or_else(|poisoned| poisoned.into_inner())
                                    .remove(&generation);
                                emit_event(
                                    shared,
                                    RuntimeEvent::RuntimeStateFailed {
                                        generation,
                                        command: "messages_page".into(),
                                        error: error.to_string(),
                                    },
                                );
                                return Ok(());
                            }
                        }
                        let complete = advance.complete();
                        emit_event(
                            shared,
                            RuntimeEvent::HistoryPageLoaded {
                                generation,
                                history: advance.entries,
                                start: advance.start,
                                next_start: advance.next_start,
                                total: advance.total,
                                complete,
                            },
                        );
                    }
                    Some("messages_list") => {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), "messages_list")?;
                        let history = decode_history_entries(data.ok_or_else(|| {
                            SnowError::Protocol("messages_list response is missing data".into())
                        })?)?;
                        emit_event(
                            shared,
                            RuntimeEvent::HistoryLoaded {
                                generation,
                                history,
                            },
                        );
                    }
                    Some("branches_list") => {
                        let generation =
                            runtime_response_generation(response.id.as_deref(), "branches_list")?;
                        let branches: BranchCatalog =
                            serde_json::from_value(data.ok_or_else(|| {
                                SnowError::Protocol("branches_list response is missing data".into())
                            })?)
                            .map_err(|error| {
                                SnowError::Protocol(format!(
                                    "invalid branches_list response data: {error}"
                                ))
                            })?;
                        emit_event(
                            shared,
                            RuntimeEvent::BranchesLoaded {
                                generation,
                                catalog: branches,
                            },
                        );
                    }
                    Some("branch_select") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol(
                                "branch_select response is missing request id".into(),
                            )
                        })?;
                        emit_event(shared, RuntimeEvent::BranchSelected { request_id });
                    }
                    Some("branch_fork") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol("branch_fork response is missing request id".into())
                        })?;
                        let branch: SessionBranch =
                            serde_json::from_value(data.ok_or_else(|| {
                                SnowError::Protocol("branch_fork response is missing data".into())
                            })?)
                            .map_err(|error| {
                                SnowError::Protocol(format!(
                                    "invalid branch_fork response data: {error}"
                                ))
                            })?;
                        emit_event(shared, RuntimeEvent::BranchForked { request_id, branch });
                    }
                    Some("session_rename") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol(
                                "session_rename response is missing request id".into(),
                            )
                        })?;
                        let renamed: SessionRenameResult =
                            serde_json::from_value(data.ok_or_else(|| {
                                SnowError::Protocol(
                                    "session_rename response is missing data".into(),
                                )
                            })?)
                            .map_err(|error| {
                                SnowError::Protocol(format!(
                                    "invalid session_rename response data: {error}"
                                ))
                            })?;
                        emit_event(
                            shared,
                            RuntimeEvent::SessionRenamed {
                                request_id,
                                session_id: renamed.session_id,
                                name: renamed.name,
                            },
                        );
                    }
                    Some("set_model") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol("set_model response is missing request id".into())
                        })?;
                        emit_event(shared, RuntimeEvent::ModelChangeConfirmed { request_id });
                    }
                    Some("set_thinking") => {
                        let request_id = response.id.ok_or_else(|| {
                            SnowError::Protocol(
                                "set_thinking response is missing request id".into(),
                            )
                        })?;
                        emit_event(shared, RuntimeEvent::ThinkingChanged { request_id });
                    }
                    command => {
                        if let Some(command_id) = response.id
                            && let Some(pending) = take_interaction_command(shared, &command_id)
                        {
                            mark_interaction_retryable(shared, pending.kind, &pending.request_id);
                            emit_event(
                                shared,
                                RuntimeEvent::InteractionRejected {
                                    command_id: Some(command_id),
                                    request_id: Some(pending.request_id),
                                    command: command.unwrap_or("response").into(),
                                    error: format!(
                                        "interaction response command mismatch: expected {}",
                                        pending.command
                                    ),
                                },
                            );
                        }
                    }
                }
            }
            Ok(())
        }
        RpcFrame::PromptCompleted(completed) => {
            if !clear_matching_active_prompt(shared, &completed.request_id) {
                return Err(SnowError::Protocol(format!(
                    "prompt_completed has unknown correlation id {}",
                    completed.request_id
                )));
            }
            clear_interactions(shared);
            emit_event(shared, RuntimeEvent::PromptCompleted(completed));
            Ok(())
        }
        RpcFrame::PermissionRequest(request) => {
            register_pending_interaction(shared, InteractionKind::Permission, &request.id, None);
            if has_active_prompt(shared) {
                emit_event(shared, RuntimeEvent::PermissionRequested(request));
            } else {
                emit_event(
                    shared,
                    RuntimeEvent::MalformedInteraction {
                        kind: InteractionKind::Permission,
                        request_id: Some(request.id),
                        error: "received permission_request without an active prompt".into(),
                    },
                );
            }
            Ok(())
        }
        RpcFrame::UserInputRequest(request) => {
            register_pending_interaction(
                shared,
                InteractionKind::UserInput,
                &request.id,
                Some(
                    request
                        .questions
                        .iter()
                        .map(|question| question.id.clone())
                        .collect(),
                ),
            );
            if has_active_prompt(shared) {
                emit_event(shared, RuntimeEvent::UserInputRequested(request));
            } else {
                emit_event(
                    shared,
                    RuntimeEvent::MalformedInteraction {
                        kind: InteractionKind::UserInput,
                        request_id: Some(request.id),
                        error: "received user_input_request without an active prompt".into(),
                    },
                );
            }
            Ok(())
        }
        RpcFrame::MalformedInteraction(MalformedInteraction {
            kind,
            request_id,
            mut error,
        }) => {
            if let Some(request_id) = request_id.as_deref() {
                register_pending_interaction(shared, kind, request_id, None);
            }
            if request_id.is_none()
                && let Err(abort_error) = begin_fail_closed_abort(shared)
            {
                error.push_str(&format!("; could not abort blocked turn: {abort_error}"));
            }
            emit_event(
                shared,
                RuntimeEvent::MalformedInteraction {
                    kind,
                    request_id,
                    error,
                },
            );
            Ok(())
        }
        RpcFrame::Agent(event) => {
            dispatch_agent_event(shared, event);
            Ok(())
        }
        RpcFrame::Unknown(frame) => {
            emit_event(
                shared,
                RuntimeEvent::Diagnostic(format!(
                    "ignored unknown Snow RPC frame type {}",
                    frame.kind
                )),
            );
            Ok(())
        }
    }
}

fn dispatch_agent_event(shared: &Shared, event: AgentEvent) {
    if event.has_agent() {
        let path = event
            .nested_string("agent", "path")
            .unwrap_or("child")
            .to_owned();
        let detail = event
            .string("tool_name")
            .or_else(|| event.string("message"))
            .or_else(|| event.string("text"))
            .map(|detail| truncate_chars(detail, 160));
        emit_event(
            shared,
            RuntimeEvent::ChildActivity {
                path,
                kind: event.kind,
                detail,
            },
        );
        return;
    }

    let runtime_event = match event.kind.as_str() {
        "text_delta" => event
            .string("text")
            .map(|text| RuntimeEvent::TextDelta { text: text.into() }),
        "plan_delta" => event
            .string("text")
            .map(|text| RuntimeEvent::PlanDelta { text: text.into() }),
        "thinking_delta" => event
            .string("text")
            .map(|text| RuntimeEvent::ThinkingDelta { text: text.into() }),
        "tool_start" => Some(RuntimeEvent::ToolStarted {
            call_id: event.string("tool_call_id").unwrap_or_default().into(),
            name: event.string("tool_name").unwrap_or("tool").into(),
        }),
        "tool_progress" => Some(RuntimeEvent::ToolProgress {
            call_id: event
                .nested_string("tool_progress", "tool_call_id")
                .unwrap_or_default()
                .into(),
            message: event
                .nested_string("tool_progress", "message")
                .map(str::to_owned),
        }),
        "tool_end" => Some(RuntimeEvent::ToolFinished {
            call_id: event.string("tool_call_id").unwrap_or_default().into(),
            name: event.string("tool_name").unwrap_or("tool").into(),
            is_error: event.boolean("is_error").unwrap_or(false),
            preview: event
                .string("tool_output")
                .map(|output| truncate_chars(output, 400)),
        }),
        "turn_done" => Some(RuntimeEvent::TurnDone {
            turn_id: event.string("turn_id").map(str::to_owned),
        }),
        "user_input_request" => Some(RuntimeEvent::UnsupportedInteraction {
            kind: "user input".into(),
            request_id: event.nested_string("user_input", "id").map(str::to_owned),
        }),
        "permission_request" => Some(RuntimeEvent::UnsupportedInteraction {
            kind: "permission".into(),
            request_id: event
                .nested_object_string("permission", "request", "id")
                .map(str::to_owned),
        }),
        "model_changed" => event
            .nested_string("model", "id")
            .map(|model| RuntimeEvent::ModelChanged(model.into())),
        "subagent_started" | "subagent_status" | "subagent_message" | "subagent_activity" => {
            Some(RuntimeEvent::ChildActivity {
                path: event
                    .nested_object_string("subagent", "agent", "path")
                    .or_else(|| event.nested_string("agent_message", "author"))
                    .unwrap_or("subagent")
                    .into(),
                kind: event.kind.clone(),
                detail: event
                    .nested_string("subagent", "status")
                    .or_else(|| event.nested_string("agent_message", "kind"))
                    .map(str::to_owned),
            })
        }
        "session_updated" | "run_stats_updated" => Some(RuntimeEvent::SessionStateInvalidated),
        "provider_retry" => Some(RuntimeEvent::Status("Provider retrying…".into())),
        "error" => Some(RuntimeEvent::Failed(
            event.string("message").unwrap_or("agent error").into(),
        )),
        "aborted" => Some(RuntimeEvent::Status("Turn aborted".into())),
        _ => None,
    };
    if let Some(event) = runtime_event {
        emit_event(shared, event);
    }
}

fn truncate_chars(value: &str, limit: usize) -> String {
    let mut chars = value.chars();
    let truncated: String = chars.by_ref().take(limit).collect();
    if chars.next().is_some() {
        format!("{truncated}…")
    } else {
        truncated
    }
}

fn is_interaction_command(command: &str) -> bool {
    matches!(
        command,
        "permission_reply" | "permission_reject" | "user_input_reply" | "user_input_reject"
    )
}

fn register_pending_interaction(
    shared: &Shared,
    kind: InteractionKind,
    request_id: &str,
    question_ids: Option<Vec<String>>,
) {
    shared
        .pending_interactions
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .entry((kind, request_id.to_owned()))
        .or_insert(PendingInteraction {
            resolving: false,
            question_ids,
        });
}

fn mark_interaction_retryable(shared: &Shared, kind: InteractionKind, request_id: &str) {
    if let Some(interaction) = shared
        .pending_interactions
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .get_mut(&(kind, request_id.to_owned()))
    {
        interaction.resolving = false;
    }
}

fn resolve_pending_interaction(shared: &Shared, pending: &PendingInteractionCommand) {
    shared
        .pending_interactions
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .remove(&(pending.kind, pending.request_id.clone()));
}

fn take_interaction_command(
    shared: &Shared,
    command_id: &str,
) -> Option<PendingInteractionCommand> {
    shared
        .interaction_commands
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .remove(command_id)
}

fn clear_interactions(shared: &Shared) {
    shared
        .pending_interactions
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .clear();
    shared
        .interaction_commands
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .clear();
}

fn has_active_prompt(shared: &Shared) -> bool {
    shared
        .active_prompt
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .is_some()
}

fn active_prompt_matches(shared: &Shared, expected: &str) -> bool {
    shared
        .active_prompt
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .as_ref()
        .is_some_and(|active| active.id == expected)
}

fn clear_matching_active_prompt(shared: &Shared, expected: &str) -> bool {
    let mut active = shared
        .active_prompt
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    if !active.as_ref().is_some_and(|active| active.id == expected) {
        return false;
    }
    *active = None;
    true
}

fn begin_fail_closed_abort(shared: &Shared) -> Result<(), SnowError> {
    {
        let mut active = shared
            .active_prompt
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let Some(active) = active.as_mut() else {
            return Ok(());
        };
        if active.abort_pending {
            return Ok(());
        }
        active.abort_pending = true;
    }
    let frame = encode_request(
        &RpcRequest::Abort {
            id: Uuid::new_v4().to_string(),
        },
        shared.max_input_bytes.load(Ordering::Acquire),
    )?;
    shared
        .command_tx
        .try_send(WriterCommand::Frame(frame))
        .map_err(|error| {
            clear_abort_pending(shared);
            match error {
                flume::TrySendError::Full(_) => SnowError::CommandQueueFull,
                flume::TrySendError::Disconnected(_) => SnowError::ChannelClosed,
            }
        })
}

fn clear_abort_pending(shared: &Shared) {
    if let Some(active) = shared
        .active_prompt
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .as_mut()
    {
        active.abort_pending = false;
    }
}

fn force_clear_active_prompt(shared: &Shared) {
    *shared
        .active_prompt
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner()) = None;
    clear_interactions(shared);
}

fn frame_kind(frame: &RpcFrame) -> String {
    match frame {
        RpcFrame::Ready(_) => "rpc_ready",
        RpcFrame::Response(_) => "response",
        RpcFrame::PromptCompleted(_) => "prompt_completed",
        RpcFrame::PermissionRequest(_) => "permission_request",
        RpcFrame::UserInputRequest(_) => "user_input_request",
        RpcFrame::MalformedInteraction(interaction) => match interaction.kind {
            InteractionKind::Permission => "permission_request",
            InteractionKind::UserInput => "user_input_request",
        },
        RpcFrame::Agent(event) => event.kind.as_str(),
        RpcFrame::Unknown(frame) => frame.kind.as_str(),
    }
    .to_owned()
}

fn fail_runtime(shared: &Arc<Shared>, error: SnowError) {
    emit_event(shared, RuntimeEvent::Failed(error.to_string()));
    initiate_shutdown(shared);
    start_termination_watchdog(shared);
}

fn start_termination_watchdog(shared: &Arc<Shared>) {
    if shared
        .termination_watchdog_started
        .swap(true, Ordering::AcqRel)
    {
        return;
    }
    let shared = Arc::clone(shared);
    let _ = thread::Builder::new()
        .name("snow-rpc-termination-watchdog".into())
        .spawn(move || {
            let deadline = Instant::now() + shared.shutdown_timeout;
            while Instant::now() < deadline {
                let exited = {
                    let mut child = shared
                        .child
                        .lock()
                        .unwrap_or_else(|poisoned| poisoned.into_inner());
                    child.try_wait().ok().flatten().is_some()
                };
                if exited {
                    return;
                }
                thread::sleep(Duration::from_millis(25));
            }
            let mut child = shared
                .child
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            let _ = child.kill();
            let _ = child.wait();
        });
}

fn emit_event(shared: &Shared, event: RuntimeEvent) {
    if shared
        .event_tx
        .send_timeout(event, EVENT_SEND_TIMEOUT)
        .is_err()
    {
        initiate_shutdown(shared);
        // The host is no longer draining lifecycle events, so there is no safe
        // interactive recovery path. Reap immediately instead of relying on a
        // child that may ignore stdin closure or waiting for another event to
        // start the termination watchdog.
        force_stop_child(shared);
    }
}

fn initiate_shutdown(shared: &Shared) {
    shared.shutting_down.store(true, Ordering::Release);
    shared.ready.store(false, Ordering::Release);
    let _ = shared.command_tx.try_send(WriterCommand::Shutdown);
}

pub fn completion_summary(completed: &PromptCompleted) -> &'static str {
    match completed.status {
        PromptStatus::Completed => "Completed",
        PromptStatus::Failed => "Failed",
        PromptStatus::Canceled => "Canceled",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ready(version: &str, capabilities: &[&str]) -> RpcReady {
        RpcReady {
            protocol_version: version.into(),
            snow_version: "test".into(),
            capabilities: capabilities.iter().map(|value| (*value).into()).collect(),
            max_input_bytes: 1024,
        }
    }

    #[test]
    fn validates_protocol_version_and_required_capabilities() {
        assert!(validate_ready(&ready("1", &REQUIRED_CAPABILITIES)).is_ok());
        assert!(matches!(
            validate_ready(&ready("2", &REQUIRED_CAPABILITIES)),
            Err(SnowError::UnsupportedProtocol(_))
        ));
        assert!(matches!(
            validate_ready(&ready("1", &["prompt_completion"])),
            Err(SnowError::MissingCapability(_))
        ));
        for required in [
            "permission_interaction",
            "presentation_settings",
            "subagent_messages",
            "user_input",
        ] {
            let capabilities: Vec<_> = REQUIRED_CAPABILITIES
                .into_iter()
                .filter(|capability| *capability != required)
                .collect();
            assert!(matches!(
                validate_ready(&ready("1", &capabilities)),
                Err(SnowError::MissingCapability(capability)) if capability == required
            ));
        }
    }

    #[test]
    fn newer_history_load_supersedes_prior_page_chains() {
        let mut loads = HashMap::new();
        begin_pending_history_load(&mut loads, "older".into());
        loads.get_mut("older").unwrap().next_start = 32;

        begin_pending_history_load(&mut loads, "newer".into());

        assert!(loads["older"].superseded);
        assert_eq!(loads["older"].next_start, 32);
        assert!(!loads["newer"].superseded);
        assert_eq!(loads["newer"].next_start, 0);
    }

    #[test]
    fn pending_prompt_accepts_only_one_abort() {
        let mut pending = PendingPrompt {
            id: "p1".into(),
            abort_pending: false,
        };
        assert!(pending.begin_abort().is_ok());
        assert!(matches!(
            pending.begin_abort(),
            Err(SnowError::AbortAlreadyRequested)
        ));
    }

    #[test]
    fn tool_preview_is_bounded_by_characters() {
        assert_eq!(truncate_chars("éclair", 2), "éc…");
        assert_eq!(truncate_chars("ok", 8), "ok");
    }

    #[test]
    fn completion_labels_are_stable() {
        for (status, label) in [
            (PromptStatus::Completed, "Completed"),
            (PromptStatus::Failed, "Failed"),
            (PromptStatus::Canceled, "Canceled"),
        ] {
            assert_eq!(
                completion_summary(&PromptCompleted {
                    request_id: "p".into(),
                    status,
                    error: None,
                }),
                label
            );
        }
    }

    #[test]
    fn subagent_message_pages_match_only_stable_requested_identity() {
        let page = SubagentMessagesPage {
            agent: crate::snow::AgentRef {
                path: "/root/reviewer".into(),
                thread_id: "thread-reviewer".into(),
                ..Default::default()
            },
            generation: 4,
            messages: Vec::new(),
            next_cursor: None,
            start: 0,
            total: 0,
            wire_count: 0,
            has_more: false,
        };
        assert!(subagent_page_matches_target(&page, "/root/reviewer"));
        assert!(subagent_page_matches_target(&page, "thread-reviewer"));
        assert!(!subagent_page_matches_target(&page, "/root/stale"));
        assert!(!subagent_page_matches_target(&page, "thread-stale"));
    }

    #[test]
    fn subagent_message_command_mismatch_is_stale_safe_and_consumed() {
        let pending = Mutex::new(HashMap::from([(
            "request-1".into(),
            PendingSubagentMessages {
                target: "/root/reviewer".into(),
                max_bytes: MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
            },
        )]));
        let response = RpcResponse {
            id: Some("request-1".into()),
            command: Some("subagent_get".into()),
            success: true,
            data: None,
            error: None,
            error_code: None,
        };
        let error = validate_subagent_response_command(&pending, &response).unwrap_err();
        assert!(error.to_string().contains("command mismatch"));
        assert!(pending.lock().unwrap().is_empty());
    }

    #[test]
    fn raw_command_success_is_correlated_to_completion() {
        let pending = Mutex::new(HashMap::from([(
            "command-1".into(),
            "thread_goal_set".into(),
        )]));
        let response = RpcResponse {
            id: Some("command-1".into()),
            command: Some("thread_goal_set".into()),
            success: true,
            data: Some(serde_json::json!({"goal": "Ship it"})),
            error: None,
            error_code: None,
        };

        let Err(event) = correlate_command_response(&pending, response) else {
            panic!("expected a correlated command completion");
        };
        let RuntimeEvent::CommandCompleted {
            request_id,
            command,
            data,
        } = *event
        else {
            panic!("expected a correlated command completion");
        };
        assert_eq!(request_id, "command-1");
        assert_eq!(command, "thread_goal_set");
        assert_eq!(data, Some(serde_json::json!({"goal": "Ship it"})));
        assert!(pending.lock().unwrap().is_empty());
    }

    #[test]
    fn presentation_completion_is_correlated_sanitized_and_private_free() {
        let pending = Mutex::new(HashMap::from([("themes-1".into(), "themes_list".into())]));
        let colors = serde_json::json!({
            "accent":{"light":"#0969DA","dark":"#58A6FF"},
            "muted":{"light":"#57606A","dark":"#8B949E"},
            "foreground":{"light":"#24292F","dark":"#F0F6FC"},
            "warning":{"light":"#9A6700","dark":"#E3B341"},
            "error":{"light":"#CF222E","dark":"#FF7B72"},
            "success":{"light":"#1A7F37","dark":"#7EE787"},
            "separator":{"light":"#8C959F","dark":"#6E7681"}
        });
        let response = RpcResponse {
            id: Some("themes-1".into()),
            command: Some("themes_list".into()),
            success: true,
            data: Some(serde_json::json!({
                "selected":"default",
                "themes":[
                    {"name":"default","display_name":"Snow","scope":"builtin","colors":colors.clone()},
                    {"name":"frost","display_name":"Frost","scope":"builtin","colors":colors.clone()},
                    {"name":"ember","display_name":"Ember","scope":"builtin","colors":colors.clone()},
                    {"name":"aurora","display_name":"Aurora","scope":"builtin","colors":colors}
                ]
            })),
            error: None,
            error_code: None,
        };
        let Err(event) = correlate_command_response(&pending, response) else {
            panic!("expected correlated completion");
        };
        assert!(
            matches!(*event, RuntimeEvent::CommandCompleted { command, .. } if command == "themes_list")
        );

        let pending = Mutex::new(HashMap::from([(
            "themes-private".into(),
            "themes_list".into(),
        )]));
        let response = RpcResponse {
            id: Some("themes-private".into()),
            command: Some("themes_list".into()),
            success: true,
            data: Some(serde_json::json!({"selected":"default","themes":[],"path":"/secret"})),
            error: None,
            error_code: None,
        };
        let Err(event) = correlate_command_response(&pending, response) else {
            panic!("expected private response rejection");
        };
        assert!(matches!(*event, RuntimeEvent::RequestRejected { .. }));
    }

    #[test]
    fn raw_command_failure_uses_existing_rejection_event() {
        let pending = Mutex::new(HashMap::from([("command-1".into(), "debug_get".into())]));
        let response = RpcResponse {
            id: Some("command-1".into()),
            command: Some("debug_get".into()),
            success: false,
            data: None,
            error: Some("debugging is unavailable".into()),
            error_code: Some("unavailable".into()),
        };

        let Err(event) = correlate_command_response(&pending, response) else {
            panic!("expected a command rejection");
        };
        let RuntimeEvent::RequestRejected { request_id, error } = *event else {
            panic!("expected a command rejection");
        };
        assert_eq!(request_id.as_deref(), Some("command-1"));
        assert_eq!(error, "debugging is unavailable");
        assert!(pending.lock().unwrap().is_empty());
    }

    #[test]
    fn raw_command_response_mismatch_is_rejected_and_consumed() {
        let pending = Mutex::new(HashMap::from([(
            "command-1".into(),
            "thread_goal_set".into(),
        )]));
        let response = RpcResponse {
            id: Some("command-1".into()),
            command: Some("session_info".into()),
            success: true,
            data: None,
            error: None,
            error_code: None,
        };

        let Err(event) = correlate_command_response(&pending, response) else {
            panic!("expected a correlation mismatch rejection");
        };
        let RuntimeEvent::RequestRejected { request_id, error } = *event else {
            panic!("expected a correlation mismatch rejection");
        };
        assert_eq!(request_id.as_deref(), Some("command-1"));
        assert!(error.contains("expected thread_goal_set"));
        assert!(error.contains("received session_info"));
        assert!(pending.lock().unwrap().is_empty());
    }
}
