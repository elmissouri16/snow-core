//! Pure state for the desktop subagent fleet and selected-agent detail views.

use std::collections::{HashMap, HashSet, VecDeque};

#[cfg(test)]
use crate::snow::AgentRef;
use crate::snow::{SubagentList, SubagentState};

pub const MAX_LIVE_ACTIVITY_LINES: usize = 128;
pub const MAX_LIVE_ACTIVITY_BYTES: usize = 32 * 1024;
const MAX_LIVE_ACTIVITY_LINE_BYTES: usize = 4 * 1024;
const ROOT_AGENT_PATH: &str = "/root";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SubagentAction {
    Interrupt,
    Resume,
    Close,
}

impl SubagentAction {
    pub fn rpc_command(self) -> &'static str {
        match self {
            Self::Interrupt => "subagent_interrupt",
            Self::Resume => "subagent_resume",
            Self::Close => "subagent_close",
        }
    }
}

/// Return the sole lifecycle action valid for a child snapshot.
pub fn eligible_action(state: &SubagentState) -> Option<SubagentAction> {
    if is_root(state) {
        return None;
    }
    match state.status.as_str() {
        "pending_init" => None,
        "running" | "queued" => Some(SubagentAction::Interrupt),
        "closed" => Some(SubagentAction::Resume),
        "interrupted" | "completed" | "errored" | "shutdown" | "not_loaded" => {
            Some(SubagentAction::Close)
        }
        _ => None,
    }
}

fn is_root(state: &SubagentState) -> bool {
    state.agent.path == ROOT_AGENT_PATH
}

fn is_terminal(status: &str) -> bool {
    matches!(
        status,
        "interrupted" | "completed" | "errored" | "shutdown" | "closed" | "not_loaded"
    )
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VersionedSubagentState {
    pub state: SubagentState,
    pub generation: u64,
}

impl VersionedSubagentState {
    pub fn new(state: SubagentState, generation: u64) -> Self {
        Self { state, generation }
    }
}

impl From<SubagentState> for VersionedSubagentState {
    fn from(state: SubagentState) -> Self {
        Self::new(state, 0)
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct FleetSummary {
    pub running: usize,
    pub queued: usize,
    pub terminal: usize,
    pub open: usize,
    pub closed: usize,
    pub concurrent_limit: usize,
    pub agent_limit: usize,
    pub truncated: bool,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct FleetListSnapshot {
    pub agents: Vec<VersionedSubagentState>,
    pub summary: FleetSummary,
}

impl From<SubagentList> for FleetListSnapshot {
    fn from(list: SubagentList) -> Self {
        Self {
            agents: list.agents.into_iter().map(Into::into).collect(),
            summary: FleetSummary {
                running: list.running,
                queued: list.queued,
                terminal: list.terminal,
                open: list.open,
                closed: list.closed,
                concurrent_limit: list.concurrent_limit,
                agent_limit: list.agent_limit,
                truncated: list.truncated,
            },
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AgentSelection {
    pub path: String,
    pub thread_id: String,
}

impl AgentSelection {
    fn from_state(state: &SubagentState) -> Self {
        Self {
            path: state.agent.path.clone(),
            thread_id: state.agent.thread_id.clone(),
        }
    }

    fn matches(&self, state: &SubagentState) -> bool {
        self.thread_id == state.agent.thread_id || self.path == state.agent.path
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ListRequest {
    pub generation: u64,
    pub path_prefix: String,
    pub target: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DetailRequest {
    pub generation: u64,
    pub selection: AgentSelection,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ListApply {
    Applied,
    TargetMissing,
    Stale,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DetailApply {
    Applied,
    Stale,
    WrongAgent,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SnapshotApply {
    Inserted,
    Updated,
    Stale,
    Filtered,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ActivityKind {
    Text,
    Thinking,
    Tool,
    Status,
    Error,
    Other,
}

impl ActivityKind {
    pub fn from_event_kind(kind: &str) -> Self {
        match kind {
            "text_delta" => Self::Text,
            "thinking_delta" => Self::Thinking,
            "tool_start" | "tool_progress" | "tool_end" => Self::Tool,
            "subagent_started" | "subagent_status" | "subagent_activity" => Self::Status,
            "error" | "aborted" => Self::Error,
            _ => Self::Other,
        }
    }

    fn coalesces(self) -> bool {
        matches!(self, Self::Text | Self::Thinking)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActivityLine {
    pub kind: ActivityKind,
    pub text: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct LiveActivity {
    lines: VecDeque<ActivityLine>,
    bytes: usize,
    previous_fragment_ended_space: bool,
}

impl LiveActivity {
    pub fn lines(&self) -> &VecDeque<ActivityLine> {
        &self.lines
    }

    pub fn byte_len(&self) -> usize {
        self.bytes
    }

    pub fn push(&mut self, kind: ActivityKind, text: &str) {
        let starts_space = starts_with_space(text);
        let ends_space = ends_with_space(text);
        let compact = compact_fragment(text);

        if kind.coalesces() && self.lines.back().is_some_and(|line| line.kind == kind) {
            if !compact.is_empty() {
                let line = self.lines.back_mut().expect("checked above");
                self.bytes -= line.text.len();
                if (self.previous_fragment_ended_space || starts_space) && !line.text.is_empty() {
                    line.text.push(' ');
                }
                line.text.push_str(&compact);
                line.text = utf8_tail(&line.text, MAX_LIVE_ACTIVITY_LINE_BYTES).to_owned();
                self.bytes += line.text.len();
            }
        } else if !compact.is_empty() {
            let text = utf8_tail(&compact, MAX_LIVE_ACTIVITY_LINE_BYTES).to_owned();
            self.bytes += text.len();
            self.lines.push_back(ActivityLine { kind, text });
        }

        self.previous_fragment_ended_space = kind.coalesces() && ends_space;
        self.enforce_bounds();
    }

    fn enforce_bounds(&mut self) {
        while self.lines.len() > MAX_LIVE_ACTIVITY_LINES {
            self.pop_front();
        }
        while self.bytes > MAX_LIVE_ACTIVITY_BYTES && self.lines.len() > 1 {
            self.pop_front();
        }
        debug_assert!(self.lines.len() <= MAX_LIVE_ACTIVITY_LINES);
        debug_assert!(self.bytes <= MAX_LIVE_ACTIVITY_BYTES);
    }

    fn pop_front(&mut self) {
        if let Some(line) = self.lines.pop_front() {
            self.bytes -= line.text.len();
        }
    }
}

#[derive(Debug, Default)]
pub struct SubagentFleetState {
    agents: Vec<VersionedSubagentState>,
    summary: FleetSummary,
    selection: Option<AgentSelection>,
    detail: Option<VersionedSubagentState>,
    path_prefix: String,
    list_request_generation: u64,
    detail_request_generation: u64,
    activity: HashMap<String, LiveActivity>,
    event_threads: HashSet<String>,
}

impl SubagentFleetState {
    pub fn agents(&self) -> &[VersionedSubagentState] {
        &self.agents
    }

    pub fn summary(&self) -> &FleetSummary {
        &self.summary
    }

    pub fn selection(&self) -> Option<&AgentSelection> {
        self.selection.as_ref()
    }

    pub fn selected(&self) -> Option<&VersionedSubagentState> {
        let selection = self.selection.as_ref()?;
        self.agents
            .iter()
            .find(|agent| selection.matches(&agent.state))
    }

    pub fn detail(&self) -> Option<&VersionedSubagentState> {
        self.detail.as_ref()
    }

    pub fn path_prefix(&self) -> &str {
        &self.path_prefix
    }

    pub fn set_path_prefix(&mut self, path_prefix: impl Into<String>) {
        self.path_prefix = path_prefix.into().trim().to_owned();
    }

    pub fn activity(&self, thread_id: &str) -> Option<&LiveActivity> {
        self.activity.get(thread_id)
    }

    pub fn selected_action(&self) -> Option<SubagentAction> {
        eligible_action(&self.selected()?.state)
    }

    pub fn begin_list_request(&mut self, target: Option<&str>) -> ListRequest {
        self.list_request_generation = next_generation(self.list_request_generation);
        ListRequest {
            generation: self.list_request_generation,
            path_prefix: self.path_prefix.clone(),
            target: target
                .map(str::trim)
                .filter(|target| !target.is_empty())
                .map(str::to_owned),
        }
    }

    pub fn apply_list_response(
        &mut self,
        request: &ListRequest,
        list: impl Into<FleetListSnapshot>,
    ) -> ListApply {
        if request.generation != self.list_request_generation
            || request.path_prefix != self.path_prefix
        {
            return ListApply::Stale;
        }

        let mut incoming = list.into();
        let live_by_thread: HashMap<_, _> = self
            .agents
            .iter()
            .map(|agent| (agent.state.agent.thread_id.as_str(), agent))
            .collect();
        let mut seen = HashSet::with_capacity(incoming.agents.len());
        for agent in &mut incoming.agents {
            seen.insert(agent.state.agent.thread_id.clone());
            if let Some(live) = live_by_thread.get(agent.state.agent.thread_id.as_str())
                && prefer_live(live, agent)
            {
                *agent = (*live).clone();
            }
        }

        for live in &self.agents {
            if self.event_threads.contains(&live.state.agent.thread_id)
                && !seen.contains(&live.state.agent.thread_id)
                && path_matches_prefix(&live.state.agent.path, &request.path_prefix)
            {
                incoming.agents.push(live.clone());
            }
        }

        let previous = self.selection.clone();
        self.agents = incoming.agents;
        self.summary = incoming.summary;
        self.recount_visible_children();
        self.activity.retain(|thread, _| {
            self.agents
                .iter()
                .any(|agent| &agent.state.agent.thread_id == thread)
        });
        self.event_threads.retain(|thread| {
            self.agents
                .iter()
                .any(|agent| &agent.state.agent.thread_id == thread)
        });

        let selected_index = if let Some(target) = request.target.as_deref() {
            self.find_target(target)
        } else {
            previous
                .as_ref()
                .and_then(|selection| self.find_selection(selection))
                .or_else(|| (!self.agents.is_empty()).then_some(0))
        };

        let Some(index) = selected_index else {
            self.selection = None;
            self.detail = None;
            self.invalidate_detail_requests();
            return if request.target.is_some() {
                ListApply::TargetMissing
            } else {
                ListApply::Applied
            };
        };
        self.selection = Some(AgentSelection::from_state(&self.agents[index].state));
        if !self.detail.as_ref().is_some_and(|detail| {
            self.selection
                .as_ref()
                .is_some_and(|s| s.matches(&detail.state))
        }) {
            self.detail = None;
        }
        ListApply::Applied
    }

    pub fn select(&mut self, target: &str) -> bool {
        let Some(index) = self.find_target(target.trim()) else {
            return false;
        };
        let next = AgentSelection::from_state(&self.agents[index].state);
        if self.selection.as_ref() != Some(&next) {
            self.selection = Some(next);
            self.detail = None;
            self.invalidate_detail_requests();
        }
        true
    }

    pub fn begin_detail_request(&mut self) -> Option<DetailRequest> {
        let selection = self.selection.clone()?;
        self.detail_request_generation = next_generation(self.detail_request_generation);
        Some(DetailRequest {
            generation: self.detail_request_generation,
            selection,
        })
    }

    pub fn apply_detail_response(
        &mut self,
        request: &DetailRequest,
        snapshot: VersionedSubagentState,
    ) -> DetailApply {
        if request.generation != self.detail_request_generation
            || self.selection.as_ref() != Some(&request.selection)
        {
            return DetailApply::Stale;
        }
        if !request.selection.matches(&snapshot.state) {
            return DetailApply::WrongAgent;
        }
        if let Some(current) = self
            .agents
            .iter()
            .find(|agent| request.selection.matches(&agent.state))
            && prefer_live(current, &snapshot)
        {
            self.detail = Some(current.clone());
        } else {
            self.detail = Some(snapshot);
        }
        DetailApply::Applied
    }

    pub fn upsert_event_snapshot(&mut self, snapshot: VersionedSubagentState) -> SnapshotApply {
        if !path_matches_prefix(&snapshot.state.agent.path, &self.path_prefix) {
            return SnapshotApply::Filtered;
        }
        let thread_id = snapshot.state.agent.thread_id.clone();
        if let Some(index) = self
            .agents
            .iter()
            .position(|agent| agent.state.agent.thread_id == thread_id)
        {
            if snapshot.generation < self.agents[index].generation {
                return SnapshotApply::Stale;
            }
            let previous = self.agents[index].state.clone();
            self.agents[index] = snapshot;
            self.event_threads.insert(thread_id);
            self.adjust_capacity(Some(&previous), &self.agents[index].state.clone());
            self.refresh_selected_after_event(index);
            self.recount_visible_children();
            SnapshotApply::Updated
        } else {
            self.adjust_capacity(None, &snapshot.state);
            self.event_threads.insert(thread_id);
            self.agents.push(snapshot);
            self.recount_visible_children();
            SnapshotApply::Inserted
        }
    }

    pub fn record_activity(&mut self, thread_id: &str, kind: ActivityKind, text: &str) -> bool {
        if !self
            .agents
            .iter()
            .any(|agent| agent.state.agent.thread_id == thread_id)
        {
            return false;
        }
        self.activity
            .entry(thread_id.to_owned())
            .or_default()
            .push(kind, text);
        true
    }

    pub fn record_path_activity(&mut self, path: &str, kind: ActivityKind, text: &str) -> bool {
        let Some(thread_id) = self
            .agents
            .iter()
            .find(|agent| agent.state.agent.path == path)
            .map(|agent| agent.state.agent.thread_id.clone())
        else {
            return false;
        };
        self.record_activity(&thread_id, kind, text)
    }

    pub fn invalidate_requests(&mut self) {
        self.list_request_generation = next_generation(self.list_request_generation);
        self.invalidate_detail_requests();
    }

    fn invalidate_detail_requests(&mut self) {
        self.detail_request_generation = next_generation(self.detail_request_generation);
    }

    fn find_target(&self, target: &str) -> Option<usize> {
        self.agents.iter().position(|agent| {
            agent.state.agent.path == target || agent.state.agent.thread_id == target
        })
    }

    fn find_selection(&self, selection: &AgentSelection) -> Option<usize> {
        self.agents
            .iter()
            .position(|agent| agent.state.agent.thread_id == selection.thread_id)
            .or_else(|| {
                self.agents
                    .iter()
                    .position(|agent| agent.state.agent.path == selection.path)
            })
    }

    fn refresh_selected_after_event(&mut self, index: usize) {
        let Some(selection) = self.selection.as_ref() else {
            return;
        };
        if selection.matches(&self.agents[index].state) {
            self.selection = Some(AgentSelection::from_state(&self.agents[index].state));
            self.detail = Some(self.agents[index].clone());
        }
    }

    fn recount_visible_children(&mut self) {
        self.summary.running = 0;
        self.summary.queued = 0;
        self.summary.terminal = 0;
        for agent in &self.agents {
            if is_root(&agent.state) {
                continue;
            }
            match agent.state.status.as_str() {
                "running" => self.summary.running += 1,
                "pending_init" | "queued" => self.summary.queued += 1,
                status if is_terminal(status) => self.summary.terminal += 1,
                _ => {}
            }
        }
    }

    fn adjust_capacity(&mut self, previous: Option<&SubagentState>, next: &SubagentState) {
        if is_root(next) || previous.is_some_and(is_root) {
            return;
        }
        let was_closed = previous.is_some_and(|state| state.status == "closed");
        let is_closed = next.status == "closed";
        match previous {
            None if is_closed => self.summary.closed += 1,
            None => self.summary.open += 1,
            Some(_) if was_closed == is_closed => {}
            Some(_) if is_closed => {
                self.summary.open = self.summary.open.saturating_sub(1);
                self.summary.closed += 1;
            }
            Some(_) => {
                self.summary.closed = self.summary.closed.saturating_sub(1);
                self.summary.open += 1;
            }
        }
    }
}

fn prefer_live(live: &VersionedSubagentState, snapshot: &VersionedSubagentState) -> bool {
    live.generation > snapshot.generation
        || (live.generation == snapshot.generation
            && is_terminal(&live.state.status)
            && !is_terminal(&snapshot.state.status))
}

fn path_matches_prefix(path: &str, prefix: &str) -> bool {
    let prefix = prefix.trim_end_matches('/');
    prefix.is_empty()
        || path == prefix
        || path
            .strip_prefix(prefix)
            .is_some_and(|suffix| suffix.starts_with('/'))
}

fn next_generation(generation: u64) -> u64 {
    generation
        .checked_add(1)
        .expect("subagent request generation exhausted")
}

fn compact_fragment(text: &str) -> String {
    text.split_whitespace().collect::<Vec<_>>().join(" ")
}

fn starts_with_space(text: &str) -> bool {
    text.chars().next().is_some_and(char::is_whitespace)
}

fn ends_with_space(text: &str) -> bool {
    text.chars().next_back().is_some_and(char::is_whitespace)
}

fn utf8_tail(text: &str, max_bytes: usize) -> &str {
    if text.len() <= max_bytes {
        return text;
    }
    let mut start = text.len() - max_bytes;
    while !text.is_char_boundary(start) {
        start += 1;
    }
    &text[start..]
}

#[cfg(test)]
mod tests {
    use super::*;

    fn state(thread: &str, path: &str, status: &str) -> SubagentState {
        let parent_path = path.rsplit_once('/').map_or("", |(parent, _)| parent);
        SubagentState {
            agent: AgentRef {
                thread_id: thread.into(),
                parent_thread_id: (path != ROOT_AGENT_PATH)
                    .then_some("root")
                    .unwrap_or("")
                    .into(),
                path: path.into(),
                parent_path: parent_path.into(),
                role: if path == ROOT_AGENT_PATH {
                    "root"
                } else {
                    "builder"
                }
                .into(),
                nickname: String::new(),
                depth: path.matches('/').count().saturating_sub(1),
            },
            status: status.into(),
            ..SubagentState::default()
        }
    }

    fn versioned(
        thread: &str,
        path: &str,
        status: &str,
        generation: u64,
    ) -> VersionedSubagentState {
        VersionedSubagentState::new(state(thread, path, status), generation)
    }

    fn list(agents: Vec<VersionedSubagentState>) -> FleetListSnapshot {
        FleetListSnapshot {
            agents,
            summary: FleetSummary {
                concurrent_limit: 4,
                agent_limit: 32,
                ..FleetSummary::default()
            },
        }
    }

    fn loaded() -> SubagentFleetState {
        let mut fleet = SubagentFleetState::default();
        let request = fleet.begin_list_request(None);
        assert_eq!(
            fleet.apply_list_response(
                &request,
                list(vec![
                    versioned("one", "/root/one", "running", 1),
                    versioned("two", "/root/two", "completed", 2),
                ])
            ),
            ListApply::Applied
        );
        fleet
    }

    #[test]
    fn action_matrix_is_exact_and_root_never_has_an_action() {
        let cases = [
            ("pending_init", None),
            ("running", Some(SubagentAction::Interrupt)),
            ("queued", Some(SubagentAction::Interrupt)),
            ("closed", Some(SubagentAction::Resume)),
            ("interrupted", Some(SubagentAction::Close)),
            ("completed", Some(SubagentAction::Close)),
            ("errored", Some(SubagentAction::Close)),
            ("shutdown", Some(SubagentAction::Close)),
            ("not_loaded", Some(SubagentAction::Close)),
            ("not_found", None),
            ("future_status", None),
        ];
        for (status, expected) in cases {
            assert_eq!(
                eligible_action(&state("child", "/root/child", status)),
                expected
            );
        }
        for status in cases.map(|(status, _)| status) {
            assert_eq!(
                eligible_action(&state("root", ROOT_AGENT_PATH, status)),
                None
            );
        }
        assert_eq!(
            SubagentAction::Interrupt.rpc_command(),
            "subagent_interrupt"
        );
    }

    #[test]
    fn list_reconciliation_keeps_newer_and_equal_terminal_live_states() {
        let mut fleet = loaded();
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("one", "/root/one", "completed", 4)),
            SnapshotApply::Updated
        );
        let request = fleet.begin_list_request(None);
        let response = list(vec![
            versioned("one", "/root/one", "running", 3),
            versioned("two", "/root/two", "running", 2),
        ]);
        assert_eq!(
            fleet.apply_list_response(&request, response),
            ListApply::Applied
        );
        assert_eq!(fleet.agents[0].state.status, "completed");
        assert_eq!(fleet.agents[0].generation, 4);
        assert_eq!(fleet.agents[1].state.status, "completed");
        assert_eq!(fleet.agents[1].generation, 2);
    }

    #[test]
    fn selection_survives_reorder_by_thread_then_path() {
        let mut fleet = loaded();
        assert!(fleet.select("/root/two"));
        let request = fleet.begin_list_request(None);
        assert_eq!(
            fleet.apply_list_response(
                &request,
                list(vec![
                    versioned("two", "/root/renamed", "completed", 3),
                    versioned("one", "/root/one", "running", 1),
                ])
            ),
            ListApply::Applied
        );
        assert_eq!(fleet.selection().unwrap().thread_id, "two");
        assert_eq!(fleet.selection().unwrap().path, "/root/renamed");

        fleet.selection = Some(AgentSelection {
            path: "/root/one".into(),
            thread_id: "obsolete".into(),
        });
        let request = fleet.begin_list_request(None);
        assert_eq!(
            fleet.apply_list_response(
                &request,
                list(vec![versioned("replacement", "/root/one", "running", 1)])
            ),
            ListApply::Applied
        );
        assert_eq!(fleet.selection().unwrap().thread_id, "replacement");
    }

    #[test]
    fn explicit_missing_target_does_not_silently_select_another_agent() {
        let mut fleet = loaded();
        let request = fleet.begin_list_request(Some("/root/missing"));
        assert_eq!(
            fleet.apply_list_response(
                &request,
                list(vec![versioned("one", "/root/one", "running", 1)])
            ),
            ListApply::TargetMissing
        );
        assert!(fleet.selection().is_none());
        assert!(fleet.detail().is_none());
    }

    #[test]
    fn request_generations_reject_stale_list_and_detail_results() {
        let mut fleet = loaded();
        let stale_list = fleet.begin_list_request(None);
        let fresh_list = fleet.begin_list_request(None);
        let original = fleet.agents.clone();
        assert_eq!(
            fleet.apply_list_response(
                &stale_list,
                list(vec![versioned("stale", "/root/stale", "running", 1)])
            ),
            ListApply::Stale
        );
        assert_eq!(fleet.agents, original);
        assert_eq!(
            fleet.apply_list_response(&fresh_list, list(original)),
            ListApply::Applied
        );

        assert!(fleet.select("one"));
        let stale_detail = fleet.begin_detail_request().unwrap();
        let fresh_detail = fleet.begin_detail_request().unwrap();
        assert_eq!(
            fleet.apply_detail_response(
                &stale_detail,
                versioned("one", "/root/one", "completed", 9)
            ),
            DetailApply::Stale
        );
        assert!(fleet.detail().is_none());
        assert_eq!(
            fleet.apply_detail_response(
                &fresh_detail,
                versioned("two", "/root/two", "completed", 9)
            ),
            DetailApply::WrongAgent
        );
        assert_eq!(
            fleet.apply_detail_response(
                &fresh_detail,
                versioned("one", "/root/one", "completed", 9)
            ),
            DetailApply::Applied
        );
        fleet.invalidate_requests();
        assert_eq!(
            fleet.apply_detail_response(
                &fresh_detail,
                versioned("one", "/root/one", "completed", 10)
            ),
            DetailApply::Stale
        );
    }

    #[test]
    fn event_upsert_is_generation_monotonic_and_updates_selected_detail() {
        let mut fleet = loaded();
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("one", "/root/one", "completed", 5)),
            SnapshotApply::Updated
        );
        assert_eq!(fleet.selected().unwrap().generation, 5);
        assert_eq!(fleet.detail().unwrap().state.status, "completed");
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("one", "/root/one", "running", 4)),
            SnapshotApply::Stale
        );
        assert_eq!(fleet.selected().unwrap().state.status, "completed");
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("one", "/root/one", "errored", 5)),
            SnapshotApply::Updated
        );
        assert_eq!(fleet.selected().unwrap().state.status, "errored");
    }

    #[test]
    fn event_rows_racing_a_list_are_preserved_but_filter_is_respected() {
        let mut fleet = loaded();
        let request = fleet.begin_list_request(None);
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("three", "/root/three", "running", 1)),
            SnapshotApply::Inserted
        );
        assert_eq!(
            fleet.apply_list_response(
                &request,
                list(vec![versioned("one", "/root/one", "running", 1)])
            ),
            ListApply::Applied
        );
        assert!(
            fleet
                .agents
                .iter()
                .any(|agent| agent.state.agent.thread_id == "three")
        );

        fleet.set_path_prefix("/root/one/");
        let filtered = fleet.begin_list_request(None);
        assert_eq!(
            fleet.apply_list_response(
                &filtered,
                list(vec![versioned("one", "/root/one", "running", 1)])
            ),
            ListApply::Applied
        );
        assert_eq!(fleet.agents.len(), 1);
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("outside", "/root/two", "running", 1)),
            SnapshotApply::Filtered
        );
    }

    #[test]
    fn path_prefix_is_trimmed_preserved_and_part_of_staleness() {
        let mut fleet = SubagentFleetState::default();
        fleet.set_path_prefix("  /root/team/  ");
        let old = fleet.begin_list_request(None);
        assert_eq!(old.path_prefix, "/root/team/");
        fleet.set_path_prefix("/root/other");
        assert_eq!(
            fleet.apply_list_response(&old, FleetListSnapshot::default()),
            ListApply::Stale
        );
        let current = fleet.begin_list_request(None);
        assert_eq!(current.path_prefix, "/root/other");
        assert_eq!(
            fleet.apply_list_response(&current, FleetListSnapshot::default()),
            ListApply::Applied
        );
        fleet.invalidate_requests();
        assert_eq!(fleet.path_prefix(), "/root/other");
    }

    #[test]
    fn root_is_excluded_from_counts_capacity_and_actions() {
        let mut fleet = SubagentFleetState::default();
        let request = fleet.begin_list_request(None);
        let snapshot = FleetListSnapshot {
            agents: vec![
                versioned("root", ROOT_AGENT_PATH, "running", 1),
                versioned("pending", "/root/pending", "pending_init", 1),
                versioned("running", "/root/running", "running", 1),
                versioned("closed", "/root/closed", "closed", 1),
            ],
            summary: FleetSummary {
                open: 2,
                closed: 1,
                ..FleetSummary::default()
            },
        };
        assert_eq!(
            fleet.apply_list_response(&request, snapshot),
            ListApply::Applied
        );
        assert_eq!(fleet.summary.running, 1);
        assert_eq!(fleet.summary.queued, 1);
        assert_eq!(fleet.summary.terminal, 1);
        assert_eq!((fleet.summary.open, fleet.summary.closed), (2, 1));
        assert_eq!(fleet.selected_action(), None);
        assert_eq!(
            fleet.upsert_event_snapshot(versioned("root", ROOT_AGENT_PATH, "closed", 2)),
            SnapshotApply::Updated
        );
        assert_eq!((fleet.summary.open, fleet.summary.closed), (2, 1));
    }

    #[test]
    fn activity_coalesces_only_adjacent_text_or_thinking_tails() {
        let mut activity = LiveActivity::default();
        activity.push(ActivityKind::Thinking, "checking ");
        activity.push(ActivityKind::Thinking, " files\nnow");
        assert_eq!(activity.lines.len(), 1);
        assert_eq!(activity.lines[0].text, "checking files now");
        activity.push(ActivityKind::Text, "done");
        activity.push(ActivityKind::Text, "!");
        assert_eq!(activity.lines.len(), 2);
        assert_eq!(activity.lines[1].text, "done!");
        activity.push(ActivityKind::Tool, "bash");
        activity.push(ActivityKind::Tool, "grep");
        assert_eq!(activity.lines.len(), 4);
    }

    #[test]
    fn activity_keeps_utf8_safe_tail_and_strict_line_and_byte_bounds() {
        let mut activity = LiveActivity::default();
        activity.push(ActivityKind::Text, &"é".repeat(10_000));
        assert!(activity.lines[0].text.is_char_boundary(0));
        assert!(activity.lines[0].text.len() <= MAX_LIVE_ACTIVITY_LINE_BYTES);
        for index in 0..400 {
            activity.push(
                ActivityKind::Tool,
                &format!("line-{index:03} {}", "x".repeat(500)),
            );
        }
        assert!(activity.lines.len() <= MAX_LIVE_ACTIVITY_LINES);
        assert!(activity.byte_len() <= MAX_LIVE_ACTIVITY_BYTES);
        assert!(activity.lines.back().unwrap().text.starts_with("line-399"));
        let exact: usize = activity.lines.iter().map(|line| line.text.len()).sum();
        assert_eq!(activity.byte_len(), exact);
    }
}
