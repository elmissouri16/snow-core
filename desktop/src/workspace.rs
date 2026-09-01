use std::{
    collections::{HashMap, HashSet, VecDeque},
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::Duration,
};

use gpui::{
    AnyElement, App, ClipboardItem, Context, Corner, Entity, FocusHandle, Image, ImageFormat,
    IntoElement, KeyBinding, ListAlignment, ListOffset, ListState, Render, SharedString,
    Subscription, Task, UniformListScrollHandle, Window, actions, div, img, list, prelude::*, px,
    uniform_list,
};
use gpui_component::{
    ActiveTheme, Disableable, IconName, Selectable, StyledExt,
    button::{Button, ButtonVariants},
    h_flex,
    input::{Input, InputEvent, InputState},
    popover::Popover,
    scroll::ScrollableElement,
    text::TextView,
    v_flex,
};

use crate::{
    appearance::{self, AppearanceState},
    attachments::{ImageAttachment, ImageAttachments},
    commands::{self, CommandAction, LocalCommand, RpcCommand},
    composer_support::{
        CommandCompletion as SlashCompletion, CompletionLimits, MentionDiscovery, MentionLimits,
        PasteLimits, PasteStore, SkillSpec, SlashAction, SlashCommand, SlashKey,
        SlashSelectionState, complete_skills, expand_mention_prompt, match_mentions, mention_query,
        replace_mention_token, replace_skill_token,
    },
    preferences::Appearance,
    presentation_runtime,
    process_live::{
        NextPollDecision, ProcessLiveState, RequestMetadata as ProcessRequestMetadata,
        ResponseDisposition as ProcessResponseDisposition,
    },
    provider_catalog::{
        ProviderCatalogItem, build_provider_catalog, is_user_visible_provider, model_presentation,
        provider_label as provider_catalog_label, search_models, search_provider_catalog,
    },
    semantic_resources::{SemanticResource, SemanticTone, project_rpc_resource},
    snow::{
        AuthLoginJob, AuthProvider, AuthProviderList, GoalSummary, HistoryBlock, HistoryEntry,
        HistoryImage, HistoryToolCall, HistoryToolDisplay, HistoryToolResult, InteractionKind,
        KeybindingScope, Keybindings, KeybindingsUpdateParams, ManagedProcessList,
        ManagedProcessLogs, ModelInfo, PendingInputCounts, PermissionDecision, PermissionRequest,
        PromptCompleted, PromptStatus, RuntimeConfig, RuntimeEvent, SessionBranch, SessionList,
        SessionSummary, Settings, ShutdownTracker, SnowClient, SubagentHistoryMessage,
        SubagentLimits, SubagentList, SubagentMessagesPage, SubagentState, ThemeCatalog,
        UserInputAnswer, UserInputQuestion, UserInputRequest, completion_summary,
        decode_subagent_messages_page,
    },
    subagent_live::{
        ActivityKind as SubagentActivityKind, DetailApply as SubagentDetailApply,
        DetailRequest as SubagentDetailRequest, FleetListSnapshot, ListApply as SubagentListApply,
        ListRequest as SubagentListRequest, SubagentAction, SubagentFleetState,
        VersionedSubagentState,
    },
    telemetry::{ContextTelemetry, ContextTelemetryDto, UsageDto, UsageTelemetry},
};

actions!(
    desktop_picker,
    [
        PickerUp,
        PickerDown,
        PickerDismiss,
        HistoryPrevious,
        HistoryNext,
        ComposerTab,
        ComposerBackTab,
        PasteComposer
    ]
);

pub fn init(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("up", PickerUp, Some("PickerSearch && Input")),
        KeyBinding::new("down", PickerDown, Some("PickerSearch && Input")),
        KeyBinding::new("escape", PickerDismiss, Some("PickerSearch && Input")),
        KeyBinding::new("up", PickerUp, Some("DesktopComposer && Input")),
        KeyBinding::new("down", PickerDown, Some("DesktopComposer && Input")),
        KeyBinding::new("escape", PickerDismiss, Some("DesktopComposer && Input")),
        KeyBinding::new(
            "ctrl-up",
            HistoryPrevious,
            Some("DesktopWorkspace && Input"),
        ),
        KeyBinding::new("ctrl-down", HistoryNext, Some("DesktopWorkspace && Input")),
        KeyBinding::new("tab", ComposerTab, Some("DesktopComposer && Input")),
        KeyBinding::new(
            "shift-tab",
            ComposerBackTab,
            Some("DesktopComposer && Input"),
        ),
        KeyBinding::new("cmd-v", PasteComposer, Some("DesktopWorkspace && Input")),
        KeyBinding::new("ctrl-v", PasteComposer, Some("DesktopWorkspace && Input")),
    ]);
}

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

#[derive(Debug, Clone, PartialEq)]
pub struct ChatMessage {
    pub role: ChatRole,
    pub text: String,
    presentation_text: SharedString,
    pub streaming: bool,
    history_blocks: Vec<HistoryBlock>,
    history_tool_results: Vec<HistoryToolResult>,
    render_id: u64,
}

fn is_tool_only_message(message: &ChatMessage) -> bool {
    message.role == ChatRole::Assistant
        && message.text.is_empty()
        && (!message.history_blocks.is_empty() || !message.history_tool_results.is_empty())
        && message
            .history_blocks
            .iter()
            .all(|block| matches!(block, HistoryBlock::ToolCall(_)))
}

fn tool_activity_run_bounds(messages: &[ChatMessage], index: usize) -> Option<(usize, usize)> {
    messages
        .get(index)
        .filter(|message| is_tool_only_message(message))?;
    let mut start = index;
    while start > 0 && is_tool_only_message(&messages[start - 1]) {
        start -= 1;
    }
    let mut end = index;
    while end + 1 < messages.len() && is_tool_only_message(&messages[end + 1]) {
        end += 1;
    }
    Some((start, end))
}

fn coalesced_tool_activity_message(
    messages: &[ChatMessage],
    start: usize,
    end: usize,
) -> Option<ChatMessage> {
    let mut coalesced = messages.get(start)?.clone();
    coalesced.history_blocks.clear();
    coalesced.history_tool_results.clear();
    coalesced.streaming = false;
    for message in messages.get(start..=end)? {
        if !is_tool_only_message(message) {
            return None;
        }
        coalesced
            .history_blocks
            .extend(message.history_blocks.iter().cloned());
        coalesced
            .history_tool_results
            .extend(message.history_tool_results.iter().cloned());
        coalesced.streaming |= message.streaming;
    }
    Some(coalesced)
}

fn chat_message_copy_text(message: &ChatMessage) -> String {
    if message.history_blocks.is_empty() && message.history_tool_results.is_empty() {
        return message.text.clone();
    }
    let mut parts = Vec::new();
    for block in &message.history_blocks {
        match block {
            HistoryBlock::Text { text } => parts.push(text.clone()),
            HistoryBlock::Plan { text, complete } => parts.push(format!(
                "[Plan · {}]\n{text}",
                if *complete { "Complete" } else { "In progress" }
            )),
            HistoryBlock::Image(image) => parts.push(format!(
                "[Image: {} · {} KiB]",
                image.mime_type,
                image.data.len().div_ceil(1024)
            )),
            HistoryBlock::ToolCall(tool) => {
                let mut text = format!("[Tool: {}]", tool.name);
                if !tool.arguments_display.is_empty() {
                    text.push('\n');
                    text.push_str(&tool.arguments_display);
                }
                parts.push(text);
            }
        }
    }
    for result in &message.history_tool_results {
        let mut text = format!(
            "[Tool result: {} · {}]",
            result.tool_name,
            if result.is_error {
                "Failed"
            } else {
                "Completed"
            }
        );
        if !result.display.output.is_empty() {
            text.push('\n');
            text.push_str(&result.display.output);
        }
        parts.push(text);
    }
    parts.join("\n\n")
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActivePrompt {
    pub request_id: String,
    pub assistant_message_index: usize,
    message_render_ids: Vec<u64>,
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
enum ComposerPicker {
    Provider,
    Model,
    Thinking,
    Permission,
}

const PERMISSION_MODES: [&str; 3] = ["ask", "allow", "deny"];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerSuggestion {
    Slash,
    Mention,
    Skill,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerEnterAction {
    ActivatePicker,
    AcceptSlash,
    AcceptMention,
    AcceptSkill,
    Submit,
}

fn composer_enter_action(
    active_picker: Option<ComposerPicker>,
    suggestion: Option<ComposerSuggestion>,
) -> ComposerEnterAction {
    if matches!(
        active_picker,
        Some(ComposerPicker::Thinking | ComposerPicker::Permission)
    ) {
        return ComposerEnterAction::ActivatePicker;
    }
    match suggestion {
        Some(ComposerSuggestion::Slash) => ComposerEnterAction::AcceptSlash,
        Some(ComposerSuggestion::Mention) => ComposerEnterAction::AcceptMention,
        Some(ComposerSuggestion::Skill) => ComposerEnterAction::AcceptSkill,
        None => ComposerEnterAction::Submit,
    }
}

fn picker_highlight_for_value(values: &[String], current: &str) -> usize {
    values
        .iter()
        .position(|value| value == current)
        .unwrap_or(0)
}

fn sidebar_session_title(name: &str) -> String {
    let name = name.trim();
    if name.is_empty() {
        "New thread".into()
    } else {
        name.into()
    }
}

fn should_follow_transcript(
    previous_message_count: usize,
    previous_scroll: ListOffset,
    transcript_content_changed: bool,
    transcript_reset: bool,
) -> bool {
    transcript_reset
        || (transcript_content_changed && previous_scroll.item_ix >= previous_message_count)
}

fn sync_transcript_list_items(
    list_state: &ListState,
    message_count: usize,
    replace_existing: bool,
) -> bool {
    if !replace_existing && list_state.item_count() == message_count {
        return false;
    }
    list_state.reset(message_count);
    true
}

fn markdown_code_block(language: &str, content: &str) -> String {
    let longest_backtick_run = content
        .split(|character| character != '`')
        .map(str::len)
        .max()
        .unwrap_or(0);
    let fence = "`".repeat(longest_backtick_run.saturating_add(1).max(3));
    format!("{fence}{language}\n{content}\n{fence}")
}

fn tool_card_summary(content: &str) -> String {
    let compact = content.split_whitespace().collect::<Vec<_>>().join(" ");
    bounded_display(&compact, TOOL_CARD_SUMMARY_CHARS)
}

fn compact_tool_duration(duration_ms: i64) -> String {
    let duration_ms = duration_ms.max(0);
    if duration_ms < 1_000 {
        return format!("{duration_ms} ms");
    }
    if duration_ms < 60_000 {
        return format!("{:.1}s", duration_ms as f64 / 1_000.);
    }
    let minutes = duration_ms / 60_000;
    let seconds = duration_ms % 60_000 / 1_000;
    format!("{minutes}m {seconds}s")
}

fn tool_activity_label(
    tool_count: usize,
    completed_count: usize,
    failed_count: usize,
    duration_ms: i64,
) -> String {
    if completed_count < tool_count {
        return if tool_count == 1 {
            "Working…".into()
        } else {
            format!("Working · {tool_count} tools")
        };
    }

    let mut label = if duration_ms > 0 {
        format!("Worked for {}", compact_tool_duration(duration_ms))
    } else if tool_count == 1 {
        "Used 1 tool".into()
    } else {
        format!("Used {tool_count} tools")
    };
    if failed_count > 0 {
        label.push_str(&format!(" · {failed_count} failed"));
    }
    label
}

fn composer_suggestion_priority(
    slash_available: bool,
    mention_available: bool,
    skill_available: bool,
) -> Option<ComposerSuggestion> {
    if slash_available {
        Some(ComposerSuggestion::Slash)
    } else if mention_available {
        Some(ComposerSuggestion::Mention)
    } else if skill_available {
        Some(ComposerSuggestion::Skill)
    } else {
        None
    }
}

fn selected_mention_completion(
    matches: &[String],
    selected: usize,
    backwards: bool,
) -> Option<&str> {
    if backwards {
        return matches.last().map(String::as_str);
    }
    let selected = selected.min(matches.len().checked_sub(1)?);
    matches.get(selected).map(String::as_str)
}

#[derive(Debug, Default, Clone, PartialEq, Eq)]
struct SearchPickerState {
    query: String,
    highlighted: usize,
}

impl SearchPickerState {
    fn set_query(&mut self, query: impl Into<String>) {
        self.query = query.into();
        self.highlighted = 0;
    }

    fn move_highlight(&mut self, delta: isize, result_count: usize) {
        if result_count == 0 {
            self.highlighted = 0;
            return;
        }
        self.highlighted = (self.highlighted as isize + delta)
            .clamp(0, result_count.saturating_sub(1) as isize) as usize;
    }

    fn normalize_highlight(&mut self, result_count: usize) {
        self.highlighted = self.highlighted.min(result_count.saturating_sub(1));
    }
}

#[derive(Debug, Default, Clone, PartialEq, Eq)]
struct SuggestionSelectionState {
    selected: usize,
}

impl SuggestionSelectionState {
    fn reset(&mut self) {
        self.selected = 0;
    }

    fn move_selection(&mut self, delta: isize, result_count: usize) {
        if result_count == 0 {
            self.selected = 0;
            return;
        }
        self.selected = (self.selected as isize + delta)
            .clamp(0, result_count.saturating_sub(1) as isize) as usize;
    }

    fn normalize(&mut self, result_count: usize) {
        self.selected = self.selected.min(result_count.saturating_sub(1));
    }
}

#[derive(Debug, Default)]
struct ComposerPickerState {
    active: Option<ComposerPicker>,
    search: SearchPickerState,
    dismissed_suggestion_input: Option<String>,
}

impl ComposerPickerState {
    /// Apply controlled Popover state. The return value is true only for a new
    /// open transition, which is the contract for resetting and focusing the
    /// shared picker search input. Repeated or stale callbacks are no-ops.
    fn set_open(&mut self, picker: ComposerPicker, open: bool) -> bool {
        if open {
            if self.active == Some(picker) {
                return false;
            }
            self.search.set_query("");
            self.active = Some(picker);
            true
        } else {
            if self.active != Some(picker) {
                return false;
            }
            self.active = None;
            self.search.set_query("");
            false
        }
    }

    fn close(&mut self) {
        self.active = None;
        self.search.set_query("");
    }

    /// Prepare a provider row activation without desynchronizing a controlled
    /// Popover. Activating the current healthy provider is an inert keyboard
    /// action, so both the state owner and rendered popover stay open.
    fn prepare_provider_activation(
        &mut self,
        highlighted_provider: &str,
        active_provider: &str,
        retries_failed_provider: bool,
    ) -> bool {
        if highlighted_provider == active_provider && !retries_failed_provider {
            return false;
        }
        self.close();
        true
    }

    fn note_suggestion_input(&mut self, input: &str) {
        if self
            .dismissed_suggestion_input
            .as_deref()
            .is_some_and(|dismissed| dismissed != input)
        {
            self.dismissed_suggestion_input = None;
        }
    }

    fn dismiss_suggestions_for(&mut self, input: &str) -> bool {
        if self.dismissed_suggestion_input.as_deref() == Some(input) {
            return false;
        }
        self.dismissed_suggestion_input = Some(input.to_owned());
        true
    }

    fn suggestions_allowed_for(&self, input: &str) -> bool {
        self.active.is_none() && self.dismissed_suggestion_input.as_deref() != Some(input)
    }
}

const MAX_USER_INPUT_BYTES: usize = 8 * 1024;
const MAX_MANUAL_MODEL_CHARS: usize = 256;
const SEEN_INTERACTION_LIMIT: usize = 64;
const MAX_COMPOSER_COMPLETIONS: usize = 8;

fn slash_completion_limits() -> CompletionLimits {
    CompletionLimits {
        max_results: MAX_COMPOSER_COMPLETIONS,
        ..CompletionLimits::default()
    }
}

#[derive(Debug, serde::Deserialize)]
struct SkillCatalogWire {
    #[serde(default)]
    skills: Vec<SkillWire>,
}

#[derive(Debug, serde::Deserialize)]
struct SkillWire {
    name: String,
    #[serde(default)]
    description: String,
    #[serde(default)]
    enabled: bool,
}

fn decode_skill_catalog(data: serde_json::Value) -> Result<Vec<SkillSpec>, String> {
    let catalog = serde_json::from_value::<SkillCatalogWire>(data)
        .map_err(|error| format!("invalid skills response: {error}"))?;
    Ok(catalog
        .skills
        .into_iter()
        .take(CompletionLimits::default().max_catalog_items)
        .map(|skill| SkillSpec {
            name: skill.name,
            description: skill.description,
            enabled: skill.enabled,
        })
        .collect())
}

fn slash_command_catalog() -> Vec<SlashCommand> {
    commands::command_catalog()
        .iter()
        .map(|(name, description)| SlashCommand {
            name: (*name).to_owned(),
            description: (*description).to_owned(),
            completion: match commands::command_completion(name) {
                commands::CommandCompletion::Immediate => SlashCompletion::Immediate,
                commands::CommandCompletion::Editable => SlashCompletion::Editable,
            },
        })
        .collect()
}

fn manual_model_id(models: &[ModelInfo], query: &str) -> Option<String> {
    let candidate = query.trim();
    (!candidate.is_empty()
        && candidate.chars().count() <= MAX_MANUAL_MODEL_CHARS
        && !models.iter().any(|model| model.id == candidate))
    .then(|| candidate.to_owned())
}

fn manual_model_row_highlighted(
    discovered_result_count: usize,
    manual_available: bool,
    highlighted: usize,
) -> bool {
    manual_available && highlighted == discovered_result_count
}

fn user_visible_auth_providers(providers: Vec<AuthProvider>) -> Vec<AuthProvider> {
    providers
        .into_iter()
        .filter(|provider| is_user_visible_provider(&provider.provider_id))
        .collect()
}

fn presented_provider_model<'a>(provider: &'a str, model: &'a str) -> (&'a str, &'a str) {
    if is_user_visible_provider(provider) {
        (provider, model)
    } else {
        ("Choose provider", "Choose model")
    }
}

fn bounded_display(value: &str, max_chars: usize) -> String {
    let mut chars = value.chars();
    let bounded: String = chars.by_ref().take(max_chars).collect();
    if chars.next().is_some() {
        format!("{bounded}…")
    } else {
        bounded
    }
}

const MAX_TRANSCRIPT_PUBLIC_CHARS: usize = 32 * 1024;
const TRANSCRIPT_LIST_OVERDRAW: f32 = 640.;
const TOOL_CARD_DETAILS_HEIGHT: f32 = 280.;
const TOOL_CARD_SUMMARY_CHARS: usize = 160;
const MAX_TOOL_PUBLIC_CHARS: usize = 8 * 1024;
const MAX_INPUT_HISTORY_CHARS: usize = 16 * 1024;
const INPUT_HISTORY_LIMIT: usize = 100;
const LIVE_PANEL_POLL_INTERVAL: Duration = Duration::from_secs(2);
const THINKING_HEADING: &str = "**Thinking**\n\n";

fn normalize_public_text(value: &str, max_chars: usize) -> String {
    let normalized = value.replace("\r\n", "\n").replace('\r', "\n");
    let filtered = normalized
        .chars()
        .filter(|character| !character.is_control() || matches!(character, '\n' | '\t'))
        .collect::<String>();
    bounded_display(&filtered, max_chars)
}

fn append_bounded_public(target: &mut String, value: &str, max_chars: usize) {
    let used = target.chars().count();
    if used >= max_chars {
        return;
    }
    let normalized = normalize_public_text(value, max_chars - used);
    target.push_str(&normalized);
}

fn hydrated_input_history(history: &[HistoryEntry]) -> VecDeque<String> {
    let mut newest_inputs = Vec::with_capacity(INPUT_HISTORY_LIMIT);
    for entry in history.iter().rev().filter(|entry| entry.role == "user") {
        let text = entry
            .blocks
            .iter()
            .filter_map(|block| match block {
                HistoryBlock::Text { text } if !text.trim().is_empty() => Some(text.as_str()),
                _ => None,
            })
            .collect::<Vec<_>>()
            .join("\n\n");
        let text = normalize_public_text(text.trim(), MAX_INPUT_HISTORY_CHARS);
        if text.is_empty() {
            continue;
        }
        if newest_inputs
            .last()
            .is_none_or(|newer_input| newer_input != &text)
        {
            newest_inputs.push(text);
            if newest_inputs.len() == INPUT_HISTORY_LIMIT {
                break;
            }
        }
    }
    newest_inputs.reverse();
    newest_inputs.into()
}

fn resource_panel_title(command: &str) -> Option<&'static str> {
    match command {
        "context" => Some("Provider context"),
        "usage" => Some("Usage"),
        "goal_get" | "goal_create" | "goal_edit" | "goal_set" | "goal_pause" | "goal_resume" => {
            Some("Thread goal")
        }
        "diagnostics" => Some("Diagnostics"),
        "debug_status" | "debug_enable" | "debug_disable" | "debug_clear" | "debug_dump" => {
            Some("Debug diagnostics")
        }
        "mcp_servers" => Some("MCP servers"),
        "skills" | "skills_clear" => Some("Agent Skills"),
        "pending_inputs" | "pending_inputs_clear" => Some("Pending input"),
        "permission_mode_get" | "permission_mode_set" => Some("Permission mode"),
        "trust_get" | "trust_set" => Some("Project trust"),
        "settings_get" | "settings_update" => Some("Settings"),
        _ => None,
    }
}

fn bounded_paths(paths: &[String]) -> String {
    let mut preview = paths
        .iter()
        .take(8)
        .map(|path| bounded_display(path, 256))
        .collect::<Vec<_>>()
        .join(", ");
    if paths.len() > 8 {
        preview.push_str(&format!(" … and {} more", paths.len() - 8));
    }
    preview
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PendingInteractionCommand {
    command_id: String,
    command: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct UserInputDraft {
    selected: Option<String>,
    other: String,
    use_other: bool,
}

impl UserInputDraft {
    fn new(question: &UserInputQuestion) -> Self {
        Self {
            selected: None,
            other: String::new(),
            use_other: question.options.is_empty(),
        }
    }

    fn answer(&self) -> Result<String, &'static str> {
        let answer = if self.use_other {
            self.other.trim()
        } else {
            self.selected.as_deref().unwrap_or("").trim()
        };
        if answer.is_empty() {
            return Err("Answer is required");
        }
        if answer.len() > MAX_USER_INPUT_BYTES {
            return Err("Answer must be at most 8 KiB");
        }
        Ok(answer.to_owned())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PermissionInteraction {
    request: PermissionRequest,
    decision: Option<PermissionDecision>,
    pending: Option<PendingInteractionCommand>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct UserInputInteraction {
    request: UserInputRequest,
    question_index: usize,
    drafts: Vec<UserInputDraft>,
    pending: Option<PendingInteractionCommand>,
    validation_error: Option<String>,
}

impl UserInputInteraction {
    fn new(request: UserInputRequest) -> Self {
        let drafts = request.questions.iter().map(UserInputDraft::new).collect();
        Self {
            request,
            question_index: 0,
            drafts,
            pending: None,
            validation_error: None,
        }
    }

    fn question(&self) -> &UserInputQuestion {
        &self.request.questions[self.question_index]
    }

    fn draft(&self) -> &UserInputDraft {
        &self.drafts[self.question_index]
    }

    fn draft_mut(&mut self) -> &mut UserInputDraft {
        &mut self.drafts[self.question_index]
    }

    fn answers(&self) -> Result<Vec<UserInputAnswer>, String> {
        self.request
            .questions
            .iter()
            .zip(&self.drafts)
            .map(|(question, draft)| {
                draft
                    .answer()
                    .map(|answer| UserInputAnswer {
                        question_id: question.id.clone(),
                        answer,
                    })
                    .map_err(|error| format!("{}: {error}", question.header))
            })
            .collect()
    }
}

#[derive(Debug, Clone, PartialEq)]
enum InteractionRequest {
    Permission(PermissionRequest),
    UserInput(UserInputRequest),
}

impl InteractionRequest {
    fn kind(&self) -> InteractionKind {
        match self {
            Self::Permission(_) => InteractionKind::Permission,
            Self::UserInput(_) => InteractionKind::UserInput,
        }
    }

    fn request_id(&self) -> &str {
        match self {
            Self::Permission(request) => &request.id,
            Self::UserInput(request) => &request.id,
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
enum ActiveInteraction {
    Permission(PermissionInteraction),
    UserInput(UserInputInteraction),
}

impl ActiveInteraction {
    fn kind(&self) -> InteractionKind {
        match self {
            Self::Permission(_) => InteractionKind::Permission,
            Self::UserInput(_) => InteractionKind::UserInput,
        }
    }

    fn request_id(&self) -> &str {
        match self {
            Self::Permission(interaction) => &interaction.request.id,
            Self::UserInput(interaction) => &interaction.request.id,
        }
    }

    fn pending(&self) -> Option<&PendingInteractionCommand> {
        match self {
            Self::Permission(interaction) => interaction.pending.as_ref(),
            Self::UserInput(interaction) => interaction.pending.as_ref(),
        }
    }

    fn set_pending(&mut self, pending: Option<PendingInteractionCommand>) {
        match self {
            Self::Permission(interaction) => interaction.pending = pending,
            Self::UserInput(interaction) => interaction.pending = pending,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct InteractionRejection {
    kind: InteractionKind,
    request_id: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerAction {
    Send,
    Stop,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum PendingProcessRequest {
    List(ProcessRequestMetadata),
    Logs(ProcessRequestMetadata),
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum PendingSubagentRequest {
    List(SubagentListRequest),
    Detail(SubagentDetailRequest),
    Messages {
        generation: u64,
        selection: crate::subagent_live::AgentSelection,
        cursor: Option<String>,
    },
}

#[derive(Debug, Default)]
struct SubagentTranscript {
    generation: u64,
    selection: Option<crate::subagent_live::AgentSelection>,
    messages: Vec<SubagentHistoryMessage>,
    next_cursor: Option<String>,
    page_generation: Option<u64>,
    total: usize,
    loading: bool,
    complete: bool,
}

impl SubagentTranscript {
    fn reset(&mut self, generation: u64, selection: crate::subagent_live::AgentSelection) {
        self.generation = generation;
        self.selection = Some(selection);
        self.messages.clear();
        self.next_cursor = None;
        self.page_generation = None;
        self.total = 0;
        self.loading = false;
        self.complete = false;
    }

    fn clear(&mut self) {
        *self = Self::default();
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PendingDesktopCommand {
    name: String,
    refresh_runtime: bool,
    show_result: bool,
    silent: bool,
    input_draft: Option<String>,
    process_request: Option<PendingProcessRequest>,
    subagent_request: Option<PendingSubagentRequest>,
}

fn is_session_transition_command(name: &str) -> bool {
    matches!(name, "session_create" | "session_open")
}

#[derive(Debug, Clone, PartialEq)]
struct CompletedDesktopCommand {
    command: String,
    data: Option<serde_json::Value>,
    show_result: bool,
    silent: bool,
    process_request: Option<PendingProcessRequest>,
    subagent_request: Option<PendingSubagentRequest>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PendingPromptSubmission {
    request_id: String,
    draft: String,
    attachments: ImageAttachments,
    clear_composer_on_admit: bool,
    remember_on_admit: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
struct PlanNudgeScope {
    session_id: String,
    branch_id: String,
}

#[derive(Debug, Default)]
struct PlanNudgeDismissals {
    scopes: HashSet<PlanNudgeScope>,
}

impl PlanNudgeDismissals {
    fn is_dismissed(&self, scope: Option<&PlanNudgeScope>) -> bool {
        scope.is_some_and(|scope| self.scopes.contains(scope))
    }

    fn dismiss(&mut self, scope: Option<PlanNudgeScope>) {
        if let Some(scope) = scope {
            self.scopes.insert(scope);
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum AttachmentSource {
    File(String),
    Clipboard,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ResourcePanel {
    command: String,
    resource: SemanticResource,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
enum AuthEntryMode {
    #[default]
    Login,
    CompatibleProfile,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
enum SettingsSection {
    #[default]
    General,
    Capabilities,
    Appearance,
    Keybindings,
}

impl SettingsSection {
    const ALL: [Self; 4] = [
        Self::General,
        Self::Capabilities,
        Self::Appearance,
        Self::Keybindings,
    ];

    const fn id(self) -> &'static str {
        match self {
            Self::General => "general",
            Self::Capabilities => "capabilities",
            Self::Appearance => "appearance",
            Self::Keybindings => "keybindings",
        }
    }

    const fn label(self) -> &'static str {
        match self {
            Self::General => "General",
            Self::Capabilities => "Capabilities",
            Self::Appearance => "Appearance",
            Self::Keybindings => "Keybindings",
        }
    }

    const fn description(self) -> &'static str {
        match self {
            Self::General => "Choose how Snow responds and handles this session.",
            Self::Capabilities => "Configure optional agent features and diagnostics.",
            Self::Appearance => "Set Snow's colors and native window appearance.",
            Self::Keybindings => "Customize semantic shortcuts globally or for this project.",
        }
    }
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

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum WorkspaceCanvasLayout {
    CenteredEmpty,
    Active,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SettingsWorkspaceLayout {
    Hidden,
    Loading,
    Ready,
}

fn settings_workspace_layout(open: bool, settings_loaded: bool) -> SettingsWorkspaceLayout {
    match (open, settings_loaded) {
        (false, _) => SettingsWorkspaceLayout::Hidden,
        (true, false) => SettingsWorkspaceLayout::Loading,
        (true, true) => SettingsWorkspaceLayout::Ready,
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
struct ManagementPanelState {
    sessions: bool,
    processes: bool,
    subagents: bool,
    resource: bool,
    auth: bool,
    settings: bool,
}

fn management_panels_for_interaction(
    active_interaction: bool,
    requested: ManagementPanelState,
) -> ManagementPanelState {
    if active_interaction {
        ManagementPanelState::default()
    } else {
        requested
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ComposerFooterLayout {
    Inline,
    Wrapped,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum WorkspaceTopBarLayout {
    Full,
    Compact,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum PickerCloseFocusTarget {
    Composer,
    Settings,
    None,
}

fn picker_close_focus_target(
    settings_workspace_open: bool,
    composer_editable: bool,
) -> PickerCloseFocusTarget {
    if settings_workspace_open {
        PickerCloseFocusTarget::Settings
    } else if composer_editable {
        PickerCloseFocusTarget::Composer
    } else {
        PickerCloseFocusTarget::None
    }
}

const EXPANDED_SIDEBAR_WIDTH: f32 = 296.;
const COLLAPSED_SIDEBAR_WIDTH: f32 = 58.;
const COMPOSER_HORIZONTAL_INSET: f32 = 80.;
const COMPOSER_INLINE_FOOTER_MIN_WIDTH: f32 = 840.;
const TOP_BAR_FULL_MIN_WIDTH: f32 = 900.;

fn workspace_top_bar_layout(
    workspace_width: f32,
    sidebar_collapsed: bool,
) -> WorkspaceTopBarLayout {
    let sidebar_width = if sidebar_collapsed {
        COLLAPSED_SIDEBAR_WIDTH
    } else {
        EXPANDED_SIDEBAR_WIDTH
    };
    if workspace_width - sidebar_width < TOP_BAR_FULL_MIN_WIDTH {
        WorkspaceTopBarLayout::Compact
    } else {
        WorkspaceTopBarLayout::Full
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct ComposerLayoutProjection {
    footer: ComposerFooterLayout,
    persistent_rows: usize,
}

fn composer_layout_projection(
    workspace_width: f32,
    sidebar_collapsed: bool,
    has_error: bool,
    has_activity: bool,
) -> ComposerLayoutProjection {
    let sidebar_width = if sidebar_collapsed {
        COLLAPSED_SIDEBAR_WIDTH
    } else {
        EXPANDED_SIDEBAR_WIDTH
    };
    let composer_width = (workspace_width - sidebar_width - COMPOSER_HORIZONTAL_INSET).max(0.);
    let footer = if composer_width < COMPOSER_INLINE_FOOTER_MIN_WIDTH {
        ComposerFooterLayout::Wrapped
    } else {
        ComposerFooterLayout::Inline
    };
    ComposerLayoutProjection {
        footer,
        persistent_rows: usize::from(has_error) + usize::from(has_activity),
    }
}

fn composer_footer_layout(workspace_width: f32, sidebar_collapsed: bool) -> ComposerFooterLayout {
    composer_layout_projection(workspace_width, sidebar_collapsed, false, false).footer
}

fn can_open_model_picker(provider: &str, can_send: bool) -> bool {
    can_send && is_user_visible_provider(provider)
}

fn provider_picker_empty_message(
    catalog_count: usize,
    result_count: usize,
    query: &str,
) -> Option<&'static str> {
    if result_count > 0 {
        None
    } else if catalog_count == 0 || query.trim().is_empty() {
        Some("No providers available.")
    } else {
        Some("No providers match your search.")
    }
}

fn composer_model_label(provider: &str, current_model: &str, models: &[ModelInfo]) -> String {
    if !is_user_visible_provider(provider) {
        return "Choose model".into();
    }
    models
        .iter()
        .find(|model| model.id == current_model)
        .map(|model| {
            if model.display_name.is_empty() {
                model.id.clone()
            } else {
                model.display_name.clone()
            }
        })
        .filter(|model| !model.is_empty())
        .unwrap_or_else(|| {
            if current_model.is_empty() {
                "Loading model…".into()
            } else {
                current_model.to_owned()
            }
        })
}

fn workspace_canvas_layout(
    message_count: usize,
    has_blocking_surface: bool,
    has_open_panel: bool,
) -> WorkspaceCanvasLayout {
    if message_count == 0 && !has_blocking_surface && !has_open_panel {
        WorkspaceCanvasLayout::CenteredEmpty
    } else {
        WorkspaceCanvasLayout::Active
    }
}

fn apply_session_inventory(
    sessions: &mut Vec<SessionSummary>,
    sessions_panel_open: &mut bool,
    session_menu_open: &mut bool,
    list: SessionList,
    silent: bool,
) {
    *sessions = list.sessions;
    if !silent {
        *sessions_panel_open = true;
        *session_menu_open = false;
    }
}

fn session_inventory_command_after_confirmed_metadata_mutation(
    state: &ChatState,
    event: &RuntimeEvent,
) -> Option<RpcCommand> {
    let RuntimeEvent::SessionRenamed {
        request_id,
        session_id,
        ..
    } = event
    else {
        return None;
    };
    let correlated = matches!(
        state.session_action_pending.as_ref(),
        Some(PendingSessionAction::Rename {
            request_id: pending_id,
        }) if pending_id == request_id
    );
    if !correlated || state.session_id != *session_id {
        return None;
    }
    Some(RpcCommand {
        name: "sessions_list".into(),
        fields: serde_json::Map::new(),
        refresh_runtime: false,
    })
}

fn project_name(path: &str) -> Option<String> {
    std::path::Path::new(path)
        .file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .map(str::to_owned)
}

fn appearance_label(appearance: Appearance) -> &'static str {
    match appearance {
        Appearance::System => "System",
        Appearance::Light => "Light",
        Appearance::Dark => "Dark",
    }
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
    pub reasoning_summary: String,
    pub text_verbosity: String,
    pub collaboration_mode: String,
    pub goal: Option<GoalSummary>,
    pub subagent_limits: SubagentLimits,
    pub pending_inputs: PendingInputCounts,
    pub permission_mode: String,
    pub project_cwd: String,
    pub session_id: String,
    pub session_name: String,
    pub branches: Vec<SessionBranch>,
    next_render_id: u64,
    restored_tool_calls: HashMap<String, Vec<usize>>,
    pub runtime_loaded: bool,
    runtime_generation: Option<String>,
    session_metadata_stale: bool,
    session_loaded: bool,
    history_loaded: bool,
    history_next_start: usize,
    history_total: Option<usize>,
    models_loaded: bool,
    branches_loaded: bool,
    model_change_pending: Option<PendingModelChange>,
    thinking_change_pending: Option<(String, String)>,
    session_action_pending: Option<PendingSessionAction>,
    active_interaction: Option<ActiveInteraction>,
    queued_interaction: Option<InteractionRequest>,
    seen_interactions: VecDeque<(InteractionKind, String)>,
    interaction_rejections: Vec<InteractionRejection>,
    latest_plan: String,
    plan_received_this_turn: bool,
    plan_review_ready: bool,
    pub status_text: String,
    pub last_error: Option<String>,
}

fn image_format_for_mime(mime_type: &str) -> Option<ImageFormat> {
    match mime_type {
        "image/png" => Some(ImageFormat::Png),
        "image/jpeg" => Some(ImageFormat::Jpeg),
        "image/gif" => Some(ImageFormat::Gif),
        "image/webp" => Some(ImageFormat::Webp),
        _ => None,
    }
}

const MAX_RUNTIME_EVENTS_PER_BATCH: usize = 64;

fn push_coalesced(batch: &mut Vec<RuntimeEvent>, event: RuntimeEvent) {
    match event {
        RuntimeEvent::TextDelta { text } => {
            if let Some(RuntimeEvent::TextDelta { text: pending }) = batch.last_mut() {
                pending.push_str(&text);
            } else {
                batch.push(RuntimeEvent::TextDelta { text });
            }
        }
        RuntimeEvent::PlanDelta { text } => {
            if let Some(RuntimeEvent::PlanDelta { text: pending }) = batch.last_mut() {
                pending.push_str(&text);
            } else {
                batch.push(RuntimeEvent::PlanDelta { text });
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

fn receive_runtime_batch(
    events: &flume::Receiver<RuntimeEvent>,
    first_event: RuntimeEvent,
) -> Vec<RuntimeEvent> {
    let mut batch = Vec::with_capacity(16);
    let mut history_page_in_batch = matches!(&first_event, RuntimeEvent::HistoryPageLoaded { .. });
    let mut received_event_count = 1;
    push_coalesced(&mut batch, first_event);
    while received_event_count < MAX_RUNTIME_EVENTS_PER_BATCH && !history_page_in_batch {
        let Ok(event) = events.try_recv() else {
            break;
        };
        received_event_count += 1;
        history_page_in_batch = matches!(&event, RuntimeEvent::HistoryPageLoaded { .. });
        push_coalesced(&mut batch, event);
    }
    batch
}

fn apply_runtime_batch(
    state: &mut ChatState,
    batch: Vec<RuntimeEvent>,
    presented_command_ids: &HashSet<String>,
) {
    for event in batch {
        if let RuntimeEvent::CommandCompleted {
            request_id,
            command,
            ..
        } = &event
            && presented_command_ids.contains(request_id)
        {
            state.status_text = format!("{command} completed");
            state.last_error = None;
        }
        state.apply(event);
    }
}

fn runtime_event_generation(event: &RuntimeEvent) -> Option<&str> {
    match event {
        RuntimeEvent::ModelsLoaded { generation, .. }
        | RuntimeEvent::SessionLoaded { generation, .. }
        | RuntimeEvent::HistoryLoaded { generation, .. }
        | RuntimeEvent::HistoryPageLoaded { generation, .. }
        | RuntimeEvent::BranchesLoaded { generation, .. }
        | RuntimeEvent::RuntimeStateFailed { generation, .. } => Some(generation),
        _ => None,
    }
}

fn should_stop_process_poll(disposition: ProcessResponseDisposition, terminal_eof: bool) -> bool {
    disposition == ProcessResponseDisposition::Applied && terminal_eof
}

fn tool_transcript_rows(state: &ChatState, batch: &[RuntimeEvent]) -> Vec<usize> {
    let call_ids = batch
        .iter()
        .filter_map(|event| match event {
            RuntimeEvent::ToolProgress { call_id, .. }
            | RuntimeEvent::ToolFinished { call_id, .. } => Some(call_id.as_str()),
            _ => None,
        })
        .collect::<HashSet<_>>();
    let mut rows = call_ids
        .into_iter()
        .filter_map(|call_id| {
            let row = state.messages.iter().rposition(|message| {
                message.history_blocks.iter().any(|block| {
                    matches!(
                        block,
                        HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id
                    )
                })
            })?;
            Some(tool_activity_run_bounds(&state.messages, row).map_or(row, |(_, run_end)| run_end))
        })
        .collect::<Vec<_>>();
    rows.sort_unstable();
    rows.dedup();
    rows
}

fn runtime_event_changes_transcript_content(event: &RuntimeEvent) -> bool {
    matches!(
        event,
        RuntimeEvent::HistoryPageLoaded { .. }
            | RuntimeEvent::TextDelta { .. }
            | RuntimeEvent::PlanDelta { .. }
            | RuntimeEvent::ThinkingDelta { .. }
            | RuntimeEvent::ToolStarted { .. }
            | RuntimeEvent::ToolProgress { .. }
            | RuntimeEvent::ToolFinished { .. }
            | RuntimeEvent::PromptCompleted(_)
            | RuntimeEvent::Exited { .. }
    )
}

fn apply_runtime_config_event(config: &mut RuntimeConfig, event: &RuntimeEvent) {
    match event {
        RuntimeEvent::SessionLoaded { info, .. } => {
            if is_user_visible_provider(&info.provider) {
                config.provider = info.provider.trim().to_owned();
            }
            config.session_path = (!info.path.is_empty()).then(|| info.path.clone().into());
            config.no_session = info.path.is_empty();
            config.model = Some(info.model.clone());
        }
        RuntimeEvent::ModelsLoaded { catalog, .. } => {
            if is_user_visible_provider(&catalog.provider) {
                config.provider = catalog.provider.trim().to_owned();
            }
            if !catalog.current.is_empty() {
                config.model = Some(catalog.current.clone());
            }
        }
        _ => {}
    }
}

fn replacement_provider_config(config: &RuntimeConfig, provider: &str) -> RuntimeConfig {
    let mut replacement = config.clone();
    replacement.provider = provider.into();
    replacement.model = None;
    // Thinking capabilities belong to a concrete provider/model pair. A value
    // retained from the previous provider can make Snow reject the replacement
    // runtime before its model catalog is available for negotiation.
    replacement.thinking = Some("off".into());
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
            reasoning_summary: String::new(),
            text_verbosity: String::new(),
            collaboration_mode: "default".into(),
            goal: None,
            subagent_limits: SubagentLimits::default(),
            pending_inputs: PendingInputCounts::default(),
            permission_mode: String::new(),
            project_cwd: String::new(),
            session_id: String::new(),
            session_name: String::new(),
            branches: Vec::new(),
            next_render_id: 1,
            restored_tool_calls: HashMap::new(),
            runtime_loaded: false,
            runtime_generation: None,
            session_metadata_stale: false,
            session_loaded: false,
            history_loaded: false,
            history_next_start: 0,
            history_total: None,
            models_loaded: false,
            branches_loaded: false,
            model_change_pending: None,
            thinking_change_pending: None,
            session_action_pending: None,
            active_interaction: None,
            queued_interaction: None,
            seen_interactions: VecDeque::new(),
            interaction_rejections: Vec::new(),
            latest_plan: String::new(),
            plan_received_this_turn: false,
            plan_review_ready: false,
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

    fn accepts_history_page(
        &self,
        generation: &str,
        start: usize,
        next_start: usize,
        total: usize,
        complete: bool,
    ) -> bool {
        self.accepts_runtime_generation(generation)
            && self.history_page_bounds_valid(start, next_start, total, complete)
    }

    fn history_page_bounds_valid(
        &self,
        start: usize,
        next_start: usize,
        total: usize,
        complete: bool,
    ) -> bool {
        start == self.history_next_start
            && self.history_total.is_none_or(|expected| expected == total)
            && next_start >= start
            && next_start <= total
            && complete == (next_start == total)
    }

    fn reset_runtime_load(&mut self) {
        self.runtime_loaded = false;
        self.session_loaded = false;
        self.history_loaded = false;
        self.history_next_start = 0;
        self.history_total = None;
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

    pub fn can_edit_composer(&self) -> bool {
        matches!(self.connection, ConnectionState::Ready { .. })
            && self.active_interaction.is_none()
    }

    pub fn can_send(&self) -> bool {
        self.can_edit_composer()
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
            && self.active_interaction.is_none()
            && self.queued_interaction.is_none()
            && (matches!(
                self.connection,
                ConnectionState::Failed(_) | ConnectionState::Stopped
            ) || (self.runtime_loaded
                && matches!(self.connection, ConnectionState::Ready { .. })))
    }

    pub fn can_switch_model(&self) -> bool {
        self.can_send()
    }

    fn can_select_model(&self, model: &str) -> bool {
        let model = model.trim();
        self.can_switch_model()
            && !model.is_empty()
            && model.chars().count() <= MAX_MANUAL_MODEL_CHARS
            && model != self.current_model
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

    fn mark_interaction_seen(&mut self, kind: InteractionKind, request_id: &str) {
        self.seen_interactions
            .push_back((kind, request_id.to_owned()));
        while self.seen_interactions.len() > SEEN_INTERACTION_LIMIT {
            self.seen_interactions.pop_front();
        }
    }

    fn has_seen_interaction(&self, kind: InteractionKind, request_id: &str) -> bool {
        self.seen_interactions
            .iter()
            .any(|(seen_kind, seen_id)| *seen_kind == kind && seen_id == request_id)
    }

    fn activate_interaction(request: InteractionRequest) -> ActiveInteraction {
        match request {
            InteractionRequest::Permission(request) => {
                ActiveInteraction::Permission(PermissionInteraction {
                    request,
                    decision: None,
                    pending: None,
                })
            }
            InteractionRequest::UserInput(request) => {
                ActiveInteraction::UserInput(UserInputInteraction::new(request))
            }
        }
    }

    fn enqueue_interaction(&mut self, request: InteractionRequest) {
        let kind = request.kind();
        let request_id = request.request_id().to_owned();
        if self.has_seen_interaction(kind, &request_id) {
            return;
        }
        self.mark_interaction_seen(kind, &request_id);
        if self.active_interaction.is_none() {
            self.active_interaction = Some(Self::activate_interaction(request));
        } else if self.queued_interaction.is_none() {
            self.queued_interaction = Some(request);
        } else {
            self.interaction_rejections
                .push(InteractionRejection { kind, request_id });
            self.last_error = Some(
                "Snow sent more blocking interactions than the desktop can safely display; the newest request was declined."
                    .into(),
            );
        }
        self.status_text = "Waiting for your response…".into();
    }

    fn promote_queued_interaction(&mut self) {
        self.active_interaction = self
            .queued_interaction
            .take()
            .map(Self::activate_interaction);
        self.status_text = if self.active_interaction.is_some() {
            "Waiting for your response…".into()
        } else if self.active_prompt.is_some() {
            "Thinking…".into()
        } else {
            "Ready".into()
        };
    }

    fn clear_interactions(&mut self) {
        self.active_interaction = None;
        self.queued_interaction = None;
        self.interaction_rejections.clear();
    }

    fn take_interaction_rejections(&mut self) -> Vec<InteractionRejection> {
        std::mem::take(&mut self.interaction_rejections)
    }

    fn begin_interaction_command(
        &mut self,
        request_id: &str,
        command_id: String,
        command: &str,
    ) -> bool {
        let Some(active) = self.active_interaction.as_mut() else {
            return false;
        };
        if active.request_id() != request_id || active.pending().is_some() {
            return false;
        }
        active.set_pending(Some(PendingInteractionCommand {
            command_id,
            command: command.to_owned(),
        }));
        self.status_text = "Waiting for Snow to confirm…".into();
        self.last_error = None;
        true
    }

    fn can_select_permission_decision(&self, request_id: &str) -> bool {
        matches!(
            &self.active_interaction,
            Some(ActiveInteraction::Permission(interaction))
                if interaction.request.id == request_id && interaction.pending.is_none()
        )
    }

    fn begin_permission_command(
        &mut self,
        request_id: &str,
        decision: PermissionDecision,
        command_id: String,
    ) -> bool {
        if !self.can_select_permission_decision(request_id) {
            return false;
        }
        if let Some(ActiveInteraction::Permission(interaction)) = self.active_interaction.as_mut() {
            interaction.decision = Some(decision);
        }
        self.begin_interaction_command(request_id, command_id, "permission_reply")
    }

    fn current_user_input(&self) -> Option<&UserInputInteraction> {
        match &self.active_interaction {
            Some(ActiveInteraction::UserInput(interaction)) => Some(interaction),
            _ => None,
        }
    }

    fn current_user_input_mut(&mut self) -> Option<&mut UserInputInteraction> {
        match &mut self.active_interaction {
            Some(ActiveInteraction::UserInput(interaction)) => Some(interaction),
            _ => None,
        }
    }

    fn select_user_input_option(&mut self, label: &str) {
        let Some(interaction) = self.current_user_input_mut() else {
            return;
        };
        if interaction.pending.is_some()
            || !interaction
                .question()
                .options
                .iter()
                .any(|option| option.label == label)
        {
            return;
        }
        let draft = interaction.draft_mut();
        draft.selected = Some(label.to_owned());
        draft.use_other = false;
        interaction.validation_error = None;
    }

    fn select_user_input_other(&mut self) {
        let Some(interaction) = self.current_user_input_mut() else {
            return;
        };
        if interaction.pending.is_none() {
            interaction.draft_mut().use_other = true;
            interaction.validation_error = None;
        }
    }

    fn set_user_input_draft(&mut self, value: impl Into<String>) {
        let Some(interaction) = self.current_user_input_mut() else {
            return;
        };
        if interaction.pending.is_none() {
            let draft = interaction.draft_mut();
            draft.other = value.into();
            draft.use_other = true;
            interaction.validation_error = None;
        }
    }

    fn move_user_input_question(&mut self, delta: isize) -> bool {
        let Some(interaction) = self.current_user_input_mut() else {
            return false;
        };
        if interaction.pending.is_some() {
            return false;
        }
        let next = interaction.question_index as isize + delta;
        if !(0..interaction.request.questions.len() as isize).contains(&next) {
            return false;
        }
        interaction.question_index = next as usize;
        interaction.validation_error = None;
        true
    }

    fn user_input_answers(&mut self) -> Option<(String, Vec<UserInputAnswer>)> {
        let interaction = self.current_user_input_mut()?;
        if interaction.pending.is_some() {
            return None;
        }
        match interaction.answers() {
            Ok(answers) => Some((interaction.request.id.clone(), answers)),
            Err(error) => {
                interaction.validation_error = Some(error);
                None
            }
        }
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
        self.clear_interactions();
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
        self.clear_interactions();
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

    fn push_system_message(&mut self, text: impl Into<String>) {
        let render_id = self.allocate_render_id();
        let text = text.into();
        self.messages.push(ChatMessage {
            role: ChatRole::System,
            presentation_text: text.clone().into(),
            text,
            streaming: false,
            history_blocks: Vec::new(),
            history_tool_results: Vec::new(),
            render_id,
        });
    }

    pub fn begin_prompt(&mut self, request_id: String, message: String) {
        self.begin_prompt_with_blocks(request_id, message, Vec::new());
    }

    fn begin_prompt_with_blocks(
        &mut self,
        request_id: String,
        message: String,
        history_blocks: Vec<HistoryBlock>,
    ) {
        self.clear_interactions();
        self.latest_plan.clear();
        self.plan_received_this_turn = false;
        self.plan_review_ready = false;
        let user_render_id = self.allocate_render_id();
        self.messages.push(ChatMessage {
            role: ChatRole::User,
            presentation_text: message.clone().into(),
            text: message,
            streaming: false,
            history_blocks,
            history_tool_results: Vec::new(),
            render_id: user_render_id,
        });
        let assistant_message_index = self.messages.len();
        let assistant_render_id = self.allocate_render_id();
        self.messages.push(ChatMessage {
            role: ChatRole::Assistant,
            text: String::new(),
            presentation_text: "".into(),
            streaming: true,
            history_blocks: Vec::new(),
            history_tool_results: Vec::new(),
            render_id: assistant_render_id,
        });
        self.active_prompt = Some(ActivePrompt {
            request_id,
            assistant_message_index,
            message_render_ids: vec![user_render_id, assistant_render_id],
            admitted: false,
            abort_pending: false,
        });
        self.status_text = "Sending…".into();
        self.last_error = None;
    }

    fn push_active_assistant_segment(
        &mut self,
        history_blocks: Vec<HistoryBlock>,
        history_tool_result: Option<HistoryToolResult>,
        streaming: bool,
    ) -> Option<usize> {
        self.active_prompt.as_ref()?;
        let render_id = self.allocate_render_id();
        let index = self.messages.len();
        self.messages.push(ChatMessage {
            role: ChatRole::Assistant,
            text: String::new(),
            presentation_text: "".into(),
            streaming,
            history_blocks,
            history_tool_results: history_tool_result.into_iter().collect(),
            render_id,
        });
        if let Some(active) = self.active_prompt.as_mut() {
            active.assistant_message_index = index;
            active.message_render_ids.push(render_id);
        }
        Some(index)
    }

    fn ensure_active_text_segment(&mut self) -> Option<usize> {
        let index = self.active_prompt.as_ref()?.assistant_message_index;
        let needs_segment = self.messages.get(index).is_some_and(|message| {
            !message.history_blocks.is_empty() || !message.history_tool_results.is_empty()
        });
        if needs_segment {
            if let Some(message) = self.messages.get_mut(index) {
                message.streaming = false;
            }
            self.push_active_assistant_segment(Vec::new(), None, true)
        } else {
            Some(index)
        }
    }

    fn append_thinking_transcript(&mut self, text: &str) {
        let text = normalize_public_text(text, MAX_TRANSCRIPT_PUBLIC_CHARS);
        if text.is_empty() {
            return;
        }
        let Some(current_index) = self
            .active_prompt
            .as_ref()
            .map(|active| active.assistant_message_index)
        else {
            return;
        };
        let current_is_thinking = self.messages.get(current_index).is_some_and(|message| {
            message.text.is_empty()
                && message.history_tool_results.is_empty()
                && matches!(
                    message.history_blocks.as_slice(),
                    [HistoryBlock::Text { text }] if text.starts_with(THINKING_HEADING)
                )
        });
        let target_index = if current_is_thinking {
            current_index
        } else {
            let current_is_empty = self.messages.get(current_index).is_some_and(|message| {
                message.text.is_empty()
                    && message.history_blocks.is_empty()
                    && message.history_tool_results.is_empty()
            });
            if current_is_empty {
                current_index
            } else {
                if let Some(message) = self.messages.get_mut(current_index) {
                    message.streaming = false;
                }
                let Some(index) = self.push_active_assistant_segment(Vec::new(), None, true) else {
                    return;
                };
                index
            }
        };
        let Some(message) = self.messages.get_mut(target_index) else {
            return;
        };
        if message.history_blocks.is_empty() {
            message.history_blocks.push(HistoryBlock::Text {
                text: THINKING_HEADING.into(),
            });
        }
        if let Some(HistoryBlock::Text { text: target }) = message.history_blocks.first_mut() {
            append_bounded_public(target, &text, MAX_TRANSCRIPT_PUBLIC_CHARS);
        }
    }

    fn begin_tool_transcript(&mut self, call_id: &str, name: &str) {
        let call_id = normalize_public_text(call_id, MAX_TOOL_PUBLIC_CHARS);
        let name = normalize_public_text(name, MAX_TOOL_PUBLIC_CHARS);
        let Some(current_index) = self
            .active_prompt
            .as_ref()
            .map(|active| active.assistant_message_index)
        else {
            return;
        };
        let current_is_empty = self.messages.get(current_index).is_some_and(|message| {
            message.text.is_empty()
                && message.history_blocks.is_empty()
                && message.history_tool_results.is_empty()
        });
        let card = HistoryBlock::ToolCall(HistoryToolCall {
            tool_call_id: call_id,
            name,
            arguments_display: "Running".into(),
        });
        if current_is_empty {
            if let Some(message) = self.messages.get_mut(current_index) {
                message.history_blocks.push(card);
                message.streaming = false;
            }
        } else {
            if let Some(message) = self.messages.get_mut(current_index) {
                message.streaming = false;
            }
            self.push_active_assistant_segment(vec![card], None, false);
        }
        self.push_active_assistant_segment(Vec::new(), None, true);
    }

    fn update_tool_transcript(&mut self, call_id: &str, progress: Option<&str>) {
        let Some(message) = self.messages.iter_mut().rev().find(|message| {
            message.history_blocks.iter().any(|block| {
                matches!(block, HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id)
            })
        }) else {
            return;
        };
        let Some(HistoryBlock::ToolCall(tool)) = message.history_blocks.iter_mut().find(
            |block| matches!(block, HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id),
        ) else {
            return;
        };
        tool.arguments_display = progress
            .map(|progress| {
                let progress = normalize_public_text(progress, MAX_TOOL_PUBLIC_CHARS);
                if progress.is_empty() {
                    "Running".into()
                } else {
                    format!("Running\n{progress}")
                }
            })
            .unwrap_or_else(|| "Running".into());
    }

    fn finish_tool_transcript(
        &mut self,
        call_id: &str,
        name: &str,
        is_error: bool,
        preview: Option<&str>,
    ) {
        let Some(message) = self.messages.iter_mut().rev().find(|message| {
            message.history_blocks.iter().any(|block| {
                matches!(block, HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id)
            })
        }) else {
            return;
        };
        let progress = message
            .history_blocks
            .iter()
            .find_map(|block| match block {
                HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id => tool
                    .arguments_display
                    .strip_prefix("Running\n")
                    .filter(|progress| !progress.is_empty())
                    .map(|progress| vec![progress.to_owned()]),
                _ => None,
            })
            .unwrap_or_default();
        let result = HistoryToolResult {
            tool_call_id: normalize_public_text(call_id, MAX_TOOL_PUBLIC_CHARS),
            tool_name: normalize_public_text(name, MAX_TOOL_PUBLIC_CHARS),
            is_error,
            display: HistoryToolDisplay {
                started: true,
                start_message: String::new(),
                progress,
                output: preview
                    .map(|preview| normalize_public_text(preview, MAX_TOOL_PUBLIC_CHARS))
                    .unwrap_or_default(),
                duration_ms: 0,
            },
        };
        if let Some(existing) = message
            .history_tool_results
            .iter_mut()
            .find(|existing| existing.tool_call_id == result.tool_call_id)
        {
            *existing = result;
        } else {
            message.history_tool_results.push(result);
        }
    }

    pub fn apply(&mut self, event: RuntimeEvent) {
        match event {
            RuntimeEvent::Ready(ready) => {
                self.connection = ConnectionState::Ready {
                    snow_version: ready.snow_version,
                };
                self.reset_runtime_load();
                self.clear_interactions();
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
                self.reasoning_summary = info.reasoning_summary;
                self.text_verbosity = info.text_verbosity;
                self.collaboration_mode = info.collaboration_mode;
                self.goal = info.goal;
                self.subagent_limits = info.subagents;
                self.pending_inputs = info.pending_inputs;
                self.permission_mode = info.permission_mode;
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
                self.restored_tool_calls.clear();
                self.history_loaded = true;
                self.refresh_runtime_load();
            }
            RuntimeEvent::HistoryPageLoaded {
                generation,
                history,
                start,
                next_start,
                total,
                complete,
            } => {
                if !self.accepts_runtime_generation(&generation) {
                    return;
                }
                if !self.history_page_bounds_valid(start, next_start, total, complete) {
                    let error = format!(
                        "history page bounds are inconsistent: {start}..{next_start} of {total}"
                    );
                    self.reset_runtime_load();
                    self.connection = ConnectionState::Failed(error.clone());
                    self.status_text = "Could not restore session history".into();
                    self.last_error = Some(error);
                    return;
                }
                if start == 0 {
                    self.restore_history(history);
                } else {
                    self.append_restored_history(history);
                }
                self.history_next_start = next_start;
                self.history_total = Some(total);
                self.history_loaded = complete;
                if complete {
                    self.restored_tool_calls.clear();
                }
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
                    if let Some(active) = self.active_prompt.as_ref()
                        && !active.admitted
                    {
                        let optimistic_ids = active
                            .message_render_ids
                            .iter()
                            .copied()
                            .collect::<HashSet<_>>();
                        self.messages
                            .retain(|message| !optimistic_ids.contains(&message.render_id));
                    } else {
                        self.finish_active_message(true);
                    }
                    self.active_prompt = None;
                } else if let Some(active) = self.active_prompt.as_mut() {
                    active.abort_pending = false;
                }
                self.status_text = "Request rejected".into();
                self.last_error = Some(error);
            }
            RuntimeEvent::CommandCompleted { .. } => {}
            RuntimeEvent::ChildActivity { path, kind, detail } => {
                self.status_text = detail.map_or_else(
                    || format!("{path} · {kind}"),
                    |detail| format!("{path} · {kind} · {detail}"),
                );
            }
            RuntimeEvent::TextDelta { text } => {
                if let Some(index) = self.ensure_active_text_segment()
                    && let Some(message) = self.messages.get_mut(index)
                {
                    append_bounded_public(&mut message.text, &text, MAX_TRANSCRIPT_PUBLIC_CHARS);
                    message.presentation_text = message.text.clone().into();
                    self.status_text = "Responding…".into();
                }
            }
            RuntimeEvent::PlanDelta { text } => {
                if let Some(index) = self.ensure_active_text_segment()
                    && let Some(message) = self.messages.get_mut(index)
                {
                    append_bounded_public(&mut message.text, &text, MAX_TRANSCRIPT_PUBLIC_CHARS);
                    message.presentation_text = message.text.clone().into();
                    append_bounded_public(
                        &mut self.latest_plan,
                        &text,
                        MAX_TRANSCRIPT_PUBLIC_CHARS,
                    );
                    self.plan_received_this_turn = true;
                    self.status_text = "Drafting plan…".into();
                }
            }
            RuntimeEvent::ThinkingDelta { text } => {
                self.append_thinking_transcript(&text);
                if !text.is_empty() {
                    self.status_text = "Thinking…".into();
                }
            }
            RuntimeEvent::ToolStarted { call_id, name } => {
                self.begin_tool_transcript(&call_id, &name);
                self.tools.push(ToolActivity {
                    call_id,
                    name: name.clone(),
                    status: "Running".into(),
                    state: ToolState::Running,
                });
                self.status_text = format!("Running {name}…");
            }
            RuntimeEvent::ToolProgress { call_id, message } => {
                self.update_tool_transcript(&call_id, message.as_deref());
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
                self.finish_tool_transcript(&call_id, &name, is_error, preview.as_deref());
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
                        "Snow requested {kind} ({request_id}), which this client cannot answer. Stop the turn to continue."
                    ),
                    None => format!(
                        "Snow requested {kind}, which this client cannot answer. Stop the turn to continue."
                    ),
                });
            }
            RuntimeEvent::PermissionRequested(request) => {
                self.enqueue_interaction(InteractionRequest::Permission(request));
            }
            RuntimeEvent::UserInputRequested(request) => {
                self.enqueue_interaction(InteractionRequest::UserInput(request));
            }
            RuntimeEvent::InteractionResolved {
                command_id,
                request_id,
                command,
            } => {
                let matches = self.active_interaction.as_ref().is_some_and(|active| {
                    active.request_id() == request_id
                        && active.pending().is_some_and(|pending| {
                            pending.command_id == command_id && pending.command == command
                        })
                });
                if matches {
                    self.promote_queued_interaction();
                    self.last_error = None;
                }
            }
            RuntimeEvent::InteractionRejected {
                command_id,
                request_id,
                command,
                error,
            } => {
                let matches = self.active_interaction.as_ref().is_some_and(|active| {
                    request_id.as_deref() == Some(active.request_id())
                        && active.pending().is_some_and(|pending| {
                            command_id.as_deref() == Some(pending.command_id.as_str())
                                && pending.command == command
                        })
                });
                if matches {
                    if let Some(active) = self.active_interaction.as_mut() {
                        active.set_pending(None);
                        if let ActiveInteraction::Permission(interaction) = active {
                            interaction.decision = None;
                        }
                    }
                    self.status_text = "Could not submit response".into();
                    self.last_error = Some(error);
                }
            }
            RuntimeEvent::MalformedInteraction {
                kind,
                request_id,
                error,
            } => {
                if let Some(request_id) = request_id
                    && !self.has_seen_interaction(kind, &request_id)
                {
                    self.mark_interaction_seen(kind, &request_id);
                    self.interaction_rejections
                        .push(InteractionRejection { kind, request_id });
                }
                self.status_text = "Invalid blocking request declined".into();
                self.last_error = Some(error);
            }
            RuntimeEvent::PromptCompleted(completed) => self.finish_prompt(completed),
            RuntimeEvent::Status(status) => self.status_text = status,
            RuntimeEvent::Diagnostic(message) => {
                if self.last_error.is_none() {
                    self.last_error = Some(message);
                }
            }
            RuntimeEvent::Failed(error) => {
                self.clear_interactions();
                self.status_text = "Snow error".into();
                self.last_error = Some(error);
            }
            RuntimeEvent::Exited { expected, status } => {
                self.finish_active_message(true);
                self.active_prompt = None;
                self.clear_interactions();
                self.model_change_pending = None;
                self.thinking_change_pending = None;
                self.session_action_pending = None;
                self.connection = ConnectionState::Stopped;
                self.status_text = if expected {
                    "Stopped".into()
                } else {
                    "Snow exited".into()
                };
                if !expected && self.last_error.is_none() {
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

    fn restore_history(&mut self, history: Vec<HistoryEntry>) {
        self.messages.clear();
        self.restored_tool_calls.clear();
        self.tools.clear();
        self.append_restored_history(history);
    }

    fn append_restored_history(&mut self, history: Vec<HistoryEntry>) {
        self.messages.reserve(history.len());
        for message in history {
            let HistoryEntry {
                role,
                blocks,
                mut tool_result,
            } = message;
            if blocks.is_empty()
                && let Some(result) = tool_result.take()
            {
                match self.attach_restored_tool_result(result) {
                    Ok(()) => continue,
                    Err(result) => tool_result = Some(result),
                }
            }

            let render_id = self.allocate_render_id();
            let text = blocks
                .iter()
                .filter_map(|block| match block {
                    HistoryBlock::Text { text } | HistoryBlock::Plan { text, .. } => {
                        Some(text.as_str())
                    }
                    HistoryBlock::Image(_) | HistoryBlock::ToolCall(_) => None,
                })
                .collect::<Vec<_>>()
                .join("\n");
            let message_index = self.messages.len();
            self.messages.push(ChatMessage {
                role: match role.as_str() {
                    "user" => ChatRole::User,
                    "assistant" | "tool_result" | "tool" => ChatRole::Assistant,
                    _ => ChatRole::System,
                },
                presentation_text: text.clone().into(),
                text,
                streaming: false,
                history_blocks: blocks,
                history_tool_results: tool_result.into_iter().collect(),
                render_id,
            });
            self.index_restored_tool_calls(message_index);
        }
    }

    fn index_restored_tool_calls(&mut self, message_index: usize) {
        let Some(message) = self.messages.get(message_index) else {
            return;
        };
        for tool_call_id in message
            .history_blocks
            .iter()
            .filter_map(|block| match block {
                HistoryBlock::ToolCall(tool) => Some(tool.tool_call_id.as_str()),
                _ => None,
            })
        {
            if message
                .history_tool_results
                .iter()
                .all(|result| result.tool_call_id != tool_call_id)
            {
                self.restored_tool_calls
                    .entry(tool_call_id.to_owned())
                    .or_default()
                    .push(message_index);
            }
        }
    }

    fn attach_restored_tool_result(
        &mut self,
        result: HistoryToolResult,
    ) -> Result<(), HistoryToolResult> {
        let call_id = result.tool_call_id.clone();
        let Some(mut candidates) = self.restored_tool_calls.remove(&call_id) else {
            return Err(result);
        };
        while let Some(message_index) = candidates.pop() {
            let Some(message) = self.messages.get_mut(message_index) else {
                continue;
            };
            let has_call = message.history_blocks.iter().any(|block| {
                matches!(block, HistoryBlock::ToolCall(tool) if tool.tool_call_id == call_id)
            });
            let has_result = message
                .history_tool_results
                .iter()
                .any(|existing| existing.tool_call_id == call_id);
            if has_call && !has_result {
                message.history_tool_results.push(result);
                if !candidates.is_empty() {
                    self.restored_tool_calls.insert(call_id, candidates);
                }
                return Ok(());
            }
        }
        Err(result)
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
        self.clear_interactions();
        self.status_text = completion_summary(&completed).into();
        self.plan_review_ready = completed.status == PromptStatus::Completed
            && self.plan_received_this_turn
            && !self.latest_plan.trim().is_empty();
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
    branch_name_input: Entity<InputState>,
    branch_editing_id: Option<String>,
    branch_delete_confirm: Option<String>,
    session_delete_confirm: Option<String>,
    picker_search_input: Entity<InputState>,
    interaction_input: Entity<InputState>,
    session_menu_open: bool,
    sidebar_collapsed: bool,
    sessions_panel_open: bool,
    sessions: Vec<SessionSummary>,
    processes_panel_open: bool,
    process_detail_open: bool,
    process_live: ProcessLiveState,
    pending_process_target: Option<String>,
    process_poll_task: Option<Task<()>>,
    subagents_panel_open: bool,
    subagent_fleet: SubagentFleetState,
    subagent_transcript: SubagentTranscript,
    pending_subagent_target: Option<String>,
    subagent_poll_task: Option<Task<()>>,
    resource_panel: Option<ResourcePanel>,
    usage_telemetry: Option<UsageTelemetry>,
    context_telemetry: Option<ContextTelemetry>,
    settings_panel: Option<Settings>,
    settings_workspace_open: bool,
    settings_section: SettingsSection,
    settings_focus_handle: FocusHandle,
    theme_catalog: Option<ThemeCatalog>,
    keybindings: Option<Keybindings>,
    keybinding_edit_action: Option<String>,
    keybinding_edit_scope: KeybindingScope,
    keybinding_input: Entity<InputState>,
    settings_concurrency_input: Entity<InputState>,
    auth_panel_open: bool,
    auth_inventory_opens_panel: bool,
    auth_providers: Vec<AuthProvider>,
    auth_selected_provider: Option<String>,
    auth_selected_method: Option<String>,
    auth_entry_mode: AuthEntryMode,
    auth_job: Option<AuthLoginJob>,
    auth_secret_input: Entity<InputState>,
    auth_profile_id_input: Entity<InputState>,
    auth_base_url_input: Entity<InputState>,
    auth_poll_task: Option<Task<()>>,
    plan_nudge_dismissed: bool,
    plan_nudge_dismissals: PlanNudgeDismissals,
    pending_fresh_plan: Option<String>,
    runtime: Option<SnowClient>,
    runtime_config: Option<RuntimeConfig>,
    provider: String,
    composer_picker: ComposerPickerState,
    slash_selection: SlashSelectionState,
    skill_catalog: Vec<SkillSpec>,
    mention_discovery: Option<MentionDiscovery>,
    mention_matches: Vec<String>,
    mention_token_start: Option<usize>,
    mention_selected: usize,
    skill_selection: SuggestionSelectionState,
    paste_store: PasteStore,
    pending_commands: HashMap<String, PendingDesktopCommand>,
    pending_prompt_submission: Option<PendingPromptSubmission>,
    input_history: VecDeque<String>,
    input_history_index: Option<usize>,
    input_history_draft: String,
    attachments: ImageAttachments,
    attachment_task: Option<Task<()>>,
    sidebar_session_list: UniformListScrollHandle,
    management_session_list: UniformListScrollHandle,
    transcript_list: ListState,
    expanded_tool_cards: HashSet<u64>,
    focus_composer_when_ready: bool,
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
        let mention_discovery = config.as_ref().ok().and_then(|config| {
            MentionDiscovery::discover(&config.project_root, MentionLimits::default()).ok()
        });
        let input = cx.new(|cx| {
            InputState::new(window, cx)
                .auto_grow(2, 8)
                .placeholder("Ask anything, @tag files/folders, $use skills, or / for commands")
        });
        let subscription = cx.subscribe_in(
            &input,
            window,
            |this, input, event: &InputEvent, window, cx| match event {
                InputEvent::Change => {
                    let value = input.read(cx).value().to_string();
                    this.paste_store.prune(&value);
                    this.refresh_slash_selection(&value);
                    cx.notify();
                }
                InputEvent::PressEnter { secondary: false } => {
                    let action = composer_enter_action(
                        this.composer_picker.active,
                        this.active_composer_suggestion(cx),
                    );
                    match action {
                        ComposerEnterAction::ActivatePicker => {
                            this.activate_highlighted_picker(window, cx)
                        }
                        ComposerEnterAction::AcceptSlash => {
                            if !this.handle_slash_key(SlashKey::Enter, window, cx) {
                                this.submit(window, cx);
                            }
                        }
                        ComposerEnterAction::AcceptMention => {
                            if !this.complete_mention(false, window, cx) {
                                this.submit(window, cx);
                            }
                        }
                        ComposerEnterAction::AcceptSkill => {
                            if !this.complete_highlighted_skill(window, cx) {
                                this.submit(window, cx);
                            }
                        }
                        ComposerEnterAction::Submit => this.submit(window, cx),
                    }
                }
                InputEvent::PressEnter { secondary: true }
                    if this.state.active_prompt.is_some() =>
                {
                    this.submit_follow_up(window, cx)
                }
                _ => {}
            },
        );
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
        let branch_name_input = cx.new(|cx| InputState::new(window, cx).placeholder("Branch name"));
        let branch_subscription = cx.subscribe_in(
            &branch_name_input,
            window,
            |this, _, event: &InputEvent, _window, cx| {
                if matches!(event, InputEvent::PressEnter { secondary: false }) {
                    this.confirm_branch_rename(cx);
                }
            },
        );
        let picker_search_input = cx.new(|cx| InputState::new(window, cx).placeholder("Search…"));
        let picker_subscription = cx.subscribe_in(
            &picker_search_input,
            window,
            |this, input, event: &InputEvent, window, cx| match event {
                InputEvent::Change => {
                    this.composer_picker
                        .search
                        .set_query(input.read(cx).value().to_string());
                    cx.notify();
                }
                InputEvent::PressEnter { secondary: false } => {
                    this.activate_highlighted_picker(window, cx);
                }
                _ => {}
            },
        );
        let settings_concurrency_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("Concurrent subagents"));
        let keybinding_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("Bindings, separated by commas"));
        let auth_secret_input = cx.new(|cx| {
            InputState::new(window, cx)
                .masked(true)
                .placeholder("API key or access token")
        });
        let auth_profile_id_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("Profile ID (optional)"));
        let auth_base_url_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("https://api.example.com/v1"));
        let interaction_input = cx.new(|cx| {
            InputState::new(window, cx)
                .auto_grow(1, 4)
                .placeholder("Type an answer…")
        });
        let interaction_subscription = cx.subscribe_in(
            &interaction_input,
            window,
            |this, input, event: &InputEvent, window, cx| match event {
                InputEvent::Change => {
                    this.state
                        .set_user_input_draft(input.read(cx).value().to_string());
                    cx.notify();
                }
                InputEvent::PressEnter { secondary: false } => {
                    if this.state.current_user_input().is_some_and(|interaction| {
                        interaction.question_index + 1 == interaction.request.questions.len()
                    }) {
                        this.submit_user_input(window, cx);
                    } else {
                        this.move_user_input_question(1, window, cx);
                    }
                }
                _ => {}
            },
        );

        let mut workspace = Self {
            state: ChatState::default(),
            input,
            session_name_input,
            branch_name_input,
            branch_editing_id: None,
            branch_delete_confirm: None,
            session_delete_confirm: None,
            picker_search_input,
            interaction_input,
            session_menu_open: false,
            sidebar_collapsed: false,
            sessions_panel_open: false,
            sessions: Vec::new(),
            processes_panel_open: false,
            process_detail_open: false,
            process_live: ProcessLiveState::new(),
            pending_process_target: None,
            process_poll_task: None,
            subagents_panel_open: false,
            subagent_fleet: SubagentFleetState::default(),
            subagent_transcript: SubagentTranscript::default(),
            pending_subagent_target: None,
            subagent_poll_task: None,
            resource_panel: None,
            usage_telemetry: None,
            context_telemetry: None,
            settings_panel: None,
            settings_workspace_open: false,
            settings_section: SettingsSection::default(),
            settings_focus_handle: cx.focus_handle(),
            theme_catalog: None,
            keybindings: None,
            keybinding_edit_action: None,
            keybinding_edit_scope: KeybindingScope::Global,
            keybinding_input,
            settings_concurrency_input,
            auth_panel_open: false,
            auth_inventory_opens_panel: false,
            auth_providers: Vec::new(),
            auth_selected_provider: None,
            auth_selected_method: None,
            auth_entry_mode: AuthEntryMode::Login,
            auth_job: None,
            auth_secret_input,
            auth_profile_id_input,
            auth_base_url_input,
            auth_poll_task: None,
            plan_nudge_dismissed: false,
            plan_nudge_dismissals: PlanNudgeDismissals::default(),
            pending_fresh_plan: None,
            runtime: None,
            runtime_config: config.as_ref().ok().cloned(),
            provider,
            composer_picker: ComposerPickerState::default(),
            slash_selection: SlashSelectionState::new(slash_completion_limits()),
            skill_catalog: Vec::new(),
            mention_discovery,
            mention_matches: Vec::new(),
            mention_token_start: None,
            mention_selected: 0,
            skill_selection: SuggestionSelectionState::default(),
            paste_store: PasteStore::new(PasteLimits::default()),
            pending_commands: HashMap::new(),
            pending_prompt_submission: None,
            input_history: VecDeque::new(),
            input_history_index: None,
            input_history_draft: String::new(),
            attachments: ImageAttachments::new(),
            attachment_task: None,
            sidebar_session_list: UniformListScrollHandle::default(),
            management_session_list: UniformListScrollHandle::default(),
            transcript_list: ListState::new(0, ListAlignment::Bottom, px(TRANSCRIPT_LIST_OVERDRAW)),
            expanded_tool_cards: HashSet::new(),
            focus_composer_when_ready: false,
            runtime_task: None,
            provider_shutdown: None,
            _provider_switch_task: None,
            _subscriptions: vec![
                subscription,
                session_subscription,
                branch_subscription,
                picker_subscription,
                interaction_subscription,
            ],
        };
        match config {
            Ok(config) => workspace.connect(config, window, cx),
            Err(error) => workspace.show_start_error(error.to_string()),
        }
        workspace
    }

    fn connect(&mut self, config: RuntimeConfig, window: &mut Window, cx: &mut Context<Self>) {
        self.provider_shutdown = None;
        self.provider = config.provider.clone();
        self.runtime_config = Some(config.clone());
        self.state.begin_connect(&config.provider);
        match SnowClient::start(config) {
            Ok(connection) => {
                let events = connection.events;
                self.runtime = Some(connection.client);
                self.runtime_task = Some(cx.spawn_in(window, async move |this, window| {
                    while let Ok(event) = events.recv_async().await {
                        let batch = receive_runtime_batch(&events, event);
                        if this
                            .update_in(window, |this, window, cx| {
                                let became_ready = batch
                                    .iter()
                                    .any(|event| matches!(event, RuntimeEvent::Ready(_)));
                                let closes_menus = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::PermissionRequested(_)
                                            | RuntimeEvent::UserInputRequested(_)
                                            | RuntimeEvent::MalformedInteraction { .. }
                                    )
                                });
                                if closes_menus {
                                    this.close_composer_picker(window, cx);
                                    this.session_menu_open = false;
                                }
                                let may_update_model = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::ModelChanged(_)
                                            | RuntimeEvent::ModelChangeConfirmed { .. }
                                    )
                                });
                                let sync_interaction_input = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::PermissionRequested(_)
                                            | RuntimeEvent::UserInputRequested(_)
                                            | RuntimeEvent::InteractionResolved { .. }
                                            | RuntimeEvent::InteractionRejected { .. }
                                            | RuntimeEvent::MalformedInteraction { .. }
                                            | RuntimeEvent::PromptCompleted(_)
                                            | RuntimeEvent::Failed(_)
                                            | RuntimeEvent::Exited { .. }
                                    )
                                });
                                let reload_branch = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::BranchSelected { .. }
                                            | RuntimeEvent::BranchForked { .. }
                                    )
                                });
                                this.reconcile_prompt_submissions(&batch, window, cx);
                                for event in &batch {
                                    match event {
                                        RuntimeEvent::HistoryLoaded {
                                            generation,
                                            history,
                                        } if this.state.accepts_runtime_generation(generation) => {
                                            this.replace_input_history(history);
                                        }
                                        RuntimeEvent::HistoryPageLoaded {
                                            generation,
                                            history,
                                            start,
                                            next_start,
                                            total,
                                            complete,
                                        } if this.state.accepts_history_page(
                                            generation,
                                            *start,
                                            *next_start,
                                            *total,
                                            *complete,
                                        ) =>
                                        {
                                            this.append_input_history_page(history, *start == 0);
                                        }
                                        _ => {}
                                    }
                                    if let RuntimeEvent::ChildActivity { path, kind, detail } =
                                        event
                                    {
                                        let text = detail.as_deref().unwrap_or(kind);
                                        this.subagent_fleet.record_path_activity(
                                            path,
                                            SubagentActivityKind::from_event_kind(kind),
                                            text,
                                        );
                                    }
                                }
                                let command_completions = batch
                                    .iter()
                                    .filter_map(|event| match event {
                                        RuntimeEvent::CommandCompleted {
                                            request_id,
                                            command,
                                            data,
                                        } => Some(CompletedDesktopCommand {
                                            command: command.clone(),
                                            data: data.clone(),
                                            show_result: this
                                                .pending_commands
                                                .get(request_id)
                                                .is_some_and(|pending| pending.show_result),
                                            silent: this
                                                .pending_commands
                                                .get(request_id)
                                                .is_some_and(|pending| pending.silent),
                                            process_request: this
                                                .pending_commands
                                                .get(request_id)
                                                .and_then(|pending| pending.process_request),
                                            subagent_request: this
                                                .pending_commands
                                                .get(request_id)
                                                .and_then(|pending| {
                                                    pending.subagent_request.clone()
                                                }),
                                        }),
                                        _ => None,
                                    })
                                    .collect::<Vec<_>>();
                                let session_inventory_refresh = batch.iter().find_map(|event| {
                                    session_inventory_command_after_confirmed_metadata_mutation(
                                        &this.state,
                                        event,
                                    )
                                });
                                let presented_command_ids = batch
                                    .iter()
                                    .filter_map(|event| match event {
                                        RuntimeEvent::CommandCompleted { request_id, .. }
                                            if this
                                                .pending_commands
                                                .get(request_id)
                                                .is_some_and(|pending| !pending.silent) =>
                                        {
                                            Some(request_id.clone())
                                        }
                                        _ => None,
                                    })
                                    .collect::<HashSet<_>>();
                                let refresh_settings_panel = this.settings_workspace_open
                                    && (command_completions.iter().any(|completion| {
                                        matches!(
                                            completion.command.as_str(),
                                            "set_reasoning_summary"
                                                | "set_text_verbosity"
                                                | "permission_mode_set"
                                                | "debug_enable"
                                                | "debug_disable"
                                        )
                                    }) || batch.iter().any(|event| {
                                        matches!(
                                            event,
                                            RuntimeEvent::ModelChanged(_)
                                                | RuntimeEvent::ThinkingChanged { .. }
                                        )
                                    }));
                                let mut refresh_command = false;
                                let mut focus_composer_after_refresh = false;
                                let mut completed_input_drafts = Vec::new();
                                for event in &batch {
                                    match event {
                                        RuntimeEvent::CommandCompleted { request_id, .. } => {
                                            if let Some(pending) =
                                                this.pending_commands.remove(request_id)
                                            {
                                                refresh_command |= pending.refresh_runtime;
                                                focus_composer_after_refresh |= pending
                                                    .refresh_runtime
                                                    && is_session_transition_command(&pending.name);
                                                completed_input_drafts.push(pending.input_draft);
                                            }
                                        }
                                        RuntimeEvent::RequestRejected {
                                            request_id: Some(request_id),
                                            ..
                                        } => {
                                            if let Some(pending) =
                                                this.pending_commands.remove(request_id)
                                            {
                                                if matches!(
                                                    pending.name.as_str(),
                                                    "session_create" | "session_open"
                                                ) {
                                                    this.focus_composer_when_ready = false;
                                                }
                                                if let Some(request) = pending.process_request {
                                                    this.finish_process_request(request);
                                                }
                                                if let Some(request) = pending.subagent_request {
                                                    this.finish_subagent_request(request);
                                                }
                                            }
                                        }
                                        _ => {}
                                    }
                                }
                                if (reload_branch || refresh_command)
                                    && let Some(runtime) = &this.runtime
                                {
                                    match runtime.load_runtime_state() {
                                        Ok(generation) => {
                                            this.state.begin_runtime_load(generation, true);
                                            if focus_composer_after_refresh {
                                                this.focus_composer_when_ready = true;
                                            }
                                        }
                                        Err(error) => {
                                            if focus_composer_after_refresh {
                                                this.focus_composer_when_ready = false;
                                            }
                                            this.state.connection =
                                                ConnectionState::Failed(error.to_string());
                                            this.state.status_text =
                                                "Could not load Snow runtime state".into();
                                            this.state.last_error = Some(error.to_string());
                                        }
                                    }
                                }
                                let history_page_applied = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::HistoryPageLoaded {
                                            generation,
                                            start,
                                            next_start,
                                            total,
                                            complete,
                                            ..
                                        } if this.state.accepts_history_page(
                                            generation,
                                            *start,
                                            *next_start,
                                            *total,
                                            *complete,
                                        )
                                    )
                                });
                                let replace_transcript = batch.iter().any(|event| {
                                    matches!(
                                        event,
                                        RuntimeEvent::HistoryLoaded { generation, .. }
                                            if this.state.accepts_runtime_generation(generation)
                                    ) || matches!(
                                        event,
                                        RuntimeEvent::HistoryPageLoaded {
                                            generation,
                                            start: 0,
                                            next_start,
                                            total,
                                            complete,
                                            ..
                                        } if this.state.accepts_history_page(
                                            generation,
                                            0,
                                            *next_start,
                                            *total,
                                            *complete,
                                        )
                                    )
                                });
                                let discovered_provider =
                                    this.runtime_config.as_mut().map(|config| {
                                        for event in &batch {
                                            if runtime_event_generation(event).is_none_or(
                                                |generation| {
                                                    this.state
                                                        .accepts_runtime_generation(generation)
                                                },
                                            ) {
                                                apply_runtime_config_event(config, event);
                                            }
                                        }
                                        config.provider.clone()
                                    });
                                if let Some(provider) = discovered_provider
                                    && is_user_visible_provider(&provider)
                                {
                                    this.provider = provider;
                                }
                                let transcript_content_changed =
                                    batch.iter().any(runtime_event_changes_transcript_content);
                                let previous_message_count = this.state.messages.len();
                                let previous_transcript_scroll =
                                    this.transcript_list.logical_scroll_top();
                                let tool_rows = tool_transcript_rows(&this.state, &batch);
                                apply_runtime_batch(&mut this.state, batch, &presented_command_ids);
                                let transcript_reset = sync_transcript_list_items(
                                    &this.transcript_list,
                                    this.state.messages.len(),
                                    replace_transcript || history_page_applied,
                                );
                                if replace_transcript {
                                    this.expanded_tool_cards.clear();
                                } else if transcript_content_changed && !transcript_reset {
                                    let end = this.state.messages.len();
                                    let start = end.saturating_sub(4);
                                    if start < end {
                                        this.transcript_list.splice(start..end, end - start);
                                    }
                                    for row in tool_rows {
                                        if row < start && row < end {
                                            this.transcript_list.splice(row..row + 1, 1);
                                        }
                                    }
                                }
                                this.sync_plan_nudge_scope();
                                for completion in command_completions {
                                    this.apply_command_completion(completion, window, cx);
                                }
                                this.suppress_management_panels_for_interaction(window, cx);
                                if let Some(command) = session_inventory_refresh {
                                    this.run_silent_rpc_command(command);
                                }
                                for draft in completed_input_drafts {
                                    this.finish_input_command(draft, window, cx);
                                }
                                if refresh_settings_panel {
                                    this.run_rpc_command(RpcCommand {
                                        name: "settings_get".into(),
                                        fields: serde_json::Map::new(),
                                        refresh_runtime: false,
                                    });
                                }
                                if sync_interaction_input {
                                    let value = this
                                        .state
                                        .current_user_input()
                                        .filter(|interaction| interaction.draft().use_other)
                                        .map(|interaction| interaction.draft().other.clone())
                                        .unwrap_or_default();
                                    this.interaction_input.update(cx, |input, cx| {
                                        input.set_value(&value, window, cx)
                                    });
                                }
                                let automatic_rejections = this.state.take_interaction_rejections();
                                let must_abort_fail_closed = !automatic_rejections.is_empty();
                                for rejection in automatic_rejections {
                                    let result =
                                        this.runtime.as_ref().map(|runtime| match rejection.kind {
                                            InteractionKind::Permission => runtime
                                                .permission_reject(rejection.request_id.clone()),
                                            InteractionKind::UserInput => runtime
                                                .user_input_reject(rejection.request_id.clone()),
                                        });
                                    if let Some(Err(error)) = result {
                                        this.state.status_text =
                                            "Could not safely decline blocking request".into();
                                        this.state.last_error = Some(error.to_string());
                                    }
                                }
                                if must_abort_fail_closed
                                    && let Some(runtime) = &this.runtime
                                    && this.state.can_abort()
                                {
                                    match runtime.abort() {
                                        Ok(_) => {
                                            if let Some(active) = this.state.active_prompt.as_mut()
                                            {
                                                active.abort_pending = true;
                                            }
                                            this.state.status_text =
                                                "Stopping after an invalid interaction…".into();
                                        }
                                        Err(error) => {
                                            this.state.status_text =
                                                "Could not stop after an invalid interaction"
                                                    .into();
                                            this.state.last_error = Some(error.to_string());
                                            let (completion, _ignored) = flume::bounded(1);
                                            runtime.shutdown_in_background(completion);
                                        }
                                    }
                                }
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
                                if became_ready {
                                    this.auth_inventory_opens_panel = false;
                                    this.run_silent_rpc_command(RpcCommand {
                                        name: "auth_providers".into(),
                                        fields: serde_json::Map::new(),
                                        refresh_runtime: false,
                                    });
                                    this.run_silent_rpc_command(RpcCommand {
                                        name: "skills".into(),
                                        fields: serde_json::Map::new(),
                                        refresh_runtime: false,
                                    });
                                    this.run_silent_rpc_command(RpcCommand {
                                        name: "sessions_list".into(),
                                        fields: serde_json::Map::new(),
                                        refresh_runtime: false,
                                    });
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
                                this.try_submit_pending_fresh_plan(window, cx);
                                if this.focus_composer_when_ready && this.state.can_send() {
                                    this.focus_composer_when_ready = false;
                                    this.input.update(cx, |input, cx| input.focus(window, cx));
                                }
                                if should_follow_transcript(
                                    previous_message_count,
                                    previous_transcript_scroll,
                                    transcript_content_changed,
                                    transcript_reset,
                                ) {
                                    this.scroll_transcript_to_bottom();
                                }
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

    fn provider_catalog(&self) -> Vec<ProviderCatalogItem> {
        build_provider_catalog(&self.auth_providers, Some(&self.provider))
    }

    fn set_desktop_appearance(
        &mut self,
        target: Appearance,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        match appearance::set_appearance(target, Some(window), cx) {
            Ok(()) => {
                self.state.status_text = format!("Appearance set to {}", appearance_label(target));
                self.state.last_error = None;
            }
            Err(error) => {
                self.state.status_text = "Could not save appearance".into();
                self.state.last_error = Some(bounded_display(&error.to_string(), 1_024));
            }
        }
        cx.notify();
    }

    fn update_persisted_settings(&mut self, params: serde_json::Value, cx: &mut Context<Self>) {
        self.run_rpc_command(RpcCommand {
            name: "settings_update".into(),
            fields: serde_json::Map::from_iter([("params".into(), params)]),
            refresh_runtime: false,
        });
        cx.notify();
    }

    fn toggle_subagents_setting(&mut self, cx: &mut Context<Self>) {
        let Some(settings) = self.settings_panel.as_ref() else {
            return;
        };
        self.update_persisted_settings(
            serde_json::json!({"subagents_enabled": !settings.subagents_enabled}),
            cx,
        );
    }

    fn save_subagent_concurrency(&mut self, cx: &mut Context<Self>) {
        let value = self
            .settings_concurrency_input
            .read(cx)
            .value()
            .trim()
            .parse::<i64>();
        let Ok(value) = value else {
            self.state.last_error = Some("concurrent subagents must be an integer".into());
            cx.notify();
            return;
        };
        if !(1..=1_000).contains(&value) {
            self.state.last_error = Some("concurrent subagents must be between 1 and 1000".into());
            cx.notify();
            return;
        }
        self.update_persisted_settings(serde_json::json!({"subagents_max_concurrent": value}), cx);
    }

    fn toggle_skills_setting(&mut self, cx: &mut Context<Self>) {
        let Some(settings) = self.settings_panel.as_ref() else {
            return;
        };
        self.update_persisted_settings(
            serde_json::json!({"skills_enabled": !settings.skills_enabled}),
            cx,
        );
    }

    fn set_live_setting(
        &mut self,
        command: &str,
        fields: serde_json::Map<String, serde_json::Value>,
        cx: &mut Context<Self>,
    ) {
        self.run_rpc_command(RpcCommand {
            name: command.into(),
            fields,
            refresh_runtime: true,
        });
        cx.notify();
    }

    fn track_typed_command(
        &mut self,
        name: &'static str,
        request: Result<String, crate::snow::SnowError>,
    ) -> bool {
        match request {
            Ok(request_id) => {
                self.pending_commands.insert(
                    request_id,
                    PendingDesktopCommand {
                        name: name.into(),
                        refresh_runtime: false,
                        show_result: false,
                        silent: false,
                        input_draft: None,
                        process_request: None,
                        subagent_request: None,
                    },
                );
                self.state.status_text = format!("Running {name}…");
                self.state.last_error = None;
                true
            }
            Err(error) => {
                self.state.status_text = format!("Could not run {name}");
                self.state.last_error = Some(error.to_string());
                false
            }
        }
    }

    fn refresh_presentation_resources(&mut self) {
        let Some(runtime) = self.runtime.as_ref() else {
            return;
        };
        let themes = runtime.load_themes();
        let keybindings = runtime.load_keybindings();
        self.track_typed_command("themes_list", themes);
        self.track_typed_command("keybindings_get", keybindings);
    }

    fn open_settings_section(
        &mut self,
        section: SettingsSection,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.settings_workspace_open = true;
        self.settings_section = section;
        self.sessions_panel_open = false;
        self.session_menu_open = false;
        self.processes_panel_open = false;
        self.process_detail_open = false;
        self.process_poll_task = None;
        self.subagents_panel_open = false;
        self.subagent_poll_task = None;
        self.resource_panel = None;
        self.auth_panel_open = false;
        self.composer_picker.close();
        self.restore_composer_focus_after_picker_close(window, cx);
        self.refresh_settings(cx);
    }

    fn select_settings_section(
        &mut self,
        section: SettingsSection,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.settings_section = section;
        self.close_composer_picker(window, cx);
        cx.notify();
    }

    fn refresh_settings(&mut self, cx: &mut Context<Self>) {
        self.run_rpc_command(RpcCommand {
            name: "settings_get".into(),
            fields: serde_json::Map::new(),
            refresh_runtime: false,
        });
        self.refresh_presentation_resources();
        cx.notify();
    }

    fn select_theme(&mut self, name: &str, cx: &mut Context<Self>) {
        let Some(runtime) = self.runtime.as_ref() else {
            return;
        };
        let request = runtime.update_theme(name.to_owned());
        self.track_typed_command("settings_update", request);
        cx.notify();
    }

    fn edit_keybinding(
        &mut self,
        action: &str,
        scope: KeybindingScope,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(bindings) = self.keybindings.as_ref() else {
            return;
        };
        if scope == KeybindingScope::Project && !bindings.project_allowed {
            self.state.last_error = Some("Project keybindings require a trusted project".into());
            cx.notify();
            return;
        }
        let Some(item) = bindings.actions.iter().find(|item| item.name == action) else {
            return;
        };
        let values = match scope {
            KeybindingScope::Global => &item.global,
            KeybindingScope::Project => &item.project,
        };
        let value = values.join(", ");
        self.keybinding_edit_action = Some(action.to_owned());
        self.keybinding_edit_scope = scope;
        self.keybinding_input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        cx.notify();
    }

    fn save_keybinding(&mut self, cx: &mut Context<Self>) {
        let Some(action) = self.keybinding_edit_action.clone() else {
            return;
        };
        let values = self
            .keybinding_input
            .read(cx)
            .value()
            .split(',')
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .collect::<Vec<_>>();
        if values.is_empty() {
            self.state.last_error =
                Some("Enter at least one binding or use Reset for this layer".into());
            cx.notify();
            return;
        }
        let Some(runtime) = self.runtime.as_ref() else {
            return;
        };
        let project_allowed = self
            .keybindings
            .as_ref()
            .is_some_and(|bindings| bindings.project_allowed);
        let params = KeybindingsUpdateParams {
            scope: self.keybinding_edit_scope,
            bindings: std::collections::BTreeMap::from_iter([(action, values)]),
            reset: Vec::new(),
        };
        let request = runtime.update_keybindings(params, project_allowed);
        if self.track_typed_command("keybindings_update", request) {
            self.keybinding_edit_action = None;
        }
        cx.notify();
    }

    fn reset_keybinding(&mut self, action: &str, scope: KeybindingScope, cx: &mut Context<Self>) {
        let Some(runtime) = self.runtime.as_ref() else {
            return;
        };
        let project_allowed = self
            .keybindings
            .as_ref()
            .is_some_and(|bindings| bindings.project_allowed);
        let params = KeybindingsUpdateParams {
            scope,
            bindings: std::collections::BTreeMap::new(),
            reset: vec![action.to_owned()],
        };
        let request = runtime.update_keybindings(params, project_allowed);
        self.track_typed_command("keybindings_update", request);
        cx.notify();
    }

    fn close_keybinding_editor(&mut self, cx: &mut Context<Self>) {
        self.keybinding_edit_action = None;
        cx.notify();
    }

    fn close_settings_panel(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.settings_workspace_open = false;
        self.composer_picker.close();
        self.restore_composer_focus_after_picker_close(window, cx);
        cx.notify();
    }

    fn selected_auth_provider(&self) -> Option<&AuthProvider> {
        let selected = self.auth_selected_provider.as_deref()?;
        self.auth_providers
            .iter()
            .find(|provider| provider.provider_id == selected)
    }

    fn ensure_auth_method_selection(&mut self) {
        let selected_provider = self.auth_selected_provider.clone();
        let current_method = self.auth_selected_method.clone();
        let method = selected_provider
            .as_deref()
            .and_then(|provider_id| {
                self.auth_providers
                    .iter()
                    .find(|provider| provider.provider_id == provider_id)
            })
            .and_then(|provider| {
                current_method
                    .as_ref()
                    .filter(|method| provider.methods.iter().any(|item| &item.id == *method))
                    .cloned()
                    .or_else(|| provider.methods.first().map(|method| method.id.clone()))
            });
        self.auth_selected_method = method;
    }

    fn select_auth_provider(&mut self, provider_id: &str, cx: &mut Context<Self>) {
        self.auth_selected_provider = Some(provider_id.into());
        self.auth_selected_method = None;
        self.auth_job = None;
        self.ensure_auth_method_selection();
        cx.notify();
    }

    fn select_auth_method(&mut self, method: &str, cx: &mut Context<Self>) {
        self.auth_selected_method = Some(method.into());
        self.auth_job = None;
        cx.notify();
    }

    fn set_auth_entry_mode(&mut self, mode: AuthEntryMode, cx: &mut Context<Self>) {
        self.auth_entry_mode = mode;
        self.auth_job = None;
        if mode == AuthEntryMode::Login {
            self.ensure_auth_method_selection();
        }
        cx.notify();
    }

    fn start_auth_login(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let profile_mode = self.auth_entry_mode == AuthEntryMode::CompatibleProfile;
        let profile_id = self
            .auth_profile_id_input
            .read(cx)
            .value()
            .trim()
            .to_owned();
        let base_url = self.auth_base_url_input.read(cx).value().trim().to_owned();
        let (provider, method) = if profile_mode {
            if profile_id.is_empty() {
                self.state.last_error = Some("profile ID is required".into());
                cx.notify();
                return;
            }
            if base_url.is_empty() {
                self.state.last_error = Some("compatible profile base URL is required".into());
                cx.notify();
                return;
            }
            (profile_id.clone(), "api_key".to_owned())
        } else {
            let Some(provider) = self.auth_selected_provider.clone() else {
                self.state.last_error = Some("select an authentication provider".into());
                cx.notify();
                return;
            };
            let Some(method) = self.auth_selected_method.clone() else {
                self.state.last_error = Some("select a login method".into());
                cx.notify();
                return;
            };
            (provider, method)
        };
        let secret = self.auth_secret_input.read(cx).value().to_string();
        if secret.len() > 64 << 10 {
            self.state.last_error = Some("credential exceeds the 64 KiB limit".into());
            cx.notify();
            return;
        }
        if profile_mode && secret.trim().is_empty() {
            self.state.last_error = Some("compatible profile API key is required".into());
            cx.notify();
            return;
        }
        let mut fields = serde_json::Map::from_iter([
            ("provider".into(), serde_json::Value::String(provider)),
            ("method".into(), serde_json::Value::String(method)),
        ]);
        if !secret.trim().is_empty() {
            fields.insert("secret".into(), serde_json::Value::String(secret));
        }
        if profile_mode {
            fields.insert(
                "params".into(),
                serde_json::json!({"profile_id": profile_id, "base_url": base_url}),
            );
        }
        let queued = self.run_rpc_command(RpcCommand {
            name: if profile_mode {
                "auth_profile_set".into()
            } else {
                "auth_login_start".into()
            },
            fields,
            refresh_runtime: false,
        });
        if !queued {
            cx.notify();
            return;
        }
        self.auth_job = None;
        self.auth_secret_input
            .update(cx, |input, cx| input.set_value("", window, cx));
        let executor = cx.background_executor().clone();
        self.auth_poll_task = Some(cx.spawn_in(window, async move |this, window| {
            loop {
                executor.timer(Duration::from_millis(750)).await;
                let keep_polling = this
                    .update_in(window, |this, _, cx| {
                        let Some(job) = this.auth_job.as_ref() else {
                            return this.auth_panel_open;
                        };
                        if job.state != "running" {
                            return false;
                        }
                        let job_id = job.job_id.clone();
                        this.run_rpc_command(RpcCommand {
                            name: "auth_login_status".into(),
                            fields: serde_json::Map::from_iter([(
                                "params".into(),
                                serde_json::json!({"job_id": job_id}),
                            )]),
                            refresh_runtime: false,
                        });
                        cx.notify();
                        true
                    })
                    .unwrap_or(false);
                if !keep_polling {
                    break;
                }
            }
        }));
        cx.notify();
    }

    fn cancel_auth_login(&mut self, cx: &mut Context<Self>) {
        let Some(job_id) = self
            .auth_job
            .as_ref()
            .filter(|job| job.state == "running")
            .map(|job| job.job_id.clone())
        else {
            return;
        };
        self.run_rpc_command(RpcCommand {
            name: "auth_login_cancel".into(),
            fields: serde_json::Map::from_iter([(
                "params".into(),
                serde_json::json!({"job_id": job_id}),
            )]),
            refresh_runtime: false,
        });
        cx.notify();
    }

    fn logout_selected_auth_provider(&mut self, cx: &mut Context<Self>) {
        let Some(provider) = self.auth_selected_provider.clone() else {
            return;
        };
        self.run_rpc_command(RpcCommand {
            name: "auth_logout".into(),
            fields: serde_json::Map::from_iter([(
                "provider".into(),
                serde_json::Value::String(provider),
            )]),
            refresh_runtime: false,
        });
        cx.notify();
    }

    fn close_auth_panel(&mut self, cx: &mut Context<Self>) {
        self.auth_panel_open = false;
        self.auth_job = None;
        self.auth_poll_task = None;
        cx.notify();
    }

    fn close_resource_panel(&mut self, cx: &mut Context<Self>) {
        self.resource_panel = None;
        cx.notify();
    }

    fn close_subagents_panel(&mut self, cx: &mut Context<Self>) {
        self.subagents_panel_open = false;
        self.subagent_poll_task = None;
        self.subagent_fleet.invalidate_requests();
        self.subagent_transcript.clear();
        self.pending_subagent_target = None;
        cx.notify();
    }

    fn dispatch_subagent_command(
        &mut self,
        name: &'static str,
        fields: serde_json::Map<String, serde_json::Value>,
        request: PendingSubagentRequest,
    ) -> bool {
        let Some(runtime) = &self.runtime else {
            self.state.last_error = Some("Snow is not connected".into());
            return false;
        };
        match runtime.command(name.to_owned(), fields) {
            Ok(request_id) => {
                self.pending_commands.insert(
                    request_id,
                    PendingDesktopCommand {
                        name: name.into(),
                        refresh_runtime: false,
                        show_result: false,
                        silent: false,
                        input_draft: None,
                        process_request: None,
                        subagent_request: Some(request),
                    },
                );
                self.state.status_text = format!("Running {name}…");
                self.state.last_error = None;
                true
            }
            Err(error) => {
                self.state.status_text = format!("Could not run {name}");
                self.state.last_error = Some(error.to_string());
                false
            }
        }
    }

    fn refresh_subagents(&mut self, cx: &mut Context<Self>) {
        if self.command_pending("subagent_list") {
            return;
        }
        let target = self.pending_subagent_target.take();
        let request = self.subagent_fleet.begin_list_request(target.as_deref());
        let mut fields = serde_json::Map::new();
        if !request.path_prefix.is_empty() {
            fields.insert(
                "params".into(),
                serde_json::json!({"path_prefix": request.path_prefix}),
            );
        }
        self.dispatch_subagent_command(
            "subagent_list",
            fields,
            PendingSubagentRequest::List(request),
        );
        cx.notify();
    }

    fn request_selected_subagent_detail(&mut self) {
        if self.command_pending("subagent_get") {
            return;
        }
        let Some(request) = self.subagent_fleet.begin_detail_request() else {
            return;
        };
        let target = request.selection.path.clone();
        self.dispatch_subagent_command(
            "subagent_get",
            serde_json::Map::from_iter([("params".into(), serde_json::json!({"target": target}))]),
            PendingSubagentRequest::Detail(request),
        );
    }

    fn select_subagent(&mut self, target: &str, cx: &mut Context<Self>) {
        if self.subagent_fleet.select(target) {
            self.request_selected_subagent_detail();
            self.start_selected_subagent_history();
        }
        cx.notify();
    }

    fn start_selected_subagent_history(&mut self) {
        let Some(selected) = self.subagent_fleet.selected() else {
            self.subagent_transcript.clear();
            return;
        };
        let Some(selection) = self.subagent_fleet.selection().cloned() else {
            self.subagent_transcript.clear();
            return;
        };
        self.subagent_transcript
            .reset(selected.generation, selection);
        self.request_next_subagent_history_page();
    }

    fn request_next_subagent_history_page(&mut self) {
        if self.subagent_transcript.loading || self.subagent_transcript.complete {
            return;
        }
        let Some(selection) = self.subagent_transcript.selection.clone() else {
            return;
        };
        let Some(runtime) = &self.runtime else {
            self.state.last_error = Some("Snow is not connected".into());
            return;
        };
        let cursor = self.subagent_transcript.next_cursor.clone();
        match runtime.load_subagent_messages(
            selection.path.clone(),
            self.subagent_transcript.generation,
            cursor.clone(),
            64,
            256 * 1024,
        ) {
            Ok(request) => {
                self.subagent_transcript.loading = true;
                self.pending_commands.insert(
                    request.request_id,
                    PendingDesktopCommand {
                        name: "subagent_messages".into(),
                        refresh_runtime: false,
                        show_result: false,
                        silent: false,
                        input_draft: None,
                        process_request: None,
                        subagent_request: Some(PendingSubagentRequest::Messages {
                            generation: request.generation,
                            selection,
                            cursor,
                        }),
                    },
                );
            }
            Err(error) => {
                self.state.status_text = "Could not load subagent history".into();
                self.state.last_error = Some(error.to_string());
            }
        }
    }

    fn apply_subagent_messages_page(
        &mut self,
        generation: u64,
        selection: crate::subagent_live::AgentSelection,
        cursor: Option<String>,
        data: Option<serde_json::Value>,
    ) {
        if generation != self.subagent_transcript.generation
            || self.subagent_transcript.selection.as_ref() != Some(&selection)
            || self.subagent_transcript.next_cursor != cursor
        {
            return;
        }
        self.subagent_transcript.loading = false;
        let result = data
            .ok_or_else(|| "subagent_messages returned no data".to_owned())
            .and_then(|data| {
                decode_subagent_messages_page(data, 256 * 1024)
                    .map_err(|error| format!("invalid subagent_messages response: {error}"))
            });
        let page: SubagentMessagesPage = match result {
            Ok(page) => page,
            Err(error) => {
                self.state.status_text = "Could not load subagent history".into();
                self.state.last_error = Some(error);
                return;
            }
        };
        let page_matches_selection =
            page.agent.path == selection.path && page.agent.thread_id == selection.thread_id;
        let page_generation_matches = self
            .subagent_transcript
            .page_generation
            .is_none_or(|known| known == page.generation);
        let total_matches = self.subagent_transcript.page_generation.is_none()
            || self.subagent_transcript.total == page.total;
        let ids_are_new = page.messages.iter().all(|message| {
            !self
                .subagent_transcript
                .messages
                .iter()
                .any(|existing| existing.id == message.id)
        });
        if !page_matches_selection
            || !page_generation_matches
            || !total_matches
            || page.start != self.subagent_transcript.messages.len()
            || !ids_are_new
        {
            self.state.status_text = "Could not load subagent history".into();
            self.state.last_error = Some("subagent history page was stale or inconsistent".into());
            return;
        }
        self.subagent_transcript.page_generation = Some(page.generation);
        self.subagent_transcript.total = page.total;
        self.subagent_transcript.messages.extend(page.messages);
        self.subagent_transcript.next_cursor = page.next_cursor;
        self.subagent_transcript.complete = !page.has_more;
        self.state.status_text = if self.subagent_transcript.complete {
            "Subagent history loaded".into()
        } else {
            "Loading subagent history…".into()
        };
        self.state.last_error = None;
        if !self.subagent_transcript.complete {
            self.request_next_subagent_history_page();
        }
    }

    fn subagent_action(&mut self, action: SubagentAction, target: &str, cx: &mut Context<Self>) {
        let valid = self.subagent_fleet.selected().is_some_and(|selected| {
            selected.state.agent.path == target
                && self.subagent_fleet.selected_action() == Some(action)
        });
        if !valid {
            self.state.last_error = Some("Subagent action is no longer valid".into());
            cx.notify();
            return;
        }
        self.run_rpc_command(RpcCommand {
            name: action.rpc_command().into(),
            fields: serde_json::Map::from_iter([(
                "params".into(),
                serde_json::json!({"target": target}),
            )]),
            refresh_runtime: false,
        });
        cx.notify();
    }

    fn finish_subagent_request(&mut self, request: PendingSubagentRequest) {
        if let PendingSubagentRequest::Messages {
            generation,
            selection,
            cursor,
        } = request
            && generation == self.subagent_transcript.generation
            && self.subagent_transcript.selection.as_ref() == Some(&selection)
            && self.subagent_transcript.next_cursor == cursor
        {
            self.subagent_transcript.loading = false;
        }
    }

    fn finish_process_request(&mut self, request: PendingProcessRequest) {
        match request {
            PendingProcessRequest::List(metadata) => {
                self.process_live.finish_list_request(metadata);
            }
            PendingProcessRequest::Logs(metadata) => {
                self.process_live.finish_log_request(metadata);
            }
        }
    }

    fn dispatch_process_command(
        &mut self,
        name: &'static str,
        fields: serde_json::Map<String, serde_json::Value>,
        request: PendingProcessRequest,
    ) -> bool {
        let Some(runtime) = &self.runtime else {
            self.finish_process_request(request);
            self.state.last_error = Some("Snow is not connected".into());
            return false;
        };
        match runtime.command(name.to_owned(), fields) {
            Ok(request_id) => {
                self.pending_commands.insert(
                    request_id,
                    PendingDesktopCommand {
                        name: name.into(),
                        refresh_runtime: false,
                        show_result: false,
                        silent: false,
                        input_draft: None,
                        process_request: Some(request),
                        subagent_request: None,
                    },
                );
                self.state.status_text = format!("Running {name}…");
                self.state.last_error = None;
                true
            }
            Err(error) => {
                self.finish_process_request(request);
                self.state.status_text = format!("Could not run {name}");
                self.state.last_error = Some(error.to_string());
                false
            }
        }
    }

    fn request_process_list(&mut self) -> bool {
        let Some(request) = self.process_live.start_list_request() else {
            return false;
        };
        self.dispatch_process_command(
            "processes_list",
            serde_json::Map::new(),
            PendingProcessRequest::List(request.metadata),
        )
    }

    fn request_next_process_logs(&mut self) -> bool {
        let Some(request) = self.process_live.start_log_request() else {
            return false;
        };
        let fields = serde_json::Map::from_iter([(
            "params".into(),
            serde_json::json!({
                "process_id": request.process_id,
                "cursor": request.cursor,
                "max_bytes": 64 * 1024,
            }),
        )]);
        self.dispatch_process_command(
            "process_logs",
            fields,
            PendingProcessRequest::Logs(request.metadata),
        )
    }

    fn close_processes_panel(&mut self, cx: &mut Context<Self>) {
        self.processes_panel_open = false;
        self.process_detail_open = false;
        self.process_live.clear();
        self.pending_process_target = None;
        self.process_poll_task = None;
        cx.notify();
    }

    fn close_process_logs(&mut self, cx: &mut Context<Self>) {
        self.process_detail_open = false;
        cx.notify();
    }

    fn refresh_processes(&mut self, cx: &mut Context<Self>) {
        self.request_process_list();
        self.request_next_process_logs();
        cx.notify();
    }

    fn open_process_logs(&mut self, process_id: &str, cx: &mut Context<Self>) {
        if self.process_live.select_process(process_id) {
            self.process_detail_open = true;
            self.request_next_process_logs();
        }
        cx.notify();
    }

    fn load_more_process_logs(&mut self, cx: &mut Context<Self>) {
        self.request_next_process_logs();
        cx.notify();
    }

    fn toggle_sidebar(&mut self, cx: &mut Context<Self>) {
        self.sidebar_collapsed = !self.sidebar_collapsed;
        cx.notify();
    }

    fn refresh_session_inventory(&mut self, cx: &mut Context<Self>) {
        if !self.run_silent_rpc_command(RpcCommand {
            name: "sessions_list".into(),
            fields: serde_json::Map::new(),
            refresh_runtime: false,
        }) {
            cx.notify();
        }
    }

    fn management_panel_state(&self) -> ManagementPanelState {
        ManagementPanelState {
            sessions: self.sessions_panel_open,
            processes: self.processes_panel_open,
            subagents: self.subagents_panel_open,
            resource: self.resource_panel.is_some(),
            auth: self.auth_panel_open,
            settings: self.settings_workspace_open,
        }
    }

    fn visible_management_panels(&self) -> ManagementPanelState {
        management_panels_for_interaction(
            self.state.active_interaction.is_some(),
            self.management_panel_state(),
        )
    }

    fn suppress_management_panels_for_interaction(
        &mut self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let requested = self.management_panel_state();
        let visible =
            management_panels_for_interaction(self.state.active_interaction.is_some(), requested);
        if visible == requested {
            return;
        }
        self.sessions_panel_open = visible.sessions;
        self.processes_panel_open = visible.processes;
        self.process_detail_open = false;
        self.process_poll_task = None;
        self.pending_process_target = None;
        self.subagents_panel_open = visible.subagents;
        self.subagent_poll_task = None;
        self.pending_subagent_target = None;
        if !visible.resource {
            self.resource_panel = None;
        }
        self.auth_panel_open = visible.auth;
        self.auth_inventory_opens_panel = false;
        self.settings_workspace_open = visible.settings;
        if !visible.settings {
            self.close_composer_picker(window, cx);
        }
    }

    fn has_open_workspace_panel(&self) -> bool {
        self.management_panel_state() != ManagementPanelState::default()
    }

    fn canvas_layout(&self) -> WorkspaceCanvasLayout {
        workspace_canvas_layout(
            self.state.messages.len(),
            self.state.active_interaction.is_some() || self.state.plan_review_ready,
            self.has_open_workspace_panel(),
        )
    }

    fn close_sessions_panel(&mut self, cx: &mut Context<Self>) {
        self.sessions_panel_open = false;
        cx.notify();
    }

    fn has_pending_session_transition(&self) -> bool {
        self.pending_commands
            .values()
            .any(|pending| is_session_transition_command(&pending.name))
    }

    fn create_session(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.has_pending_session_transition() {
            return;
        }
        let started = self.run_rpc_command(RpcCommand {
            name: "session_create".into(),
            fields: serde_json::Map::from_iter([("params".into(), serde_json::json!({}))]),
            refresh_runtime: true,
        });
        if started {
            self.sessions_panel_open = false;
            self.session_menu_open = false;
            self.focus_composer_when_ready = false;
            self.dismiss_input_suggestions(cx);
            self.input.update(cx, |input, cx| input.focus(window, cx));
        }
        cx.notify();
    }

    fn open_session(&mut self, session_id: &str, window: &mut Window, cx: &mut Context<Self>) {
        if self.has_pending_session_transition() {
            return;
        }
        let started = self.run_rpc_command(RpcCommand {
            name: "session_open".into(),
            fields: serde_json::Map::from_iter([(
                "params".into(),
                serde_json::json!({"session_id": session_id}),
            )]),
            refresh_runtime: true,
        });
        if started {
            self.sessions_panel_open = false;
            self.session_menu_open = false;
            self.focus_composer_when_ready = false;
            self.dismiss_input_suggestions(cx);
            self.input.update(cx, |input, cx| input.focus(window, cx));
        }
        cx.notify();
    }

    fn request_session_delete(&mut self, session_id: &str, cx: &mut Context<Self>) {
        if self.session_delete_confirm.as_deref() == Some(session_id) {
            self.run_rpc_command(RpcCommand {
                name: "session_delete".into(),
                fields: serde_json::Map::from_iter([(
                    "params".into(),
                    serde_json::json!({"session_id": session_id}),
                )]),
                refresh_runtime: false,
            });
            self.session_delete_confirm = None;
        } else {
            self.session_delete_confirm = Some(session_id.to_owned());
            self.state.status_text = "Confirm session deletion".into();
        }
        cx.notify();
    }

    fn cancel_session_delete(&mut self, cx: &mut Context<Self>) {
        self.session_delete_confirm = None;
        cx.notify();
    }

    fn set_session_menu_open(&mut self, open: bool, window: &mut Window, cx: &mut Context<Self>) {
        let was_open = self.session_menu_open;
        let changed = was_open != open || self.sessions_panel_open;
        self.sessions_panel_open = false;
        self.session_menu_open = open;
        if open && !was_open {
            self.dismiss_input_suggestions(cx);
            self.close_composer_picker(window, cx);
            let name = self.state.session_name.clone();
            self.session_name_input
                .update(cx, |input, cx| input.set_value(&name, window, cx));
        }
        if changed {
            cx.notify();
        }
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

    fn begin_branch_rename(
        &mut self,
        branch_id: &str,
        current_name: &str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !self.state.can_manage_session() {
            return;
        }
        self.branch_editing_id = Some(branch_id.to_owned());
        self.branch_delete_confirm = None;
        let current_name = current_name.to_owned();
        self.branch_name_input.update(cx, move |input, cx| {
            input.set_value(&current_name, window, cx);
            input.focus(window, cx);
        });
        cx.notify();
    }

    fn confirm_branch_rename(&mut self, cx: &mut Context<Self>) {
        if !self.state.can_manage_session() {
            return;
        }
        let Some(branch_id) = self.branch_editing_id.clone() else {
            return;
        };
        let name = self.branch_name_input.read(cx).value().trim().to_owned();
        if name.is_empty() {
            self.state.last_error = Some("branch name cannot be empty".into());
            cx.notify();
            return;
        }
        self.run_rpc_command(RpcCommand {
            name: "branch_rename".into(),
            fields: serde_json::Map::from_iter([(
                "params".into(),
                serde_json::json!({"branch_id": branch_id, "name": name}),
            )]),
            refresh_runtime: true,
        });
        self.branch_editing_id = None;
        cx.notify();
    }

    fn cancel_branch_rename(&mut self, cx: &mut Context<Self>) {
        self.branch_editing_id = None;
        cx.notify();
    }

    fn request_branch_delete(&mut self, branch_id: &str, cx: &mut Context<Self>) {
        if !self.state.can_manage_session() {
            return;
        }
        if self.branch_delete_confirm.as_deref() == Some(branch_id) {
            self.run_rpc_command(RpcCommand {
                name: "branch_delete".into(),
                fields: serde_json::Map::from_iter([(
                    "params".into(),
                    serde_json::json!({"branch_id": branch_id}),
                )]),
                refresh_runtime: true,
            });
            self.branch_delete_confirm = None;
        } else {
            self.branch_delete_confirm = Some(branch_id.to_owned());
            self.branch_editing_id = None;
            self.state.status_text = "Confirm branch deletion".into();
        }
        cx.notify();
    }

    fn cancel_branch_delete(&mut self, cx: &mut Context<Self>) {
        self.branch_delete_confirm = None;
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

    fn select_provider(&mut self, provider: &str, window: &mut Window, cx: &mut Context<Self>) {
        let retries_failed_provider = provider == self.provider
            && matches!(self.state.connection, ConnectionState::Failed(_));
        if !self.composer_picker.prepare_provider_activation(
            provider,
            &self.provider,
            retries_failed_provider,
        ) {
            return;
        }
        self.restore_composer_focus_after_picker_close(window, cx);
        if !self.state.can_switch_provider() {
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

        self._provider_switch_task = Some(cx.spawn_in(window, async move |this, window| {
            let _ = shutdown_finished.recv_async().await;
            let _ = this.update_in(window, |this, window, cx| this.connect(config, window, cx));
        }));
        cx.notify();
    }

    fn can_open_composer_picker(&self, picker: ComposerPicker) -> bool {
        match picker {
            ComposerPicker::Provider => self.state.can_switch_provider(),
            ComposerPicker::Model => can_open_model_picker(&self.provider, self.state.can_send()),
            ComposerPicker::Thinking => self.state.can_switch_thinking(),
            ComposerPicker::Permission => self.state.active_interaction.is_none(),
        }
    }

    fn raw_composer_suggestion(&self, cx: &App) -> Option<ComposerSuggestion> {
        let value = self.input.read(cx).value().to_string();
        let skill_available =
            complete_skills(&value, &self.skill_catalog, CompletionLimits::default())
                .is_some_and(|completion| !completion.matches.is_empty());
        composer_suggestion_priority(
            self.slash_selection.visible && !self.slash_selection.matches.is_empty(),
            self.mention_token_start.is_some() && !self.mention_matches.is_empty(),
            skill_available,
        )
    }

    fn active_composer_suggestion(&self, cx: &App) -> Option<ComposerSuggestion> {
        let value = self.input.read(cx).value();
        self.composer_picker
            .suggestions_allowed_for(&value)
            .then(|| self.raw_composer_suggestion(cx))
            .flatten()
    }

    fn dismiss_input_suggestions(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(suggestion) = self.raw_composer_suggestion(cx) else {
            return false;
        };
        let value = self.input.read(cx).value().to_string();
        let changed = self.composer_picker.dismiss_suggestions_for(&value);
        match suggestion {
            ComposerSuggestion::Slash => {
                let _ = self.slash_selection.handle_key(SlashKey::Escape);
            }
            ComposerSuggestion::Mention => {
                self.mention_matches.clear();
                self.mention_token_start = None;
                self.mention_selected = 0;
            }
            ComposerSuggestion::Skill => {}
        }
        changed
    }

    fn set_input_suggestion_popover_open(
        &mut self,
        open: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let mut changed = false;
        if open {
            if self.composer_picker.active.is_some() {
                self.close_composer_picker(window, cx);
                changed = true;
            }
            if self.session_menu_open {
                self.session_menu_open = false;
                changed = true;
            }
        } else {
            changed = self.dismiss_input_suggestions(cx);
        }
        if changed {
            cx.notify();
        }
    }

    fn restore_composer_focus_after_picker_close(
        &mut self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.picker_search_input
            .update(cx, |input, cx| input.set_value("", window, cx));
        match picker_close_focus_target(
            self.settings_workspace_open,
            self.state.can_edit_composer(),
        ) {
            PickerCloseFocusTarget::Settings => self.settings_focus_handle.focus(window),
            PickerCloseFocusTarget::Composer => {
                self.input.update(cx, |input, cx| input.focus(window, cx));
            }
            PickerCloseFocusTarget::None => {}
        }
    }

    fn close_composer_picker(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if self.composer_picker.active.is_none() {
            return false;
        }
        self.composer_picker.close();
        self.restore_composer_focus_after_picker_close(window, cx);
        cx.notify();
        true
    }

    fn set_composer_picker_open(
        &mut self,
        picker: ComposerPicker,
        open: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !open {
            if self.composer_picker.active == Some(picker) {
                self.close_composer_picker(window, cx);
            }
            return;
        }
        if !self.can_open_composer_picker(picker) {
            return;
        }
        let previous = self.composer_picker.active;
        let dismissed_suggestion = self.dismiss_input_suggestions(cx);
        let should_focus_picker = self.composer_picker.set_open(picker, true);
        let changed = previous != self.composer_picker.active;
        self.session_menu_open = false;
        if should_focus_picker && picker == ComposerPicker::Thinking {
            self.composer_picker.search.highlighted = picker_highlight_for_value(
                &self.state.thinking_levels,
                &self.state.current_thinking,
            );
            self.input.update(cx, |input, cx| input.focus(window, cx));
        } else if should_focus_picker
            && matches!(picker, ComposerPicker::Provider | ComposerPicker::Model)
        {
            self.picker_search_input.update(cx, |input, cx| {
                input.set_value("", window, cx);
                input.focus(window, cx);
            });
        }
        if changed || dismissed_suggestion {
            cx.notify();
        }
    }

    fn toggle_picker(
        &mut self,
        picker: ComposerPicker,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let open = self.composer_picker.active != Some(picker);
        self.set_composer_picker_open(picker, open, window, cx);
    }

    fn active_picker_result_count(&self) -> usize {
        match self.composer_picker.active {
            Some(ComposerPicker::Provider) => {
                let catalog = self.provider_catalog();
                search_provider_catalog(&catalog, &self.composer_picker.search.query).len()
            }
            Some(ComposerPicker::Model)
                if can_open_model_picker(&self.provider, self.state.can_send()) =>
            {
                search_models(&self.state.models, &self.composer_picker.search.query).len()
                    + usize::from(
                        manual_model_id(&self.state.models, &self.composer_picker.search.query)
                            .is_some(),
                    )
            }
            Some(ComposerPicker::Thinking) => self.state.thinking_levels.len(),
            Some(ComposerPicker::Permission) => PERMISSION_MODES.len(),
            _ => 0,
        }
    }

    fn move_picker_highlight(&mut self, delta: isize, cx: &mut Context<Self>) {
        let result_count = self.active_picker_result_count();
        self.composer_picker
            .search
            .move_highlight(delta, result_count);
        cx.notify();
    }

    fn refresh_slash_selection(&mut self, value: &str) {
        self.composer_picker.note_suggestion_input(value);
        self.skill_selection.reset();
        self.slash_selection
            .refresh(value, &slash_command_catalog());
        let Some(discovery) = self.mention_discovery.as_ref() else {
            self.mention_matches.clear();
            self.mention_token_start = None;
            self.mention_selected = 0;
            return;
        };
        if let Some((query, token_start)) = mention_query(value) {
            self.mention_matches =
                match_mentions(&discovery.files, query, MAX_COMPOSER_COMPLETIONS);
            self.mention_token_start = Some(token_start);
            self.mention_selected = self
                .mention_selected
                .min(self.mention_matches.len().saturating_sub(1));
        } else {
            self.mention_matches.clear();
            self.mention_token_start = None;
            self.mention_selected = 0;
        }
    }

    fn handle_slash_key(
        &mut self,
        key: SlashKey,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        match self.slash_selection.handle_key(key) {
            SlashAction::Ignored => false,
            SlashAction::SelectionChanged | SlashAction::Dismissed => {
                cx.notify();
                true
            }
            SlashAction::Insert(value) => {
                self.input
                    .update(cx, |input, cx| input.set_value(&value, window, cx));
                cx.notify();
                true
            }
            SlashAction::Execute(value) => {
                self.input
                    .update(cx, |input, cx| input.set_value(&value, window, cx));
                self.submit(window, cx);
                true
            }
        }
    }

    fn select_mention_completion(
        &mut self,
        path: &str,
        token_start: usize,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let value = self.input.read(cx).value().to_string();
        let Some(value) = replace_mention_token(&value, token_start, path) else {
            self.state.last_error = Some("File mention completion is stale".into());
            cx.notify();
            return;
        };
        self.input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        self.refresh_slash_selection(&value);
        cx.notify();
    }

    fn complete_mention(
        &mut self,
        backwards: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        let Some(token_start) = self.mention_token_start else {
            return false;
        };
        let Some(path) =
            selected_mention_completion(&self.mention_matches, self.mention_selected, backwards)
                .map(str::to_owned)
        else {
            return false;
        };
        self.select_mention_completion(&path, token_start, window, cx);
        true
    }

    fn move_mention_selection(&mut self, delta: isize, cx: &mut Context<Self>) -> bool {
        if self.mention_matches.is_empty() {
            return false;
        }
        let len = self.mention_matches.len() as isize;
        self.mention_selected = (self.mention_selected as isize + delta).rem_euclid(len) as usize;
        cx.notify();
        true
    }

    fn select_skill_completion(
        &mut self,
        name: &str,
        token_start: usize,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let value = self.input.read(cx).value().to_string();
        let Some(value) = replace_skill_token(&value, token_start, name) else {
            self.state.last_error = Some("Agent Skill completion is stale".into());
            cx.notify();
            return;
        };
        self.input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        cx.notify();
    }

    fn complete_skill(
        &mut self,
        backwards: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        let value = self.input.read(cx).value().to_string();
        let Some(completion) =
            complete_skills(&value, &self.skill_catalog, CompletionLimits::default())
        else {
            return false;
        };
        self.skill_selection.normalize(completion.matches.len());
        let selected = if backwards {
            completion.matches.last()
        } else {
            completion.matches.get(self.skill_selection.selected)
        };
        let Some(skill) = selected else {
            return false;
        };
        let Some(value) = replace_skill_token(&value, completion.token_start, &skill.name) else {
            return false;
        };
        self.input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        self.skill_selection.reset();
        cx.notify();
        true
    }

    fn complete_highlighted_skill(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if self.active_composer_suggestion(cx) != Some(ComposerSuggestion::Skill) {
            return false;
        }
        self.complete_skill(false, window, cx)
    }

    fn move_skill_selection(&mut self, delta: isize, cx: &App) -> bool {
        if self.active_composer_suggestion(cx) != Some(ComposerSuggestion::Skill) {
            return false;
        }
        let value = self.input.read(cx).value().to_string();
        let result_count =
            complete_skills(&value, &self.skill_catalog, CompletionLimits::default())
                .map(|completion| completion.matches.len())
                .unwrap_or_default();
        self.skill_selection
            .move_selection(delta, result_count.min(MAX_COMPOSER_COMPLETIONS));
        true
    }

    fn picker_up(&mut self, _: &PickerUp, window: &mut Window, cx: &mut Context<Self>) {
        if self.handle_slash_key(SlashKey::Up, window, cx) {
            return;
        }
        if self.move_mention_selection(-1, cx) {
            return;
        }
        if self.move_skill_selection(-1, cx) {
            cx.notify();
            return;
        }
        self.move_picker_highlight(-1, cx);
    }

    fn picker_down(&mut self, _: &PickerDown, window: &mut Window, cx: &mut Context<Self>) {
        if self.handle_slash_key(SlashKey::Down, window, cx) {
            return;
        }
        if self.move_mention_selection(1, cx) {
            return;
        }
        if self.move_skill_selection(1, cx) {
            cx.notify();
            return;
        }
        self.move_picker_highlight(1, cx);
    }

    fn composer_tab(&mut self, _: &ComposerTab, window: &mut Window, cx: &mut Context<Self>) {
        if self.handle_slash_key(SlashKey::Tab, window, cx) {
            return;
        }
        if self.complete_mention(false, window, cx) {
            return;
        }
        self.complete_skill(false, window, cx);
    }

    fn composer_back_tab(
        &mut self,
        _: &ComposerBackTab,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.handle_slash_key(SlashKey::BackTab, window, cx) {
            return;
        }
        if self.complete_mention(true, window, cx) {
            return;
        }
        self.complete_skill(true, window, cx);
    }

    fn paste_composer(&mut self, _: &PasteComposer, window: &mut Window, cx: &mut Context<Self>) {
        let Some(item) = cx.read_from_clipboard() else {
            return;
        };
        if let Some(text) = item.text() {
            let insertion = match self.paste_store.collapse(text.clone()) {
                Ok(Some(token)) => token,
                Ok(None) => text,
                Err(error) => {
                    self.state.status_text = "Could not paste".into();
                    self.state.last_error = Some(error.to_string());
                    cx.notify();
                    return;
                }
            };
            self.input
                .update(cx, |input, cx| input.insert(insertion, window, cx));
            let value = self.input.read(cx).value().to_string();
            self.paste_store.prune(&value);
            self.refresh_slash_selection(&value);
            self.state.status_text = if self.paste_store.attachments().is_empty() {
                "Pasted text".into()
            } else {
                "Attached large paste".into()
            };
            self.state.last_error = None;
            cx.notify();
            return;
        }
        self.load_attachment(AttachmentSource::Clipboard, window, cx);
    }

    fn remove_collapsed_paste(&mut self, id: u64, window: &mut Window, cx: &mut Context<Self>) {
        let mut value = self.input.read(cx).value().to_string();
        if self.paste_store.remove(&mut value, id) {
            self.input
                .update(cx, |input, cx| input.set_value(&value, window, cx));
            self.refresh_slash_selection(&value);
            self.state.status_text = "Removed pasted text".into();
            self.state.last_error = None;
        }
        cx.notify();
    }

    fn dismiss_picker(&mut self, _: &PickerDismiss, window: &mut Window, cx: &mut Context<Self>) {
        if self.active_composer_suggestion(cx).is_some() {
            self.set_input_suggestion_popover_open(false, window, cx);
            return;
        }
        if self.session_menu_open {
            self.set_session_menu_open(false, window, cx);
            return;
        }
        if let Some(picker) = self.composer_picker.active {
            self.set_composer_picker_open(picker, false, window, cx);
        }
    }

    fn remember_input(&mut self, input: &str) {
        let input = normalize_public_text(input.trim(), MAX_INPUT_HISTORY_CHARS);
        if input.is_empty() {
            return;
        }
        if self
            .input_history
            .back()
            .is_none_or(|latest| latest != &input)
        {
            self.input_history.push_back(input);
            while self.input_history.len() > INPUT_HISTORY_LIMIT {
                self.input_history.pop_front();
            }
        }
        self.input_history_index = None;
        self.input_history_draft.clear();
    }

    fn replace_input_history(&mut self, history: &[HistoryEntry]) {
        self.input_history = hydrated_input_history(history);
        self.input_history_index = None;
        self.input_history_draft.clear();
    }

    fn append_input_history_page(&mut self, history: &[HistoryEntry], reset: bool) {
        if reset {
            self.input_history.clear();
        }
        for input in hydrated_input_history(history) {
            if self
                .input_history
                .back()
                .is_none_or(|latest| latest != &input)
            {
                self.input_history.push_back(input);
                while self.input_history.len() > INPUT_HISTORY_LIMIT {
                    self.input_history.pop_front();
                }
            }
        }
        self.input_history_index = None;
        self.input_history_draft.clear();
    }

    fn reconcile_prompt_submissions(
        &mut self,
        events: &[RuntimeEvent],
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        for event in events {
            let request_id = match event {
                RuntimeEvent::PromptAdmitted { request_id } => request_id,
                RuntimeEvent::RequestRejected {
                    request_id: Some(request_id),
                    ..
                } => {
                    if self
                        .pending_prompt_submission
                        .as_ref()
                        .is_some_and(|pending| &pending.request_id == request_id)
                    {
                        self.pending_prompt_submission = None;
                    }
                    continue;
                }
                _ => continue,
            };
            let Some(pending) = self
                .pending_prompt_submission
                .take_if(|pending| &pending.request_id == request_id)
            else {
                continue;
            };
            if pending.remember_on_admit {
                self.remember_input(&pending.draft);
            }
            if !pending.clear_composer_on_admit {
                continue;
            }
            let current = self.input.read(cx).value().to_string();
            if current == pending.draft {
                self.input
                    .update(cx, |input, cx| input.set_value("", window, cx));
            }
            if self.attachments == pending.attachments {
                self.attachments.clear();
            }
            let retained = self.input.read(cx).value().to_string();
            self.paste_store.prune(&retained);
        }
    }

    fn finish_input_command(
        &mut self,
        draft: Option<String>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(draft) = draft else {
            return;
        };
        self.remember_input(&draft);
        if self.input.read(cx).value() == draft {
            self.input
                .update(cx, |input, cx| input.set_value("", window, cx));
        }
        let retained = self.input.read(cx).value().to_string();
        self.paste_store.prune(&retained);
    }

    fn current_plan_nudge_scope(&self) -> Option<PlanNudgeScope> {
        let session_id = self.state.session_id.trim();
        if session_id.is_empty() {
            return None;
        }
        let branch_id = self
            .state
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| branch.id.trim())
            .filter(|branch| !branch.is_empty())?;
        Some(PlanNudgeScope {
            session_id: session_id.into(),
            branch_id: branch_id.into(),
        })
    }

    fn sync_plan_nudge_scope(&mut self) {
        let scope = self.current_plan_nudge_scope();
        self.plan_nudge_dismissed = self.plan_nudge_dismissals.is_dismissed(scope.as_ref());
    }

    fn history_previous(
        &mut self,
        _: &HistoryPrevious,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.input_history.is_empty() || self.composer_picker.active.is_some() {
            return;
        }
        let index = match self.input_history_index {
            Some(index) => index.saturating_sub(1),
            None => {
                self.input_history_draft = self.input.read(cx).value().to_string();
                self.input_history.len() - 1
            }
        };
        self.input_history_index = Some(index);
        let value = self.input_history[index].clone();
        self.input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        cx.notify();
    }

    fn history_next(&mut self, _: &HistoryNext, window: &mut Window, cx: &mut Context<Self>) {
        let Some(index) = self.input_history_index else {
            return;
        };
        if index + 1 < self.input_history.len() {
            let next = index + 1;
            self.input_history_index = Some(next);
            let value = self.input_history[next].clone();
            self.input
                .update(cx, |input, cx| input.set_value(&value, window, cx));
        } else {
            self.input_history_index = None;
            let value = std::mem::take(&mut self.input_history_draft);
            self.input
                .update(cx, |input, cx| input.set_value(&value, window, cx));
        }
        cx.notify();
    }

    fn activate_highlighted_picker(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let result_count = self.active_picker_result_count();
        self.composer_picker
            .search
            .normalize_highlight(result_count);
        match self.composer_picker.active {
            Some(ComposerPicker::Provider) => {
                let catalog = self.provider_catalog();
                let results = search_provider_catalog(&catalog, &self.composer_picker.search.query);
                if let Some(provider) = results
                    .get(self.composer_picker.search.highlighted)
                    .and_then(|index| catalog.get(*index))
                    .map(|provider| provider.id.clone())
                {
                    self.select_provider(&provider, window, cx);
                }
            }
            Some(ComposerPicker::Model)
                if can_open_model_picker(&self.provider, self.state.can_send()) =>
            {
                let results = search_models(&self.state.models, &self.composer_picker.search.query);
                let model = results
                    .get(self.composer_picker.search.highlighted)
                    .and_then(|index| self.state.models.get(*index))
                    .map(|model| model.id.clone())
                    .or_else(|| {
                        manual_model_id(&self.state.models, &self.composer_picker.search.query)
                    });
                if let Some(model) = model {
                    self.select_model(&model, window, cx);
                }
            }
            Some(ComposerPicker::Thinking) => {
                let level = self
                    .state
                    .thinking_levels
                    .get(self.composer_picker.search.highlighted)
                    .cloned();
                if let Some(level) = level {
                    self.select_thinking(&level, window, cx);
                }
            }
            Some(ComposerPicker::Permission) => {
                if let Some(mode) = PERMISSION_MODES.get(self.composer_picker.search.highlighted) {
                    self.select_permission_mode(mode, window, cx);
                }
            }
            _ => {}
        }
    }

    fn select_command_completion(
        &mut self,
        command: &str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let value = format!("{command} ");
        self.input
            .update(cx, |input, cx| input.set_value(&value, window, cx));
        self.input.update(cx, |input, cx| input.focus(window, cx));
        cx.notify();
    }

    fn select_model(&mut self, model: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.close_composer_picker(window, cx);
        if !self.state.can_select_model(model) {
            cx.notify();
            return;
        }
        let Some(runtime) = &self.runtime else {
            cx.notify();
            return;
        };
        let target = self
            .state
            .models
            .iter()
            .find(|candidate| candidate.id == model);
        let thinking = if let Some(target) = target {
            let levels = model_thinking_levels(target);
            if levels.contains(&self.state.current_thinking) {
                self.state.current_thinking.clone()
            } else if levels.contains(&target.default_thinking) {
                target.default_thinking.clone()
            } else {
                "off".into()
            }
        } else {
            // Manual IDs have no trusted capability metadata. Select the model and
            // disable thinking atomically so stale model metadata cannot leak through.
            "off".into()
        };
        match runtime.set_model_thinking(model.trim().to_owned(), thinking.clone()) {
            Ok(request_id) => self
                .state
                .begin_model_change(request_id, model.trim(), thinking),
            Err(error) => {
                self.state.status_text = "Could not select model".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn select_thinking(&mut self, level: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.close_composer_picker(window, cx);
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

    fn select_permission_mode(&mut self, mode: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.close_composer_picker(window, cx);
        if self.state.active_interaction.is_some() || !PERMISSION_MODES.contains(&mode) {
            cx.notify();
            return;
        }
        self.run_silent_rpc_command(RpcCommand {
            name: "permission_mode_set".into(),
            fields: serde_json::Map::from_iter([(
                "params".into(),
                serde_json::json!({"mode": mode}),
            )]),
            refresh_runtime: true,
        });
        cx.notify();
    }

    fn toggle_collaboration_mode(&mut self, cx: &mut Context<Self>) {
        let mode = if self.state.collaboration_mode == "plan" {
            "default"
        } else {
            "plan"
        };
        self.run_rpc_command(RpcCommand {
            name: "set_mode".into(),
            fields: serde_json::Map::from_iter([(
                "mode".into(),
                serde_json::Value::String(mode.into()),
            )]),
            refresh_runtime: true,
        });
        cx.notify();
    }

    fn semantic_submit(
        &mut self,
        _: &presentation_runtime::SubmitAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.submit(window, cx);
    }

    fn semantic_follow_up(
        &mut self,
        _: &presentation_runtime::FollowUpAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.submit_follow_up(window, cx);
    }

    fn semantic_newline(
        &mut self,
        _: &presentation_runtime::NewlineAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.input
            .update(cx, |input, cx| input.insert("\n", window, cx));
    }

    fn semantic_paste(
        &mut self,
        _: &presentation_runtime::PasteAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.paste_composer(&PasteComposer, window, cx);
    }

    fn semantic_abort(
        &mut self,
        _: &presentation_runtime::AbortAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.abort(cx);
    }

    fn semantic_quit(
        &mut self,
        _: &presentation_runtime::QuitAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        cx.quit();
    }

    fn semantic_toggle_mode(
        &mut self,
        _: &presentation_runtime::ToggleModeAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.toggle_collaboration_mode(cx);
    }

    fn semantic_thinking(
        &mut self,
        _: &presentation_runtime::ThinkingAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.settings_workspace_open {
            self.settings_section = SettingsSection::General;
            self.close_composer_picker(window, cx);
            cx.notify();
            return;
        }
        self.toggle_picker(ComposerPicker::Thinking, window, cx);
    }

    fn semantic_models(
        &mut self,
        _: &presentation_runtime::ModelsAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.settings_workspace_open {
            self.settings_section = SettingsSection::General;
            if self.settings_panel.is_none() {
                self.close_composer_picker(window, cx);
                cx.notify();
                return;
            }
        }
        self.toggle_picker(ComposerPicker::Model, window, cx);
    }

    fn semantic_agents(
        &mut self,
        _: &presentation_runtime::AgentsAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.subagent_fleet.set_path_prefix("");
        self.pending_subagent_target = None;
        self.refresh_subagents(cx);
    }

    fn semantic_processes(
        &mut self,
        _: &presentation_runtime::ProcessesAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.pending_process_target = None;
        self.request_process_list();
        cx.notify();
    }

    fn scroll_transcript_to_bottom(&self) {
        self.transcript_list.scroll_to(ListOffset {
            item_ix: self.state.messages.len(),
            offset_in_item: px(0.),
        });
    }

    fn toggle_tool_card(&mut self, card_id: u64, message_index: usize, cx: &mut Context<Self>) {
        if !self.expanded_tool_cards.remove(&card_id) {
            self.expanded_tool_cards.insert(card_id);
        }
        if message_index < self.transcript_list.item_count() {
            self.transcript_list
                .splice(message_index..message_index + 1, 1);
        }
        cx.notify();
    }

    fn scroll_transcript_by(&mut self, delta: f32, cx: &mut Context<Self>) {
        // The previous ScrollHandle stored negative pixel offsets, while
        // ListState::scroll_by uses positive distances to move down.
        self.transcript_list.scroll_by(px(-delta));
        cx.notify();
    }

    fn semantic_page_up(
        &mut self,
        _: &presentation_runtime::PageUpAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let page = self.transcript_list.viewport_bounds().size.height.to_f64() as f32 * 0.85;
        self.scroll_transcript_by(page, cx);
    }

    fn semantic_page_down(
        &mut self,
        _: &presentation_runtime::PageDownAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let page = self.transcript_list.viewport_bounds().size.height.to_f64() as f32 * 0.85;
        self.scroll_transcript_by(-page, cx);
    }

    fn semantic_top(
        &mut self,
        _: &presentation_runtime::TopAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.transcript_list.scroll_to(ListOffset {
            item_ix: 0,
            offset_in_item: px(0.),
        });
        cx.notify();
    }

    fn semantic_bottom(
        &mut self,
        _: &presentation_runtime::BottomAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.scroll_transcript_to_bottom();
        cx.notify();
    }

    fn semantic_line_up(
        &mut self,
        _: &presentation_runtime::LineUpAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.scroll_transcript_by(48., cx);
    }

    fn semantic_line_down(
        &mut self,
        _: &presentation_runtime::LineDownAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.scroll_transcript_by(-48., cx);
    }

    fn semantic_picker_up(
        &mut self,
        _: &presentation_runtime::PickerUpAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.picker_up(&PickerUp, window, cx);
    }

    fn semantic_picker_down(
        &mut self,
        _: &presentation_runtime::PickerDownAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.picker_down(&PickerDown, window, cx);
    }

    fn move_semantic_picker(&mut self, delta: isize, cx: &mut Context<Self>) {
        self.move_picker_highlight(delta, cx);
    }

    fn semantic_picker_previous(
        &mut self,
        _: &presentation_runtime::PickerPreviousAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.move_semantic_picker(-1, cx);
    }

    fn semantic_picker_next(
        &mut self,
        _: &presentation_runtime::PickerNextAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.move_semantic_picker(1, cx);
    }

    fn semantic_picker_page_up(
        &mut self,
        _: &presentation_runtime::PickerPageUpAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.move_semantic_picker(-8, cx);
    }

    fn semantic_picker_page_down(
        &mut self,
        _: &presentation_runtime::PickerPageDownAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.move_semantic_picker(8, cx);
    }

    fn semantic_picker_top(
        &mut self,
        _: &presentation_runtime::PickerTopAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.composer_picker.search.highlighted = 0;
        cx.notify();
    }

    fn semantic_picker_bottom(
        &mut self,
        _: &presentation_runtime::PickerBottomAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.composer_picker.search.highlighted =
            self.active_picker_result_count().saturating_sub(1);
        cx.notify();
    }

    fn semantic_accept(
        &mut self,
        _: &presentation_runtime::AcceptAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.composer_picker.active.is_some() {
            self.activate_highlighted_picker(window, cx);
        } else {
            self.submit(window, cx);
        }
    }

    fn semantic_close(
        &mut self,
        _: &presentation_runtime::CloseAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.dismiss_picker(&PickerDismiss, window, cx);
    }

    fn semantic_branch_fork(
        &mut self,
        _: &presentation_runtime::BranchForkAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.fork_branch(cx);
    }

    fn semantic_branch_rename(
        &mut self,
        _: &presentation_runtime::BranchRenameAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if let Some(branch) = self.state.branches.iter().find(|branch| branch.active) {
            let id = branch.id.clone();
            let name = branch.name.clone();
            self.begin_branch_rename(&id, &name, window, cx);
        }
    }

    fn semantic_branch_delete(
        &mut self,
        _: &presentation_runtime::BranchDeleteAction,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if let Some(id) = self
            .state
            .branches
            .iter()
            .find(|branch| branch.active)
            .map(|branch| branch.id.clone())
        {
            self.request_branch_delete(&id, cx);
        }
    }

    fn semantic_confirm(
        &mut self,
        _: &presentation_runtime::ConfirmAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.branch_editing_id.is_some() {
            self.confirm_branch_rename(cx);
        } else if let Some(id) = self.branch_delete_confirm.clone() {
            self.request_branch_delete(&id, cx);
        } else if self.composer_picker.active.is_some() {
            self.activate_highlighted_picker(window, cx);
        }
    }

    fn resolve_permission(&mut self, decision: PermissionDecision, cx: &mut Context<Self>) {
        let Some(request_id) =
            self.state
                .active_interaction
                .as_ref()
                .and_then(|active| match active {
                    ActiveInteraction::Permission(interaction) => {
                        Some(interaction.request.id.clone())
                    }
                    _ => None,
                })
        else {
            return;
        };
        if !self.state.can_select_permission_decision(&request_id) {
            return;
        }
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.permission_reply(request_id.clone(), decision) {
            Ok(command_id) => {
                self.state
                    .begin_permission_command(&request_id, decision, command_id);
            }
            Err(error) => {
                self.state.status_text = "Could not submit permission decision".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn select_user_input_option(&mut self, label: &str, cx: &mut Context<Self>) {
        self.state.select_user_input_option(label);
        cx.notify();
    }

    fn select_user_input_other(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.state.select_user_input_other();
        let draft = self
            .state
            .current_user_input()
            .map(|interaction| interaction.draft().other.clone())
            .unwrap_or_default();
        self.interaction_input
            .update(cx, |input, cx| input.set_value(&draft, window, cx));
        cx.notify();
    }

    fn move_user_input_question(
        &mut self,
        delta: isize,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !self.state.move_user_input_question(delta) {
            return;
        }
        let draft = self
            .state
            .current_user_input()
            .filter(|interaction| interaction.draft().use_other)
            .map(|interaction| interaction.draft().other.clone())
            .unwrap_or_default();
        self.interaction_input
            .update(cx, |input, cx| input.set_value(&draft, window, cx));
        cx.notify();
    }

    fn submit_user_input(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        let Some((request_id, answers)) = self.state.user_input_answers() else {
            cx.notify();
            return;
        };
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.user_input_reply(request_id.clone(), answers) {
            Ok(command_id) => {
                self.state
                    .begin_interaction_command(&request_id, command_id, "user_input_reply");
            }
            Err(error) => {
                self.state.status_text = "Could not submit answers".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn decline_user_input(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        let Some(request_id) = self.state.current_user_input().and_then(|interaction| {
            interaction
                .pending
                .is_none()
                .then(|| interaction.request.id.clone())
        }) else {
            return;
        };
        let Some(runtime) = &self.runtime else {
            return;
        };
        match runtime.user_input_reject(request_id.clone()) {
            Ok(command_id) => {
                self.state
                    .begin_interaction_command(&request_id, command_id, "user_input_reject");
            }
            Err(error) => {
                self.state.status_text = "Could not decline questions".into();
                self.state.last_error = Some(error.to_string());
            }
        }
        cx.notify();
    }

    fn apply_command_completion(
        &mut self,
        completion: CompletedDesktopCommand,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let CompletedDesktopCommand {
            command,
            data,
            show_result,
            silent,
            process_request,
            subagent_request,
        } = completion;
        let command = command.as_str();
        match command {
            "skills" | "skills_clear" => {
                let resource_data = show_result.then(|| data.clone()).flatten();
                let result = data
                    .ok_or_else(|| format!("{command} returned no data"))
                    .and_then(|data| {
                        let catalog = if command == "skills_clear" {
                            data.get("catalog")
                                .cloned()
                                .ok_or_else(|| "skills_clear response omitted catalog".to_owned())?
                        } else {
                            data
                        };
                        decode_skill_catalog(catalog)
                    });
                match result {
                    Ok(skills) => {
                        self.skill_catalog = skills;
                        let value = self.input.read(cx).value().to_string();
                        self.refresh_slash_selection(&value);
                        if let Some(data) = resource_data {
                            match project_rpc_resource(command, &data) {
                                Ok(resource) => {
                                    self.resource_panel = Some(ResourcePanel {
                                        command: command.into(),
                                        resource,
                                    });
                                    self.sessions_panel_open = false;
                                    self.processes_panel_open = false;
                                    self.subagents_panel_open = false;
                                    self.state.last_error = None;
                                }
                                Err(error) => {
                                    self.state.status_text =
                                        "Could not display Agent Skills safely".into();
                                    self.state.last_error = Some(error.to_string());
                                }
                            }
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not load Agent Skills".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "settings_get" | "settings_update" => {
                let result = data
                    .ok_or_else(|| format!("{command} returned no data"))
                    .and_then(|data| {
                        serde_json::from_value::<Settings>(data)
                            .map_err(|error| format!("invalid {command} response: {error}"))
                    });
                match result {
                    Ok(settings) => {
                        self.settings_concurrency_input.update(cx, |input, cx| {
                            input.set_value(
                                settings.subagents_max_concurrent.to_string(),
                                window,
                                cx,
                            )
                        });
                        if !settings.theme.is_empty() {
                            let _ = presentation_runtime::select_theme(&settings.theme, cx);
                        }
                        self.settings_panel = Some(settings);
                        self.resource_panel = None;
                        self.auth_panel_open = false;
                        if command == "settings_update" {
                            self.refresh_presentation_resources();
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not load settings".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "themes_list" => {
                let result = data
                    .ok_or_else(|| "themes_list returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<ThemeCatalog>(data)
                            .map_err(|error| format!("invalid themes_list response: {error}"))
                    });
                match result {
                    Ok(catalog) => {
                        match presentation_runtime::apply_theme_catalog(catalog.clone(), cx) {
                            Ok(()) => {
                                self.theme_catalog = Some(catalog);
                                self.state.status_text = "Theme catalog loaded".into();
                                self.state.last_error = None;
                            }
                            Err(error) => {
                                self.state.status_text = "Could not apply theme catalog".into();
                                self.state.last_error = Some(error.to_string());
                            }
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not load themes".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "keybindings_get" | "keybindings_update" => {
                let result = data
                    .ok_or_else(|| format!("{command} returned no data"))
                    .and_then(|data| {
                        serde_json::from_value::<Keybindings>(data)
                            .map_err(|error| format!("invalid {command} response: {error}"))
                    });
                match result {
                    Ok(bindings) => {
                        match presentation_runtime::apply_keybindings(bindings.clone(), cx) {
                            Ok(()) => {
                                self.keybindings = Some(bindings);
                                self.state.status_text = "Keybindings loaded".into();
                                self.state.last_error = None;
                            }
                            Err(error) => {
                                self.state.status_text = "Could not apply keybindings".into();
                                self.state.last_error = Some(error.to_string());
                            }
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not load keybindings".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "auth_providers" => {
                let result = data
                    .ok_or_else(|| "auth_providers returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<AuthProviderList>(data)
                            .map_err(|error| format!("invalid auth_providers response: {error}"))
                    });
                match result {
                    Ok(list) => {
                        let open_panel = self.auth_inventory_opens_panel || self.auth_panel_open;
                        self.auth_inventory_opens_panel = false;
                        self.auth_providers = user_visible_auth_providers(list.providers);
                        let preferred = self
                            .auth_selected_provider
                            .as_ref()
                            .filter(|provider| {
                                self.auth_providers
                                    .iter()
                                    .any(|candidate| &candidate.provider_id == *provider)
                            })
                            .cloned()
                            .or_else(|| {
                                self.auth_providers
                                    .iter()
                                    .find(|candidate| candidate.provider_id == self.provider)
                                    .map(|candidate| candidate.provider_id.clone())
                            })
                            .or_else(|| {
                                self.auth_providers
                                    .first()
                                    .map(|candidate| candidate.provider_id.clone())
                            });
                        self.auth_selected_provider = preferred;
                        self.ensure_auth_method_selection();
                        self.auth_panel_open = open_panel;
                        if open_panel {
                            self.resource_panel = None;
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not load authentication providers".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "auth_login_start" | "auth_profile_set" | "auth_login_status" | "auth_login_cancel" => {
                let result = data
                    .ok_or_else(|| format!("{command} returned no data"))
                    .and_then(|data| {
                        serde_json::from_value::<AuthLoginJob>(data)
                            .map_err(|error| format!("invalid {command} response: {error}"))
                    });
                match result {
                    Ok(job) => {
                        let terminal = job.state != "running";
                        self.auth_job = Some(job);
                        if terminal {
                            self.run_rpc_command(RpcCommand {
                                name: "auth_providers".into(),
                                fields: serde_json::Map::new(),
                                refresh_runtime: false,
                            });
                        }
                    }
                    Err(error) => {
                        self.state.status_text = "Could not update authentication".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "auth_logout" => {
                self.run_rpc_command(RpcCommand {
                    name: "auth_providers".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            "sessions_list" => {
                let result = data
                    .ok_or_else(|| "sessions_list returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<SessionList>(data)
                            .map_err(|error| format!("invalid sessions_list response: {error}"))
                    });
                match result {
                    Ok(list) => apply_session_inventory(
                        &mut self.sessions,
                        &mut self.sessions_panel_open,
                        &mut self.session_menu_open,
                        list,
                        silent,
                    ),
                    Err(error) => {
                        self.state.status_text = "Could not load sessions".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "session_create" | "session_open" | "session_delete" => {
                self.run_silent_rpc_command(RpcCommand {
                    name: "sessions_list".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            "processes_list" => {
                let Some(PendingProcessRequest::List(metadata)) = process_request else {
                    return;
                };
                let result = data
                    .ok_or_else(|| "processes_list returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<ManagedProcessList>(data)
                            .map_err(|error| format!("invalid processes_list response: {error}"))
                    });
                match result {
                    Ok(list) => {
                        if self
                            .process_live
                            .apply_list_response(metadata, list.processes)
                            != ProcessResponseDisposition::Applied
                        {
                            return;
                        }
                        self.processes_panel_open = true;
                        self.sessions_panel_open = false;
                        self.subagents_panel_open = false;
                        self.subagent_poll_task = None;

                        if let Some(target) = self.pending_process_target.take() {
                            let process_id = self
                                .process_live
                                .processes()
                                .iter()
                                .find(|process| {
                                    process.process_id == target || process.name == target
                                })
                                .map(|process| process.process_id.clone());
                            if let Some(process_id) = process_id {
                                self.process_live.select_process(&process_id);
                                self.process_detail_open = true;
                            } else {
                                self.state.last_error =
                                    Some("managed process target was not found".into());
                            }
                        }
                        self.request_next_process_logs();
                        self.start_process_polling(window, cx);
                    }
                    Err(error) => {
                        self.process_live.finish_list_request(metadata);
                        self.state.status_text = "Could not load processes".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "subagent_list" => {
                let Some(PendingSubagentRequest::List(request)) = subagent_request else {
                    return;
                };
                let result = data
                    .ok_or_else(|| "subagent_list returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<SubagentList>(data)
                            .map_err(|error| format!("invalid subagent_list response: {error}"))
                    });
                match result {
                    Ok(list) => match self
                        .subagent_fleet
                        .apply_list_response(&request, FleetListSnapshot::from(list))
                    {
                        SubagentListApply::Stale => {}
                        SubagentListApply::TargetMissing => {
                            self.subagents_panel_open = true;
                            self.state.last_error =
                                Some("requested subagent target was not found".into());
                        }
                        SubagentListApply::Applied => {
                            self.subagents_panel_open = true;
                            self.sessions_panel_open = false;
                            self.processes_panel_open = false;
                            self.process_poll_task = None;
                            self.request_selected_subagent_detail();
                            let selection_changed = self.subagent_transcript.selection.as_ref()
                                != self.subagent_fleet.selection();
                            if selection_changed {
                                self.start_selected_subagent_history();
                            }
                            self.start_subagent_polling(window, cx);
                        }
                    },
                    Err(error) => {
                        self.state.status_text = "Could not load subagents".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "subagent_get" => {
                let Some(PendingSubagentRequest::Detail(request)) = subagent_request else {
                    return;
                };
                let result = data
                    .ok_or_else(|| "subagent_get returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<SubagentState>(data)
                            .map_err(|error| format!("invalid subagent_get response: {error}"))
                    });
                match result {
                    Ok(state) => match self.subagent_fleet.apply_detail_response(
                        &request,
                        VersionedSubagentState::new(state, request.generation),
                    ) {
                        SubagentDetailApply::Applied => {
                            self.state.status_text = "Subagent detail loaded".into();
                            self.state.last_error = None;
                        }
                        SubagentDetailApply::Stale => {}
                        SubagentDetailApply::WrongAgent => {
                            self.state.status_text = "Could not load subagent detail".into();
                            self.state.last_error =
                                Some("subagent detail identity did not match selection".into());
                        }
                    },
                    Err(error) => {
                        self.state.status_text = "Could not load subagent detail".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            "subagent_messages" => {
                let Some(PendingSubagentRequest::Messages {
                    generation,
                    selection,
                    cursor,
                }) = subagent_request
                else {
                    return;
                };
                self.apply_subagent_messages_page(generation, selection, cursor, data);
            }
            "subagent_interrupt" | "subagent_close" | "subagent_resume" => {
                self.refresh_subagents(cx);
            }
            "process_logs" => {
                let Some(PendingProcessRequest::Logs(metadata)) = process_request else {
                    return;
                };
                let result = data
                    .ok_or_else(|| "process_logs returned no data".to_owned())
                    .and_then(|data| {
                        serde_json::from_value::<ManagedProcessLogs>(data)
                            .map_err(|error| format!("invalid process_logs response: {error}"))
                    });
                match result {
                    Ok(logs) => match self.process_live.apply_log_response(metadata, logs) {
                        ProcessResponseDisposition::Applied => {
                            self.state.status_text = "Watching managed processes".into();
                            self.state.last_error = None;
                            if should_stop_process_poll(
                                ProcessResponseDisposition::Applied,
                                self.process_live.terminal_eof(),
                            ) {
                                self.process_poll_task = None;
                            }
                        }
                        ProcessResponseDisposition::Stale => {}
                        ProcessResponseDisposition::Invalid => {
                            self.state.status_text = "Could not load process logs".into();
                            self.state.last_error =
                                Some("managed process log response was inconsistent".into());
                        }
                    },
                    Err(error) => {
                        self.process_live.finish_log_request(metadata);
                        self.state.status_text = "Could not load process logs".into();
                        self.state.last_error = Some(error);
                    }
                }
            }
            _ => {
                if let Some(data) = data {
                    if command == "usage"
                        && let Ok(dto) = serde_json::from_value::<UsageDto>(data.clone())
                        && let Ok(telemetry) = dto.project()
                    {
                        self.usage_telemetry = Some(telemetry);
                    }
                    if command == "context"
                        && let Ok(dto) = serde_json::from_value::<ContextTelemetryDto>(data.clone())
                        && let Ok(telemetry) = dto.project()
                    {
                        self.context_telemetry = Some(telemetry);
                    }
                    if show_result {
                        match project_rpc_resource(command, &data) {
                            Ok(resource) => {
                                self.resource_panel = Some(ResourcePanel {
                                    command: command.into(),
                                    resource,
                                });
                                self.sessions_panel_open = false;
                                self.processes_panel_open = false;
                                self.subagents_panel_open = false;
                                self.state.last_error = None;
                            }
                            Err(error) if resource_panel_title(command).is_some() => {
                                self.state.status_text = "Could not display resource safely".into();
                                self.state.last_error = Some(error.to_string());
                            }
                            Err(_) => {}
                        }
                    }
                }
            }
        }
    }

    fn command_pending(&self, name: &str) -> bool {
        self.pending_commands
            .values()
            .any(|pending| pending.name == name)
    }

    fn start_process_polling(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.process_poll_task.is_some() {
            return;
        }
        let executor = cx.background_executor().clone();
        self.process_poll_task = Some(cx.spawn_in(window, async move |this, window| {
            loop {
                executor.timer(LIVE_PANEL_POLL_INTERVAL).await;
                let keep_polling = this
                    .update_in(window, |this, _, cx| {
                        if !this.processes_panel_open {
                            return false;
                        }
                        match this.process_live.next_poll_decision() {
                            NextPollDecision::StopTerminalEof => false,
                            NextPollDecision::WaitForResponse => true,
                            NextPollDecision::Schedule => {
                                this.refresh_processes(cx);
                                true
                            }
                        }
                    })
                    .unwrap_or(false);
                if !keep_polling {
                    break;
                }
            }
        }));
    }

    fn start_subagent_polling(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.subagent_poll_task.is_some() {
            return;
        }
        let executor = cx.background_executor().clone();
        self.subagent_poll_task = Some(cx.spawn_in(window, async move |this, window| {
            loop {
                executor.timer(LIVE_PANEL_POLL_INTERVAL).await;
                let keep_polling = this
                    .update_in(window, |this, _, cx| {
                        if !this.subagents_panel_open {
                            return false;
                        }
                        this.refresh_subagents(cx);
                        true
                    })
                    .unwrap_or(false);
                if !keep_polling {
                    break;
                }
            }
        }));
    }

    fn run_rpc_command(&mut self, command: RpcCommand) -> bool {
        self.run_rpc_command_internal(command, None, false, false)
    }

    fn run_silent_rpc_command(&mut self, command: RpcCommand) -> bool {
        self.run_rpc_command_internal(command, None, false, true)
    }

    fn run_presented_rpc_command(&mut self, command: RpcCommand) -> bool {
        self.run_rpc_command_internal(command, None, true, false)
    }

    fn run_rpc_command_with_input(&mut self, command: RpcCommand, draft: String) -> bool {
        self.run_rpc_command_internal(command, Some(draft), false, false)
    }

    fn run_rpc_command_internal(
        &mut self,
        command: RpcCommand,
        input_draft: Option<String>,
        show_result: bool,
        silent: bool,
    ) -> bool {
        if command.name == "project_init" {
            let Some(runtime) = &self.runtime else {
                self.state.last_error = Some("Snow is not connected".into());
                return false;
            };
            return match runtime.project_init() {
                Ok(request_id) => {
                    self.pending_prompt_submission = Some(PendingPromptSubmission {
                        request_id: request_id.clone(),
                        draft: input_draft.unwrap_or_else(|| "/init".into()),
                        attachments: self.attachments.clone(),
                        clear_composer_on_admit: true,
                        remember_on_admit: true,
                    });
                    self.state.begin_prompt(request_id, "/init".into());
                    self.scroll_transcript_to_bottom();
                    true
                }
                Err(error) => {
                    self.state.status_text = "Could not initialize project".into();
                    self.state.last_error = Some(error.to_string());
                    false
                }
            };
        }
        let Some(runtime) = &self.runtime else {
            self.state.last_error = Some("Snow is not connected".into());
            return false;
        };
        let name = command.name.clone();
        match runtime.command(command.name, command.fields) {
            Ok(request_id) => {
                self.pending_commands.insert(
                    request_id,
                    PendingDesktopCommand {
                        name: name.clone(),
                        refresh_runtime: command.refresh_runtime,
                        show_result,
                        silent,
                        input_draft,
                        process_request: None,
                        subagent_request: None,
                    },
                );
                if !silent {
                    self.state.status_text = format!("Running {name}…");
                    self.state.last_error = None;
                }
                true
            }
            Err(error) => {
                self.state.status_text = format!("Could not run {name}");
                self.state.last_error = Some(error.to_string());
                false
            }
        }
    }

    fn remove_attachment(&mut self, index: usize, cx: &mut Context<Self>) {
        if let Some(image) = self.attachments.remove(index) {
            self.state.status_text = format!("Detached {}", image.label());
            self.state.last_error = None;
        }
        cx.notify();
    }

    fn load_attachment(
        &mut self,
        source: AttachmentSource,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.state.status_text = match &source {
            AttachmentSource::File(path) => format!("Loading image {path}…"),
            AttachmentSource::Clipboard => "Reading clipboard image…".into(),
        };
        self.state.last_error = None;
        let load = cx.background_executor().spawn(async move {
            match source {
                AttachmentSource::File(path) => ImageAttachment::from_file(path),
                AttachmentSource::Clipboard => ImageAttachment::from_clipboard(),
            }
        });
        self.attachment_task = Some(cx.spawn_in(window, async move |this, window| {
            let result = load.await;
            let _ = this.update_in(window, |this, _, cx| {
                match result {
                    Ok(image) => {
                        let label = image.label().to_owned();
                        match this.attachments.push(image) {
                            Ok(()) => {
                                this.state.status_text = format!("Attached {label}");
                                this.state.last_error = None;
                            }
                            Err(error) => {
                                this.state.status_text = "Could not attach image".into();
                                this.state.last_error = Some(error.to_string());
                            }
                        }
                    }
                    Err(error) => {
                        this.state.status_text = "Could not attach image".into();
                        this.state.last_error = Some(error.to_string());
                    }
                }
                this.attachment_task = None;
                cx.notify();
            });
        }));
    }

    fn run_local_command(
        &mut self,
        command: LocalCommand,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        match command {
            LocalCommand::Help => {
                let shortcuts = cx
                    .global::<presentation_runtime::PresentationRuntimeState>()
                    .help_text();
                self.state.push_system_message(format!(
                    "{}\n\nSemantic keybindings\n\n{}",
                    commands::help_text(),
                    shortcuts
                ));
            }
            LocalCommand::Keybindings => {
                self.open_settings_section(SettingsSection::Keybindings, window, cx);
                return true;
            }
            LocalCommand::OpenModelPicker => self.toggle_picker(ComposerPicker::Model, window, cx),
            LocalCommand::OpenThinkingPicker => {
                self.toggle_picker(ComposerPicker::Thinking, window, cx)
            }
            LocalCommand::OpenSettings => {
                self.open_settings_section(SettingsSection::General, window, cx);
                return true;
            }
            LocalCommand::OpenPermissions => {
                self.open_settings_section(SettingsSection::General, window, cx);
                return true;
            }
            LocalCommand::OpenForkChooser => {
                self.processes_panel_open = false;
                self.subagents_panel_open = false;
                self.set_session_menu_open(true, window, cx);
                self.state.status_text =
                    "Choose the active branch, then fork it from the session panel".into();
            }
            LocalCommand::OpenSessions => {
                return self.run_rpc_command(RpcCommand {
                    name: "sessions_list".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            LocalCommand::OpenBranches => {
                self.set_session_menu_open(true, window, cx);
            }
            LocalCommand::OpenSubagents(prefix) => {
                self.subagent_fleet
                    .set_path_prefix(prefix.unwrap_or_default());
                self.pending_subagent_target = None;
                self.refresh_subagents(cx);
                return true;
            }
            LocalCommand::OpenProcesses(target) => {
                self.pending_process_target = target;
                return self.run_rpc_command(RpcCommand {
                    name: "processes_list".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            LocalCommand::OpenLogin(provider) => {
                self.auth_inventory_opens_panel = true;
                self.auth_selected_provider = provider;
                return self.run_rpc_command(RpcCommand {
                    name: "auth_providers".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            LocalCommand::OpenLoginProfile {
                provider,
                profile_name,
            } => {
                self.auth_inventory_opens_panel = true;
                self.auth_selected_provider = Some(provider);
                self.auth_selected_method = None;
                self.auth_job = None;
                self.auth_profile_id_input.update(cx, |input, cx| {
                    input.set_value(profile_name.clone(), window, cx)
                });
                self.auth_secret_input
                    .update(cx, |input, cx| input.set_value("", window, cx));
                return self.run_rpc_command(RpcCommand {
                    name: "auth_providers".into(),
                    fields: serde_json::Map::new(),
                    refresh_runtime: false,
                });
            }
            LocalCommand::OpenLogout(provider) => {
                let provider = provider.unwrap_or_else(|| self.provider.clone());
                return self.run_rpc_command(RpcCommand {
                    name: "auth_logout".into(),
                    fields: serde_json::Map::from_iter([(
                        "provider".into(),
                        serde_json::Value::String(provider),
                    )]),
                    refresh_runtime: false,
                });
            }
            LocalCommand::AttachFile(path) => {
                self.load_attachment(AttachmentSource::File(path), window, cx)
            }
            LocalCommand::PasteImage => {
                self.load_attachment(AttachmentSource::Clipboard, window, cx)
            }
            LocalCommand::ListAttachments => {
                let text = if self.attachments.is_empty() {
                    "No images are attached to the next prompt.".into()
                } else {
                    self.attachments
                        .images()
                        .iter()
                        .enumerate()
                        .map(|(index, image)| {
                            format!(
                                "{}. {} · {} · {} KiB",
                                index + 1,
                                image.label(),
                                image.mime_type(),
                                image.len().div_ceil(1024)
                            )
                        })
                        .collect::<Vec<_>>()
                        .join("\n")
                };
                self.state.push_system_message(text);
            }
            LocalCommand::RemoveAttachment(index) => match index {
                Some(index) => {
                    if let Some(image) = self.attachments.remove(index) {
                        self.state.status_text = format!("Detached {}", image.label());
                        self.state.last_error = None;
                    } else {
                        self.state.last_error = Some("attachment number is out of range".into());
                        return false;
                    }
                }
                None => {
                    self.attachments.clear();
                    self.state.status_text = "Detached all images".into();
                    self.state.last_error = None;
                }
            },
            LocalCommand::ResolvePermission(decision) => {
                if !self
                    .state
                    .active_interaction
                    .as_ref()
                    .is_some_and(|interaction| {
                        matches!(interaction, ActiveInteraction::Permission(_))
                    })
                {
                    self.state.last_error = Some("no permission request is pending".into());
                    return false;
                }
                self.resolve_permission(decision, cx);
            }
            LocalCommand::Quit => cx.quit(),
        }
        true
    }

    fn run_command_action(
        &mut self,
        action: CommandAction,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        match action {
            CommandAction::Local(command) => self.run_local_command(command, window, cx),
            CommandAction::Rpc(command) => self.run_presented_rpc_command(command),
            CommandAction::Prompt { message, mode } => self.send_prompt_with_mode(message, mode),
        }
    }

    fn send_prompt(&mut self, message: String) -> bool {
        self.send_prompt_with_mode(message, None)
    }

    fn send_prompt_with_mode(&mut self, message: String, mode: Option<String>) -> bool {
        if !self.state.can_send() || self.pending_prompt_submission.is_some() {
            self.state.last_error = Some("Snow is not ready for a new prompt".into());
            return false;
        }
        let Some(runtime) = &self.runtime else {
            return false;
        };
        let provider_message = match self.prepare_composer_text(&message) {
            Ok(expanded) => expanded,
            Err(error) => {
                self.state.status_text = "Could not prepare prompt".into();
                self.state.last_error = Some(error);
                return false;
            }
        };
        let content = self
            .attachments
            .rpc_content_blocks()
            .into_iter()
            .map(|block| {
                serde_json::to_value(block).expect("image content blocks are always serializable")
            })
            .collect::<Vec<_>>();
        let mut history_blocks = Vec::with_capacity(self.attachments.images().len() + 1);
        if !message.is_empty() {
            history_blocks.push(HistoryBlock::Text {
                text: message.clone(),
            });
        }
        history_blocks.extend(self.attachments.images().iter().map(|image| {
            HistoryBlock::Image(HistoryImage {
                mime_type: image.mime_type().to_owned(),
                data: image.data().to_vec(),
                preview: image.preview(),
            })
        }));
        match runtime.prompt_content(provider_message, content, mode) {
            Ok(request_id) => {
                self.pending_prompt_submission = Some(PendingPromptSubmission {
                    request_id: request_id.clone(),
                    draft: message.clone(),
                    attachments: self.attachments.clone(),
                    clear_composer_on_admit: true,
                    remember_on_admit: true,
                });
                self.state
                    .begin_prompt_with_blocks(request_id, message, history_blocks);
                self.scroll_transcript_to_bottom();
                true
            }
            Err(error) => {
                self.state.last_error = Some(error.to_string());
                self.state.status_text = "Could not send prompt".into();
                false
            }
        }
    }

    fn dismiss_plan_nudge(&mut self, cx: &mut Context<Self>) {
        let scope = self.current_plan_nudge_scope();
        self.plan_nudge_dismissals.dismiss(scope);
        self.plan_nudge_dismissed = true;
        cx.notify();
    }

    fn enable_plan_mode(&mut self, cx: &mut Context<Self>) {
        self.run_rpc_command(RpcCommand {
            name: "set_mode".into(),
            fields: serde_json::Map::from_iter([(
                "mode".into(),
                serde_json::Value::String("plan".into()),
            )]),
            refresh_runtime: true,
        });
        let scope = self.current_plan_nudge_scope();
        self.plan_nudge_dismissals.dismiss(scope);
        self.plan_nudge_dismissed = true;
        cx.notify();
    }

    fn implement_latest_plan(
        &mut self,
        fresh_context: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let plan = self.state.latest_plan.trim().to_owned();
        if plan.is_empty() {
            self.state.plan_review_ready = false;
            self.state.last_error = Some("No plan is available to implement".into());
            cx.notify();
            return;
        }
        self.state.plan_review_ready = false;
        if fresh_context {
            self.pending_fresh_plan = Some(plan);
            self.run_rpc_command(RpcCommand {
                name: "session_create".into(),
                fields: serde_json::Map::from_iter([("params".into(), serde_json::json!({}))]),
                refresh_runtime: true,
            });
            cx.notify();
            return;
        }
        self.submit_prompt_with_mode(
            "Implement the plan.".into(),
            Some("default".into()),
            window,
            cx,
        );
    }

    fn stay_in_plan_mode(&mut self, cx: &mut Context<Self>) {
        self.state.plan_review_ready = false;
        cx.notify();
    }

    fn try_submit_pending_fresh_plan(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.state.can_send() {
            return;
        }
        let Some(plan) = self.pending_fresh_plan.take() else {
            return;
        };
        let message = format!(
            "A previous agent produced the plan below. Implement it in a fresh context, re-read files as needed, and carry the work through implementation and verification.\n\n{plan}"
        );
        self.submit_prompt_with_mode(message, Some("default".into()), window, cx);
    }

    fn submit_prompt_with_mode(
        &mut self,
        message: String,
        mode: Option<String>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(runtime) = &self.runtime else {
            self.state.last_error = Some("Snow is not connected".into());
            return;
        };
        match runtime.prompt_with_mode(message.clone(), mode) {
            Ok(request_id) => {
                self.state.begin_prompt(request_id, message);
                self.input
                    .update(cx, |input, cx| input.set_value("", window, cx));
                self.scroll_transcript_to_bottom();
            }
            Err(error) => self.state.last_error = Some(error.to_string()),
        }
        cx.notify();
    }

    fn prepare_composer_text(&self, compact: &str) -> Result<String, String> {
        let expanded = self
            .paste_store
            .expand(compact)
            .map_err(|error| error.to_string())?;
        match self.mention_discovery.as_ref() {
            Some(discovery) => expand_mention_prompt(&expanded, discovery, 2 * 1024 * 1024)
                .map_err(|error| error.to_string()),
            None => Ok(expanded),
        }
    }

    fn submit_follow_up(&mut self, _window: &mut Window, cx: &mut Context<Self>) {
        let message = self.input.read(cx).value().to_string();
        let trimmed = message.trim();
        if trimmed.is_empty() || self.state.active_prompt.is_none() {
            return;
        }
        let expanded = match self.prepare_composer_text(trimmed) {
            Ok(expanded) => expanded,
            Err(error) => {
                self.state.status_text = "Could not prepare follow-up".into();
                self.state.last_error = Some(error);
                cx.notify();
                return;
            }
        };
        let handled = self.run_rpc_command_with_input(
            RpcCommand {
                name: "follow_up".into(),
                fields: serde_json::Map::from_iter([(
                    "message".into(),
                    serde_json::Value::String(expanded),
                )]),
                refresh_runtime: true,
            },
            message,
        );
        if handled {
            self.state.status_text = "Queueing follow-up…".into();
        }
        cx.notify();
    }

    fn submit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.close_composer_picker(window, cx);
        if self.pending_prompt_submission.is_some() {
            self.state.status_text = "Waiting for prompt admission…".into();
            cx.notify();
            return;
        }
        let message = self.input.read(cx).value().to_string();
        let trimmed = message.trim();
        if trimmed.is_empty() && self.attachments.is_empty() {
            return;
        }

        let mut deferred_clear = false;
        let handled = match commands::parse_command(trimmed) {
            Ok(Some(action)) => {
                let handled = self.run_command_action(action, window, cx);
                deferred_clear = self.pending_prompt_submission.is_some();
                handled
            }
            Ok(None) if self.state.active_prompt.is_some() => {
                deferred_clear = true;
                match self.prepare_composer_text(trimmed) {
                    Ok(expanded) => self.run_rpc_command_with_input(
                        RpcCommand {
                            name: "steer".into(),
                            fields: serde_json::Map::from_iter([(
                                "message".into(),
                                serde_json::Value::String(expanded),
                            )]),
                            refresh_runtime: true,
                        },
                        message.clone(),
                    ),
                    Err(error) => {
                        self.state.status_text = "Could not prepare steer message".into();
                        self.state.last_error = Some(error);
                        false
                    }
                }
            }
            Ok(None) => {
                deferred_clear = true;
                self.send_prompt(trimmed.to_owned())
            }
            Err(error) => {
                self.state.status_text = "Invalid command".into();
                self.state.last_error = Some(error);
                false
            }
        };
        if handled {
            if let Some(pending) = self.pending_prompt_submission.as_mut() {
                pending.draft = message.clone();
            }
            if !deferred_clear {
                self.remember_input(trimmed);
                self.input
                    .update(cx, |input, cx| input.set_value("", window, cx));
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
            let pending =
                self.state
                    .active_interaction
                    .as_ref()
                    .map(|interaction| (interaction.kind(), interaction.request_id().to_owned()))
                    .into_iter()
                    .chain(self.state.queued_interaction.as_ref().map(|interaction| {
                        (interaction.kind(), interaction.request_id().to_owned())
                    }))
                    .collect::<Vec<_>>();
            for (kind, request_id) in pending {
                match kind {
                    InteractionKind::Permission => {
                        let _ = runtime.permission_reject(request_id);
                    }
                    InteractionKind::UserInput => {
                        let _ = runtime.user_input_reject(request_id);
                    }
                }
            }
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
