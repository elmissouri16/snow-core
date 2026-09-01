//! Bounded, presentation-only projections for Snow RPC resource payloads.
//!
//! This module deliberately does not deserialize resource responses into an
//! open-ended JSON tree for rendering. Every projection uses a small allowlist
//! of public fields, bounds its output, and ignores unknown fields (including
//! provider-private continuity fields). The resulting rows contain plain text
//! suitable for GPUI labels, accessibility output, and copying.

use std::{error::Error, fmt};

use serde_json::{Map, Number, Value};

/// Maximum JSON nesting accepted before inspecting a resource.
pub const MAX_RESOURCE_DEPTH: usize = 8;
/// Maximum JSON containers/scalars inspected by the complexity preflight.
pub const MAX_RESOURCE_NODES: usize = 4_096;
/// Maximum rows retained across all sections of one projection.
pub const MAX_RESOURCE_ROWS: usize = 96;
/// Maximum characters in a row label.
pub const MAX_LABEL_CHARS: usize = 128;
/// Maximum characters in a row's primary value.
pub const MAX_VALUE_CHARS: usize = 512;
/// Maximum characters in a row's supporting detail.
pub const MAX_DETAIL_CHARS: usize = 1_024;
/// Maximum characters returned by whole-resource accessible/copy text.
pub const MAX_ACCESSIBLE_TEXT_CHARS: usize = 64 * 1_024;

const MAX_NESTED_ITEMS: usize = 24;
const REDACTED_TEXT: &str = "[redacted sensitive text]";
const OMITTED_TEXT: &str = "Additional items were omitted.";

/// The semantic resource represented by an RPC response's `data` value.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ResourceKind {
    Diagnostics,
    ContextReport,
    Usage,
    McpServers,
    Skills,
    PermissionMode,
    Goal,
    Processes,
    Subagents,
}

impl ResourceKind {
    pub const fn title(self) -> &'static str {
        match self {
            Self::Diagnostics => "Diagnostics",
            Self::ContextReport => "Provider context",
            Self::Usage => "Usage",
            Self::McpServers => "MCP servers",
            Self::Skills => "Agent Skills",
            Self::PermissionMode => "Permission mode",
            Self::Goal => "Thread goal",
            Self::Processes => "Managed processes",
            Self::Subagents => "Subagents",
        }
    }
}

/// Suggested visual emphasis. It carries no behavior or authorization meaning.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum SemanticTone {
    #[default]
    Neutral,
    Positive,
    Caution,
    Negative,
}

/// One bounded, already-sanitized display row.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticRow {
    pub label: String,
    pub value: String,
    pub detail: Option<String>,
    pub tone: SemanticTone,
}

impl SemanticRow {
    /// Deterministic plain text for an accessibility label or an individual copy action.
    pub fn accessible_text(&self) -> String {
        let mut text = format!("{}: {}", self.label, self.value);
        if let Some(detail) = &self.detail {
            text.push_str(". ");
            text.push_str(detail);
        }
        bounded_text(&text, MAX_DETAIL_CHARS + MAX_VALUE_CHARS + MAX_LABEL_CHARS)
    }

    pub fn copy_text(&self) -> String {
        self.accessible_text()
    }
}

/// A named group of semantically related rows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticSection {
    pub heading: String,
    pub rows: Vec<SemanticRow>,
}

impl SemanticSection {
    pub fn accessible_text(&self) -> String {
        let mut text = self.heading.clone();
        for row in &self.rows {
            text.push('\n');
            text.push_str(&row.accessible_text());
        }
        bounded_text(&text, MAX_ACCESSIBLE_TEXT_CHARS)
    }
}

/// Complete bounded projection consumed by a resource panel.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SemanticResource {
    pub kind: ResourceKind,
    pub title: String,
    pub summary: String,
    pub sections: Vec<SemanticSection>,
    /// True when otherwise valid source items could not fit in the row budget.
    pub truncated: bool,
}

impl SemanticResource {
    /// Stable plain text containing only the same sanitized fields as the rows.
    pub fn accessible_text(&self) -> String {
        let mut text = format!("{}\n{}", self.title, self.summary);
        for section in &self.sections {
            if !section.rows.is_empty() {
                text.push_str("\n\n");
                text.push_str(&section.accessible_text());
            }
        }
        if self.truncated {
            text.push_str("\n\n");
            text.push_str(OMITTED_TEXT);
        }
        bounded_text(&text, MAX_ACCESSIBLE_TEXT_CHARS)
    }

    /// Copy text intentionally matches accessibility text, so copying cannot
    /// reveal fields hidden by the visual projection.
    pub fn copy_text(&self) -> String {
        self.accessible_text()
    }
}

/// Safe-to-display projection failure. No source values or parser internals are
/// retained in the error.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProjectionError {
    UnsupportedResource,
    MalformedPayload,
    PayloadTooComplex,
}

impl fmt::Display for ProjectionError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(match self {
            Self::UnsupportedResource => "This resource is not supported.",
            Self::MalformedPayload => "This resource response could not be displayed safely.",
            Self::PayloadTooComplex => "This resource response is too complex to display safely.",
        })
    }
}

impl Error for ProjectionError {}

pub type ProjectionResult = Result<SemanticResource, ProjectionError>;

/// Dispatch a successful RPC response's `data` value by command name.
///
/// Unknown commands are rejected rather than rendered as generic JSON. The
/// `skills_clear` wrapper is unwrapped to its public catalog. Goal clear is a
/// small acknowledgement rather than a goal object and gets its own safe view.
pub fn project_rpc_resource(command: &str, data: &Value) -> ProjectionResult {
    match command {
        "diagnostics" => project_diagnostics(data),
        "context" => project_context_report(data),
        "usage" => project_usage(data),
        "mcp_servers" => project_mcp_servers(data),
        "skills" => project_skills(data),
        "skills_clear" => {
            check_complexity(data)?;
            let object = object_map(data)?;
            project_skills(required_value(object, "catalog")?)
        }
        "permission_mode_get" | "permission_mode_set" => project_permission_mode(data),
        "goal_get" | "goal_create" | "goal_set" | "goal_edit" | "goal_pause" | "goal_resume"
        | "goal_continue" => project_goal(data),
        "goal_clear" => project_goal_clear(data),
        "processes_list" => project_processes(data),
        "subagent_list" | "subagent_get" | "subagent_spawn" | "subagent_resume" => {
            project_subagents(data)
        }
        _ => Err(ProjectionError::UnsupportedResource),
    }
}

pub fn project_diagnostics(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let diagnostics = required_array(object, "diagnostics")?;
    let mut builder = Builder::new(
        ResourceKind::Diagnostics,
        if diagnostics.is_empty() {
            "No configuration diagnostics".into()
        } else {
            format!(
                "{} configuration {}",
                diagnostics.len(),
                plural(diagnostics.len(), "diagnostic", "diagnostics")
            )
        },
    );
    let section = builder.section("Configuration");
    for diagnostic in diagnostics {
        let item = object_map(diagnostic)?;
        let path = required_string(item, "path")?;
        let message = required_nonempty_string(item, "message")?;
        builder.row(
            section,
            row(
                if path.trim().is_empty() {
                    "General"
                } else {
                    path
                },
                message,
                None,
                SemanticTone::Caution,
            ),
        );
    }
    Ok(builder.finish())
}

pub fn project_context_report(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let latest = required_bool(object, "latest_request")?;
    let estimated = required_count(object, "estimated_input_tokens")?;
    let fixed = required_count(object, "fixed_context_tokens")?;
    let budget = required_count(object, "fixed_context_budget_tokens")?;
    let over_budget = required_bool(object, "fixed_context_over_budget")?;
    let messages = required_count(object, "message_count")?;
    let tools = required_count(object, "tool_count")?;
    let window = required_count(object, "context_window")?;
    let categories = required_array(object, "categories")?;

    let mut builder = Builder::new(
        ResourceKind::ContextReport,
        format!("{} estimated input tokens", format_integer(estimated)),
    );
    let overview = builder.section("Overview");
    builder.row(
        overview,
        row(
            "Projection",
            if latest {
                "Latest request"
            } else {
                "Next request"
            },
            None,
            SemanticTone::Neutral,
        ),
    );
    builder.row(overview, metric_row("Estimated input", estimated, "tokens"));
    builder.row(overview, metric_row("Context window", window, "tokens"));
    builder.row(overview, metric_row("Messages", messages, "items"));
    builder.row(overview, metric_row("Tools", tools, "items"));

    let fixed_section = builder.section("Fixed context");
    builder.row(fixed_section, metric_row("Estimated", fixed, "tokens"));
    builder.row(
        fixed_section,
        row(
            "Budget",
            &format!("{} tokens", format_integer(budget)),
            Some(if over_budget {
                "Over budget"
            } else {
                "Within budget"
            }),
            if over_budget {
                SemanticTone::Negative
            } else {
                SemanticTone::Positive
            },
        ),
    );

    let category_section = builder.section("Contributors");
    for category in categories {
        let item = object_map(category)?;
        let name = required_nonempty_string(item, "name")?;
        let bytes = required_count(item, "bytes")?;
        let tokens = required_count(item, "estimated_tokens")?;
        let items = required_count(item, "items")?;
        builder.row(
            category_section,
            row(
                name,
                &format!("{} tokens", format_integer(tokens)),
                Some(&format!(
                    "{} bytes; {} items",
                    format_integer(bytes),
                    format_integer(items)
                )),
                SemanticTone::Neutral,
            ),
        );
    }
    if let Some(usage) = optional_value(object, "usage")?
        && !usage.is_null()
    {
        append_usage(&mut builder, usage, "Aggregate usage")?;
    }
    Ok(builder.finish())
}

pub fn project_usage(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let total = required_count(object, "total_tokens")?;
    let mut builder = Builder::new(
        ResourceKind::Usage,
        format!("{} total tokens", format_integer(total)),
    );
    append_usage_rows(&mut builder, object, "Token usage")?;
    Ok(builder.finish())
}

pub fn project_mcp_servers(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let servers = required_array(object, "servers")?;
    let mut connected = 0usize;
    for server in servers {
        if required_bool(object_map(server)?, "connected")? {
            connected += 1;
        }
    }
    let mut builder = Builder::new(
        ResourceKind::McpServers,
        format!("{} of {} connected", connected, servers.len()),
    );
    let section = builder.section("Servers");
    for server in servers {
        let item = object_map(server)?;
        let id = required_nonempty_string(item, "id")?;
        let transport = required_nonempty_string(item, "transport")?;
        let is_connected = required_bool(item, "connected")?;
        let name = optional_string(item, "server_name")?
            .filter(|value| !value.trim().is_empty())
            .unwrap_or(id);
        let state = optional_string(item, "state")?.filter(|value| !value.trim().is_empty());
        let server_version =
            optional_string(item, "server_version")?.filter(|value| !value.trim().is_empty());
        optional_string(item, "cached_at")?;
        optional_string(item, "last_used_at")?;
        let tools = optional_count(item, "tool_count")?.unwrap_or(0);
        let capabilities = optional_array(item, "capabilities")?.unwrap_or(&[]);
        let mut capability_names = Vec::new();
        for capability in capabilities.iter().take(MAX_NESTED_ITEMS) {
            capability_names.push(value_string(capability)?);
        }
        let mut detail = vec![
            format!("ID: {}", safe_text(id, MAX_VALUE_CHARS)),
            format!("Transport: {}", safe_text(transport, MAX_VALUE_CHARS)),
        ];
        if let Some(protocol) =
            optional_string(item, "protocol_version")?.filter(|v| !v.trim().is_empty())
        {
            detail.push(format!(
                "Protocol: {}",
                safe_text(protocol, MAX_VALUE_CHARS)
            ));
        }
        if let Some(version) = server_version {
            detail.push(format!("Version: {}", safe_text(version, MAX_VALUE_CHARS)));
        }
        detail.push(format!("Tools: {}", format_integer(tools)));
        if !capability_names.is_empty() {
            let mut rendered = capability_names
                .iter()
                .map(|v| safe_text(v, MAX_LABEL_CHARS))
                .collect::<Vec<_>>()
                .join(", ");
            if capabilities.len() > MAX_NESTED_ITEMS {
                rendered.push_str(", …");
                builder.truncated = true;
            }
            detail.push(format!("Capabilities: {rendered}"));
        }
        if optional_bool(item, "cached")?.unwrap_or(false) {
            detail.push("Cached discovery".into());
        }
        if let Some(message) = optional_string(item, "message")?.filter(|v| !v.trim().is_empty()) {
            detail.push(format!("Message: {}", safe_text(message, MAX_DETAIL_CHARS)));
        }
        let value = state.unwrap_or(if is_connected {
            "connected"
        } else {
            "disconnected"
        });
        builder.row(
            section,
            row(
                name,
                value,
                Some(&detail.join("; ")),
                if is_connected {
                    SemanticTone::Positive
                } else {
                    SemanticTone::Caution
                },
            ),
        );
    }
    Ok(builder.finish())
}

pub fn project_skills(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let skills = required_array(object, "skills")?;
    let diagnostics = optional_array(object, "diagnostics")?.unwrap_or(&[]);
    let enabled = skills.iter().try_fold(0usize, |count, skill| {
        Ok::<_, ProjectionError>(count + usize::from(required_bool(object_map(skill)?, "enabled")?))
    })?;
    let mut builder = Builder::new(
        ResourceKind::Skills,
        format!("{} skills; {} enabled", skills.len(), enabled),
    );
    let section = builder.section("Catalog");
    for skill in skills {
        let item = object_map(skill)?;
        let name = required_nonempty_string(item, "name")?;
        let description = required_string(item, "description")?;
        let location = required_string(item, "location")?;
        let scope = required_nonempty_string(item, "scope")?;
        let source = required_nonempty_string(item, "source")?;
        let is_enabled = required_bool(item, "enabled")?;
        let license = optional_string(item, "license")?.filter(|value| !value.trim().is_empty());
        let compatibility =
            optional_string(item, "compatibility")?.filter(|value| !value.trim().is_empty());
        let mut detail = Vec::new();
        if !description.trim().is_empty() {
            detail.push(safe_text(description, MAX_DETAIL_CHARS));
        }
        detail.push(format!("Scope: {}", safe_text(scope, MAX_LABEL_CHARS)));
        detail.push(format!("Source: {}", safe_text(source, MAX_LABEL_CHARS)));
        if let Some(license) = license {
            detail.push(format!("License: {}", safe_text(license, MAX_LABEL_CHARS)));
        }
        if let Some(compatibility) = compatibility {
            detail.push(format!(
                "Compatibility: {}",
                safe_text(compatibility, MAX_VALUE_CHARS)
            ));
        }
        if !location.trim().is_empty() {
            detail.push(format!(
                "Location: {}",
                safe_text(location, MAX_VALUE_CHARS)
            ));
        }
        if let Some(reason) = optional_string(item, "disabled_by")?.filter(|v| !v.trim().is_empty())
        {
            detail.push(format!(
                "Disabled by: {}",
                safe_text(reason, MAX_VALUE_CHARS)
            ));
        }
        if let Some(tools) =
            optional_string(item, "allowed_tools")?.filter(|v| !v.trim().is_empty())
        {
            detail.push(format!(
                "Allowed tools: {}",
                safe_text(tools, MAX_VALUE_CHARS)
            ));
        }
        // `metadata` is intentionally never projected: it is open-ended and is
        // not needed to identify or operate a skill.
        builder.row(
            section,
            row(
                name,
                if is_enabled { "Enabled" } else { "Disabled" },
                Some(&detail.join("; ")),
                if is_enabled {
                    SemanticTone::Positive
                } else {
                    SemanticTone::Neutral
                },
            ),
        );
    }
    if !diagnostics.is_empty() {
        let section = builder.section("Discovery diagnostics");
        for diagnostic in diagnostics {
            let item = object_map(diagnostic)?;
            let level = required_nonempty_string(item, "level")?;
            let message = required_nonempty_string(item, "message")?;
            let skill = optional_string(item, "skill")?.filter(|v| !v.trim().is_empty());
            let path = optional_string(item, "path")?.filter(|v| !v.trim().is_empty());
            let label = skill.or(path).unwrap_or("Discovery");
            builder.row(
                section,
                row(
                    label,
                    message,
                    Some(&format!("Level: {}", safe_text(level, MAX_LABEL_CHARS))),
                    tone_for_level(level),
                ),
            );
        }
    }
    Ok(builder.finish())
}

pub fn project_permission_mode(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let mode = required_nonempty_string(object, "mode")?;
    let (description, tone) = match mode {
        "ask" => ("Ask before gated operations", SemanticTone::Caution),
        "allow" => (
            "Allow gated operations for this session",
            SemanticTone::Positive,
        ),
        "deny" => ("Deny gated operations", SemanticTone::Neutral),
        _ => return Err(ProjectionError::MalformedPayload),
    };
    let mut builder = Builder::new(ResourceKind::PermissionMode, format!("{mode} mode"));
    let section = builder.section("Active policy");
    builder.row(section, row("Mode", mode, Some(description), tone));
    Ok(builder.finish())
}

pub fn project_goal(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    if data.is_null() {
        return Ok(Builder::new(ResourceKind::Goal, "No active goal".into()).finish());
    }
    let object = object_map(data)?;
    let id = required_nonempty_string(object, "goal_id")?;
    let objective = required_nonempty_string(object, "objective")?;
    let status = required_nonempty_string(object, "status")?;
    if !matches!(
        status,
        "active" | "paused" | "blocked" | "usage_limited" | "budget_limited" | "complete"
    ) {
        return Err(ProjectionError::MalformedPayload);
    }
    let used = required_count(object, "tokens_used")?;
    let seconds = required_count(object, "seconds_used")?;
    let budget = optional_nullable_count(object, "token_budget")?;
    // Validate the rest of the stable ThreadGoal shape without displaying
    // session/branch identities or internal timestamps.
    required_nonempty_string(object, "session_id")?;
    required_nonempty_string(object, "branch_id")?;
    required_count(object, "created_at")?;
    required_count(object, "updated_at")?;

    let mut builder = Builder::new(
        ResourceKind::Goal,
        format!("{} goal", display_status(status)),
    );
    let overview = builder.section("Goal");
    builder.row(
        overview,
        row("Objective", objective, None, tone_for_goal(status)),
    );
    builder.row(
        overview,
        row(
            "Status",
            &display_status(status),
            optional_string(object, "blocked_reason")?.filter(|v| !v.trim().is_empty()),
            tone_for_goal(status),
        ),
    );
    builder.row(overview, row("Goal ID", id, None, SemanticTone::Neutral));
    let usage = builder.section("Budget and usage");
    builder.row(usage, metric_row("Tokens used", used, "tokens"));
    builder.row(usage, metric_row("Time used", seconds, "seconds"));
    let budget_value = budget
        .map(|value| format!("{} tokens", format_integer(value)))
        .unwrap_or_else(|| "Unlimited".into());
    let budget_detail = budget.map(|value| {
        format!(
            "{} tokens remaining",
            format_integer(value.saturating_sub(used))
        )
    });
    builder.row(
        usage,
        row(
            "Token budget",
            &budget_value,
            budget_detail.as_deref(),
            SemanticTone::Neutral,
        ),
    );
    if let Some(costs) = optional_nullable_array(object, "estimated_costs")? {
        append_costs(&mut builder, costs, "Estimated costs")?;
    }
    Ok(builder.finish())
}

pub fn project_processes(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let processes = required_array(object, "processes")?;
    let running = processes.iter().try_fold(0usize, |count, process| {
        let status = required_nonempty_string(object_map(process)?, "status")?;
        Ok::<_, ProjectionError>(count + usize::from(status == "running"))
    })?;
    let mut builder = Builder::new(
        ResourceKind::Processes,
        format!("{} processes; {} running", processes.len(), running),
    );
    let section = builder.section("Processes");
    for process in processes {
        let item = object_map(process)?;
        let id = required_nonempty_string(item, "process_id")?;
        let name = required_nonempty_string(item, "name")?;
        let status = required_nonempty_string(item, "status")?;
        let started = required_count(item, "started_at")?;
        let mut detail = vec![
            format!("ID: {}", safe_text(id, MAX_VALUE_CHARS)),
            format!("Started: {started}"),
        ];
        if let Some(finished) = optional_count(item, "finished_at")? {
            detail.push(format!("Finished: {finished}"));
        }
        if let Some(code) = optional_signed_integer(item, "exit_code")? {
            detail.push(format!("Exit code: {code}"));
        }
        if optional_bool(item, "ready")?.unwrap_or(false) {
            detail.push("Ready".into());
        }
        if let Some(signal) = optional_string(item, "signal")?.filter(|v| !v.trim().is_empty()) {
            detail.push(format!("Signal: {}", safe_text(signal, MAX_LABEL_CHARS)));
        }
        if let Some(reason) = optional_string(item, "reason")?.filter(|v| !v.trim().is_empty()) {
            detail.push(format!("Reason: {}", safe_text(reason, MAX_DETAIL_CHARS)));
        }
        builder.row(
            section,
            row(
                name,
                &display_status(status),
                Some(&detail.join("; ")),
                tone_for_process(status, optional_signed_integer(item, "exit_code")?),
            ),
        );
    }
    Ok(builder.finish())
}

/// Projects either a `SubagentList` or one `SubagentState`. Child result and
/// error bodies are deliberately not included; their presence is represented
/// without copying potentially sensitive generated/tool text.
pub fn project_subagents(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let object = object_map(data)?;
    let source_truncated = optional_bool(object, "truncated")?.unwrap_or(false);
    let (agents, summary) = if let Some(agents) = optional_array(object, "agents")? {
        let running = required_count(object, "running")?;
        let queued = required_count(object, "queued")?;
        let terminal = required_count(object, "terminal")?;
        required_count(object, "open")?;
        required_count(object, "closed")?;
        required_count(object, "concurrent_limit")?;
        required_count(object, "agent_limit")?;
        (
            agents,
            format!(
                "{} agents; {} running; {} queued; {} terminal",
                agents.len(),
                running,
                queued,
                terminal
            ),
        )
    } else {
        (std::slice::from_ref(data), "Subagent details".into())
    };
    let mut builder = Builder::new(ResourceKind::Subagents, summary);
    builder.truncated = source_truncated;
    let section = builder.section("Agents");
    for state in agents {
        let item = object_map(state)?;
        let agent = object_map(required_value(item, "agent")?)?;
        let thread = required_nonempty_string(agent, "thread_id")?;
        let path = required_nonempty_string(agent, "path")?;
        optional_string(agent, "parent_thread_id")?;
        optional_string(agent, "parent_path")?;
        optional_string(agent, "role")?;
        optional_string(agent, "nickname")?;
        required_count(agent, "depth")?;
        let status = required_nonempty_string(item, "status")?;
        if !matches!(
            status,
            "pending_init"
                | "queued"
                | "running"
                | "interrupted"
                | "completed"
                | "errored"
                | "shutdown"
                | "closed"
                | "not_loaded"
        ) {
            return Err(ProjectionError::MalformedPayload);
        }
        required_count(item, "created_at")?;
        optional_count(item, "started_at")?;
        optional_count(item, "finished_at")?;
        optional_count(item, "generation")?;
        optional_string(item, "thinking")?;
        let label = optional_string(agent, "nickname")?
            .filter(|v| !v.trim().is_empty())
            .unwrap_or(path);
        let mut detail = vec![
            format!("Path: {}", safe_text(path, MAX_VALUE_CHARS)),
            format!("Thread: {}", safe_text(thread, MAX_VALUE_CHARS)),
        ];
        if let Some(role) = optional_string(agent, "role")?.filter(|v| !v.trim().is_empty()) {
            detail.push(format!("Role: {}", safe_text(role, MAX_LABEL_CHARS)));
        }
        if let Some(provider) = optional_string(item, "provider")?.filter(|v| !v.trim().is_empty())
        {
            detail.push(format!(
                "Provider: {}",
                safe_text(provider, MAX_LABEL_CHARS)
            ));
        }
        if let Some(model) = optional_string(item, "model")?.filter(|v| !v.trim().is_empty()) {
            detail.push(format!("Model: {}", safe_text(model, MAX_VALUE_CHARS)));
        }
        if let Some(usage) = optional_value(item, "usage")?
            && !usage.is_null()
        {
            let total = validate_usage(usage)?;
            detail.push(format!("Usage: {} tokens", format_integer(total)));
        }
        if optional_string(item, "result")?.is_some_and(|v| !v.is_empty()) {
            detail.push("Result available (content hidden)".into());
        }
        if optional_string(item, "error")?.is_some_and(|v| !v.is_empty()) {
            detail.push("Error reported (details hidden)".into());
        }
        builder.row(
            section,
            row(
                label,
                &display_status(status),
                Some(&detail.join("; ")),
                tone_for_subagent(status),
            ),
        );
    }
    Ok(builder.finish())
}

fn project_goal_clear(data: &Value) -> ProjectionResult {
    check_complexity(data)?;
    let cleared = required_bool(object_map(data)?, "cleared")?;
    let mut builder = Builder::new(
        ResourceKind::Goal,
        if cleared {
            "Goal cleared".into()
        } else {
            "No active goal".into()
        },
    );
    let section = builder.section("Result");
    builder.row(
        section,
        row(
            "Cleared",
            if cleared { "Yes" } else { "No" },
            None,
            SemanticTone::Neutral,
        ),
    );
    Ok(builder.finish())
}

struct Builder {
    kind: ResourceKind,
    summary: String,
    sections: Vec<SemanticSection>,
    rows: usize,
    truncated: bool,
}

impl Builder {
    fn new(kind: ResourceKind, summary: String) -> Self {
        Self {
            kind,
            summary: safe_text(&summary, MAX_VALUE_CHARS),
            sections: Vec::new(),
            rows: 0,
            truncated: false,
        }
    }

    fn section(&mut self, heading: &str) -> usize {
        self.sections.push(SemanticSection {
            heading: safe_text(heading, MAX_LABEL_CHARS),
            rows: Vec::new(),
        });
        self.sections.len() - 1
    }

    fn row(&mut self, section: usize, row: SemanticRow) {
        if self.rows < MAX_RESOURCE_ROWS {
            self.sections[section].rows.push(row);
            self.rows += 1;
        } else {
            self.truncated = true;
        }
    }

    fn finish(mut self) -> SemanticResource {
        self.sections.retain(|section| !section.rows.is_empty());
        SemanticResource {
            kind: self.kind,
            title: self.kind.title().into(),
            summary: self.summary,
            sections: self.sections,
            truncated: self.truncated,
        }
    }
}

fn row(label: &str, value: &str, detail: Option<&str>, tone: SemanticTone) -> SemanticRow {
    SemanticRow {
        label: safe_text(label, MAX_LABEL_CHARS),
        value: safe_text(value, MAX_VALUE_CHARS),
        detail: detail
            .filter(|v| !v.is_empty())
            .map(|v| safe_text(v, MAX_DETAIL_CHARS)),
        tone,
    }
}

fn metric_row(label: &str, count: i64, unit: &str) -> SemanticRow {
    row(
        label,
        &format!("{} {unit}", format_integer(count)),
        None,
        SemanticTone::Neutral,
    )
}

fn validate_usage(value: &Value) -> Result<i64, ProjectionError> {
    let usage = object_map(value)?;
    required_count(usage, "input")?;
    required_count(usage, "output")?;
    optional_count(usage, "reasoning")?;
    required_count(usage, "cache_read")?;
    optional_bool(usage, "cache_read_known")?;
    required_count(usage, "cache_write")?;
    let total = required_count(usage, "total_tokens")?;
    optional_count(usage, "requests")?;
    if let Some(cost) = optional_value(usage, "cost")?
        && !cost.is_null()
    {
        cost_row(object_map(cost)?, "Total")?;
    }
    Ok(total)
}

fn append_usage(
    builder: &mut Builder,
    value: &Value,
    heading: &str,
) -> Result<(), ProjectionError> {
    append_usage_rows(builder, object_map(value)?, heading)
}

fn append_usage_rows(
    builder: &mut Builder,
    usage: &Map<String, Value>,
    heading: &str,
) -> Result<(), ProjectionError> {
    let input = required_count(usage, "input")?;
    let output = required_count(usage, "output")?;
    let reasoning = optional_count(usage, "reasoning")?.unwrap_or(0);
    let cache_read = required_count(usage, "cache_read")?;
    let cache_write = required_count(usage, "cache_write")?;
    let total = required_count(usage, "total_tokens")?;
    let requests = optional_count(usage, "requests")?.unwrap_or(0);
    optional_bool(usage, "cache_read_known")?;
    let section = builder.section(heading);
    for (label, count) in [
        ("Input", input),
        ("Output", output),
        ("Reasoning", reasoning),
        ("Cache read", cache_read),
        ("Cache write", cache_write),
        ("Total", total),
    ] {
        builder.row(section, metric_row(label, count, "tokens"));
    }
    if requests > 0 {
        builder.row(section, metric_row("Requests", requests, "requests"));
    }
    if let Some(cost) = optional_value(usage, "cost")?
        && !cost.is_null()
    {
        append_cost(builder, object_map(cost)?, "Estimated cost")?;
    }
    Ok(())
}

fn append_costs(
    builder: &mut Builder,
    costs: &[Value],
    heading: &str,
) -> Result<(), ProjectionError> {
    let section = builder.section(heading);
    for cost in costs {
        let object = object_map(cost)?;
        builder.row(section, cost_row(object, "Total")?);
    }
    Ok(())
}

fn append_cost(
    builder: &mut Builder,
    cost: &Map<String, Value>,
    heading: &str,
) -> Result<(), ProjectionError> {
    let section = builder.section(heading);
    builder.row(section, cost_row(cost, "Total")?);
    Ok(())
}

fn cost_row(cost: &Map<String, Value>, label: &str) -> Result<SemanticRow, ProjectionError> {
    let currency = optional_string(cost, "currency")?
        .filter(|v| !v.trim().is_empty())
        .unwrap_or("currency units");
    let input = required_decimal(cost, "input")?;
    let output = required_decimal(cost, "output")?;
    let cache_read = required_decimal(cost, "cache_read")?;
    let cache_write = required_decimal(cost, "cache_write")?;
    let total = required_decimal(cost, "total")?;
    Ok(row(
        label,
        &format!("{} {}", decimal_text(total), safe_text(currency, 24)),
        Some(&format!(
            "Input: {}; output: {}; cache read: {}; cache write: {}",
            decimal_text(input),
            decimal_text(output),
            decimal_text(cache_read),
            decimal_text(cache_write)
        )),
        SemanticTone::Neutral,
    ))
}

fn check_complexity(value: &Value) -> Result<(), ProjectionError> {
    let mut stack = vec![(value, 0usize)];
    let mut nodes = 0usize;
    while let Some((value, depth)) = stack.pop() {
        nodes += 1;
        if nodes > MAX_RESOURCE_NODES || depth > MAX_RESOURCE_DEPTH {
            return Err(ProjectionError::PayloadTooComplex);
        }
        match value {
            Value::Array(values) => stack.extend(values.iter().map(|value| (value, depth + 1))),
            Value::Object(values) => stack.extend(values.values().map(|value| (value, depth + 1))),
            _ => {}
        }
    }
    Ok(())
}

fn object_map(value: &Value) -> Result<&Map<String, Value>, ProjectionError> {
    value.as_object().ok_or(ProjectionError::MalformedPayload)
}
fn required_value<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<&'a Value, ProjectionError> {
    object.get(key).ok_or(ProjectionError::MalformedPayload)
}
fn optional_value<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<Option<&'a Value>, ProjectionError> {
    Ok(object.get(key))
}
fn required_array<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<&'a [Value], ProjectionError> {
    required_value(object, key)?
        .as_array()
        .map(Vec::as_slice)
        .ok_or(ProjectionError::MalformedPayload)
}
fn optional_array<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<Option<&'a [Value]>, ProjectionError> {
    object
        .get(key)
        .map(|v| {
            v.as_array()
                .map(Vec::as_slice)
                .ok_or(ProjectionError::MalformedPayload)
        })
        .transpose()
}
fn optional_nullable_array<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<Option<&'a [Value]>, ProjectionError> {
    match object.get(key) {
        None | Some(Value::Null) => Ok(None),
        Some(value) => value
            .as_array()
            .map(Vec::as_slice)
            .map(Some)
            .ok_or(ProjectionError::MalformedPayload),
    }
}
fn required_string<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<&'a str, ProjectionError> {
    required_value(object, key)?
        .as_str()
        .ok_or(ProjectionError::MalformedPayload)
}
fn required_nonempty_string<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<&'a str, ProjectionError> {
    let value = required_string(object, key)?;
    if value.trim().is_empty() {
        Err(ProjectionError::MalformedPayload)
    } else {
        Ok(value)
    }
}
fn optional_string<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<Option<&'a str>, ProjectionError> {
    object
        .get(key)
        .map(|v| v.as_str().ok_or(ProjectionError::MalformedPayload))
        .transpose()
}
fn value_string(value: &Value) -> Result<&str, ProjectionError> {
    value.as_str().ok_or(ProjectionError::MalformedPayload)
}
fn required_bool(object: &Map<String, Value>, key: &str) -> Result<bool, ProjectionError> {
    required_value(object, key)?
        .as_bool()
        .ok_or(ProjectionError::MalformedPayload)
}
fn optional_bool(object: &Map<String, Value>, key: &str) -> Result<Option<bool>, ProjectionError> {
    object
        .get(key)
        .map(|v| v.as_bool().ok_or(ProjectionError::MalformedPayload))
        .transpose()
}
fn required_count(object: &Map<String, Value>, key: &str) -> Result<i64, ProjectionError> {
    integer(required_value(object, key)?, false)
}
fn optional_count(object: &Map<String, Value>, key: &str) -> Result<Option<i64>, ProjectionError> {
    object.get(key).map(|v| integer(v, false)).transpose()
}
fn optional_nullable_count(
    object: &Map<String, Value>,
    key: &str,
) -> Result<Option<i64>, ProjectionError> {
    match object.get(key) {
        None | Some(Value::Null) => Ok(None),
        Some(value) => integer(value, false).map(Some),
    }
}
fn optional_signed_integer(
    object: &Map<String, Value>,
    key: &str,
) -> Result<Option<i64>, ProjectionError> {
    object.get(key).map(|v| integer(v, true)).transpose()
}

fn integer(value: &Value, signed: bool) -> Result<i64, ProjectionError> {
    let result = value
        .as_i64()
        .or_else(|| value.as_u64().and_then(|v| i64::try_from(v).ok()))
        .ok_or(ProjectionError::MalformedPayload)?;
    if !signed && result < 0 {
        return Err(ProjectionError::MalformedPayload);
    }
    Ok(result)
}

fn required_decimal<'a>(
    object: &'a Map<String, Value>,
    key: &str,
) -> Result<&'a Number, ProjectionError> {
    let number = required_value(object, key)?
        .as_number()
        .ok_or(ProjectionError::MalformedPayload)?;
    let value = number.as_f64().ok_or(ProjectionError::MalformedPayload)?;
    if !value.is_finite() || !(0.0..=9_000_000_000_000_000.0).contains(&value) {
        return Err(ProjectionError::MalformedPayload);
    }
    Ok(number)
}

fn decimal_text(number: &Number) -> String {
    number.to_string()
}
fn plural<'a>(count: usize, one: &'a str, many: &'a str) -> &'a str {
    if count == 1 { one } else { many }
}

fn format_integer(value: i64) -> String {
    let digits = value.to_string();
    let (sign, digits) = digits
        .strip_prefix('-')
        .map_or(("", digits.as_str()), |rest| ("-", rest));
    let mut output = String::with_capacity(digits.len() + digits.len() / 3 + sign.len());
    output.push_str(sign);
    let first = digits.len() % 3;
    if first != 0 {
        output.push_str(&digits[..first]);
        if digits.len() > first {
            output.push(',');
        }
    }
    for (index, chunk) in digits.as_bytes()[first..].chunks(3).enumerate() {
        if index > 0 {
            output.push(',');
        }
        output.push_str(std::str::from_utf8(chunk).expect("digits are UTF-8"));
    }
    output
}

fn display_status(status: &str) -> String {
    let mut result = String::with_capacity(status.len());
    for (index, word) in status.split('_').enumerate() {
        if index > 0 {
            result.push(' ');
        }
        if index == 0 {
            let mut chars = word.chars();
            if let Some(first) = chars.next() {
                result.extend(first.to_uppercase());
                result.extend(chars);
            }
        } else {
            result.push_str(word);
        }
    }
    safe_text(&result, MAX_VALUE_CHARS)
}

fn tone_for_level(level: &str) -> SemanticTone {
    match level.to_ascii_lowercase().as_str() {
        "error" => SemanticTone::Negative,
        "warning" | "warn" => SemanticTone::Caution,
        _ => SemanticTone::Neutral,
    }
}
fn tone_for_goal(status: &str) -> SemanticTone {
    match status {
        "active" | "complete" => SemanticTone::Positive,
        "paused" | "usage_limited" => SemanticTone::Caution,
        "blocked" | "budget_limited" => SemanticTone::Negative,
        _ => SemanticTone::Neutral,
    }
}
fn tone_for_process(status: &str, exit_code: Option<i64>) -> SemanticTone {
    if status == "running" {
        SemanticTone::Positive
    } else if exit_code.is_some_and(|code| code != 0) || matches!(status, "failed" | "error") {
        SemanticTone::Negative
    } else {
        SemanticTone::Neutral
    }
}
fn tone_for_subagent(status: &str) -> SemanticTone {
    match status {
        "running" | "completed" => SemanticTone::Positive,
        "queued" | "pending_init" | "interrupted" => SemanticTone::Caution,
        "errored" | "shutdown" => SemanticTone::Negative,
        _ => SemanticTone::Neutral,
    }
}

fn safe_text(value: &str, max_chars: usize) -> String {
    let mut prefix = String::new();
    let mut truncated = false;
    let mut retained = 0usize;
    let inspection_limit = max_chars.saturating_mul(4).saturating_add(64);
    for (inspected, character) in value.chars().enumerate() {
        if inspected >= inspection_limit || retained >= max_chars {
            truncated = true;
            break;
        }
        if !character.is_control() || matches!(character, '\n' | '\r' | '\t') {
            prefix.push(character);
            retained += 1;
        }
    }
    let normalized = prefix.split_whitespace().collect::<Vec<_>>().join(" ");
    if looks_sensitive(&normalized) {
        return REDACTED_TEXT.into();
    }
    if truncated && max_chars > 1 {
        let mut result = normalized
            .chars()
            .take(max_chars.saturating_sub(1))
            .collect::<String>();
        result.push('…');
        result
    } else {
        normalized
    }
}

fn bounded_text(value: &str, max_chars: usize) -> String {
    let mut output = value.chars().take(max_chars).collect::<String>();
    if value.chars().nth(max_chars).is_some() && max_chars > 0 {
        output.pop();
        output.push('…');
    }
    output
}

fn looks_sensitive(value: &str) -> bool {
    let lower = value.to_ascii_lowercase();
    if lower.contains("-----begin private key")
        || lower.contains("-----begin rsa private key")
        || lower.contains("bearer ")
        || lower.contains("provider_data")
        || lower.contains("provider-private")
        || lower.contains("github_pat_")
        || lower.contains("ghp_")
        || lower.contains("xoxb-")
        || lower.contains("xoxp-")
        || lower.contains("sk-live-")
        || lower.contains("sk-proj-")
        || lower
            .split(|character: char| {
                character.is_whitespace() || matches!(character, '"' | '\'' | '=' | ':')
            })
            .any(|word| word.starts_with("sk-") && word.len() >= 8)
        || lower
            .split(|character: char| !character.is_ascii_alphanumeric())
            .any(|word| word.starts_with("akia") && word.len() >= 16)
    {
        return true;
    }
    const KEYS: [&str; 13] = [
        "api_key",
        "api-key",
        "apikey",
        "access_token",
        "refresh_token",
        "id_token",
        "client_secret",
        "password",
        "passwd",
        "credential",
        "authorization",
        "secret",
        "token",
    ];
    for key in KEYS {
        let mut start = 0;
        while let Some(relative) = lower[start..].find(key) {
            let after = start + relative + key.len();
            let suffix = lower[after..].trim_start_matches([' ', '\t', '"', '\'']);
            if suffix.starts_with(':') || suffix.starts_with('=') {
                return true;
            }
            start = after;
        }
    }
    lower
        .split_whitespace()
        .any(|word| word.starts_with("eyj") && word.matches('.').count() >= 2)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn assert_private_absent(resource: &SemanticResource) {
        let text = resource.copy_text().to_ascii_lowercase();
        for private in [
            "super-secret",
            "opaque-continuity",
            "hunter2",
            "github_pat_abc",
        ] {
            assert!(
                !text.contains(private),
                "private fixture leaked: {private}\n{text}"
            );
        }
    }

    #[test]
    fn diagnostics_are_typed_bounded_and_redacted() {
        let data = json!({
            "diagnostics": [
                {"path": "config.json", "message": "deprecated option"},
                {"path": "secrets.json", "message": "api_key=super-secret"}
            ],
            "provider_data": "opaque-continuity"
        });
        let resource = project_diagnostics(&data).unwrap();
        assert_eq!(resource.kind, ResourceKind::Diagnostics);
        assert_eq!(resource.sections[0].rows[0].label, "config.json");
        assert_eq!(resource.sections[0].rows[1].value, REDACTED_TEXT);
        assert_private_absent(&resource);
        assert_eq!(resource.copy_text(), resource.accessible_text());
    }

    #[test]
    fn context_is_counts_only_and_includes_safe_usage() {
        let data = json!({
            "latest_request": true,
            "categories": [{"name":"User messages","bytes":1200,"estimated_tokens":300,"items":2}],
            "estimated_input_tokens": 300,
            "fixed_context_tokens": 100,
            "fixed_context_budget_tokens": 90,
            "fixed_context_over_budget": true,
            "message_count": 2,
            "tool_count": 1,
            "context_window": 128000,
            "usage": {"input":10,"output":2,"cache_read":0,"cache_write":0,"total_tokens":12},
            "prompt": "password=hunter2",
            "provider_data": {"secret":"opaque-continuity"}
        });
        let resource = project_context_report(&data).unwrap();
        assert!(resource.accessible_text().contains("128,000 tokens"));
        assert!(
            resource
                .sections
                .iter()
                .flatten_rows()
                .any(|row| row.tone == SemanticTone::Negative)
        );
        assert_private_absent(&resource);
    }

    trait FlattenRows<'a> {
        fn flatten_rows(self) -> Box<dyn Iterator<Item = &'a SemanticRow> + 'a>;
    }
    impl<'a, I> FlattenRows<'a> for I
    where
        I: Iterator<Item = &'a SemanticSection> + 'a,
    {
        fn flatten_rows(self) -> Box<dyn Iterator<Item = &'a SemanticRow> + 'a> {
            Box::new(self.flat_map(|section| section.rows.iter()))
        }
    }

    #[test]
    fn usage_formats_cost_deterministically() {
        let value = json!({
            "input": 1234, "output": 56, "reasoning": 7,
            "cache_read": 8, "cache_read_known": true, "cache_write": 9,
            "total_tokens": 1290, "requests": 2,
            "cost": {"currency":"USD","input":0.1,"output":0.2,"cache_read":0.0,"cache_write":0.01,"total":0.31}
        });
        let first = project_usage(&value).unwrap();
        let second = project_usage(&value).unwrap();
        assert_eq!(first, second);
        assert!(first.copy_text().contains("1,290 total tokens"));
        assert!(first.copy_text().contains("0.31 USD"));
    }

    #[test]
    fn mcp_allowlist_drops_transport_secrets_and_caps_capabilities() {
        let capabilities = (0..40)
            .map(|i| json!(format!("cap-{i}")))
            .collect::<Vec<_>>();
        let value = json!({"servers":[{
            "id":"docs", "transport":"stdio", "connected":true,
            "server_name":"Docs", "tool_count":3, "capabilities":capabilities,
            "message":"authorization: Bearer super-secret",
            "headers":{"Authorization":"Bearer super-secret"},
            "argv":["--token","super-secret"], "provider_data":"opaque-continuity"
        }]});
        let resource = project_mcp_servers(&value).unwrap();
        assert!(resource.copy_text().contains("cap-23"));
        assert!(!resource.copy_text().contains("cap-24"));
        assert_private_absent(&resource);
    }

    #[test]
    fn skills_ignore_open_metadata_and_redact_diagnostics() {
        let value = json!({
            "skills":[{"name":"review","description":"Review code","location":"/tmp/review/SKILL.md","scope":"project","source":"local","enabled":false,"disabled_by":"policy","metadata":{"api_key":"super-secret"}}],
            "diagnostics":[{"path":"/tmp/bad","skill":"bad","level":"error","message":"password: hunter2"}]
        });
        let resource = project_skills(&value).unwrap();
        assert!(resource.copy_text().contains("review"));
        assert_private_absent(&resource);
    }

    #[test]
    fn permission_modes_are_closed_and_malformed_errors_are_safe() {
        let resource =
            project_permission_mode(&json!({"mode":"ask","secret":"super-secret"})).unwrap();
        assert_eq!(resource.sections[0].rows[0].tone, SemanticTone::Caution);
        let error =
            project_permission_mode(&json!({"mode":"root","password":"hunter2"})).unwrap_err();
        assert_eq!(error, ProjectionError::MalformedPayload);
        assert!(!error.to_string().contains("root"));
        assert!(!error.to_string().contains("hunter2"));
    }

    #[test]
    fn goal_projects_objective_but_redacts_embedded_secret() {
        let value = json!({
            "session_id":"session", "branch_id":"main", "goal_id":"goal-1",
            "objective":"ship parser; github_pat_abc", "status":"blocked",
            "blocked_reason":"api_key=super-secret", "token_budget":2000,
            "tokens_used":750, "seconds_used":45, "estimated_costs":[],
            "created_at":1, "updated_at":2, "provider_data":"opaque-continuity"
        });
        let resource = project_goal(&value).unwrap();
        assert!(resource.copy_text().contains("1,250 tokens remaining"));
        assert_private_absent(&resource);
        assert_eq!(
            project_goal(&Value::Null).unwrap().summary,
            "No active goal"
        );
    }

    #[test]
    fn processes_expose_state_not_unknown_command_data() {
        let value = json!({"processes":[{
            "process_id":"proc-1", "name":"server", "status":"exited",
            "started_at":1, "finished_at":2, "exit_code":1,
            "reason":"client_secret=super-secret", "command":"echo super-secret"
        }]});
        let resource = project_processes(&value).unwrap();
        assert_eq!(resource.sections[0].rows[0].tone, SemanticTone::Negative);
        assert_private_absent(&resource);
    }

    #[test]
    fn subagents_hide_result_error_and_provider_private_fields() {
        let value = json!({
            "agents":[{
                "agent":{"thread_id":"thread-1","path":"/root/reviewer","nickname":"reviewer","depth":1},
                "status":"errored","created_at":1,
                "result":"Bearer super-secret", "error":"password=hunter2",
                "provider_data":"opaque-continuity",
                "usage":{"input":40,"output":2,"cache_read":0,"cache_write":0,"total_tokens":42}
            }],
            "running":0,"queued":0,"terminal":1,"open":1,"closed":0,
            "concurrent_limit":4,"agent_limit":16
        });
        let resource = project_subagents(&value).unwrap();
        let text = resource.copy_text();
        assert!(text.contains("Result available (content hidden)"));
        assert!(text.contains("Error reported (details hidden)"));
        assert_private_absent(&resource);
    }

    #[test]
    fn item_and_text_truncation_are_explicit_and_utf8_safe() {
        let diagnostics = (0..(MAX_RESOURCE_ROWS + 20))
            .map(
                |i| json!({"path":format!("path-{i}"),"message":"🦀".repeat(MAX_VALUE_CHARS + 20)}),
            )
            .collect::<Vec<_>>();
        let resource = project_diagnostics(&json!({"diagnostics":diagnostics})).unwrap();
        assert_eq!(resource.sections[0].rows.len(), MAX_RESOURCE_ROWS);
        assert!(resource.truncated);
        assert!(resource.copy_text().contains(OMITTED_TEXT));
        assert!(resource.sections[0].rows[0].value.ends_with('…'));
        assert!(resource.sections[0].rows[0].value.chars().count() <= MAX_VALUE_CHARS);
    }

    #[test]
    fn deep_or_malformed_shapes_are_rejected_without_echoing_values() {
        let mut deep = json!("super-secret");
        for _ in 0..=MAX_RESOURCE_DEPTH {
            deep = json!({"nested":deep});
        }
        assert_eq!(
            project_diagnostics(&deep).unwrap_err(),
            ProjectionError::PayloadTooComplex
        );

        let malformed = json!({"diagnostics":[{"path":12,"message":"password=hunter2"}]});
        let message = project_diagnostics(&malformed).unwrap_err().to_string();
        assert_eq!(
            message,
            "This resource response could not be displayed safely."
        );
        assert!(!message.contains("hunter2"));
    }

    #[test]
    fn dispatcher_unwraps_skills_clear_and_rejects_generic_json() {
        let value = json!({"cleared":1,"catalog":{"skills":[],"diagnostics":[]}});
        assert_eq!(
            project_rpc_resource("skills_clear", &value).unwrap().kind,
            ResourceKind::Skills
        );
        let unknown = json!({"provider_data":"opaque-continuity","api_key":"super-secret"});
        assert_eq!(
            project_rpc_resource("future_resource", &unknown).unwrap_err(),
            ProjectionError::UnsupportedResource
        );
    }
}
