use std::{
    collections::{BTreeMap, HashSet},
    sync::Arc,
};

use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64_STANDARD};
use gpui::Image;
use serde::{Deserialize, Serialize, ser::SerializeMap};
use serde_json::{Map, Value};

use crate::image_safety::validate_and_cache_image;

use super::SnowError;

pub const RPC_PROTOCOL_VERSION: &str = "1";
pub const DEFAULT_MAX_FRAME_BYTES: usize = 16 * 1024 * 1024;
/// Maximum decoded payload retained for one historical image.
pub const MAX_HISTORY_IMAGE_BYTES: usize = 8 * 1024 * 1024;
/// Maximum number of decoded images accepted in one history response page.
pub const MAX_HISTORY_IMAGES: usize = 32;
/// Maximum wire messages accepted in one bounded history page.
pub const MAX_HISTORY_PAGE_MESSAGES: usize = 128;
/// Maximum opaque cursor bytes accepted from the child process.
pub const MAX_HISTORY_PAGE_CURSOR_BYTES: usize = 4096;
/// Maximum surface-safe preview retained for one historical tool display field.
pub const MAX_HISTORY_TOOL_TEXT_CHARS: usize = 64 * 1024;
/// Maximum progress rows retained for one historical tool result card.
pub const MAX_HISTORY_TOOL_PROGRESS_ITEMS: usize = 64;
/// Maximum number of messages accepted from one child-history page.
pub const MAX_SUBAGENT_MESSAGES_PER_PAGE: usize = 128;
/// Maximum decoded images accepted from one child-history page.
pub const MAX_SUBAGENT_MESSAGE_IMAGES: usize = 16;
/// Maximum server-issued child-history cursor size.
pub const MAX_SUBAGENT_MESSAGE_CURSOR_BYTES: usize = 4096;
/// Maximum requested child-history page size.
pub const MAX_SUBAGENT_MESSAGE_PAGE_BYTES: usize = 8 * 1024 * 1024;
/// Minimum requested child-history page size.
pub const MIN_SUBAGENT_MESSAGE_PAGE_BYTES: usize = 16 * 1024;
/// Maximum stable child path or thread identity size.
pub const MAX_SUBAGENT_IDENTITY_BYTES: usize = 1024;
const MAX_SUBAGENT_MESSAGE_ID_BYTES: usize = 4096;
const MAX_SUBAGENT_BLOCKS_PER_MESSAGE: usize = 512;
const MAX_SUBAGENT_BLOCKS_PER_PAGE: usize = 4096;
/// Maximum serialized `data` value accepted for a presentation response.
pub const MAX_PRESENTATION_DATA_BYTES: usize = 512 * 1024;
pub const MAX_THEME_CATALOG_ITEMS: usize = 132;
pub const MIN_THEME_CATALOG_ITEMS: usize = 4;
pub const MAX_THEME_TEXT_CHARS: usize = 64;
pub const KEYBINDING_ACTION_COUNT: usize = 31;
pub const MAX_BINDINGS_PER_ACTION: usize = 32;
pub const MAX_KEYBINDING_TEXT_CHARS: usize = 64;

pub const KEYBINDING_ACTIONS: [&str; KEYBINDING_ACTION_COUNT] = [
    "submit",
    "follow_up",
    "newline",
    "paste",
    "abort",
    "quit",
    "toggle_mode",
    "thinking",
    "models",
    "agents",
    "processes",
    "page_up",
    "page_down",
    "top",
    "bottom",
    "line_up",
    "line_down",
    "picker_up",
    "picker_down",
    "picker_previous",
    "picker_next",
    "picker_page_up",
    "picker_page_down",
    "picker_top",
    "picker_bottom",
    "accept",
    "close",
    "branch_fork",
    "branch_rename",
    "branch_delete",
    "confirm",
];

pub const REQUIRED_CAPABILITIES: [&str; 32] = [
    "active_input",
    "authentication",
    "branch_management",
    "compaction",
    "context_report",
    "debug_diagnostics",
    "diagnostics",
    "goals",
    "managed_processes",
    "mcp_servers",
    "messages_list",
    "messages_page",
    "models_list",
    "multimodal_prompts",
    "pending_inputs",
    "permission_interaction",
    "permission_mode",
    "presentation_settings",
    "project_init",
    "project_trust",
    "prompt_completion",
    "response_controls",
    "session_forks",
    "session_info",
    "session_management",
    "settings",
    "skills",
    "subagent_messages",
    "subagent_models",
    "subagents",
    "usage",
    "user_input",
];

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum RpcRequest {
    Prompt {
        id: String,
        message: String,
    },
    #[serde(rename = "prompt")]
    PromptWithMode {
        id: String,
        message: String,
        mode: String,
    },
    #[serde(rename = "project_init")]
    ProjectInit {
        id: String,
    },
    #[serde(rename = "prompt")]
    PromptContent {
        id: String,
        #[serde(skip_serializing_if = "String::is_empty")]
        message: String,
        content: Vec<Value>,
        #[serde(skip_serializing_if = "Option::is_none")]
        mode: Option<String>,
    },
    Abort {
        id: String,
    },
    ModelsList {
        id: String,
    },
    SessionInfo {
        id: String,
    },
    MessagesList {
        id: String,
    },
    MessagesPage {
        id: String,
        params: MessagesPageParams,
    },
    SubagentMessages {
        id: String,
        params: SubagentMessagesParams,
    },
    ThemesList {
        id: String,
    },
    KeybindingsGet {
        id: String,
    },
    KeybindingsUpdate {
        id: String,
        params: KeybindingsUpdateParams,
    },
    SettingsGet {
        id: String,
    },
    #[serde(rename = "settings_update")]
    SettingsThemeUpdate {
        id: String,
        params: ThemeSettingsUpdateParams,
    },
    BranchesList {
        id: String,
    },
    BranchSelect {
        id: String,
        params: BranchSelectParams,
    },
    BranchFork {
        id: String,
        params: BranchForkParams,
    },
    SessionRename {
        id: String,
        params: SessionRenameParams,
    },
    SetModel {
        id: String,
        model: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        thinking: Option<String>,
    },
    SetThinking {
        id: String,
        thinking: String,
    },
    PermissionReply {
        id: String,
        params: PermissionReplyParams,
    },
    PermissionReject {
        id: String,
        params: PermissionRejectParams,
    },
    UserInputReply {
        id: String,
        params: UserInputReplyParams,
    },
    UserInputReject {
        id: String,
        params: UserInputRejectParams,
    },
    #[serde(skip)]
    Raw {
        id: String,
        command: String,
        fields: Map<String, Value>,
    },
}

impl RpcRequest {
    pub fn raw(id: String, command: String, fields: Map<String, Value>) -> Result<Self, SnowError> {
        validate_raw_request(&id, &command, &fields)?;
        Ok(Self::Raw {
            id,
            command,
            fields,
        })
    }
}

struct RawRequestRef<'a> {
    id: &'a str,
    command: &'a str,
    fields: &'a Map<String, Value>,
}

impl Serialize for RawRequestRef<'_> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        let mut map = serializer.serialize_map(Some(self.fields.len() + 2))?;
        map.serialize_entry("type", self.command)?;
        map.serialize_entry("id", self.id)?;
        for (field, value) in self.fields {
            map.serialize_entry(field, value)?;
        }
        map.end()
    }
}

fn validate_raw_request(
    id: &str,
    command: &str,
    fields: &Map<String, Value>,
) -> Result<(), SnowError> {
    if id.trim().is_empty() {
        return Err(SnowError::Protocol(
            "raw RPC request id must be non-empty".into(),
        ));
    }
    if command.trim().is_empty() {
        return Err(SnowError::Protocol(
            "raw RPC command must be non-empty".into(),
        ));
    }
    for reserved in ["type", "id"] {
        if fields.contains_key(reserved) {
            return Err(SnowError::Protocol(format!(
                "raw RPC fields must not contain reserved field {reserved}"
            )));
        }
    }
    Ok(())
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct BranchSelectParams {
    pub branch_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct BranchForkParams {
    #[serde(skip_serializing_if = "String::is_empty")]
    pub source_branch_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SessionRenameParams {
    pub name: String,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum PermissionDecision {
    Allow,
    AllowSession,
    AllowAlways,
    Deny,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PermissionReplyParams {
    pub request_id: String,
    pub decision: PermissionDecision,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PermissionRejectParams {
    pub request_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputRejectParams {
    pub request_id: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputReplyParams {
    pub request_id: String,
    pub answers: Vec<UserInputAnswer>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct UserInputAnswer {
    #[serde(rename = "id")]
    pub question_id: String,
    pub answer: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum KeybindingScope {
    Global,
    Project,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct KeybindingsUpdateParams {
    pub scope: KeybindingScope,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub bindings: BTreeMap<String, Vec<String>>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub reset: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct ThemeSettingsUpdateParams {
    pub theme: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AgentRef {
    #[serde(default)]
    pub thread_id: String,
    #[serde(default)]
    pub parent_thread_id: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub parent_path: String,
    #[serde(default)]
    pub role: String,
    #[serde(default)]
    pub nickname: String,
    #[serde(default)]
    pub depth: usize,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct PermissionRequest {
    pub id: String,
    #[serde(default)]
    pub agent: Option<AgentRef>,
    pub tool: String,
    pub args: Value,
    #[serde(default)]
    pub paths: Vec<String>,
    pub risk: String,
    #[serde(default)]
    pub reason: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputOption {
    pub label: String,
    #[serde(default)]
    pub description: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputQuestion {
    pub id: String,
    pub header: String,
    pub question: String,
    #[serde(default)]
    pub options: Vec<UserInputOption>,
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct UserInputRequest {
    pub id: String,
    #[serde(default)]
    pub agent: Option<AgentRef>,
    pub tool_call_id: String,
    pub questions: Vec<UserInputQuestion>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum InteractionKind {
    Permission,
    UserInput,
}

impl InteractionKind {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Permission => "permission",
            Self::UserInput => "user input",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MalformedInteraction {
    pub kind: InteractionKind,
    pub request_id: Option<String>,
    pub error: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionBranch {
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub parent_branch_id: String,
    #[serde(default)]
    pub forked_from_id: String,
    pub tip_id: String,
    pub messages: usize,
    #[serde(default)]
    pub preview: String,
    pub created_at: i64,
    pub updated_at: i64,
    pub active: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct BranchCatalog {
    pub branches: Vec<SessionBranch>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionRenameResult {
    pub session_id: String,
    pub name: String,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize)]
pub struct ModelInfo {
    pub provider: String,
    pub id: String,
    #[serde(default)]
    pub display_name: String,
    /// Provider-owned safe description. Catalogs use this field for privacy
    /// notices and data-policy warnings when they apply.
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub context_window: u64,
    #[serde(default)]
    pub max_context_window: u64,
    #[serde(default)]
    pub max_output_tokens: u64,
    #[serde(default)]
    pub supports_tools: bool,
    #[serde(default)]
    pub supports_thinking: bool,
    #[serde(default)]
    pub default_thinking: String,
    #[serde(default)]
    pub thinking_levels: Vec<String>,
    #[serde(default)]
    pub supports_vision: bool,
    #[serde(default)]
    pub supports_verbosity: bool,
    /// `None` preserves the Go protocol's legacy/unknown distinction.
    #[serde(default)]
    pub supports_reasoning_summary: Option<bool>,
    #[serde(default)]
    pub upgrade: Option<ModelUpgrade>,
    #[serde(default)]
    pub pricing: Option<ModelPricing>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct ModelUpgrade {
    pub model: String,
    #[serde(default)]
    pub message: String,
}

#[derive(Debug, Clone, Default, PartialEq, Deserialize)]
pub struct ModelPricing {
    #[serde(default)]
    pub currency: String,
    #[serde(default)]
    pub input_per_million: f64,
    #[serde(default)]
    pub output_per_million: f64,
    #[serde(default)]
    pub cache_read_per_million: f64,
    #[serde(default)]
    pub cache_write_per_million: f64,
}

#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct ModelCatalog {
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub current: String,
    #[serde(default)]
    pub models: Vec<ModelInfo>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct GoalSummary {
    #[serde(default)]
    pub goal_id: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub blocked_reason: String,
    #[serde(default)]
    pub tokens_used: i64,
    pub token_budget: Option<i64>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SubagentLimits {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub max_concurrent_agents: usize,
    #[serde(default)]
    pub max_concurrent_threads: usize,
    #[serde(default)]
    pub max_agents_per_session: usize,
    #[serde(default)]
    pub max_depth: usize,
    #[serde(default)]
    pub durable: bool,
    #[serde(default)]
    pub allow_mutation: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct PendingInputCounts {
    #[serde(default)]
    pub steering: usize,
    #[serde(default)]
    pub follow_up: usize,
    #[serde(default)]
    pub total: usize,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionInfo {
    pub session_id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub path: String,
    #[serde(default)]
    pub cwd: String,
    pub provider: String,
    pub model: String,
    #[serde(default = "default_thinking_level")]
    pub thinking: String,
    #[serde(default)]
    pub thinking_levels: Vec<String>,
    #[serde(default)]
    pub reasoning_summary: String,
    #[serde(default)]
    pub text_verbosity: String,
    #[serde(default = "default_collaboration_mode")]
    pub collaboration_mode: String,
    #[serde(default)]
    pub goal: Option<GoalSummary>,
    #[serde(default)]
    pub subagents: SubagentLimits,
    #[serde(default)]
    pub pending_inputs: PendingInputCounts,
    #[serde(default)]
    pub permission_mode: String,
}

fn default_thinking_level() -> String {
    "off".into()
}

fn default_collaboration_mode() -> String {
    "default".into()
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionSummary {
    pub session_id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub created_at: i64,
    #[serde(default)]
    pub updated_at: i64,
    #[serde(default)]
    pub messages: usize,
    #[serde(default)]
    pub messages_capped: bool,
    #[serde(default)]
    pub active: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SessionList {
    #[serde(default)]
    pub sessions: Vec<SessionSummary>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthMethod {
    pub id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub kind: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthStatus {
    pub provider_id: String,
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub method: String,
    #[serde(default)]
    pub refreshable: bool,
    #[serde(default)]
    pub expires_at: i64,
    #[serde(default)]
    pub account_id: String,
    #[serde(default)]
    pub summary: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthProvider {
    pub provider_id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub required: bool,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub kinds: Vec<String>,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub environment: Vec<String>,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub methods: Vec<AuthMethod>,
    #[serde(default)]
    pub status: AuthStatus,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthProviderList {
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub providers: Vec<AuthProvider>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthProgress {
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub message: String,
    #[serde(default)]
    pub url: String,
    #[serde(default)]
    pub user_code: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AdaptiveColor {
    pub light: String,
    pub dark: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ThemeColors {
    pub accent: AdaptiveColor,
    pub muted: AdaptiveColor,
    pub foreground: AdaptiveColor,
    pub warning: AdaptiveColor,
    pub error: AdaptiveColor,
    pub success: AdaptiveColor,
    pub separator: AdaptiveColor,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ThemeDescriptor {
    pub name: String,
    pub display_name: String,
    pub scope: String,
    pub colors: ThemeColors,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ThemeCatalog {
    pub selected: String,
    pub themes: Vec<ThemeDescriptor>,
}

fn deserialize_sequence<'de, D, T>(deserializer: D) -> Result<Vec<T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    Option::<Vec<T>>::deserialize(deserializer).map(Option::unwrap_or_default)
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct KeybindingAction {
    pub name: String,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub global: Vec<String>,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub project: Vec<String>,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub effective: Vec<String>,
    pub source: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Keybindings {
    pub project_allowed: bool,
    pub actions: Vec<KeybindingAction>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Settings {
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub thinking: String,
    #[serde(default)]
    pub reasoning_summary: String,
    #[serde(default)]
    pub text_verbosity: String,
    #[serde(default)]
    pub theme: String,
    #[serde(default)]
    pub permission_mode: String,
    #[serde(default)]
    pub debug_enabled: bool,
    #[serde(default)]
    pub subagents_enabled: bool,
    #[serde(default)]
    pub subagents_max_concurrent: i64,
    #[serde(default)]
    pub subagents_max_agents: i64,
    #[serde(default)]
    pub skills_enabled: bool,
    #[serde(default)]
    pub subagents_restart_required: bool,
    #[serde(default)]
    pub skills_restart_required: bool,
    #[serde(default)]
    pub restart_required: bool,
}

pub fn decode_theme_catalog(value: Value) -> Result<ThemeCatalog, SnowError> {
    validate_presentation_data_size(&value, "themes_list")?;
    let catalog: ThemeCatalog = serde_json::from_value(value).map_err(|error| {
        SnowError::Protocol(format!("invalid themes_list response data: {error}"))
    })?;
    if !(MIN_THEME_CATALOG_ITEMS..=MAX_THEME_CATALOG_ITEMS).contains(&catalog.themes.len()) {
        return Err(SnowError::Protocol(format!(
            "themes_list must contain {MIN_THEME_CATALOG_ITEMS}..={MAX_THEME_CATALOG_ITEMS} themes"
        )));
    }
    validate_theme_text(&catalog.selected, "selected theme")?;
    let mut names = HashSet::with_capacity(catalog.themes.len());
    for theme in &catalog.themes {
        validate_theme_text(&theme.name, "theme name")?;
        validate_theme_text(&theme.display_name, "theme display name")?;
        if !names.insert(theme.name.as_str()) {
            return Err(SnowError::Protocol(
                "themes_list contains duplicate theme names".into(),
            ));
        }
        if !matches!(theme.scope.as_str(), "builtin" | "global" | "project") {
            return Err(SnowError::Protocol(
                "themes_list contains an invalid theme scope".into(),
            ));
        }
        for (role, color) in [
            ("accent", &theme.colors.accent),
            ("muted", &theme.colors.muted),
            ("foreground", &theme.colors.foreground),
            ("warning", &theme.colors.warning),
            ("error", &theme.colors.error),
            ("success", &theme.colors.success),
            ("separator", &theme.colors.separator),
        ] {
            validate_color(&color.light, role)?;
            validate_color(&color.dark, role)?;
        }
    }
    if !names.contains(catalog.selected.as_str()) {
        return Err(SnowError::Protocol(
            "themes_list selected theme is absent from the catalog".into(),
        ));
    }
    Ok(catalog)
}

pub fn decode_keybindings(value: Value) -> Result<Keybindings, SnowError> {
    validate_presentation_data_size(&value, "keybindings")?;
    let bindings: Keybindings = serde_json::from_value(value).map_err(|error| {
        SnowError::Protocol(format!("invalid keybindings response data: {error}"))
    })?;
    if bindings.actions.len() != KEYBINDING_ACTION_COUNT {
        return Err(SnowError::Protocol(format!(
            "keybindings response must contain exactly {KEYBINDING_ACTION_COUNT} actions"
        )));
    }
    let mut seen = HashSet::with_capacity(KEYBINDING_ACTION_COUNT);
    for action in &bindings.actions {
        if !KEYBINDING_ACTIONS.contains(&action.name.as_str()) || !seen.insert(action.name.as_str())
        {
            return Err(SnowError::Protocol(
                "keybindings response contains an unknown or duplicate action".into(),
            ));
        }
        validate_binding_list(&action.global, false)?;
        validate_binding_list(&action.project, false)?;
        validate_binding_list(&action.effective, true)?;
        if !matches!(action.source.as_str(), "default" | "global" | "project") {
            return Err(SnowError::Protocol(
                "keybindings response contains an invalid source".into(),
            ));
        }
        if action.source == "project" && !bindings.project_allowed {
            return Err(SnowError::Protocol(
                "keybindings response uses project bindings while project input is denied".into(),
            ));
        }
    }
    Ok(bindings)
}

pub fn decode_settings(value: Value) -> Result<Settings, SnowError> {
    validate_presentation_data_size(&value, "settings")?;
    let settings: Settings = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid settings response data: {error}")))?;
    if !settings.theme.is_empty() {
        validate_theme_text(&settings.theme, "settings theme")?;
    }
    Ok(settings)
}

pub fn validate_keybindings_update(
    params: &KeybindingsUpdateParams,
    project_allowed: bool,
) -> Result<(), SnowError> {
    if params.bindings.is_empty() && params.reset.is_empty() {
        return Err(SnowError::Protocol(
            "keybindings_update requires bindings or reset".into(),
        ));
    }
    if params.bindings.len() > KEYBINDING_ACTION_COUNT
        || params.reset.len() > KEYBINDING_ACTION_COUNT
    {
        return Err(SnowError::Protocol(
            "keybindings_update exceeds the action limit".into(),
        ));
    }
    if params.scope == KeybindingScope::Project && !project_allowed {
        return Err(SnowError::Protocol(
            "keybindings_update project scope requires trusted project input".into(),
        ));
    }
    let mut reset = HashSet::with_capacity(params.reset.len());
    for action in &params.reset {
        validate_action_name(action)?;
        if !reset.insert(action.as_str()) {
            return Err(SnowError::Protocol(
                "keybindings_update reset contains duplicate actions".into(),
            ));
        }
    }
    for (action, values) in &params.bindings {
        validate_action_name(action)?;
        if reset.contains(action.as_str()) {
            return Err(SnowError::Protocol(
                "keybindings_update cannot bind and reset the same action".into(),
            ));
        }
        validate_binding_list(values, true)?;
    }
    Ok(())
}

pub fn validate_theme_selection(theme: &str) -> Result<(), SnowError> {
    validate_theme_text(theme, "theme")
}

fn validate_presentation_data_size(value: &Value, command: &str) -> Result<(), SnowError> {
    let bytes = serde_json::to_vec(value)
        .map_err(|_| SnowError::Protocol(format!("could not measure {command} response data")))?;
    if bytes.len() > MAX_PRESENTATION_DATA_BYTES {
        return Err(SnowError::Protocol(format!(
            "{command} response data exceeds {MAX_PRESENTATION_DATA_BYTES} bytes"
        )));
    }
    Ok(())
}

fn validate_theme_text(value: &str, field: &str) -> Result<(), SnowError> {
    let count = value.chars().count();
    if count == 0
        || count > MAX_THEME_TEXT_CHARS
        || value.trim() != value
        || value
            .chars()
            .any(|character| character.is_control() || matches!(character, '/' | '\\'))
    {
        return Err(SnowError::Protocol(format!(
            "{field} must contain 1..={MAX_THEME_TEXT_CHARS} safe characters"
        )));
    }
    Ok(())
}

fn validate_color(value: &str, role: &str) -> Result<(), SnowError> {
    let valid_hex = value.len() == 7
        && value.starts_with('#')
        && value[1..].bytes().all(|byte| byte.is_ascii_hexdigit());
    let valid_index = value
        .parse::<u8>()
        .ok()
        .is_some_and(|parsed| parsed.to_string() == value);
    if value.chars().count() > MAX_THEME_TEXT_CHARS || (!valid_hex && !valid_index) {
        return Err(SnowError::Protocol(format!(
            "themes_list {role} color must be #RRGGBB or 0..255"
        )));
    }
    Ok(())
}

fn validate_action_name(action: &str) -> Result<(), SnowError> {
    if !KEYBINDING_ACTIONS.contains(&action) {
        return Err(SnowError::Protocol(
            "keybindings_update contains an unknown action".into(),
        ));
    }
    Ok(())
}

fn validate_binding_list(values: &[String], require_nonempty: bool) -> Result<(), SnowError> {
    if values.len() > MAX_BINDINGS_PER_ACTION || (require_nonempty && values.is_empty()) {
        return Err(SnowError::Protocol(format!(
            "keybinding list must contain {}..={MAX_BINDINGS_PER_ACTION} entries",
            usize::from(require_nonempty)
        )));
    }
    let mut seen = HashSet::with_capacity(values.len());
    for value in values {
        let count = value.chars().count();
        if count == 0
            || count > MAX_KEYBINDING_TEXT_CHARS
            || value.trim() != value
            || value.chars().any(char::is_control)
            || !seen.insert(value.as_str())
        {
            return Err(SnowError::Protocol(format!(
                "keybinding entries must be unique safe strings of 1..={MAX_KEYBINDING_TEXT_CHARS} characters"
            )));
        }
    }
    Ok(())
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct AuthLoginJob {
    pub job_id: String,
    pub provider_id: String,
    #[serde(default)]
    pub method: String,
    #[serde(default)]
    pub state: String,
    #[serde(default, deserialize_with = "deserialize_sequence")]
    pub progress: Vec<AuthProgress>,
    #[serde(default)]
    pub status: Option<AuthStatus>,
    #[serde(default)]
    pub error: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct ManagedProcess {
    pub process_id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub started_at: i64,
    #[serde(default)]
    pub finished_at: i64,
    pub exit_code: Option<i32>,
    #[serde(default)]
    pub signal: String,
    #[serde(default)]
    pub reason: String,
    #[serde(default)]
    pub ready: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct ManagedProcessList {
    #[serde(default)]
    pub processes: Vec<ManagedProcess>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SubagentState {
    pub agent: AgentRef,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub thinking: String,
    #[serde(default)]
    pub created_at: i64,
    #[serde(default)]
    pub started_at: i64,
    #[serde(default)]
    pub finished_at: i64,
    #[serde(default)]
    pub result: String,
    #[serde(default)]
    pub error: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct SubagentList {
    #[serde(default)]
    pub agents: Vec<SubagentState>,
    #[serde(default)]
    pub running: usize,
    #[serde(default)]
    pub queued: usize,
    #[serde(default)]
    pub terminal: usize,
    #[serde(default)]
    pub open: usize,
    #[serde(default)]
    pub closed: usize,
    #[serde(default)]
    pub concurrent_limit: usize,
    #[serde(default)]
    pub agent_limit: usize,
    #[serde(default)]
    pub truncated: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct ManagedProcessLogs {
    pub process_id: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub output: String,
    #[serde(default)]
    pub next_cursor: i64,
    #[serde(default)]
    pub omitted_bytes: i64,
    #[serde(default)]
    pub eof: bool,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct MessagesPageParams {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cursor: Option<String>,
    pub limit: usize,
    pub max_bytes: usize,
}

/// Selects one bounded page of a child's public durable history.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SubagentMessagesParams {
    pub target: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cursor: Option<String>,
    pub limit: usize,
    pub max_bytes: usize,
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct DecodedHistoryPage {
    pub entries: Vec<HistoryEntry>,
    pub next_cursor: Option<String>,
    pub start: usize,
    pub total: usize,
    pub wire_count: usize,
    pub has_more: bool,
}

/// Compatibility projection consumed by the current transcript workspace.
///
/// New presentation code should use [`decode_history_entries`] and render its
/// typed blocks. This flattened adapter intentionally omits tool-result entries
/// until the workspace has a dedicated card surface; it must never flatten
/// provider-private thinking or continuity data.
#[cfg(test)]
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryMessage {
    pub role: String,
    pub text: String,
}

/// A surface-safe durable history entry. Thinking and provider continuity
/// blocks have no enum variants and are discarded during decoding.
#[derive(Debug, Clone, PartialEq)]
pub struct HistoryEntry {
    pub role: String,
    pub blocks: Vec<HistoryBlock>,
    pub tool_result: Option<HistoryToolResult>,
}

/// One public durable child-history message with stable tree identity.
///
/// Provider continuity, private reasoning, assistant error strings, and
/// terminal subagent result/error fields are intentionally unrepresentable.
#[derive(Debug, Clone, PartialEq)]
pub struct SubagentHistoryMessage {
    pub id: String,
    pub parent_id: String,
    pub role: String,
    pub timestamp: i64,
    pub blocks: Vec<HistoryBlock>,
    pub tool_result: Option<HistoryToolResult>,
}

/// One validated page from a stable append-only child-history snapshot.
#[derive(Debug, Clone, PartialEq)]
pub struct SubagentMessagesPage {
    pub agent: AgentRef,
    pub generation: u64,
    pub messages: Vec<SubagentHistoryMessage>,
    pub next_cursor: Option<String>,
    pub start: usize,
    pub total: usize,
    pub wire_count: usize,
    pub has_more: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub enum HistoryBlock {
    Text { text: String },
    Plan { text: String, complete: bool },
    Image(HistoryImage),
    ToolCall(HistoryToolCall),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryImage {
    pub mime_type: String,
    /// Decoded bytes, bounded by [`MAX_HISTORY_IMAGE_BYTES`].
    pub data: Vec<u8>,
    /// One validated static preview cached for the lifetime of the history row.
    pub(crate) preview: Arc<Image>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct HistoryToolCall {
    pub tool_call_id: String,
    pub name: String,
    /// Pre-rendered and character-bounded public arguments. The full value is
    /// intentionally discarded during decoding to cap retained UI memory.
    pub arguments_display: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryToolResult {
    pub tool_call_id: String,
    pub tool_name: String,
    pub is_error: bool,
    pub display: HistoryToolDisplay,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct HistoryToolDisplay {
    pub started: bool,
    pub start_message: String,
    pub progress: Vec<String>,
    pub output: String,
    pub duration_ms: i64,
}

#[derive(Debug, Deserialize)]
struct HistoryData {
    #[serde(default)]
    messages: Vec<HistoryMessageWire>,
}

#[derive(Debug, Deserialize)]
struct HistoryPageData {
    messages: Vec<HistoryMessageWire>,
    #[serde(default)]
    next_cursor: String,
    start: usize,
    total: usize,
    has_more: bool,
}

#[derive(Debug, Deserialize)]
struct SubagentMessagesPageWire {
    agent: AgentRef,
    generation: u64,
    messages: Vec<HistoryMessageWire>,
    #[serde(default)]
    next_cursor: String,
    start: usize,
    total: usize,
    has_more: bool,
}

#[derive(Debug, Deserialize)]
struct HistoryMessageWire {
    #[serde(default)]
    id: String,
    #[serde(default)]
    parent_id: String,
    role: String,
    #[serde(default, rename = "ts")]
    timestamp: i64,
    #[serde(default)]
    content: Vec<HistoryContentWire>,
    #[serde(default)]
    tool_call_id: String,
    #[serde(default)]
    tool_name: String,
    #[serde(default)]
    is_error: bool,
    #[serde(default)]
    tool_display: Option<HistoryToolDisplayWire>,
}

#[derive(Debug, Deserialize)]
struct HistoryContentWire {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    text: String,
    #[serde(default)]
    plan_complete: bool,
    #[serde(default)]
    mime_type: String,
    #[serde(default)]
    data: String,
    #[serde(default)]
    tool_call_id: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    arguments: Option<Value>,
}

#[derive(Debug, Default, Deserialize)]
struct HistoryToolDisplayWire {
    #[serde(default)]
    started: bool,
    #[serde(default)]
    start_message: String,
    #[serde(default)]
    progress: Vec<String>,
    #[serde(default)]
    output: String,
    #[serde(default)]
    duration_ms: i64,
}

/// Decode the canonical typed history projection for rich desktop rendering.
///
/// Images are base64-decoded only after count and encoded-size checks. Unknown
/// image MIME types, invalid base64, oversized payloads, or excessive image
/// counts fail the response rather than passing untrusted bytes to a renderer.
pub fn decode_history_entries(value: Value) -> Result<Vec<HistoryEntry>, SnowError> {
    let history: HistoryData = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid messages_list data: {error}")))?;
    decode_history_wires(history.messages)
}

pub(crate) fn decode_history_page(value: Value) -> Result<DecodedHistoryPage, SnowError> {
    let page: HistoryPageData = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid messages_page data: {error}")))?;
    let wire_count = page.messages.len();
    if wire_count > MAX_HISTORY_PAGE_MESSAGES {
        return Err(SnowError::Protocol(format!(
            "messages_page contains more than {MAX_HISTORY_PAGE_MESSAGES} messages"
        )));
    }
    let end = page
        .start
        .checked_add(wire_count)
        .ok_or_else(|| SnowError::Protocol("messages_page bounds overflow".into()))?;
    if page.start > page.total || end > page.total {
        return Err(SnowError::Protocol(
            "messages_page bounds exceed the declared total".into(),
        ));
    }
    if page.next_cursor.len() > MAX_HISTORY_PAGE_CURSOR_BYTES {
        return Err(SnowError::Protocol(format!(
            "messages_page cursor exceeds {MAX_HISTORY_PAGE_CURSOR_BYTES} bytes"
        )));
    }
    if page.has_more {
        if wire_count == 0 || end >= page.total || page.next_cursor.is_empty() {
            return Err(SnowError::Protocol(
                "non-terminal messages_page must make progress and include a cursor".into(),
            ));
        }
    } else if end != page.total || !page.next_cursor.is_empty() {
        return Err(SnowError::Protocol(
            "terminal messages_page has inconsistent bounds or cursor".into(),
        ));
    }
    let entries = decode_history_wires(page.messages)?;
    Ok(DecodedHistoryPage {
        entries,
        next_cursor: page.has_more.then_some(page.next_cursor),
        start: page.start,
        total: page.total,
        wire_count,
        has_more: page.has_more,
    })
}

/// Decode and validate one bounded public child-history page.
///
/// `max_bytes` must be the exact request budget used for the correlated RPC
/// call. The decoder rejects server responses that exceed that budget and
/// rejects provider-private continuity rather than silently accepting a
/// privacy-boundary regression.
pub fn decode_subagent_messages_page(
    value: Value,
    max_bytes: usize,
) -> Result<SubagentMessagesPage, SnowError> {
    if !(MIN_SUBAGENT_MESSAGE_PAGE_BYTES..=MAX_SUBAGENT_MESSAGE_PAGE_BYTES).contains(&max_bytes) {
        return Err(SnowError::Protocol(format!(
            "subagent_messages max_bytes must be between {MIN_SUBAGENT_MESSAGE_PAGE_BYTES} and {MAX_SUBAGENT_MESSAGE_PAGE_BYTES}"
        )));
    }
    let encoded_len = serde_json::to_vec(&value)
        .map_err(|error| {
            SnowError::Protocol(format!("could not measure subagent_messages data: {error}"))
        })?
        .len();
    if encoded_len > max_bytes {
        return Err(SnowError::Protocol(format!(
            "subagent_messages data exceeds the correlated {max_bytes} byte budget"
        )));
    }

    let page: SubagentMessagesPageWire = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid subagent_messages data: {error}")))?;
    validate_subagent_agent_ref(&page.agent)?;
    let wire_count = page.messages.len();
    if wire_count > MAX_SUBAGENT_MESSAGES_PER_PAGE {
        return Err(SnowError::Protocol(format!(
            "subagent_messages contains more than {MAX_SUBAGENT_MESSAGES_PER_PAGE} messages"
        )));
    }
    let end = page
        .start
        .checked_add(wire_count)
        .ok_or_else(|| SnowError::Protocol("subagent_messages bounds overflow".into()))?;
    if page.start > page.total || end > page.total {
        return Err(SnowError::Protocol(
            "subagent_messages bounds exceed the declared total".into(),
        ));
    }
    if page.next_cursor.len() > MAX_SUBAGENT_MESSAGE_CURSOR_BYTES {
        return Err(SnowError::Protocol(format!(
            "subagent_messages cursor exceeds {MAX_SUBAGENT_MESSAGE_CURSOR_BYTES} bytes"
        )));
    }
    if page.has_more {
        if wire_count == 0 || end >= page.total || page.next_cursor.is_empty() {
            return Err(SnowError::Protocol(
                "non-terminal subagent_messages page must make progress and include a cursor"
                    .into(),
            ));
        }
    } else if end != page.total || !page.next_cursor.is_empty() {
        return Err(SnowError::Protocol(
            "terminal subagent_messages page has inconsistent bounds or cursor".into(),
        ));
    }

    let mut validated_image_count = 0usize;
    let mut decoded_image_count = 0usize;
    let mut block_count = 0usize;
    let mut ids = HashSet::with_capacity(wire_count);
    let mut messages = Vec::with_capacity(wire_count);
    for message in page.messages {
        validate_subagent_message_wire(
            &message,
            &mut block_count,
            &mut validated_image_count,
            &mut ids,
        )?;
        let id = message.id.clone();
        let parent_id = message.parent_id.clone();
        let timestamp = message.timestamp;
        let entry = decode_history_entry(message, &mut decoded_image_count)?;
        messages.push(SubagentHistoryMessage {
            id,
            parent_id,
            role: entry.role,
            timestamp,
            blocks: entry.blocks,
            tool_result: entry.tool_result,
        });
    }

    Ok(SubagentMessagesPage {
        agent: page.agent,
        generation: page.generation,
        messages,
        next_cursor: page.has_more.then_some(page.next_cursor),
        start: page.start,
        total: page.total,
        wire_count,
        has_more: page.has_more,
    })
}

fn validate_subagent_agent_ref(agent: &AgentRef) -> Result<(), SnowError> {
    for (name, value) in [("path", &agent.path), ("thread_id", &agent.thread_id)] {
        if value.trim().is_empty() || value.len() > MAX_SUBAGENT_IDENTITY_BYTES {
            return Err(SnowError::Protocol(format!(
                "subagent_messages agent {name} is missing or exceeds {MAX_SUBAGENT_IDENTITY_BYTES} bytes"
            )));
        }
    }
    for (name, value) in [
        ("parent_path", &agent.parent_path),
        ("parent_thread_id", &agent.parent_thread_id),
    ] {
        if value.len() > MAX_SUBAGENT_IDENTITY_BYTES {
            return Err(SnowError::Protocol(format!(
                "subagent_messages agent {name} exceeds {MAX_SUBAGENT_IDENTITY_BYTES} bytes"
            )));
        }
    }
    Ok(())
}

fn validate_subagent_message_wire(
    message: &HistoryMessageWire,
    block_count: &mut usize,
    image_count: &mut usize,
    ids: &mut HashSet<String>,
) -> Result<(), SnowError> {
    if message.id.trim().is_empty() || message.id.len() > MAX_SUBAGENT_MESSAGE_ID_BYTES {
        return Err(SnowError::Protocol(format!(
            "subagent_messages message id is missing or exceeds {MAX_SUBAGENT_MESSAGE_ID_BYTES} bytes"
        )));
    }
    if message.parent_id.len() > MAX_SUBAGENT_MESSAGE_ID_BYTES {
        return Err(SnowError::Protocol(format!(
            "subagent_messages parent id exceeds {MAX_SUBAGENT_MESSAGE_ID_BYTES} bytes"
        )));
    }
    if !ids.insert(message.id.clone()) {
        return Err(SnowError::Protocol(
            "subagent_messages contains duplicate message ids".into(),
        ));
    }
    if message.timestamp < 0 {
        return Err(SnowError::Protocol(
            "subagent_messages message timestamp is negative".into(),
        ));
    }
    if !matches!(
        message.role.as_str(),
        "user" | "assistant" | "tool_result" | "tool" | "agent" | "system" | "custom"
    ) {
        return Err(SnowError::Protocol(format!(
            "subagent_messages contains unsupported role {:?}",
            message.role
        )));
    }
    if message.content.len() > MAX_SUBAGENT_BLOCKS_PER_MESSAGE {
        return Err(SnowError::Protocol(format!(
            "subagent_messages message exceeds {MAX_SUBAGENT_BLOCKS_PER_MESSAGE} content blocks"
        )));
    }
    *block_count = block_count
        .checked_add(message.content.len())
        .ok_or_else(|| SnowError::Protocol("subagent_messages block count overflow".into()))?;
    if *block_count > MAX_SUBAGENT_BLOCKS_PER_PAGE {
        return Err(SnowError::Protocol(format!(
            "subagent_messages page exceeds {MAX_SUBAGENT_BLOCKS_PER_PAGE} content blocks"
        )));
    }
    for block in &message.content {
        if block.kind == "provider_data" {
            return Err(SnowError::Protocol(
                "subagent_messages contains provider-private continuity data".into(),
            ));
        }
        if block.kind == "image" {
            *image_count = image_count.checked_add(1).ok_or_else(|| {
                SnowError::Protocol("subagent_messages image count overflow".into())
            })?;
            if *image_count > MAX_SUBAGENT_MESSAGE_IMAGES {
                return Err(SnowError::Protocol(format!(
                    "subagent_messages exceeds the {MAX_SUBAGENT_MESSAGE_IMAGES} image page limit"
                )));
            }
        }
    }
    Ok(())
}

fn decode_history_wires(messages: Vec<HistoryMessageWire>) -> Result<Vec<HistoryEntry>, SnowError> {
    let mut image_count = 0;
    let mut entries = Vec::new();
    for message in messages.into_iter().filter(|message| {
        matches!(
            message.role.as_str(),
            "user" | "assistant" | "tool_result" | "tool"
        )
    }) {
        let entry = decode_history_entry(message, &mut image_count)?;
        if !entry.blocks.is_empty() || entry.tool_result.is_some() {
            entries.push(entry);
        }
    }
    Ok(entries)
}

/// Compatibility adapter for the existing text transcript.
///
/// Workspace migration: switch the client event payload to `HistoryEntry`,
/// render `HistoryBlock::Image` from its decoded bytes, and render
/// `HistoryToolResult` as a card. Remove this adapter only after all consumers
/// have migrated.
#[cfg(test)]
pub fn decode_history(value: Value) -> Result<Vec<HistoryMessage>, SnowError> {
    Ok(decode_history_entries(value)?
        .into_iter()
        .filter_map(flatten_history_entry)
        .collect())
}

fn decode_history_entry(
    message: HistoryMessageWire,
    image_count: &mut usize,
) -> Result<HistoryEntry, SnowError> {
    let is_tool_result = matches!(message.role.as_str(), "tool_result" | "tool");
    let mut blocks = Vec::new();
    if !is_tool_result {
        for block in message.content {
            let decoded = match block.kind.as_str() {
                "text" if !block.text.is_empty() => Some(HistoryBlock::Text { text: block.text }),
                "plan" => Some(HistoryBlock::Plan {
                    text: block.text,
                    complete: block.plan_complete,
                }),
                "image" => Some(HistoryBlock::Image(decode_history_image(
                    block.mime_type,
                    block.data,
                    image_count,
                )?)),
                "tool_call" => Some(HistoryBlock::ToolCall(HistoryToolCall {
                    tool_call_id: block.tool_call_id,
                    name: block.name,
                    arguments_display: block
                        .arguments
                        .and_then(|arguments| serde_json::to_string_pretty(&arguments).ok())
                        .map(bounded_history_text)
                        .unwrap_or_default(),
                })),
                // These blocks can contain private reasoning or provider-owned
                // continuity state and are intentionally not representable.
                "thinking" | "provider_data" => None,
                _ => None,
            };
            if let Some(decoded) = decoded {
                blocks.push(decoded);
            }
        }
    }

    let tool_result = is_tool_result.then(|| HistoryToolResult {
        tool_call_id: message.tool_call_id,
        tool_name: message.tool_name,
        is_error: message.is_error,
        display: sanitize_tool_display(message.tool_display.unwrap_or_default()),
    });
    Ok(HistoryEntry {
        role: if message.role == "tool" {
            "tool_result".into()
        } else {
            message.role
        },
        blocks,
        tool_result,
    })
}

fn decode_history_image(
    mime_type: String,
    encoded: String,
    image_count: &mut usize,
) -> Result<HistoryImage, SnowError> {
    if !matches!(
        mime_type.as_str(),
        "image/png" | "image/jpeg" | "image/gif" | "image/webp"
    ) {
        return Err(SnowError::Protocol(format!(
            "messages_list image has unsupported MIME type {mime_type:?}"
        )));
    }
    if *image_count >= MAX_HISTORY_IMAGES {
        return Err(SnowError::Protocol(format!(
            "messages_list exceeds the {MAX_HISTORY_IMAGES} image limit"
        )));
    }
    let max_encoded_bytes = MAX_HISTORY_IMAGE_BYTES.div_ceil(3) * 4;
    if encoded.len() > max_encoded_bytes {
        return Err(SnowError::Protocol(format!(
            "messages_list image exceeds the {MAX_HISTORY_IMAGE_BYTES} decoded-byte limit"
        )));
    }
    let data = BASE64_STANDARD
        .decode(encoded.as_bytes())
        .map_err(|_| SnowError::Protocol("messages_list image contains invalid base64".into()))?;
    if data.is_empty() || data.len() > MAX_HISTORY_IMAGE_BYTES {
        return Err(SnowError::Protocol(format!(
            "messages_list image must contain 1..={MAX_HISTORY_IMAGE_BYTES} decoded bytes"
        )));
    }
    let preview = validate_and_cache_image(&mime_type, &data)
        .map_err(|error| SnowError::Protocol(format!("messages_list image is unsafe: {error}")))?;
    *image_count += 1;
    Ok(HistoryImage {
        mime_type,
        data,
        preview,
    })
}

fn sanitize_tool_display(display: HistoryToolDisplayWire) -> HistoryToolDisplay {
    HistoryToolDisplay {
        started: display.started,
        start_message: bounded_history_text(display.start_message),
        progress: display
            .progress
            .into_iter()
            .filter(|item| !item.is_empty())
            .take(MAX_HISTORY_TOOL_PROGRESS_ITEMS)
            .map(bounded_history_text)
            .collect(),
        output: bounded_history_text(display.output),
        duration_ms: display.duration_ms.max(0),
    }
}

fn bounded_history_text(text: String) -> String {
    if text.chars().count() <= MAX_HISTORY_TOOL_TEXT_CHARS {
        return text;
    }
    text.chars().take(MAX_HISTORY_TOOL_TEXT_CHARS).collect()
}

#[cfg(test)]
fn flatten_history_entry(entry: HistoryEntry) -> Option<HistoryMessage> {
    if !matches!(entry.role.as_str(), "user" | "assistant") {
        return None;
    }
    let mut parts = Vec::new();
    for block in entry.blocks {
        let rendered = match block {
            HistoryBlock::Text { text } => text,
            HistoryBlock::Plan { text, .. } if text.is_empty() => "[Plan]".into(),
            HistoryBlock::Plan { text, .. } => format!("Plan\n{text}"),
            HistoryBlock::Image(image) => format!(
                "[Image · {} · {} decoded bytes]",
                image.mime_type,
                image.data.len()
            ),
            HistoryBlock::ToolCall(call) => {
                let name = if call.name.is_empty() {
                    "tool"
                } else {
                    call.name.as_str()
                };
                let mut rendered = format!("Tool call · {name}");
                if !call.tool_call_id.is_empty() {
                    rendered.push_str(&format!(" · {}", call.tool_call_id));
                }
                if !call.arguments_display.is_empty() {
                    rendered.push('\n');
                    rendered.push_str(&call.arguments_display);
                }
                rendered
            }
        };
        if !rendered.is_empty() {
            parts.push(rendered);
        }
    }
    let text = parts.join("\n\n");
    (!text.is_empty()).then_some(HistoryMessage {
        role: entry.role,
        text,
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RpcReady {
    pub protocol_version: String,
    pub snow_version: String,
    pub capabilities: HashSet<String>,
    pub max_input_bytes: usize,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RpcResponse {
    pub id: Option<String>,
    pub command: Option<String>,
    pub success: bool,
    pub data: Option<Value>,
    pub error: Option<String>,
    pub error_code: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PromptStatus {
    Completed,
    Failed,
    Canceled,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptCompleted {
    pub request_id: String,
    pub status: PromptStatus,
    pub error: Option<String>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct AgentEvent {
    pub kind: String,
    pub fields: Map<String, Value>,
}

impl AgentEvent {
    pub fn string(&self, field: &str) -> Option<&str> {
        self.fields.get(field)?.as_str()
    }

    pub fn boolean(&self, field: &str) -> Option<bool> {
        self.fields.get(field)?.as_bool()
    }

    pub fn has_agent(&self) -> bool {
        self.fields.contains_key("agent")
    }

    pub fn nested_string(&self, object: &str, field: &str) -> Option<&str> {
        self.fields.get(object)?.as_object()?.get(field)?.as_str()
    }

    pub fn nested_object_string(&self, outer: &str, inner: &str, field: &str) -> Option<&str> {
        self.fields
            .get(outer)?
            .as_object()?
            .get(inner)?
            .as_object()?
            .get(field)?
            .as_str()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct RawFrame {
    pub kind: String,
    pub fields: Map<String, Value>,
}

#[derive(Debug, Clone, PartialEq)]
pub enum RpcFrame {
    Ready(RpcReady),
    Response(RpcResponse),
    PromptCompleted(PromptCompleted),
    PermissionRequest(PermissionRequest),
    UserInputRequest(UserInputRequest),
    MalformedInteraction(MalformedInteraction),
    Agent(AgentEvent),
    Unknown(RawFrame),
}

#[derive(Debug, Deserialize)]
struct ReadyWire {
    #[serde(rename = "type")]
    kind: String,
    protocol_version: String,
    snow_version: String,
    capabilities: Vec<String>,
    max_input_bytes: usize,
}

const AGENT_EVENT_TYPES: &[&str] = &[
    "session_updated",
    "run_stats_updated",
    "text_delta",
    "thinking_delta",
    "tool_start",
    "tool_progress",
    "tool_end",
    "tool_routing",
    "permission_request",
    "user_input_request",
    "usage",
    "provider_retry",
    "queue_updated",
    "turn_done",
    "error",
    "aborted",
    "model_changed",
    "mode_changed",
    "plan_started",
    "plan_delta",
    "plan_completed",
    "plan_update",
    "compaction_started",
    "compaction_done",
    "thread_goal_updated",
    "subagent_started",
    "subagent_status",
    "subagent_message",
    "subagent_activity",
];

pub fn encode_request(request: &RpcRequest, limit: usize) -> Result<Vec<u8>, SnowError> {
    let mut frame = match request {
        RpcRequest::Raw {
            id,
            command,
            fields,
        } => {
            validate_raw_request(id, command, fields)?;
            serde_json::to_vec(&RawRequestRef {
                id,
                command,
                fields,
            })
        }
        request => serde_json::to_vec(request),
    }
    .map_err(|error| SnowError::Protocol(format!("could not serialize request: {error}")))?;
    if frame.len() > limit {
        return Err(SnowError::FrameTooLarge { limit });
    }
    frame.push(b'\n');
    Ok(frame)
}

pub fn decode_frame(bytes: &[u8]) -> Result<RpcFrame, SnowError> {
    let text = std::str::from_utf8(bytes).map_err(|_| SnowError::InvalidUtf8)?;
    let value: Value =
        serde_json::from_str(text).map_err(|error| SnowError::InvalidJson(error.to_string()))?;
    let object = value
        .as_object()
        .ok_or_else(|| SnowError::Protocol("stdout frame must be a JSON object".into()))?;
    let kind = object
        .get("type")
        .and_then(Value::as_str)
        .ok_or_else(|| SnowError::Protocol("stdout frame is missing string field type".into()))?
        .to_owned();

    match kind.as_str() {
        "rpc_ready" => decode_ready(value),
        "response" => decode_response(object),
        "prompt_completed" => decode_prompt_completed(object),
        "permission_request" => decode_permission_request(object),
        "user_input_request" => decode_user_input_request(object),
        _ if AGENT_EVENT_TYPES.contains(&kind.as_str()) => {
            let mut fields = object.clone();
            fields.remove("type");
            Ok(RpcFrame::Agent(AgentEvent { kind, fields }))
        }
        _ => {
            let mut fields = object.clone();
            fields.remove("type");
            Ok(RpcFrame::Unknown(RawFrame { kind, fields }))
        }
    }
}

fn decode_permission_request(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = object
        .get("permission")
        .and_then(Value::as_object)
        .and_then(|permission| permission.get("request"))
        .and_then(Value::as_object)
        .and_then(|request| request.get("id"))
        .and_then(Value::as_str)
        .filter(|id| !id.trim().is_empty())
        .map(str::to_owned);
    let result = object
        .get("permission")
        .and_then(Value::as_object)
        .and_then(|permission| permission.get("request"))
        .cloned()
        .ok_or_else(|| "permission_request is missing permission.request".to_owned())
        .and_then(|value| {
            serde_json::from_value::<PermissionRequest>(value)
                .map_err(|error| format!("invalid permission_request: {error}"))
        })
        .and_then(|mut request| {
            request.agent = decode_agent_ref(object)?;
            Ok(request)
        })
        .and_then(validate_permission_request);
    match result {
        Ok(request) => Ok(RpcFrame::PermissionRequest(request)),
        Err(error) => Ok(RpcFrame::MalformedInteraction(MalformedInteraction {
            kind: InteractionKind::Permission,
            request_id,
            error,
        })),
    }
}

fn decode_agent_ref(object: &Map<String, Value>) -> Result<Option<AgentRef>, String> {
    let Some(value) = object.get("agent") else {
        return Ok(None);
    };
    let agent = serde_json::from_value::<AgentRef>(value.clone())
        .map_err(|error| format!("invalid interaction agent: {error}"))?;
    if agent.path.trim().is_empty() {
        return Err("interaction agent path must be non-empty".into());
    }
    Ok(Some(agent))
}

fn validate_permission_request(request: PermissionRequest) -> Result<PermissionRequest, String> {
    if request.id.trim().is_empty() {
        return Err("permission_request id must be non-empty".into());
    }
    if request.tool.trim().is_empty() {
        return Err("permission_request tool must be non-empty".into());
    }
    if request.risk.trim().is_empty() {
        return Err("permission_request risk must be non-empty".into());
    }
    Ok(request)
}

fn decode_user_input_request(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = object
        .get("user_input")
        .and_then(Value::as_object)
        .and_then(|request| request.get("id"))
        .and_then(Value::as_str)
        .filter(|id| !id.trim().is_empty())
        .map(str::to_owned);
    let result = object
        .get("user_input")
        .cloned()
        .ok_or_else(|| "user_input_request is missing user_input".to_owned())
        .and_then(|value| {
            serde_json::from_value::<UserInputRequest>(value)
                .map_err(|error| format!("invalid user_input_request: {error}"))
        })
        .and_then(|mut request| {
            request.agent = decode_agent_ref(object)?;
            Ok(request)
        })
        .and_then(validate_user_input_request);
    match result {
        Ok(request) => Ok(RpcFrame::UserInputRequest(request)),
        Err(error) => Ok(RpcFrame::MalformedInteraction(MalformedInteraction {
            kind: InteractionKind::UserInput,
            request_id,
            error,
        })),
    }
}

fn validate_user_input_request(request: UserInputRequest) -> Result<UserInputRequest, String> {
    if request.id.trim().is_empty() {
        return Err("user_input_request id must be non-empty".into());
    }
    if request.tool_call_id.trim().is_empty() {
        return Err("user_input_request tool_call_id must be non-empty".into());
    }
    if request.questions.is_empty() {
        return Err("user_input_request questions must be non-empty".into());
    }
    let mut ids = HashSet::with_capacity(request.questions.len());
    for question in &request.questions {
        if question.id.trim().is_empty()
            || question.header.trim().is_empty()
            || question.question.trim().is_empty()
        {
            return Err("user_input_request question fields must be non-empty".into());
        }
        if !ids.insert(question.id.as_str()) {
            return Err("user_input_request question ids must be unique".into());
        }
        if question
            .options
            .iter()
            .any(|option| option.label.trim().is_empty())
        {
            return Err("user_input_request option labels must be non-empty".into());
        }
    }
    Ok(request)
}

fn decode_ready(value: Value) -> Result<RpcFrame, SnowError> {
    let ready: ReadyWire = serde_json::from_value(value)
        .map_err(|error| SnowError::Protocol(format!("invalid rpc_ready frame: {error}")))?;
    if ready.kind != "rpc_ready" {
        return Err(SnowError::Protocol("invalid rpc_ready type".into()));
    }
    if ready.max_input_bytes == 0 {
        return Err(SnowError::Protocol(
            "rpc_ready max_input_bytes must be positive".into(),
        ));
    }
    Ok(RpcFrame::Ready(RpcReady {
        protocol_version: ready.protocol_version,
        snow_version: ready.snow_version,
        capabilities: ready.capabilities.into_iter().collect(),
        max_input_bytes: ready.max_input_bytes,
    }))
}

fn decode_response(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let success = required_bool(object, "success")?;
    let error = optional_string(object, "error")?;
    if !success && error.as_deref().is_none_or(str::is_empty) {
        return Err(SnowError::Protocol(
            "failed response must contain a non-empty error".into(),
        ));
    }
    Ok(RpcFrame::Response(RpcResponse {
        id: optional_string(object, "id")?,
        command: optional_string(object, "command")?,
        success,
        data: object.get("data").cloned(),
        error,
        error_code: optional_string(object, "error_code")?,
    }))
}

fn decode_prompt_completed(object: &Map<String, Value>) -> Result<RpcFrame, SnowError> {
    let request_id = required_string(object, "request_id")?;
    let status = match required_string(object, "status")?.as_str() {
        "completed" => PromptStatus::Completed,
        "failed" => PromptStatus::Failed,
        "canceled" => PromptStatus::Canceled,
        other => {
            return Err(SnowError::Protocol(format!(
                "unknown prompt completion status {other}"
            )));
        }
    };
    let error = optional_string(object, "error")?;
    if status == PromptStatus::Failed && error.as_deref().is_none_or(str::is_empty) {
        return Err(SnowError::Protocol(
            "failed prompt completion must contain a non-empty error".into(),
        ));
    }
    Ok(RpcFrame::PromptCompleted(PromptCompleted {
        request_id,
        status,
        error,
    }))
}

fn required_string(object: &Map<String, Value>, field: &str) -> Result<String, SnowError> {
    object
        .get(field)
        .and_then(Value::as_str)
        .map(str::to_owned)
        .ok_or_else(|| SnowError::Protocol(format!("frame is missing string field {field}")))
}

fn optional_string(object: &Map<String, Value>, field: &str) -> Result<Option<String>, SnowError> {
    match object.get(field) {
        None => Ok(None),
        Some(value) => value
            .as_str()
            .map(|value| Some(value.to_owned()))
            .ok_or_else(|| SnowError::Protocol(format!("frame field {field} must be a string"))),
    }
}

fn required_bool(object: &Map<String, Value>, field: &str) -> Result<bool, SnowError> {
    object
        .get(field)
        .and_then(Value::as_bool)
        .ok_or_else(|| SnowError::Protocol(format!("frame is missing boolean field {field}")))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_encoding_is_jsonl() {
        let frame = encode_request(
            &RpcRequest::Prompt {
                id: "p1".into(),
                message: "hello".into(),
            },
            1024,
        )
        .unwrap();
        assert_eq!(
            frame,
            br#"{"type":"prompt","id":"p1","message":"hello"}
"#
        );
    }

    #[test]
    fn project_init_encoding_uses_prompt_lifecycle_command() {
        let frame = encode_request(
            &RpcRequest::ProjectInit {
                id: "init-1".into(),
            },
            1024,
        )
        .unwrap();
        assert_eq!(frame, b"{\"type\":\"project_init\",\"id\":\"init-1\"}\n");
    }

    #[test]
    fn raw_request_encoding_preserves_arbitrary_top_level_fields() {
        let request = RpcRequest::raw(
            "raw-1".into(),
            "thread_goal_set".into(),
            Map::from_iter([
                ("goal".into(), Value::String("Ship it".into())),
                (
                    "params".into(),
                    serde_json::json!({"nested": true, "count": 2}),
                ),
            ]),
        )
        .unwrap();

        assert_eq!(
            encode_request(&request, 1024).unwrap(),
            b"{\"type\":\"thread_goal_set\",\"id\":\"raw-1\",\"goal\":\"Ship it\",\"params\":{\"nested\":true,\"count\":2}}\n"
        );
    }

    #[test]
    fn raw_request_rejects_reserved_top_level_fields() {
        for reserved in ["type", "id"] {
            let error = RpcRequest::raw(
                "raw-1".into(),
                "custom".into(),
                Map::from_iter([(reserved.into(), Value::String("override".into()))]),
            )
            .unwrap_err();
            assert!(matches!(
                error,
                SnowError::Protocol(message) if message.contains(reserved)
            ));
        }
    }

    #[test]
    fn abort_encoding_is_jsonl() {
        let frame = encode_request(&RpcRequest::Abort { id: "a1".into() }, 1024).unwrap();
        assert_eq!(
            frame,
            br#"{"type":"abort","id":"a1"}
"#
        );
    }

    #[test]
    fn runtime_state_requests_are_jsonl() {
        assert_eq!(
            encode_request(&RpcRequest::ModelsList { id: "m1".into() }, 1024).unwrap(),
            b"{\"type\":\"models_list\",\"id\":\"m1\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SetModel {
                    id: "m2".into(),
                    model: "model-two".into(),
                    thinking: Some("high".into()),
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"set_model\",\"id\":\"m2\",\"model\":\"model-two\",\"thinking\":\"high\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SetThinking {
                    id: "t1".into(),
                    thinking: "medium".into(),
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"set_thinking\",\"id\":\"t1\",\"thinking\":\"medium\"}\n"
        );
        assert_eq!(
            encode_request(&RpcRequest::BranchesList { id: "b1".into() }, 1024).unwrap(),
            b"{\"type\":\"branches_list\",\"id\":\"b1\"}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::MessagesPage {
                    id: "h1".into(),
                    params: MessagesPageParams {
                        cursor: Some("next".into()),
                        limit: 32,
                        max_bytes: 2 * 1024 * 1024,
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"messages_page\",\"id\":\"h1\",\"params\":{\"cursor\":\"next\",\"limit\":32,\"max_bytes\":2097152}}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::BranchSelect {
                    id: "b2".into(),
                    params: BranchSelectParams {
                        branch_id: "experiment".into(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"branch_select\",\"id\":\"b2\",\"params\":{\"branch_id\":\"experiment\"}}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::BranchFork {
                    id: "b3".into(),
                    params: BranchForkParams {
                        source_branch_id: String::new(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"branch_fork\",\"id\":\"b3\",\"params\":{}}\n"
        );
        assert_eq!(
            encode_request(
                &RpcRequest::SessionRename {
                    id: "r1".into(),
                    params: SessionRenameParams {
                        name: "API cleanup".into(),
                    },
                },
                1024,
            )
            .unwrap(),
            b"{\"type\":\"session_rename\",\"id\":\"r1\",\"params\":{\"name\":\"API cleanup\"}}\n"
        );
    }

    #[test]
    fn interaction_requests_have_exact_jsonl_encoding() {
        let cases = [
            (
                RpcRequest::PermissionReply {
                    id: "c1".into(),
                    params: PermissionReplyParams {
                        request_id: "perm-1".into(),
                        decision: PermissionDecision::AllowSession,
                    },
                },
                "{\"type\":\"permission_reply\",\"id\":\"c1\",\"params\":{\"request_id\":\"perm-1\",\"decision\":\"allow_session\"}}\n",
            ),
            (
                RpcRequest::PermissionReject {
                    id: "c2".into(),
                    params: PermissionRejectParams {
                        request_id: "perm-1".into(),
                    },
                },
                "{\"type\":\"permission_reject\",\"id\":\"c2\",\"params\":{\"request_id\":\"perm-1\"}}\n",
            ),
            (
                RpcRequest::UserInputReply {
                    id: "c3".into(),
                    params: UserInputReplyParams {
                        request_id: "ask-1".into(),
                        answers: vec![UserInputAnswer {
                            question_id: "language".into(),
                            answer: "Rust".into(),
                        }],
                    },
                },
                "{\"type\":\"user_input_reply\",\"id\":\"c3\",\"params\":{\"request_id\":\"ask-1\",\"answers\":[{\"id\":\"language\",\"answer\":\"Rust\"}]}}\n",
            ),
            (
                RpcRequest::UserInputReject {
                    id: "c4".into(),
                    params: UserInputRejectParams {
                        request_id: "ask-1".into(),
                    },
                },
                "{\"type\":\"user_input_reject\",\"id\":\"c4\",\"params\":{\"request_id\":\"ask-1\"}}\n",
            ),
        ];
        for (request, expected) in cases {
            assert_eq!(encode_request(&request, 1024).unwrap(), expected.as_bytes());
        }
    }

    #[test]
    fn root_interaction_events_decode_to_typed_requests() {
        let RpcFrame::PermissionRequest(permission) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1","tool":"bash","args":{"command":"pwd"},"paths":["/tmp"],"risk":"exec","reason":"run command"}}}"#,
        )
        .unwrap()
        else {
            panic!("wanted permission request")
        };
        assert_eq!(permission.id, "perm-1");
        assert_eq!(permission.args["command"], "pwd");

        let RpcFrame::UserInputRequest(user_input) = decode_frame(
            br#"{"type":"user_input_request","user_input":{"id":"ask-1","tool_call_id":"call-1","questions":[{"id":"language","header":"Language","question":"Which language?","options":[{"label":"Rust","description":"Safe"}]}]}}"#,
        )
        .unwrap()
        else {
            panic!("wanted user input request")
        };
        assert_eq!(user_input.id, "ask-1");
        assert_eq!(user_input.questions[0].options[0].label, "Rust");
    }

    #[test]
    fn malformed_interaction_preserves_usable_request_id() {
        let RpcFrame::MalformedInteraction(malformed) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1","tool":"bash","args":{},"risk":17}}}"#,
        )
        .unwrap()
        else {
            panic!("wanted malformed interaction")
        };
        assert_eq!(malformed.kind, InteractionKind::Permission);
        assert_eq!(malformed.request_id.as_deref(), Some("perm-1"));
        assert!(malformed.error.contains("invalid permission_request"));
    }

    #[test]
    fn attributed_interactions_decode_to_trusted_requests() {
        let RpcFrame::PermissionRequest(request) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"child-perm","tool":"bash","args":{},"risk":"exec"}},"agent":{"thread_id":"child-1","path":"/root/child","role":"general","depth":1}}"#,
        )
        .unwrap()
        else {
            panic!("wanted attributed permission request")
        };
        let agent = request.agent.expect("agent attribution");
        assert_eq!(agent.path, "/root/child");
        assert_eq!(agent.role, "general");
    }

    #[test]
    fn session_and_model_metadata_preserve_thinking_capabilities() {
        let session: SessionInfo = serde_json::from_value(serde_json::json!({
            "session_id": "s1",
            "name": "Desktop proof",
            "path": "/tmp/session.db",
            "cwd": "/tmp/snow-core",
            "provider": "fake",
            "model": "fake-1",
            "thinking": "high",
            "thinking_levels": ["off", "low", "high"]
        }))
        .unwrap();
        assert_eq!(session.cwd, "/tmp/snow-core");
        assert_eq!(session.thinking, "high");
        assert_eq!(session.thinking_levels, ["off", "low", "high"]);

        let catalog: ModelCatalog = serde_json::from_value(serde_json::json!({
            "provider": "fake",
            "current": "fake-1",
            "models": [{
                "provider": "fake",
                "id": "fake-1",
                "display_name": "Fake One",
                "description": "Privacy: no training use.",
                "context_window": 128000,
                "max_context_window": 200000,
                "max_output_tokens": 16384,
                "supports_tools": true,
                "supports_thinking": true,
                "default_thinking": "low",
                "thinking_levels": ["low", "high"],
                "supports_vision": true,
                "supports_verbosity": true,
                "supports_reasoning_summary": false,
                "upgrade": {"model":"fake-2","message":"New catalog model"},
                "pricing": {
                    "currency":"USD",
                    "input_per_million":1.25,
                    "output_per_million":5.0,
                    "cache_read_per_million":0.25,
                    "cache_write_per_million":1.5
                }
            }]
        }))
        .unwrap();
        let model = &catalog.models[0];
        assert!(model.supports_thinking);
        assert_eq!(model.default_thinking, "low");
        assert_eq!(model.thinking_levels, ["low", "high"]);
        assert_eq!(model.description, "Privacy: no training use.");
        assert_eq!(model.context_window, 128_000);
        assert_eq!(model.max_context_window, 200_000);
        assert_eq!(model.max_output_tokens, 16_384);
        assert!(model.supports_tools);
        assert!(model.supports_vision);
        assert!(model.supports_verbosity);
        assert_eq!(model.supports_reasoning_summary, Some(false));
        assert_eq!(model.upgrade.as_ref().unwrap().model, "fake-2");
        assert_eq!(model.pricing.as_ref().unwrap().input_per_million, 1.25);

        let legacy: ModelInfo = serde_json::from_value(serde_json::json!({
            "provider": "fake",
            "id": "legacy"
        }))
        .unwrap();
        assert_eq!(legacy.description, "");
        assert_eq!(legacy.context_window, 0);
        assert!(!legacy.supports_vision);
        assert!(!legacy.supports_verbosity);
        assert_eq!(legacy.supports_reasoning_summary, None);
        assert_eq!(legacy.upgrade, None);
        assert_eq!(legacy.pricing, None);
    }

    #[test]
    fn branch_catalog_decodes_required_and_optional_metadata() {
        let catalog: BranchCatalog = serde_json::from_value(serde_json::json!({
            "branches": [
                {
                    "id": "main",
                    "tip_id": "entry-1",
                    "messages": 2,
                    "created_at": 10,
                    "updated_at": 11,
                    "active": false
                },
                {
                    "id": "experiment",
                    "name": "Experiment",
                    "parent_branch_id": "main",
                    "forked_from_id": "entry-1",
                    "tip_id": "entry-1",
                    "messages": 2,
                    "preview": "Try another approach",
                    "created_at": 12,
                    "updated_at": 13,
                    "active": true
                }
            ]
        }))
        .unwrap();

        assert_eq!(catalog.branches.len(), 2);
        assert_eq!(catalog.branches[0].name, "");
        assert_eq!(catalog.branches[1].parent_branch_id, "main");
        assert!(catalog.branches[1].active);
        assert!(serde_json::from_value::<BranchCatalog>(serde_json::json!({})).is_err());
    }

    #[test]
    fn paged_history_validates_progress_and_preserves_ordered_tool_pairs() {
        let first = decode_history_page(serde_json::json!({
            "messages": [
                {"id":"assistant","role":"assistant","content":[
                    {"type":"tool_call","tool_call_id":"call-1","name":"read","arguments":{"path":"README.md"}}
                ]}
            ],
            "next_cursor": "opaque",
            "start": 0,
            "total": 2,
            "has_more": true
        }))
        .unwrap();
        assert_eq!(first.start, 0);
        assert_eq!(first.total, 2);
        assert_eq!(first.wire_count, 1);
        assert_eq!(first.next_cursor.as_deref(), Some("opaque"));
        assert!(matches!(
            &first.entries[0].blocks[0],
            HistoryBlock::ToolCall(call) if call.tool_call_id == "call-1"
        ));

        let second = decode_history_page(serde_json::json!({
            "messages": [{
                "id":"result",
                "parent_id":"assistant",
                "role":"tool_result",
                "tool_call_id":"call-1",
                "tool_name":"read",
                "content":[{"type":"text","text":"done"}],
                "tool_display":{"output":"done"}
            }],
            "start": 1,
            "total": 2,
            "has_more": false
        }))
        .unwrap();
        assert_eq!(second.start, 1);
        assert_eq!(second.wire_count, 1);
        assert_eq!(
            second.entries[0]
                .tool_result
                .as_ref()
                .map(|result| result.tool_call_id.as_str()),
            Some("call-1")
        );
    }

    #[test]
    fn paged_history_rejects_non_progressing_and_oversized_pages() {
        for value in [
            serde_json::json!({
                "messages": [],
                "next_cursor": "opaque",
                "start": 0,
                "total": 1,
                "has_more": true
            }),
            serde_json::json!({
                "messages": [{"role":"user","content":[]}],
                "next_cursor": "unexpected",
                "start": 0,
                "total": 1,
                "has_more": false
            }),
            serde_json::json!({
                "messages": [{"role":"user","content":[]}],
                "next_cursor": "x".repeat(MAX_HISTORY_PAGE_CURSOR_BYTES + 1),
                "start": 0,
                "total": 2,
                "has_more": true
            }),
            serde_json::json!({
                "start": 0,
                "total": 0,
                "has_more": false
            }),
        ] {
            assert!(decode_history_page(value).is_err());
        }
        let messages = (0..=MAX_HISTORY_PAGE_MESSAGES)
            .map(|_| serde_json::json!({"role":"user","content":[]}))
            .collect::<Vec<_>>();
        assert!(
            decode_history_page(serde_json::json!({
                "messages": messages,
                "start": 0,
                "total": MAX_HISTORY_PAGE_MESSAGES + 1,
                "has_more": false
            }))
            .is_err()
        );
    }

    #[test]
    fn typed_history_preserves_safe_images_and_tool_cards() {
        let value = serde_json::json!({
            "messages": [
                {"role":"user","content":[
                    {"type":"text","text":"question"},
                    {"type":"image","mime_type":"image/png","data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg=="}
                ]},
                {"role":"assistant","content":[
                    {"type":"thinking","text":"private thought"},
                    {"type":"text","text":"answer"},
                    {"type":"plan","text":"safe plan","plan_complete":true},
                    {"type":"tool_call","tool_call_id":"call-1","name":"read","arguments":{"path":"README.md"}},
                    {"type":"provider_data","data":"opaque"}
                ]},
                {
                    "role":"tool_result",
                    "tool_call_id":"call-1",
                    "tool_name":"read",
                    "is_error":false,
                    "content":[{"type":"text","text":"complete model-facing output"}],
                    "tool_display":{
                        "started":true,
                        "start_message":"README.md",
                        "progress":["Reading"],
                        "output":"bounded preview",
                        "duration_ms":12
                    }
                },
                {"role":"system","content":[{"type":"text","text":"system context"}]},
                {"role":"custom","content":[{"type":"text","text":"compaction checkpoint"}]}
            ]
        });

        let history = decode_history_entries(value.clone()).unwrap();
        assert_eq!(history.len(), 3);
        assert_eq!(history[0].role, "user");
        assert!(matches!(
            &history[0].blocks[1],
            HistoryBlock::Image(HistoryImage { mime_type, data, .. })
                if mime_type == "image/png" && data.len() == 70
        ));
        assert!(history[1].blocks.iter().all(|block| !matches!(
            block,
            HistoryBlock::Text { text } if text.contains("private thought") || text.contains("opaque")
        )));
        assert!(matches!(
            &history[1].blocks[1],
            HistoryBlock::Plan { text, complete: true } if text == "safe plan"
        ));
        assert!(matches!(
            &history[1].blocks[2],
            HistoryBlock::ToolCall(HistoryToolCall { tool_call_id, name, arguments_display })
                if tool_call_id == "call-1"
                    && name == "read"
                    && arguments_display.contains("README.md")
        ));
        let tool = history[2].tool_result.as_ref().unwrap();
        assert_eq!(tool.tool_call_id, "call-1");
        assert_eq!(tool.tool_name, "read");
        assert_eq!(tool.display.output, "bounded preview");
        assert_eq!(tool.display.progress, ["Reading"]);
        assert!(history[2].blocks.is_empty());

        // Existing transcript consumers retain user/assistant text without
        // receiving tool output or provider-private blocks.
        let compatibility = decode_history(value).unwrap();
        assert_eq!(compatibility.len(), 2);
        assert_eq!(compatibility[0].role, "user");
        assert!(compatibility[0].text.contains("question"));
        assert!(compatibility[0].text.contains("70 decoded bytes"));
        assert_eq!(compatibility[1].role, "assistant");
        assert!(compatibility[1].text.contains("answer"));
        assert!(compatibility[1].text.contains("safe plan"));
        assert!(!compatibility[1].text.contains("private thought"));
        assert!(!compatibility[1].text.contains("opaque"));
        assert!(compatibility.iter().all(|message| {
            !message.text.contains("complete model-facing output")
                && !message.text.contains("bounded preview")
        }));
    }

    #[test]
    fn typed_history_rejects_unsafe_or_excessive_images() {
        let invalid = decode_history_entries(serde_json::json!({
            "messages":[{"role":"user","content":[{
                "type":"image","mime_type":"image/svg+xml","data":"PHN2Zz4="
            }]}]
        }))
        .unwrap_err();
        assert!(invalid.to_string().contains("unsupported MIME type"));

        let images = (0..=MAX_HISTORY_IMAGES)
            .map(|_| {
                serde_json::json!({
                    "type":"image",
                    "mime_type":"image/png",
                    "data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg=="
                })
            })
            .collect::<Vec<_>>();
        let excessive = decode_history_entries(serde_json::json!({
            "messages":[{"role":"user","content":images}]
        }))
        .unwrap_err();
        assert!(excessive.to_string().contains("image limit"));

        let malformed = decode_history_entries(serde_json::json!({
            "messages":[{"role":"assistant","content":[{
                "type":"image","mime_type":"image/png","data":"not base64"
            }]}]
        }))
        .unwrap_err();
        assert!(malformed.to_string().contains("invalid base64"));
    }

    #[test]
    fn subagent_messages_request_encodes_all_correlation_fields() {
        let encoded = encode_request(
            &RpcRequest::SubagentMessages {
                id: "request-7".into(),
                params: SubagentMessagesParams {
                    target: "/root/reviewer".into(),
                    cursor: Some("opaque-cursor".into()),
                    limit: 17,
                    max_bytes: 65_536,
                },
            },
            1_048_576,
        )
        .unwrap();
        let value: Value = serde_json::from_slice(&encoded).unwrap();
        assert_eq!(value["type"], "subagent_messages");
        assert_eq!(value["id"], "request-7");
        assert_eq!(value["params"]["target"], "/root/reviewer");
        assert_eq!(value["params"]["cursor"], "opaque-cursor");
        assert_eq!(value["params"]["limit"], 17);
        assert_eq!(value["params"]["max_bytes"], 65_536);
    }

    #[test]
    fn subagent_messages_page_is_typed_bounded_and_private_free() {
        let page = decode_subagent_messages_page(
            serde_json::json!({
                "agent": {
                    "path": "/root/reviewer",
                    "thread_id": "thread-7",
                    "parent_path": "/root",
                    "parent_thread_id": "thread-root"
                },
                "generation": 11,
                "messages": [{
                    "id": "message-1",
                    "parent_id": "message-0",
                    "role": "assistant",
                    "ts": 123,
                    "provider": "private-provider",
                    "model": "private-model",
                    "error": "must not escape",
                    "content": [
                        {"type": "thinking", "text": "private reasoning"},
                        {"type": "text", "text": "public answer"},
                        {"type": "plan", "text": "public plan", "plan_complete": true}
                    ]
                }],
                "next_cursor": "opaque-next",
                "start": 0,
                "total": 2,
                "has_more": true,
                "result": "terminal result must not escape",
                "error": "terminal error must not escape"
            }),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap();

        assert_eq!(page.agent.path, "/root/reviewer");
        assert_eq!(page.agent.thread_id, "thread-7");
        assert_eq!(page.generation, 11);
        assert_eq!(page.next_cursor.as_deref(), Some("opaque-next"));
        assert_eq!(page.start, 0);
        assert_eq!(page.total, 2);
        assert_eq!(page.wire_count, 1);
        assert!(page.has_more);
        assert_eq!(page.messages[0].id, "message-1");
        assert_eq!(page.messages[0].parent_id, "message-0");
        assert_eq!(page.messages[0].timestamp, 123);
        assert_eq!(page.messages[0].blocks.len(), 2);
        let rendered = format!("{page:?}");
        assert!(rendered.contains("public answer"));
        assert!(!rendered.contains("private reasoning"));
        assert!(!rendered.contains("private-provider"));
        assert!(!rendered.contains("private-model"));
        assert!(!rendered.contains("must not escape"));

        let terminal = decode_subagent_messages_page(
            serde_json::json!({
                "agent": {"path": "/root/reviewer", "thread_id": "thread-7"},
                "generation": 11,
                "messages": [{
                    "id": "message-2",
                    "parent_id": "message-1",
                    "role": "user",
                    "ts": 124,
                    "content": [{"type": "text", "text": "follow-up"}]
                }],
                "start": 1,
                "total": 2,
                "has_more": false
            }),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap();
        assert_eq!(terminal.start, 1);
        assert_eq!(terminal.total, 2);
        assert_eq!(terminal.messages[0].parent_id, "message-1");
        assert!(terminal.next_cursor.is_none());
        assert!(!terminal.has_more);
    }

    #[test]
    fn subagent_messages_rejects_private_data_images_and_malformed_paging() {
        let base = |content: Vec<Value>| {
            serde_json::json!({
                "agent": {"path": "/root/child", "thread_id": "thread-child"},
                "generation": 1,
                "messages": [{
                    "id": "message-1",
                    "role": "assistant",
                    "ts": 1,
                    "content": content
                }],
                "start": 0,
                "total": 1,
                "has_more": false
            })
        };

        let private = decode_subagent_messages_page(
            base(vec![serde_json::json!({
                "type": "provider_data",
                "data": "b3BhcXVl"
            })]),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap_err();
        assert!(private.to_string().contains("provider-private"));

        let images = (0..=MAX_SUBAGENT_MESSAGE_IMAGES)
            .map(|_| {
                serde_json::json!({
                    "type": "image",
                    "mime_type": "image/png",
                    "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg=="
                })
            })
            .collect();
        let excessive =
            decode_subagent_messages_page(base(images), MIN_SUBAGENT_MESSAGE_PAGE_BYTES)
                .unwrap_err();
        assert!(excessive.to_string().contains("image page limit"));

        let malformed = decode_subagent_messages_page(
            serde_json::json!({
                "agent": {"path": "/root/child", "thread_id": "thread-child"},
                "generation": 1,
                "messages": [],
                "next_cursor": "stale",
                "start": 1,
                "total": 1,
                "has_more": false
            }),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap_err();
        assert!(malformed.to_string().contains("inconsistent"));

        let oversized_cursor = decode_subagent_messages_page(
            serde_json::json!({
                "agent": {"path": "/root/child", "thread_id": "thread-child"},
                "generation": 1,
                "messages": [{
                    "id": "message-1",
                    "role": "assistant",
                    "ts": 1,
                    "content": [{"type": "text", "text": "public"}]
                }],
                "next_cursor": "x".repeat(MAX_SUBAGENT_MESSAGE_CURSOR_BYTES + 1),
                "start": 0,
                "total": 2,
                "has_more": true
            }),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap_err();
        assert!(oversized_cursor.to_string().contains("cursor exceeds"));
    }

    #[test]
    fn subagent_messages_rejects_correlated_frame_budget_overrun() {
        let error = decode_subagent_messages_page(
            serde_json::json!({
                "agent": {"path": "/root/child", "thread_id": "thread-child"},
                "generation": 1,
                "messages": [{
                    "id": "message-1",
                    "role": "assistant",
                    "ts": 1,
                    "content": [{"type": "text", "text": "x".repeat(20_000)}]
                }],
                "start": 0,
                "total": 1,
                "has_more": false
            }),
            MIN_SUBAGENT_MESSAGE_PAGE_BYTES,
        )
        .unwrap_err();
        assert!(error.to_string().contains("byte budget"));
    }

    #[test]
    fn request_encoding_enforces_limit() {
        let error = encode_request(
            &RpcRequest::Prompt {
                id: "p1".into(),
                message: "too long".into(),
            },
            4,
        )
        .unwrap_err();
        assert!(matches!(error, SnowError::FrameTooLarge { limit: 4 }));
    }

    #[test]
    fn decodes_ready_and_preserves_capabilities() {
        let frame = decode_frame(
            br#"{"type":"rpc_ready","protocol_version":"1","snow_version":"dev","capabilities":["prompt_completion","session_info"],"max_input_bytes":1024}"#,
        )
        .unwrap();
        let RpcFrame::Ready(ready) = frame else {
            panic!("wanted ready")
        };
        assert_eq!(ready.protocol_version, "1");
        assert!(ready.capabilities.contains("prompt_completion"));
        assert_eq!(ready.max_input_bytes, 1024);
    }

    #[test]
    fn decodes_all_prompt_completion_states() {
        for (status, expected) in [
            ("completed", PromptStatus::Completed),
            ("canceled", PromptStatus::Canceled),
        ] {
            let json =
                format!(r#"{{"type":"prompt_completed","request_id":"p1","status":"{status}"}}"#);
            let RpcFrame::PromptCompleted(completed) = decode_frame(json.as_bytes()).unwrap()
            else {
                panic!("wanted completion")
            };
            assert_eq!(completed.status, expected);
        }

        let RpcFrame::PromptCompleted(failed) = decode_frame(
            br#"{"type":"prompt_completed","request_id":"p1","status":"failed","error":"boom"}"#,
        )
        .unwrap() else {
            panic!("wanted completion")
        };
        assert_eq!(failed.status, PromptStatus::Failed);
        assert_eq!(failed.error.as_deref(), Some("boom"));
    }

    #[test]
    fn failed_completion_requires_error() {
        let error =
            decode_frame(br#"{"type":"prompt_completed","request_id":"p1","status":"failed"}"#)
                .unwrap_err();
        assert!(matches!(error, SnowError::Protocol(_)));
    }

    #[test]
    fn unknown_frame_is_retained() {
        let RpcFrame::Unknown(frame) =
            decode_frame(br#"{"type":"future_event","answer":42}"#).unwrap()
        else {
            panic!("wanted unknown")
        };
        assert_eq!(frame.kind, "future_event");
        assert_eq!(frame.fields["answer"], 42);
    }

    #[test]
    fn agent_event_preserves_additive_fields() {
        let RpcFrame::Agent(event) =
            decode_frame(br#"{"type":"text_delta","text":"hi","future":{"enabled":true}}"#)
                .unwrap()
        else {
            panic!("wanted agent event")
        };
        assert_eq!(event.string("text"), Some("hi"));
        assert!(event.fields.contains_key("future"));
    }

    #[test]
    fn malformed_interactions_retain_correlated_ids() {
        let RpcFrame::MalformedInteraction(user_input) =
            decode_frame(br#"{"type":"user_input_request","user_input":{"id":"ask-1"}}"#).unwrap()
        else {
            panic!("wanted malformed user input event")
        };
        assert_eq!(user_input.request_id.as_deref(), Some("ask-1"));

        let RpcFrame::MalformedInteraction(permission) = decode_frame(
            br#"{"type":"permission_request","permission":{"request":{"id":"perm-1"}}}"#,
        )
        .unwrap() else {
            panic!("wanted malformed permission event")
        };
        assert_eq!(permission.request_id.as_deref(), Some("perm-1"));
    }

    #[test]
    fn presentation_requests_encode_canonical_shapes() {
        let themes = encode_request(
            &RpcRequest::ThemesList {
                id: "theme-1".into(),
            },
            4096,
        )
        .unwrap();
        assert_eq!(themes, b"{\"type\":\"themes_list\",\"id\":\"theme-1\"}\n");

        let keys = encode_request(
            &RpcRequest::KeybindingsGet {
                id: "keys-1".into(),
            },
            4096,
        )
        .unwrap();
        assert_eq!(keys, b"{\"type\":\"keybindings_get\",\"id\":\"keys-1\"}\n");

        let update = RpcRequest::KeybindingsUpdate {
            id: "keys-2".into(),
            params: KeybindingsUpdateParams {
                scope: KeybindingScope::Global,
                bindings: BTreeMap::from([("submit".into(), vec!["ctrl+enter".into()])]),
                reset: vec!["newline".into()],
            },
        };
        let value: Value = serde_json::from_slice(&encode_request(&update, 4096).unwrap()).unwrap();
        assert_eq!(value["type"], "keybindings_update");
        assert_eq!(value["params"]["scope"], "global");
        assert_eq!(value["params"]["bindings"]["submit"][0], "ctrl+enter");
        assert_eq!(value["params"]["reset"][0], "newline");

        let settings = encode_request(
            &RpcRequest::SettingsThemeUpdate {
                id: "settings-1".into(),
                params: ThemeSettingsUpdateParams {
                    theme: "frost".into(),
                },
            },
            4096,
        )
        .unwrap();
        let value: Value = serde_json::from_slice(&settings).unwrap();
        assert_eq!(value["type"], "settings_update");
        assert_eq!(value["params"]["theme"], "frost");
    }

    fn valid_theme_catalog() -> Value {
        let colors = serde_json::json!({
            "accent":{"light":"#0969DA","dark":"#58A6FF"},
            "muted":{"light":"7","dark":"8"},
            "foreground":{"light":"#24292F","dark":"#F0F6FC"},
            "warning":{"light":"#9A6700","dark":"#E3B341"},
            "error":{"light":"#CF222E","dark":"#FF7B72"},
            "success":{"light":"#1A7F37","dark":"#7EE787"},
            "separator":{"light":"#8C959F","dark":"#6E7681"}
        });
        serde_json::json!({
            "selected":"default",
            "themes":[
                {"name":"default","display_name":"Snow","scope":"builtin","colors":colors.clone()},
                {"name":"frost","display_name":"Frost","scope":"builtin","colors":colors.clone()},
                {"name":"ember","display_name":"Ember","scope":"builtin","colors":colors.clone()},
                {"name":"aurora","display_name":"Aurora","scope":"builtin","colors":colors}
            ]
        })
    }

    fn valid_keybindings() -> Value {
        Value::Object(Map::from_iter([
            ("project_allowed".into(), Value::Bool(true)),
            (
                "actions".into(),
                Value::Array(
                    KEYBINDING_ACTIONS
                        .iter()
                        .map(|name| {
                            serde_json::json!({
                                "name": name,
                                "global": [],
                                "project": [],
                                "effective": [format!("alt+{}", &name[..1])],
                                "source": "default"
                            })
                        })
                        .collect(),
                ),
            ),
        ]))
    }

    #[test]
    fn auth_sequences_accept_legacy_null_values() {
        let inventory: AuthProviderList = serde_json::from_value(serde_json::json!({
            "providers": [{
                "provider_id": "chatgpt",
                "kinds": null,
                "environment": null,
                "methods": null
            }]
        }))
        .unwrap();
        assert_eq!(inventory.providers.len(), 1);
        assert!(inventory.providers[0].kinds.is_empty());
        assert!(inventory.providers[0].environment.is_empty());
        assert!(inventory.providers[0].methods.is_empty());

        let empty_inventory: AuthProviderList =
            serde_json::from_value(serde_json::json!({"providers": null})).unwrap();
        assert!(empty_inventory.providers.is_empty());

        let login: AuthLoginJob = serde_json::from_value(serde_json::json!({
            "job_id": "auth-1",
            "provider_id": "chatgpt",
            "progress": null
        }))
        .unwrap();
        assert!(login.progress.is_empty());
    }

    #[test]
    fn presentation_decoders_are_bounded_typed_and_private_free() {
        let themes = decode_theme_catalog(valid_theme_catalog()).unwrap();
        assert_eq!(themes.selected, "default");
        assert_eq!(themes.themes.len(), 4);
        assert_eq!(themes.themes[0].colors.muted.light, "7");

        let mut private = valid_theme_catalog();
        private["themes"][0]["path"] = Value::String("/private/theme.yaml".into());
        assert!(
            decode_theme_catalog(private)
                .unwrap_err()
                .to_string()
                .contains("invalid")
        );

        let mut bad_color = valid_theme_catalog();
        bad_color["themes"][0]["colors"]["accent"]["light"] =
            Value::String("file:///secret".into());
        assert!(
            decode_theme_catalog(bad_color)
                .unwrap_err()
                .to_string()
                .contains("color")
        );

        let keys = decode_keybindings(valid_keybindings()).unwrap();
        assert_eq!(keys.actions.len(), KEYBINDING_ACTION_COUNT);
        let mut legacy_nulls = valid_keybindings();
        legacy_nulls["actions"][0]["global"] = Value::Null;
        legacy_nulls["actions"][0]["project"] = Value::Null;
        let legacy_keys = decode_keybindings(legacy_nulls).unwrap();
        assert!(legacy_keys.actions[0].global.is_empty());
        assert!(legacy_keys.actions[0].project.is_empty());
        let mut private = valid_keybindings();
        private["actions"][0]["config_path"] = Value::String("/private/keybindings.yaml".into());
        assert!(
            decode_keybindings(private)
                .unwrap_err()
                .to_string()
                .contains("invalid")
        );

        let oversized = serde_json::json!({"padding":"x".repeat(MAX_PRESENTATION_DATA_BYTES)});
        assert!(
            decode_theme_catalog(oversized)
                .unwrap_err()
                .to_string()
                .contains("exceeds")
        );
    }

    #[test]
    fn keybinding_updates_enforce_scope_actions_and_replace_reset_contract() {
        let valid = KeybindingsUpdateParams {
            scope: KeybindingScope::Global,
            bindings: BTreeMap::from([("submit".into(), vec!["ctrl+enter".into()])]),
            reset: vec!["newline".into()],
        };
        validate_keybindings_update(&valid, false).unwrap();

        let mut project = valid.clone();
        project.scope = KeybindingScope::Project;
        assert!(
            validate_keybindings_update(&project, false)
                .unwrap_err()
                .to_string()
                .contains("trusted")
        );

        let mut overlap = valid;
        overlap.reset.push("submit".into());
        assert!(
            validate_keybindings_update(&overlap, true)
                .unwrap_err()
                .to_string()
                .contains("same action")
        );

        assert!(validate_theme_selection("../private").is_err());
        let settings = decode_settings(serde_json::json!({"theme":"frost"})).unwrap();
        assert_eq!(settings.theme, "frost");
        assert!(
            decode_settings(serde_json::json!({"theme":"frost","provider_data":"opaque"})).is_err()
        );
    }

    #[test]
    fn rejects_non_object_and_invalid_utf8() {
        assert!(matches!(
            decode_frame(br#"["not","an","object"]"#),
            Err(SnowError::Protocol(_))
        ));
        assert!(matches!(decode_frame(&[0xff]), Err(SnowError::InvalidUtf8)));
    }
}
