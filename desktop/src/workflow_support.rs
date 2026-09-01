//! Pure, bounded state machines for native desktop workflows.
//!
//! This module deliberately has no GPUI, I/O, JSON, or runtime dependencies.
//! Integrators translate [`RpcProjection`] into the desktop RPC transport. The
//! split between top-level fields and `params` is explicit so projections match
//! Snow protocol-v1 request shapes without placing credentials or other secrets
//! in presentation state.

use std::{collections::BTreeMap, error::Error, fmt};

pub const MAX_REQUEST_ID_CHARS: usize = 256;
pub const MAX_RPC_VALUE_CHARS: usize = 4_096;
pub const MAX_SESSION_ID_CHARS: usize = 256;
pub const MAX_SESSION_NAME_CHARS: usize = 72;
pub const MAX_GOAL_OBJECTIVE_CHARS: usize = 32 * 1024;
pub const MAX_PROVIDER_ID_CHARS: usize = 256;
pub const MAX_PROVIDER_LABEL_CHARS: usize = 256;
pub const MAX_ACCOUNT_ID_CHARS: usize = 256;
pub const MAX_WORKSPACE_ID_CHARS: usize = 256;
pub const MAX_WORKSPACE_IDS: usize = 32;
pub const MAX_AUTH_PROVIDERS: usize = 128;

/// A non-secret scalar accepted by the workflow projections.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RpcValue {
    Text(String),
    Integer(i64),
    Boolean(bool),
}

/// A deterministic, correlation-preserving RPC request description.
///
/// `top_level` contains protocol request fields such as `provider`; `params`
/// contains the object encoded under the request's `params` member. Both maps
/// are ordered to make rendering, tests, and retries deterministic.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RpcProjection {
    pub request_id: String,
    pub command: &'static str,
    pub top_level: BTreeMap<&'static str, RpcValue>,
    pub params: BTreeMap<&'static str, RpcValue>,
}

impl RpcProjection {
    fn with_params(
        request_id: impl Into<String>,
        command: &'static str,
        params: BTreeMap<&'static str, RpcValue>,
    ) -> Result<Self, WorkflowError> {
        let request_id = validate_required(request_id.into(), MAX_REQUEST_ID_CHARS, "request ID")?;
        Ok(Self {
            request_id,
            command,
            top_level: BTreeMap::new(),
            params,
        })
    }

    fn with_top_level(
        request_id: impl Into<String>,
        command: &'static str,
        top_level: BTreeMap<&'static str, RpcValue>,
    ) -> Result<Self, WorkflowError> {
        let request_id = validate_required(request_id.into(), MAX_REQUEST_ID_CHARS, "request ID")?;
        Ok(Self {
            request_id,
            command,
            top_level,
            params: BTreeMap::new(),
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WorkflowError {
    Required(&'static str),
    TooLong {
        field: &'static str,
        max_chars: usize,
    },
    ControlCharacter(&'static str),
    InvalidTokenBudget,
    TooManyWorkspaceIds,
    DuplicateRequest,
    NoPendingRequest,
    NoCredential,
    RestrictionUnavailable,
}

impl fmt::Display for WorkflowError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Required(field) => write!(f, "{field} is required"),
            Self::TooLong { field, max_chars } => {
                write!(f, "{field} must be at most {max_chars} characters")
            }
            Self::ControlCharacter(field) => {
                write!(f, "{field} must not contain control characters")
            }
            Self::InvalidTokenBudget => write!(f, "token budget must be positive"),
            Self::TooManyWorkspaceIds => {
                write!(f, "at most {MAX_WORKSPACE_IDS} workspace IDs are accepted")
            }
            Self::DuplicateRequest => write!(f, "a request is already pending"),
            Self::NoPendingRequest => write!(f, "no request is pending"),
            Self::NoCredential => write!(f, "no configured credential is selected"),
            Self::RestrictionUnavailable => {
                write!(f, "ChatGPT account restriction metadata is unavailable")
            }
        }
    }
}

impl Error for WorkflowError {}

fn validate_required(
    value: String,
    max_chars: usize,
    field: &'static str,
) -> Result<String, WorkflowError> {
    let value = value.trim().to_owned();
    if value.is_empty() {
        return Err(WorkflowError::Required(field));
    }
    validate_public(value, max_chars, field)
}

fn validate_public(
    value: String,
    max_chars: usize,
    field: &'static str,
) -> Result<String, WorkflowError> {
    if value.chars().count() > max_chars {
        return Err(WorkflowError::TooLong { field, max_chars });
    }
    if value.chars().any(char::is_control) {
        return Err(WorkflowError::ControlCharacter(field));
    }
    Ok(value)
}

fn insert_optional(
    map: &mut BTreeMap<&'static str, RpcValue>,
    field: &'static str,
    value: &str,
    max_chars: usize,
) -> Result<(), WorkflowError> {
    let value = value.trim();
    if value.is_empty() {
        return Ok(());
    }
    map.insert(
        field,
        RpcValue::Text(validate_public(value.to_owned(), max_chars, field)?),
    );
    Ok(())
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ForkKind {
    Branch,
    Session,
    Worktree,
}

impl ForkKind {
    pub const ALL: [Self; 3] = [Self::Branch, Self::Session, Self::Worktree];

    pub const fn command(self) -> &'static str {
        match self {
            Self::Branch => "branch_fork",
            Self::Session => "session_fork",
            Self::Worktree => "session_worktree_fork",
        }
    }

    pub const fn label(self) -> &'static str {
        match self {
            Self::Branch => "Fork branch",
            Self::Session => "Fork session",
            Self::Worktree => "Fork into Git worktree",
        }
    }
}

/// Optional fields accepted by Snow's canonical fork commands.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ForkDraft {
    pub source_branch_id: String,
    pub from_entry_id: String,
    pub name: String,
    pub destination_path: String,
    pub worktree_path: String,
    pub git_branch: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ForkChooser {
    pub selected: ForkKind,
    pub draft: ForkDraft,
    pending: Option<PendingFork>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PendingFork {
    request_id: String,
    kind: ForkKind,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Correlation {
    Applied,
    Stale,
}

impl Default for ForkChooser {
    fn default() -> Self {
        Self {
            selected: ForkKind::Branch,
            draft: ForkDraft::default(),
            pending: None,
        }
    }
}

impl ForkChooser {
    pub fn select(&mut self, kind: ForkKind) {
        self.selected = kind;
    }

    pub fn pending_request_id(&self) -> Option<&str> {
        self.pending
            .as_ref()
            .map(|pending| pending.request_id.as_str())
    }

    /// Validate the selected choice, project its exact RPC request shape, and
    /// remember the request ID so stale completions cannot close a newer flow.
    pub fn begin(&mut self, request_id: impl Into<String>) -> Result<RpcProjection, WorkflowError> {
        if self.pending.is_some() {
            return Err(WorkflowError::DuplicateRequest);
        }
        let request_id = validate_required(request_id.into(), MAX_REQUEST_ID_CHARS, "request ID")?;
        let params = self.project_params()?;
        self.pending = Some(PendingFork {
            request_id: request_id.clone(),
            kind: self.selected,
        });
        RpcProjection::with_params(request_id, self.selected.command(), params)
    }

    pub fn resolve(&mut self, request_id: &str, kind: ForkKind) -> Correlation {
        let matches = self
            .pending
            .as_ref()
            .is_some_and(|pending| pending.request_id == request_id && pending.kind == kind);
        if matches {
            self.pending = None;
            Correlation::Applied
        } else {
            Correlation::Stale
        }
    }

    pub fn reject(&mut self, request_id: &str) -> Correlation {
        let Some(pending) = self.pending.as_ref() else {
            return Correlation::Stale;
        };
        if pending.request_id != request_id {
            return Correlation::Stale;
        }
        self.pending = None;
        Correlation::Applied
    }

    fn project_params(&self) -> Result<BTreeMap<&'static str, RpcValue>, WorkflowError> {
        let mut params = BTreeMap::new();
        insert_optional(
            &mut params,
            "source_branch_id",
            &self.draft.source_branch_id,
            MAX_RPC_VALUE_CHARS,
        )?;
        insert_optional(
            &mut params,
            "from_entry_id",
            &self.draft.from_entry_id,
            MAX_RPC_VALUE_CHARS,
        )?;
        insert_optional(
            &mut params,
            "name",
            &self.draft.name,
            MAX_SESSION_NAME_CHARS,
        )?;
        if matches!(self.selected, ForkKind::Session | ForkKind::Worktree) {
            insert_optional(
                &mut params,
                "destination_path",
                &self.draft.destination_path,
                MAX_RPC_VALUE_CHARS,
            )?;
        }
        if self.selected == ForkKind::Worktree {
            insert_optional(
                &mut params,
                "worktree_path",
                &self.draft.worktree_path,
                MAX_RPC_VALUE_CHARS,
            )?;
            insert_optional(
                &mut params,
                "git_branch",
                &self.draft.git_branch,
                MAX_RPC_VALUE_CHARS,
            )?;
        }
        Ok(params)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionRenameState {
    pub session_id: String,
    pub original_name: String,
    pub draft: String,
    pending_request_id: Option<String>,
}

impl SessionRenameState {
    pub fn open(
        session_id: impl Into<String>,
        current_name: impl Into<String>,
    ) -> Result<Self, WorkflowError> {
        let session_id = validate_required(session_id.into(), MAX_SESSION_ID_CHARS, "session ID")?;
        let original_name = validate_public(
            current_name.into().trim().to_owned(),
            MAX_SESSION_NAME_CHARS,
            "session name",
        )?;
        Ok(Self {
            session_id,
            draft: original_name.clone(),
            original_name,
            pending_request_id: None,
        })
    }

    pub fn is_dirty(&self) -> bool {
        self.draft.trim() != self.original_name
    }

    pub fn pending_request_id(&self) -> Option<&str> {
        self.pending_request_id.as_deref()
    }

    pub fn begin(&mut self, request_id: impl Into<String>) -> Result<RpcProjection, WorkflowError> {
        if self.pending_request_id.is_some() {
            return Err(WorkflowError::DuplicateRequest);
        }
        let name = validate_required(self.draft.clone(), MAX_SESSION_NAME_CHARS, "session name")?;
        let request_id = validate_required(request_id.into(), MAX_REQUEST_ID_CHARS, "request ID")?;
        let mut params = BTreeMap::new();
        params.insert("session_id", RpcValue::Text(self.session_id.clone()));
        params.insert("name", RpcValue::Text(name));
        self.pending_request_id = Some(request_id.clone());
        RpcProjection::with_params(request_id, "session_rename", params)
    }

    pub fn resolve(
        &mut self,
        request_id: &str,
        session_id: &str,
        canonical_name: &str,
    ) -> Result<Correlation, WorkflowError> {
        let matches =
            self.pending_request_id.as_deref() == Some(request_id) && self.session_id == session_id;
        if !matches {
            return Ok(Correlation::Stale);
        }
        let name = validate_required(
            canonical_name.to_owned(),
            MAX_SESSION_NAME_CHARS,
            "session name",
        )?;
        self.original_name = name.clone();
        self.draft = name;
        self.pending_request_id = None;
        Ok(Correlation::Applied)
    }

    pub fn reject(&mut self, request_id: &str) -> Correlation {
        if self.pending_request_id.as_deref() != Some(request_id) {
            return Correlation::Stale;
        }
        self.pending_request_id = None;
        Correlation::Applied
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum GoalStatus {
    Active,
    Paused,
    Blocked,
    UsageLimited,
    BudgetLimited,
    Complete,
    Unknown,
}

impl GoalStatus {
    pub fn from_protocol(status: &str) -> Self {
        match status.trim() {
            "active" => Self::Active,
            "paused" => Self::Paused,
            "blocked" => Self::Blocked,
            "usage_limited" => Self::UsageLimited,
            "budget_limited" => Self::BudgetLimited,
            "complete" => Self::Complete,
            _ => Self::Unknown,
        }
    }

    pub const fn is_unfinished(self) -> bool {
        matches!(
            self,
            Self::Active | Self::Paused | Self::Blocked | Self::UsageLimited | Self::Unknown
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GoalSnapshot {
    pub goal_id: String,
    pub objective: String,
    pub status: GoalStatus,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GoalDraft {
    pub objective: String,
    pub token_budget: Option<i64>,
}

impl GoalDraft {
    fn validated(&self) -> Result<Self, WorkflowError> {
        let objective = validate_required(
            self.objective.clone(),
            MAX_GOAL_OBJECTIVE_CHARS,
            "goal objective",
        )?;
        if self.token_budget.is_some_and(|budget| budget <= 0) {
            return Err(WorkflowError::InvalidTokenBudget);
        }
        Ok(Self {
            objective,
            token_budget: self.token_budget,
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GoalCreateAdmission {
    Ready(GoalDraft),
    ConfirmReplacement {
        existing_goal_id: String,
        existing_objective: String,
        replacement: GoalDraft,
    },
}

impl GoalCreateAdmission {
    pub fn begin(current: Option<&GoalSnapshot>, draft: GoalDraft) -> Result<Self, WorkflowError> {
        let draft = draft.validated()?;
        match current.filter(|goal| goal.status.is_unfinished()) {
            Some(goal) => Ok(Self::ConfirmReplacement {
                existing_goal_id: validate_public(
                    goal.goal_id.clone(),
                    MAX_RPC_VALUE_CHARS,
                    "goal ID",
                )?,
                existing_objective: validate_public(
                    goal.objective.clone(),
                    MAX_GOAL_OBJECTIVE_CHARS,
                    "goal objective",
                )?,
                replacement: draft,
            }),
            None => Ok(Self::Ready(draft)),
        }
    }

    pub fn project(
        &self,
        request_id: impl Into<String>,
        confirmed: bool,
    ) -> Result<RpcProjection, WorkflowError> {
        let (draft, replace) = match self {
            Self::Ready(draft) => (draft, false),
            Self::ConfirmReplacement { replacement, .. } if confirmed => (replacement, true),
            Self::ConfirmReplacement { .. } => return Err(WorkflowError::NoPendingRequest),
        };
        let mut params = BTreeMap::new();
        params.insert("objective", RpcValue::Text(draft.objective.clone()));
        if let Some(budget) = draft.token_budget {
            params.insert("token_budget", RpcValue::Integer(budget));
        }
        if replace {
            params.insert("replace", RpcValue::Boolean(true));
        }
        RpcProjection::with_params(request_id, "goal_create", params)
    }
}

/// Return the canonical composer prefill for bare `/goal edit`.
pub fn goal_edit_prefill(current: Option<&GoalSnapshot>) -> Result<Option<String>, WorkflowError> {
    let Some(goal) = current else {
        return Ok(None);
    };
    let objective = validate_required(
        goal.objective.clone(),
        MAX_GOAL_OBJECTIVE_CHARS,
        "goal objective",
    )?;
    Ok(Some(format!("/goal edit {objective}")))
}

/// Secret-free authentication inventory needed by native logout/restriction UI.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthProviderView {
    pub provider_id: String,
    pub display_name: String,
    pub state: String,
    pub method: String,
    pub account_id: String,
}

impl AuthProviderView {
    pub fn has_stored_credential(&self) -> bool {
        matches!(self.state.trim(), "configured" | "expired")
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LogoutChoice {
    pub provider_id: String,
    pub label: String,
    pub state: String,
    pub method: String,
    pub account_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct LogoutChooser {
    choices: Vec<LogoutChoice>,
    selected_provider_id: Option<String>,
}

impl LogoutChooser {
    pub fn from_inventory(inventory: &[AuthProviderView]) -> Self {
        let mut choices = inventory
            .iter()
            .take(MAX_AUTH_PROVIDERS)
            .filter(|provider| provider.has_stored_credential())
            .filter_map(validated_logout_choice)
            .collect::<Vec<_>>();
        choices.sort_by(|left, right| {
            left.label
                .to_lowercase()
                .cmp(&right.label.to_lowercase())
                .then_with(|| left.provider_id.cmp(&right.provider_id))
        });
        choices.dedup_by(|left, right| left.provider_id == right.provider_id);
        let selected_provider_id = choices.first().map(|choice| choice.provider_id.clone());
        Self {
            choices,
            selected_provider_id,
        }
    }

    pub fn choices(&self) -> &[LogoutChoice] {
        &self.choices
    }

    pub fn selected_provider_id(&self) -> Option<&str> {
        self.selected_provider_id.as_deref()
    }

    pub fn select(&mut self, provider_id: &str) -> bool {
        if self
            .choices
            .iter()
            .any(|choice| choice.provider_id == provider_id)
        {
            self.selected_provider_id = Some(provider_id.to_owned());
            true
        } else {
            false
        }
    }

    pub fn project_logout(
        &self,
        request_id: impl Into<String>,
    ) -> Result<RpcProjection, WorkflowError> {
        let provider_id = self
            .selected_provider_id
            .as_ref()
            .ok_or(WorkflowError::NoCredential)?;
        let mut top_level = BTreeMap::new();
        top_level.insert("provider", RpcValue::Text(provider_id.clone()));
        RpcProjection::with_top_level(request_id, "auth_logout", top_level)
    }
}

fn validated_logout_choice(provider: &AuthProviderView) -> Option<LogoutChoice> {
    let provider_id = validate_required(
        provider.provider_id.clone(),
        MAX_PROVIDER_ID_CHARS,
        "provider ID",
    )
    .ok()?;
    let label_source = if provider.display_name.trim().is_empty() {
        provider_id.clone()
    } else {
        provider.display_name.clone()
    };
    let label = validate_public(
        label_source.trim().to_owned(),
        MAX_PROVIDER_LABEL_CHARS,
        "provider label",
    )
    .ok()?;
    let state = validate_public(provider.state.clone(), 32, "credential state").ok()?;
    let method = validate_public(provider.method.clone(), 64, "credential method").ok()?;
    let account_id = validate_public(
        provider.account_id.clone(),
        MAX_ACCOUNT_ID_CHARS,
        "account ID",
    )
    .ok()?;
    Some(LogoutChoice {
        provider_id,
        label,
        state,
        method,
        account_id,
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ChatGptRestrictionCapability {
    /// The protocol inventory exposed a concrete ChatGPT account identifier.
    AccountKnown {
        account_id: String,
        allowed_workspace_ids: Vec<String>,
    },
    /// No account identifier was present. UI must not infer one from labels,
    /// provider IDs, login summaries, URLs, or other presentation strings.
    Unavailable { reason: &'static str },
}

impl ChatGptRestrictionCapability {
    pub fn from_inventory(inventory: &[AuthProviderView]) -> Self {
        let account_id = inventory
            .iter()
            .filter(|provider| provider.provider_id == "chatgpt")
            .map(|provider| provider.account_id.trim())
            .find(|account_id| !account_id.is_empty())
            .and_then(|account_id| {
                validate_public(account_id.to_owned(), MAX_ACCOUNT_ID_CHARS, "account ID").ok()
            });
        match account_id {
            Some(account_id) => Self::AccountKnown {
                account_id,
                allowed_workspace_ids: Vec::new(),
            },
            None => Self::Unavailable {
                reason: "auth provider inventory did not expose a ChatGPT account ID",
            },
        }
    }

    pub fn account_id(&self) -> Option<&str> {
        match self {
            Self::AccountKnown { account_id, .. } => Some(account_id),
            Self::Unavailable { .. } => None,
        }
    }

    /// Replace the explicit workspace allowlist. Workspace IDs are user- or
    /// protocol-supplied opaque identifiers; this module never invents them.
    pub fn set_allowed_workspace_ids(
        &mut self,
        workspace_ids: impl IntoIterator<Item = String>,
    ) -> Result<(), WorkflowError> {
        let Self::AccountKnown {
            allowed_workspace_ids,
            ..
        } = self
        else {
            return Err(WorkflowError::RestrictionUnavailable);
        };
        let mut next = workspace_ids.into_iter().collect::<Vec<_>>();
        if next.len() > MAX_WORKSPACE_IDS {
            return Err(WorkflowError::TooManyWorkspaceIds);
        }
        for workspace_id in &mut next {
            *workspace_id =
                validate_required(workspace_id.clone(), MAX_WORKSPACE_ID_CHARS, "workspace ID")?;
        }
        next.sort();
        next.dedup();
        *allowed_workspace_ids = next;
        Ok(())
    }

    /// Add exact `allowed_workspace_ids` values to a caller's
    /// `auth_login_start` params. Account ID remains presentation/capability
    /// metadata because protocol v1 does not accept an account-selection field.
    pub fn login_params(&self) -> Result<Vec<(&'static str, Vec<String>)>, WorkflowError> {
        match self {
            Self::AccountKnown {
                allowed_workspace_ids,
                ..
            } => Ok(if allowed_workspace_ids.is_empty() {
                Vec::new()
            } else {
                vec![("allowed_workspace_ids", allowed_workspace_ids.clone())]
            }),
            Self::Unavailable { .. } => Err(WorkflowError::RestrictionUnavailable),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn text(value: &str) -> RpcValue {
        RpcValue::Text(value.to_owned())
    }

    #[test]
    fn fork_chooser_projects_all_three_canonical_commands_and_params() {
        let mut chooser = ForkChooser {
            draft: ForkDraft {
                source_branch_id: "feature".into(),
                from_entry_id: "entry-7".into(),
                name: "child".into(),
                destination_path: "/tmp/session".into(),
                worktree_path: "/tmp/worktree".into(),
                git_branch: "snow/child".into(),
            },
            ..ForkChooser::default()
        };

        let branch = chooser.begin("branch-request").unwrap();
        assert_eq!(branch.command, "branch_fork");
        assert_eq!(branch.params["source_branch_id"], text("feature"));
        assert!(!branch.params.contains_key("destination_path"));
        assert_eq!(
            chooser.resolve("other", ForkKind::Branch),
            Correlation::Stale
        );
        assert_eq!(
            chooser.resolve("branch-request", ForkKind::Branch),
            Correlation::Applied
        );

        chooser.select(ForkKind::Session);
        let session = chooser.begin("session-request").unwrap();
        assert_eq!(session.command, "session_fork");
        assert_eq!(session.params["destination_path"], text("/tmp/session"));
        assert!(!session.params.contains_key("worktree_path"));
        assert_eq!(
            chooser.resolve("session-request", ForkKind::Session),
            Correlation::Applied
        );

        chooser.select(ForkKind::Worktree);
        let worktree = chooser.begin("worktree-request").unwrap();
        assert_eq!(worktree.command, "session_worktree_fork");
        assert_eq!(worktree.params["worktree_path"], text("/tmp/worktree"));
        assert_eq!(worktree.params["git_branch"], text("snow/child"));
    }

    #[test]
    fn inactive_session_rename_validates_and_correlates_identity() {
        let mut rename = SessionRenameState::open("inactive-session", "Old").unwrap();
        rename.draft = " New name ".into();
        assert!(rename.is_dirty());
        let request = rename.begin("rename-1").unwrap();
        assert_eq!(request.command, "session_rename");
        assert_eq!(request.params["session_id"], text("inactive-session"));
        assert_eq!(request.params["name"], text("New name"));
        assert_eq!(
            rename
                .resolve("rename-1", "wrong-session", "Wrong")
                .unwrap(),
            Correlation::Stale
        );
        assert_eq!(rename.pending_request_id(), Some("rename-1"));
        assert_eq!(
            rename
                .resolve("rename-1", "inactive-session", "Canonical")
                .unwrap(),
            Correlation::Applied
        );
        assert_eq!(rename.original_name, "Canonical");
        assert!(!rename.is_dirty());
    }

    #[test]
    fn inactive_session_rename_enforces_canonical_title_bound() {
        let mut rename = SessionRenameState::open("session", "Old").unwrap();
        rename.draft = "x".repeat(MAX_SESSION_NAME_CHARS + 1);
        assert!(matches!(
            rename.begin("rename"),
            Err(WorkflowError::TooLong {
                field: "session name",
                max_chars: MAX_SESSION_NAME_CHARS
            })
        ));
    }

    #[test]
    fn unfinished_goal_requires_explicit_replacement_confirmation() {
        let current = GoalSnapshot {
            goal_id: "goal-1".into(),
            objective: "Old objective".into(),
            status: GoalStatus::Paused,
        };
        let admission = GoalCreateAdmission::begin(
            Some(&current),
            GoalDraft {
                objective: "New objective".into(),
                token_budget: Some(1_000),
            },
        )
        .unwrap();
        assert!(matches!(
            admission.project("goal-request", false),
            Err(WorkflowError::NoPendingRequest)
        ));
        let request = admission.project("goal-request", true).unwrap();
        assert_eq!(request.command, "goal_create");
        assert_eq!(request.params["objective"], text("New objective"));
        assert_eq!(request.params["token_budget"], RpcValue::Integer(1_000));
        assert_eq!(request.params["replace"], RpcValue::Boolean(true));
    }

    #[test]
    fn bare_goal_edit_prefills_current_objective() {
        let goal = GoalSnapshot {
            goal_id: "goal".into(),
            objective: "Preserve exact objective".into(),
            status: GoalStatus::Active,
        };
        assert_eq!(
            goal_edit_prefill(Some(&goal)).unwrap().as_deref(),
            Some("/goal edit Preserve exact objective")
        );
        assert_eq!(goal_edit_prefill(None).unwrap(), None);
    }

    fn provider(id: &str, label: &str, state: &str, account_id: &str) -> AuthProviderView {
        AuthProviderView {
            provider_id: id.into(),
            display_name: label.into(),
            state: state.into(),
            method: "oauth".into(),
            account_id: account_id.into(),
        }
    }

    #[test]
    fn logout_chooser_filters_credentials_orders_and_projects_top_level_provider() {
        let inventory = vec![
            provider("z", "Zulu", "configured", "account-z"),
            provider("missing", "Missing", "missing", ""),
            provider("a", "Alpha", "expired", "account-a"),
            provider("invalid", "Invalid", "invalid", ""),
        ];
        let mut chooser = LogoutChooser::from_inventory(&inventory);
        assert_eq!(
            chooser
                .choices()
                .iter()
                .map(|choice| choice.provider_id.as_str())
                .collect::<Vec<_>>(),
            vec!["a", "z"]
        );
        assert!(chooser.select("z"));
        assert!(!chooser.select("missing"));
        let request = chooser.project_logout("logout-1").unwrap();
        assert_eq!(request.command, "auth_logout");
        assert_eq!(request.top_level["provider"], text("z"));
        assert!(request.params.is_empty());
    }

    #[test]
    fn logout_chooser_omits_invalid_or_oversized_public_metadata() {
        let inventory = vec![provider(
            &"p".repeat(MAX_PROVIDER_ID_CHARS + 1),
            "Too long",
            "configured",
            "",
        )];
        let chooser = LogoutChooser::from_inventory(&inventory);
        assert!(chooser.choices().is_empty());
        assert_eq!(
            chooser.project_logout("logout"),
            Err(WorkflowError::NoCredential)
        );
    }

    #[test]
    fn chatgpt_restrictions_use_only_inventory_account_ids_and_explicit_workspaces() {
        let inventory = vec![provider("chatgpt", "ChatGPT", "configured", "account-123")];
        let mut restrictions = ChatGptRestrictionCapability::from_inventory(&inventory);
        assert_eq!(restrictions.account_id(), Some("account-123"));
        restrictions
            .set_allowed_workspace_ids(vec![
                "workspace-b".into(),
                "workspace-a".into(),
                "workspace-b".into(),
            ])
            .unwrap();
        assert_eq!(
            restrictions.login_params().unwrap(),
            vec![(
                "allowed_workspace_ids",
                vec!["workspace-a".to_owned(), "workspace-b".to_owned()]
            )]
        );
    }

    #[test]
    fn chatgpt_restrictions_report_capability_absence_without_guessing() {
        let inventory = vec![provider("chatgpt", "ChatGPT account foo", "configured", "")];
        let mut restrictions = ChatGptRestrictionCapability::from_inventory(&inventory);
        assert!(matches!(
            restrictions,
            ChatGptRestrictionCapability::Unavailable { .. }
        ));
        assert_eq!(restrictions.account_id(), None);
        assert_eq!(
            restrictions.set_allowed_workspace_ids(vec!["guessed".into()]),
            Err(WorkflowError::RestrictionUnavailable)
        );
        assert_eq!(
            restrictions.login_params(),
            Err(WorkflowError::RestrictionUnavailable)
        );
    }

    #[test]
    fn all_public_text_is_bounded_and_control_free() {
        assert!(matches!(
            SessionRenameState::open("session\nother", "name"),
            Err(WorkflowError::ControlCharacter("session ID"))
        ));
        assert!(matches!(
            GoalCreateAdmission::begin(
                None,
                GoalDraft {
                    objective: "x".repeat(MAX_GOAL_OBJECTIVE_CHARS + 1),
                    token_budget: None,
                }
            ),
            Err(WorkflowError::TooLong {
                field: "goal objective",
                max_chars: MAX_GOAL_OBJECTIVE_CHARS
            })
        ));
        assert!(matches!(
            GoalCreateAdmission::begin(
                None,
                GoalDraft {
                    objective: "valid".into(),
                    token_budget: Some(0),
                }
            ),
            Err(WorkflowError::InvalidTokenBudget)
        ));
    }
}
