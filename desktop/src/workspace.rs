use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

use gpui::{
    AnyElement, ClipboardItem, Context, Entity, IntoElement, Render, ScrollHandle, Subscription,
    Task, Window, div, prelude::*, px,
};
use gpui_component::{
    ActiveTheme, Disableable, StyledExt,
    button::{Button, ButtonVariants},
    h_flex,
    input::{Input, InputEvent, InputState},
    scroll::ScrollableElement,
    text::TextView,
    v_flex,
};

use crate::snow::{
    HistoryMessage, ModelInfo, PromptCompleted, PromptStatus, RuntimeConfig, RuntimeEvent,
    SessionBranch, ShutdownTracker, SnowClient, completion_summary,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConnectionState {
    Starting,
    Ready { snow_version: String },
    Stopping,
    Stopped,
    Failed(String),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ChatRole {
    User,
    Assistant,
    System,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ChatMessage {
    pub role: ChatRole,
    pub text: String,
    pub streaming: bool,
    render_id: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActivePrompt {
    pub request_id: String,
    pub assistant_message_index: usize,
    pub admitted: bool,
    pub abort_pending: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PendingModelChange {
    request_id: String,
    model: String,
    thinking: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum PendingSessionAction {
    Select { request_id: String },
    Fork { request_id: String },
    Rename { request_id: String },
}

impl PendingSessionAction {
    fn request_id(&self) -> &str {
        match self {
            Self::Select { request_id }
            | Self::Fork { request_id }
            | Self::Rename { request_id } => request_id,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolState {
    Running,
    Completed,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolActivity {
    pub call_id: String,
    pub name: String,
    pub status: String,
    pub state: ToolState,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct ProviderChoice {
    id: &'static str,
    label: &'static str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerPicker {
    Provider,
    Model,
    Thinking,
}

#[derive(Debug, Default)]
struct ComposerPickerState {
    active: Option<ComposerPicker>,
}

impl ComposerPickerState {
    fn toggle(&mut self, picker: ComposerPicker) {
        self.active = if self.active == Some(picker) {
            None
        } else {
            Some(picker)
        };
    }

    fn close(&mut self) {
        self.active = None;
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerAction {
    Send,
    Stop,
}

const THINKING_LEVELS: [&str; 8] = [
    "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
];

fn normalize_thinking_levels(levels: &[String]) -> Vec<String> {
    let mut normalized = vec!["off".to_owned()];
    for known in THINKING_LEVELS.into_iter().skip(1) {
        if levels.iter().any(|level| level == known) {
            normalized.push(known.to_owned());
        }
    }
    normalized
}

fn model_thinking_levels(model: &ModelInfo) -> Vec<String> {
    if model.supports_thinking {
        normalize_thinking_levels(&model.thinking_levels)
    } else {
        vec!["off".into()]
    }
}

fn project_name(path: &str) -> Option<String> {
    std::path::Path::new(path)
        .file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .map(str::to_owned)
}

fn thinking_label(level: &str) -> &'static str {
    match level {
        "minimal" => "Minimal",
        "low" => "Low",
        "medium" => "Medium",
        "high" => "High",
        "xhigh" => "X-high",
        "max" => "Max",
        "ultra" => "Ultra",
        _ => "Off",
    }
}

const PROVIDER_CHOICES: [ProviderChoice; 5] = [
    ProviderChoice {
        id: "fake",
        label: "Fake",
    },
    ProviderChoice {
        id: "opencode-zen",
        label: "Zen Free",
    },
    ProviderChoice {
        id: "opencode-go",
        label: "OpenCode Go",
    },
    ProviderChoice {
        id: "chatgpt",
        label: "ChatGPT",
    },
    ProviderChoice {
        id: "openai-compatible",
        label: "Compatible",
    },
];

#[derive(Debug)]
pub struct ChatState {
    pub connection: ConnectionState,
    pub messages: Vec<ChatMessage>,
    pub active_prompt: Option<ActivePrompt>,
    pub tools: Vec<ToolActivity>,
    pub models: Vec<ModelInfo>,
    pub current_model: String,
    pub current_thinking: String,
    pub thinking_levels: Vec<String>,
    pub project_cwd: String,
    pub session_id: String,
    pub session_name: String,
    pub branches: Vec<SessionBranch>,
    next_render_id: u64,
    pub runtime_loaded: bool,
    runtime_generation: Option<String>,
    session_metadata_stale: bool,
    session_loaded: bool,
    history_loaded: bool,
    models_loaded: bool,
    branches_loaded: bool,
    model_change_pending: Option<PendingModelChange>,
    thinking_change_pending: Option<(String, String)>,
    session_action_pending: Option<PendingSessionAction>,
    pub status_text: String,
    pub last_error: Option<String>,
}

fn push_coalesced(batch: &mut Vec<RuntimeEvent>, event: RuntimeEvent) {
    match event {
        RuntimeEvent::TextDelta { text } => {
            if let Some(RuntimeEvent::TextDelta { text: pending }) = batch.last_mut() {
                pending.push_str(&text);
            } else {
                batch.push(RuntimeEvent::TextDelta { text });
            }
        }
        RuntimeEvent::ThinkingDelta { text } => {
            if let Some(RuntimeEvent::ThinkingDelta { text: pending }) = batch.last_mut() {
                pending.push_str(&text);
            } else {
                batch.push(RuntimeEvent::ThinkingDelta { text });
            }
        }
        event => batch.push(event),
    }
}

fn apply_runtime_batch(state: &mut ChatState, batch: Vec<RuntimeEvent>) {
    for event in batch {
        state.apply(event);
    }
}

fn runtime_event_generation(event: &RuntimeEvent) -> Option<&str> {
    match event {
        RuntimeEvent::ModelsLoaded { generation, .. }
        | RuntimeEvent::SessionLoaded { generation, .. }
        | RuntimeEvent::HistoryLoaded { generation, .. }
        | RuntimeEvent::BranchesLoaded { generation, .. }
        | RuntimeEvent::RuntimeStateFailed { generation, .. } => Some(generation),
        _ => None,
    }
}

fn apply_runtime_config_event(config: &mut RuntimeConfig, event: &RuntimeEvent) {
    match event {
        RuntimeEvent::SessionLoaded { info, .. } => {
            config.session_path = (!info.path.is_empty()).then(|| info.path.clone().into());
            config.no_session = info.path.is_empty();
            config.model = Some(info.model.clone());
        }
        RuntimeEvent::ModelsLoaded { catalog, .. } if !catalog.current.is_empty() => {
            config.model = Some(catalog.current.clone());
        }
        _ => {}
    }
}

fn replacement_provider_config(config: &RuntimeConfig, provider: &str) -> RuntimeConfig {
    let mut replacement = config.clone();
    replacement.provider = provider.into();
    replacement.model = None;
    replacement
}

impl Default for ChatState {
    fn default() -> Self {
        Self {
            connection: ConnectionState::Starting,
            messages: Vec::new(),
            active_prompt: None,
            tools: Vec::new(),
            models: Vec::new(),
            current_model: String::new(),
            current_thinking: "off".into(),
            thinking_levels: vec!["off".into()],
            project_cwd: String::new(),
            session_id: String::new(),
            session_name: String::new(),
            branches: Vec::new(),
            next_render_id: 1,
            runtime_loaded: false,
            runtime_generation: None,
            session_metadata_stale: false,
            session_loaded: false,
            history_loaded: false,
            models_loaded: false,
            branches_loaded: false,
            model_change_pending: None,
            thinking_change_pending: None,
            session_action_pending: None,
            status_text: "Starting Snow…".into(),
            last_error: None,
        }
    }
}

impl ChatState {
    fn begin_runtime_load(&mut self, generation: String, reset: bool) {
        self.runtime_generation = Some(generation);
        self.session_metadata_stale = false;
        if reset {
            self.reset_runtime_load();
        }
    }

    fn accepts_runtime_generation(&self, generation: &str) -> bool {
        self.runtime_generation.as_deref() == Some(generation)
    }

    fn reset_runtime_load(&mut self) {
        self.runtime_loaded = false;
        self.session_loaded = false;
        self.history_loaded = false;
        self.models_loaded = false;
        self.branches_loaded = false;
    }

    fn refresh_runtime_load(&mut self) {
        self.runtime_loaded = self.session_loaded
            && self.history_loaded
            && self.models_loaded
            && self.branches_loaded;
        if self.runtime_loaded {
            self.status_text = "Ready".into();
            self.last_error = None;
        }
    }

    pub fn can_send(&self) -> bool {
        matches!(self.connection, ConnectionState::Ready { .. })
            && self.runtime_loaded
            && self.model_change_pending.is_none()
            && self.thinking_change_pending.is_none()
            && self.session_action_pending.is_none()
            && self.active_prompt.is_none()
    }

    pub fn can_abort(&self) -> bool {
        matches!(self.connection, ConnectionState::Ready { .. })
            && self
                .active_prompt
                .as_ref()
                .is_some_and(|active| !active.abort_pending)
    }

    fn composer_action(&self) -> ComposerAction {
        if self.active_prompt.is_some() {
            ComposerAction::Stop
        } else {
            ComposerAction::Send
        }
    }

    pub fn can_switch_provider(&self) -> bool {
        self.active_prompt.is_none()
            && self.model_change_pending.is_none()
            && self.thinking_change_pending.is_none()
            && self.session_action_pending.is_none()
            && (matches!(
                self.connection,
                ConnectionState::Failed(_) | ConnectionState::Stopped
            ) || (self.runtime_loaded
                && matches!(self.connection, ConnectionState::Ready { .. })))
    }

    pub fn can_switch_model(&self) -> bool {
        self.can_send() && self.models.len() > 1
    }

    fn can_select_model(&self, model: &str) -> bool {
        self.can_switch_model()
            && model != self.current_model
            && self.models.iter().any(|candidate| candidate.id == model)
    }

    fn can_switch_thinking(&self) -> bool {
        self.can_send() && self.thinking_levels.len() > 1
    }

    fn can_select_thinking(&self, level: &str) -> bool {
        self.can_switch_thinking()
            && level != self.current_thinking
            && self
                .thinking_levels
                .iter()
                .any(|candidate| candidate == level)
    }

    fn begin_thinking_change(&mut self, request_id: String, level: String) {
        self.thinking_change_pending = Some((request_id, level.clone()));
        self.status_text = format!("Selecting {} thinking…", thinking_label(&level));
        self.last_error = None;
    }

    fn can_manage_session(&self) -> bool {
        self.can_send() && !self.session_id.is_empty()
    }

    fn begin_session_action(&mut self, pending: PendingSessionAction, status: String) {
        self.session_action_pending = Some(pending);
        self.status_text = status;
        self.last_error = None;
    }

    pub fn begin_provider_switch(&mut self, provider: &str) {
        self.connection = ConnectionState::Stopping;
        self.active_prompt = None;
        self.models.clear();
        self.current_model.clear();
        self.current_thinking = "off".into();
        self.thinking_levels = vec!["off".into()];
        self.thinking_change_pending = None;
        self.session_action_pending = None;
        self.branches.clear();
        self.reset_runtime_load();
        self.model_change_pending = None;
        self.status_text = format!("Switching to {provider}…");
        self.last_error = None;
    }

    pub fn begin_connect(&mut self, provider: &str) {
        self.connection = ConnectionState::Starting;
        self.thinking_change_pending = None;
        self.session_action_pending = None;
        self.reset_runtime_load();
        self.status_text = format!("Connecting to {provider}…");
        self.last_error = None;
    }

    fn allocate_render_id(&mut self) -> u64 {
        let id = self.next_render_id;
        self.next_render_id = self.next_render_id.saturating_add(1);
        id
    }

    pub fn begin_prompt(&mut self, request_id: String, message: String) {
        let user_render_id = self.allocate_render_id();
        self.messages.push(ChatMessage {
            role: ChatRole::User,
            text: message,
            streaming: false,
            render_id: user_render_id,
        });
        let assistant_message_index = self.messages.len();
        let assistant_render_id = self.allocate_render_id();
        self.messages.push(ChatMessage {
            role: ChatRole::Assistant,
            text: String::new(),
            streaming: true,
            render_id: assistant_render_id,
        });
        self.active_prompt = Some(ActivePrompt {
            request_id,
            assistant_message_index,
            admitted: false,
            abort_pending: false,
        });
        self.status_text = "Sending…".into();
        self.last_error = None;
    }

    pub fn apply(&mut self, event: RuntimeEvent) {
        match event {
            RuntimeEvent::Ready(ready) => {
                self.connection = ConnectionState::Ready {
                    snow_version: ready.snow_version,
                };
                self.reset_runtime_load();
                self.model_change_pending = None;
                self.thinking_change_pending = None;
                self.session_action_pending = None;
                self.runtime_generation = None;
                self.session_metadata_stale = false;
                self.status_text = "Restoring session…".into();
                self.last_error = None;
            }
            RuntimeEvent::ModelsLoaded {
                generation,
                catalog,
            } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                self.models = catalog.models;
                if !catalog.current.is_empty() {
                    self.current_model = catalog.current;
                }
                self.models_loaded = true;
                self.refresh_runtime_load();
            }
            RuntimeEvent::SessionLoaded { generation, info } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                self.current_model = info.model;
                self.current_thinking = if THINKING_LEVELS.contains(&info.thinking.as_str()) {
                    info.thinking
                } else {
                    "off".into()
                };
                self.thinking_levels = normalize_thinking_levels(&info.thinking_levels);
                if !self
                    .thinking_levels
                    .iter()
                    .any(|level| level == &self.current_thinking)
                {
                    self.current_thinking = "off".into();
                }
                self.project_cwd = info.cwd;
                self.session_id = info.session_id;
                self.session_name = info.name;
                self.session_loaded = true;
                self.refresh_runtime_load();
            }
            RuntimeEvent::HistoryLoaded {
                generation,
                history,
            } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                self.restore_history(history);
                self.history_loaded = true;
                self.refresh_runtime_load();
            }
            RuntimeEvent::BranchesLoaded {
                generation,
                catalog,
            } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                self.branches = catalog.branches;
                self.branches_loaded = true;
                self.refresh_runtime_load();
            }
            RuntimeEvent::SessionStateInvalidated => {
                self.session_metadata_stale = true;
            }
            RuntimeEvent::RuntimeStateFailed {
                generation,
                command,
                error,
            } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                self.reset_runtime_load();
                self.connection = ConnectionState::Failed(error.clone());
                self.status_text = format!("Could not load {command}");
                self.last_error = Some(error);
            }
            RuntimeEvent::ModelChanged(model) => {
                if self.model_change_pending.is_none() {
                    let thinking = self.current_thinking.clone();
                    self.finish_model_change(model, thinking);
                }
            }
            RuntimeEvent::ModelChangeConfirmed { request_id } => {
                if self
                    .model_change_pending
                    .as_ref()
                    .is_some_and(|pending| pending.request_id == request_id)
                    && let Some(pending) = self.model_change_pending.take()
                {
                    self.finish_model_change(pending.model, pending.thinking);
                }
            }
            RuntimeEvent::ThinkingChanged { request_id } => {
                if self
                    .thinking_change_pending
                    .as_ref()
                    .is_some_and(|(pending_id, _)| pending_id == &request_id)
                {
                    let (_, level) = self.thinking_change_pending.take().unwrap();
                    self.current_thinking = level;
                    self.status_text = "Ready".into();
                    self.last_error = None;
                }
            }
            RuntimeEvent::BranchSelected { request_id } => {
                if self
                    .session_action_pending
                    .as_ref()
                    .is_some_and(|pending| pending.request_id() == request_id)
                {
                    self.session_action_pending = None;
                    self.status_text = "Restoring branch…".into();
                    self.last_error = None;
                }
            }
            RuntimeEvent::BranchForked { request_id, branch } => {
                if self
                    .session_action_pending
                    .as_ref()
                    .is_some_and(|pending| pending.request_id() == request_id)
                {
                    self.session_action_pending = None;
                    self.status_text = if branch.name.is_empty() {
                        "Opened new branch".into()
                    } else {
                        format!("Opened {}", branch.name)
                    };
                    self.last_error = None;
                }
            }
            RuntimeEvent::SessionRenamed {
                request_id,
                session_id,
                name,
            } => {
                if self
                    .session_action_pending
                    .as_ref()
                    .is_some_and(|pending| pending.request_id() == request_id)
                {
                    self.session_action_pending = None;
                    if self.session_id == session_id {
                        self.session_name = name;
                        self.status_text = "Session renamed".into();
                        self.last_error = None;
                    } else {
                        self.status_text = "Could not confirm session rename".into();
                        self.last_error =
                            Some("session rename response did not match the active session".into());
                    }
                }
            }
            RuntimeEvent::PromptAdmitted { request_id } => {
                if let Some(active) = self.active_prompt.as_mut()
                    && active.request_id == request_id
                {
                    active.admitted = true;
                    self.status_text = "Thinking…".into();
                }
            }
            RuntimeEvent::RequestRejected { request_id, error } => {
                if request_id.as_deref().is_some_and(|request_id| {
                    self.model_change_pending
                        .as_ref()
                        .is_some_and(|pending| pending.request_id == request_id)
                }) {
                    self.model_change_pending = None;
                }
                if request_id.as_deref().is_some_and(|request_id| {
                    self.thinking_change_pending
                        .as_ref()
                        .is_some_and(|(pending_id, _)| pending_id == request_id)
                }) {
                    self.thinking_change_pending = None;
                }
                if request_id.as_deref().is_some_and(|request_id| {
                    self.session_action_pending
                        .as_ref()
                        .is_some_and(|pending| pending.request_id() == request_id)
                }) {
                    self.session_action_pending = None;
                }
                let rejects_prompt = request_id.as_deref().is_some_and(|request_id| {
                    self.active_prompt
                        .as_ref()
                        .is_some_and(|active| active.request_id == request_id)
                });
                if rejects_prompt {
                    self.finish_active_message(true);
                    self.active_prompt = None;
                } else if let Some(active) = self.active_prompt.as_mut() {
                    active.abort_pending = false;
                }
                self.status_text = "Request rejected".into();
                self.last_error = Some(error);
            }
            RuntimeEvent::TextDelta { text } => {
                if let Some(active) = &self.active_prompt
                    && let Some(message) = self.messages.get_mut(active.assistant_message_index)
                {
                    message.text.push_str(&text);
                    self.status_text = "Responding…".into();
                }
            }
            RuntimeEvent::ThinkingDelta { text } => {
                if !text.is_empty() {
                    self.status_text = "Thinking…".into();
                }
            }
            RuntimeEvent::ToolStarted { call_id, name } => {
                self.tools.push(ToolActivity {
                    call_id,
                    name: name.clone(),
                    status: "Running".into(),
                    state: ToolState::Running,
                });
                self.status_text = format!("Running {name}…");
            }
            RuntimeEvent::ToolProgress { call_id, message } => {
                if let Some(tool) = self
                    .tools
                    .iter_mut()
                    .rev()
                    .find(|tool| tool.call_id == call_id)
                {
                    tool.status = message.unwrap_or_else(|| "Running".into());
                }
            }
            RuntimeEvent::ToolFinished {
                call_id,
                name,
                is_error,
                preview,
            } => {
                if let Some(tool) = self
                    .tools
                    .iter_mut()
                    .rev()
                    .find(|tool| tool.call_id == call_id)
                {
                    tool.status = if is_error {
                        "Failed".into()
                    } else {
                        "Completed".into()
                    };
                    tool.state = if is_error {
                        ToolState::Failed
                    } else {
                        ToolState::Completed
                    };
                    if let Some(preview) = preview.filter(|preview| !preview.is_empty()) {
                        tool.status = format!("{} · {preview}", tool.status);
                    }
                } else {
                    self.tools.push(ToolActivity {
                        call_id,
                        name,
                        status: if is_error {
                            "Failed".into()
                        } else {
                            "Completed".into()
                        },
                        state: if is_error {
                            ToolState::Failed
                        } else {
                            ToolState::Completed
                        },
                    });
                }
                self.status_text = if is_error {
                    "Tool failed".into()
                } else {
                    "Thinking…".into()
                };
            }
            RuntimeEvent::TurnDone { turn_id } => {
                let _ = turn_id;
                self.status_text = "Finishing…".into();
            }
            RuntimeEvent::UnsupportedInteraction { kind, request_id } => {
                self.status_text = format!("Waiting for unsupported {kind}");
                self.last_error = Some(match request_id {
                    Some(request_id) => format!(
                        "Snow requested {kind} ({request_id}), which this basic client cannot answer. Stop the turn to continue."
                    ),
                    None => format!(
                        "Snow requested {kind}, which this basic client cannot answer. Stop the turn to continue."
                    ),
                });
            }
            RuntimeEvent::PromptCompleted(completed) => self.finish_prompt(completed),
            RuntimeEvent::Status(status) => self.status_text = status,
            RuntimeEvent::Diagnostic(message) => {
                if self.last_error.is_none() {
                    self.last_error = Some(message);
                }
            }
            RuntimeEvent::Failed(error) => {
                self.status_text = "Snow error".into();
                self.last_error = Some(error);
            }
            RuntimeEvent::Exited { expected, status } => {
                self.finish_active_message(true);
                self.active_prompt = None;
                self.model_change_pending = None;
                self.thinking_change_pending = None;
                self.session_action_pending = None;
                self.connection = ConnectionState::Stopped;
                self.status_text = if expected {
                    "Stopped".into()
                } else {
                    "Snow exited".into()
                };
                if !expected {
                    self.last_error = Some(match status {
                        Some(code) => format!("Snow exited unexpectedly with status {code}"),
                        None => "Snow exited unexpectedly".into(),
                    });
                }
            }
        }
    }

    fn begin_model_change(&mut self, request_id: String, model: &str, thinking: String) {
        self.model_change_pending = Some(PendingModelChange {
            request_id,
            model: model.into(),
            thinking,
        });
        self.status_text = format!("Selecting {model}…");
        self.last_error = None;
    }

    fn finish_model_change(&mut self, model: String, thinking: String) {
        self.current_model = model;
        self.thinking_levels = self
            .models
            .iter()
            .find(|candidate| candidate.id == self.current_model)
            .map(model_thinking_levels)
            .unwrap_or_else(|| vec!["off".into()]);
        self.current_thinking = if self
            .thinking_levels
            .iter()
            .any(|candidate| candidate == &thinking)
        {
            thinking
        } else {
            "off".into()
        };
        self.model_change_pending = None;
        self.status_text = "Ready".into();
        self.last_error = None;
    }

    fn restore_history(&mut self, history: Vec<HistoryMessage>) {
        let mut messages = Vec::with_capacity(history.len());
        for message in history {
            let render_id = self.allocate_render_id();
            messages.push(ChatMessage {
                role: match message.role.as_str() {
                    "user" => ChatRole::User,
                    "assistant" => ChatRole::Assistant,
                    _ => ChatRole::System,
                },
                text: message.text,
                streaming: false,
                render_id,
            });
        }
        self.messages = messages;
        self.tools.clear();
    }

    fn finish_prompt(&mut self, completed: PromptCompleted) {
        let matches_active = self
            .active_prompt
            .as_ref()
            .is_some_and(|active| active.request_id == completed.request_id);
        if !matches_active {
            return;
        }

        self.finish_active_message(true);
        self.active_prompt = None;
        self.status_text = completion_summary(&completed).into();
        match completed.status {
            PromptStatus::Completed => self.last_error = None,
            PromptStatus::Failed => {
                self.last_error = Some(
                    completed
                        .error
                        .unwrap_or_else(|| "The prompt failed".into()),
                );
            }
            PromptStatus::Canceled => {}
        }
    }

    fn finish_active_message(&mut self, remove_empty: bool) {
        let Some(index) = self
            .active_prompt
            .as_ref()
            .map(|active| active.assistant_message_index)
        else {
            return;
        };
        let should_remove = self
            .messages
            .get(index)
            .is_some_and(|message| remove_empty && message.text.is_empty());
        if should_remove {
            self.messages.remove(index);
        } else if let Some(message) = self.messages.get_mut(index) {
            message.streaming = false;
        }
    }
}

pub struct Workspace {
    state: ChatState,
    input: Entity<InputState>,
    session_name_input: Entity<InputState>,
    session_menu_open: bool,
    runtime: Option<SnowClient>,
    runtime_config: Option<RuntimeConfig>,
    provider: String,
    composer_picker: ComposerPickerState,
    scroll_handle: ScrollHandle,
    runtime_task: Option<Task<()>>,
    provider_shutdown: Option<ShutdownTracker>,
    _provider_switch_task: Option<Task<()>>,
    _subscriptions: Vec<Subscription>,
}

impl Workspace {
    pub fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let config = RuntimeConfig::from_env();
        let provider = config
            .as_ref()
            .map(|config| config.provider.clone())
            .unwrap_or_else(|_| "unknown".into());
        let input = cx.new(|cx| {
            InputState::new(window, cx)
                .auto_grow(1, 6)
                .placeholder("Ask Snow…")
        });
        let subscription =
            cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| {
                if matches!(event, InputEvent::PressEnter { secondary: false }) {
                    this.submit(window, cx);
                }
            });
        let session_name_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("Session name"));
        let session_subscription = cx.subscribe_in(
            &session_name_input,
            window,
            |this, _, event: &InputEvent, _window, cx| {
                if matches!(event, InputEvent::PressEnter { secondary: false }) {
                    this.rename_session(cx);
                }
            },
        );

        let mut workspace = Self {
            state: ChatState::default(),
            input,
            session_name_input,
            session_menu_open: false,
            runtime: None,
            runtime_config: config.as_ref().ok().cloned(),
            provider,
            composer_picker: ComposerPickerState::default(),
            scroll_handle: ScrollHandle::new(),
            runtime_task: None,
            provider_shutdown: None,
            _provider_switch_task: None,
            _subscriptions: vec![subscription, session_subscription],
        };
        match config {
            Ok(config) => workspace.connect(config, cx),
            Err(error) => workspace.show_start_error(error.to_string()),
        }
        workspace
    }

    fn connect(&mut self, config: RuntimeConfig, cx: &mut Context<Self>) {
        self.provider_shutdown = None;
        self.provider = config.provider.clone();
        self.runtime_config = Some(config.clone());
        self.state.begin_connect(&config.provider);
        match SnowClient::start(config) {
            Ok(connection) => {
                let events = connection.events;
                self.runtime = Some(connection.client);
                self.runtime_task = Some(cx.spawn(async move |this, cx| {
                    while let Ok(event) = events.recv_async().await {
                        let mut batch = Vec::with_capacity(16);
                        push_coalesced(&mut batch, event);
                        while batch.len() < 64 {
                            let Ok(event) = events.try_recv() else {
                                break;
                            };
                            push_coalesced(&mut batch, event);
                        }
                        if this
                            .update(cx, |this, cx| {
                                let became_ready = batch
                                    .iter()
                                    .any(|event| matches!(event, RuntimeEvent::Ready(_)));
                                let may_update_model = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::ModelChanged(_)
                                            | RuntimeEvent::ModelChangeConfirmed { .. }
                                    )
                                });
                                let reload_branch = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::BranchSelected { .. }
                                            | RuntimeEvent::BranchForked { .. }
                                    )
                                });
                                if reload_branch && let Some(runtime) = &this.runtime {
                                    match runtime.load_runtime_state() {
                                        Ok(generation) => {
                                            this.state.begin_runtime_load(generation, true)
                                        }
                                        Err(error) => {
                                            this.state.connection =
                                                ConnectionState::Failed(error.to_string());
                                            this.state.status_text =
                                                "Could not load Snow runtime state".into();
                                            this.state.last_error = Some(error.to_string());
                                        }
                                    }
                                }
                                if let Some(config) = this.runtime_config.as_mut() {
                                    for event in &batch {
                                        if runtime_event_generation(event).is_none_or(
                                            |generation| {
                                                this.state.accepts_runtime_generation(generation)
                                            },
                                        ) {
                                            apply_runtime_config_event(config, event);
                                        }
                                    }
                                }
                                apply_runtime_batch(&mut this.state, batch);
                                if may_update_model
                                    && let Some(config) = this.runtime_config.as_mut()
                                    && !this.state.current_model.is_empty()
                                {
                                    config.model = Some(this.state.current_model.clone());
                                }
                                if became_ready && let Some(runtime) = &this.runtime {
                                    match runtime.load_runtime_state() {
                                        Ok(generation) => {
                                            this.state.begin_runtime_load(generation, true)
                                        }
                                        Err(error) => {
                                            this.state.connection =
                                                ConnectionState::Failed(error.to_string());
                                            this.state.status_text =
                                                "Could not load Snow runtime state".into();
                                            this.state.last_error = Some(error.to_string());
                                        }
                                    }
                                }
                                if this.state.session_metadata_stale
                                    && this.state.active_prompt.is_none()
                                    && this.state.runtime_loaded
                                    && let Some(runtime) = &this.runtime
                                {
                                    match runtime.load_session_metadata() {
                                        Ok(generation) => {
                                            this.state.begin_runtime_load(generation, false)
                                        }
                                        Err(error) => {
                                            this.state.status_text =
                                                "Could not refresh session metadata".into();
                                            this.state.last_error = Some(error.to_string());
                                        }
                                    }
                                }
                                this.scroll_handle.scroll_to_bottom();
                                cx.notify();
                            })
                            .is_err()
                        {
                            break;
                        }
                    }
                }));
            }
            Err(error) => self.show_start_error(error.to_string()),
        }
        cx.notify();
    }

    fn show_start_error(&mut self, error: String) {
        self.runtime = None;
        self.runtime_task = None;
        self.state.connection = ConnectionState::Failed(error.clone());
        self.state.status_text = "Could not start Snow".into();
        self.state.last_error = Some(error);
    }

    fn toggle_session_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.session_menu_open = !self.session_menu_open;
        if self.session_menu_open {
            self.composer_picker.close();
            let name = self.state.session_name.clone();
            self.session_name_input
                .update(cx, |input, cx| input.set_value(&name, window, cx));
        }
        cx.notify();
    }

    fn rename_session(&mut self, cx: &mut Context<Self>) {
        if !self.state.can_manage_session() {
            return;
        }
        let name = self.session_name_input.read(cx).value().trim().to_owned();
        if name.is_empty() || name == self.state.session_name {
            return;
        }
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.rename_session(name) {
            Ok(request_id) => self.state.begin_session_action(
                PendingSessionAction::Rename { request_id },
                "Renaming session…".into(),
            ),
            Err(error) => {
                self.state.status_text = "Could not rename session".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn select_branch(&mut self, branch_id: &str, cx: &mut Context<Self>) {
        if !self.state.can_manage_session()
            || self
                .state
                .branches
                .iter()
                .any(|branch| branch.id == branch_id && branch.active)
        {
            return;
        }
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.select_branch(branch_id.to_owned()) {
            Ok(request_id) => {
                self.state.begin_session_action(
                    PendingSessionAction::Select { request_id },
                    "Switching branch…".into(),
                );
                self.session_menu_open = false;
            }
            Err(error) => {
                self.state.status_text = "Could not switch branch".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn fork_branch(&mut self, cx: &mut Context<Self>) {
        if !self.state.can_manage_session() {
            return;
        }
        let source_branch_id = self
            .state
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| branch.id.clone())
            .unwrap_or_default();
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.fork_branch(source_branch_id) {
            Ok(request_id) => {
                self.state.begin_session_action(
                    PendingSessionAction::Fork { request_id },
                    "Forking branch…".into(),
                );
                self.session_menu_open = false;
            }
            Err(error) => {
                self.state.status_text = "Could not fork branch".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn select_provider(&mut self, provider: &str, cx: &mut Context<Self>) {
        self.composer_picker.close();
        let retries_failed_provider = provider == self.provider
            && matches!(self.state.connection, ConnectionState::Failed(_));
        if (provider == self.provider && !retries_failed_provider)
            || !self.state.can_switch_provider()
        {
            return;
        }
        let Some(current_config) = self.runtime_config.as_ref() else {
            self.state.last_error = Some("Snow runtime configuration is unavailable".into());
            cx.notify();
            return;
        };
        let config = replacement_provider_config(current_config, provider);
        self.provider = provider.to_owned();
        self.state.begin_provider_switch(provider);
        self.runtime_task = None;

        let (shutdown_complete, shutdown_finished) = flume::bounded(1);
        if let Some(runtime) = self.runtime.take() {
            self.provider_shutdown =
                Some(runtime.shutdown_in_background_tracked(shutdown_complete));
        } else {
            self.provider_shutdown = None;
            let _ = shutdown_complete.try_send(());
        }

        self._provider_switch_task = Some(cx.spawn(async move |this, cx| {
            let _ = shutdown_finished.recv_async().await;
            let _ = this.update(cx, |this, cx| this.connect(config, cx));
        }));
        cx.notify();
    }

    fn toggle_picker(&mut self, picker: ComposerPicker, cx: &mut Context<Self>) {
        self.session_menu_open = false;
        self.composer_picker.toggle(picker);
        cx.notify();
    }

    fn select_model(&mut self, model: &str, cx: &mut Context<Self>) {
        self.composer_picker.close();
        if !self.state.can_select_model(model) {
            cx.notify();
            return;
        }
        let Some(runtime) = &self.runtime else {
            cx.notify();
            return;
        };
        let Some(target) = self
            .state
            .models
            .iter()
            .find(|candidate| candidate.id == model)
        else {
            cx.notify();
            return;
        };
        let levels = model_thinking_levels(target);
        let thinking = if levels.contains(&self.state.current_thinking) {
            self.state.current_thinking.clone()
        } else if levels.contains(&target.default_thinking) {
            target.default_thinking.clone()
        } else {
            "off".into()
        };
        match runtime.set_model_thinking(model.to_owned(), thinking.clone()) {
            Ok(request_id) => self.state.begin_model_change(request_id, model, thinking),
            Err(error) => {
                self.state.status_text = "Could not select model".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn select_thinking(&mut self, level: &str, cx: &mut Context<Self>) {
        self.composer_picker.close();
        if !self.state.can_select_thinking(level) {
            cx.notify();
            return;
        }
        let Some(runtime) = &self.runtime else {
            cx.notify();
            return;
        };
        match runtime.set_thinking(level.to_owned()) {
            Ok(request_id) => self
                .state
                .begin_thinking_change(request_id, level.to_owned()),
            Err(error) => {
                self.state.status_text = "Could not select thinking".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn submit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.state.can_send() {
            return;
        }
        self.composer_picker.close();
        let message = self.input.read(cx).value().to_string();
        let trimmed = message.trim();
        if trimmed.is_empty() {
            return;
        }
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.prompt(trimmed.to_owned()) {
            Ok(request_id) => {
                self.state.begin_prompt(request_id, trimmed.to_owned());
                self.input
                    .update(cx, |input, cx| input.set_value("", window, cx));
                self.scroll_handle.scroll_to_bottom();
            }
            Err(error) => {
                self.state.last_error = Some(error.to_string());
                self.state.status_text = "Could not send prompt".into();
            }
        }
        cx.notify();
    }

    fn abort(&mut self, cx: &mut Context<Self>) {
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.abort() {
            Ok(_) => {
                if let Some(active) = self.state.active_prompt.as_mut() {
                    active.abort_pending = true;
                }
                self.state.status_text = "Stopping…".into();
            }
            Err(error) => self.state.last_error = Some(error.to_string()),
        }
        cx.notify();
    }
}

mod view;

impl Drop for Workspace {
    fn drop(&mut self) {
        self.runtime_task = None;
        self._provider_switch_task = None;
        if let Some(runtime) = self.runtime.take() {
            let _ = runtime.shutdown();
        } else if let Some(shutdown) = self.provider_shutdown.take() {
            shutdown.wait_and_force();
        }
    }
}

impl Render for Workspace {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        div()
            .size_full()
            .bg(theme.background)
            .text_color(theme.foreground)
            .child(self.render_workspace(window, cx))
    }
}

#[cfg(test)]
mod tests {
    include!("workspace_tests.rs");
}
