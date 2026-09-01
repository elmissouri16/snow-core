use crate::snow::{ManagedProcess, ManagedProcessLogs};

/// Maximum number of UTF-8 bytes retained for the selected process's output.
pub(crate) const PROCESS_LIVE_OUTPUT_LIMIT: usize = 128 * 1024;

const PANEL_OMISSION_MARKER: &str = "[... older panel output omitted ...]\n";

/// Identity attached to every asynchronous request made for this state.
///
/// A generation invalidates a whole set of requests (for example, after the
/// selected process changes). Request IDs distinguish requests within a
/// generation. Callers must return this value unchanged with the response.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct RequestMetadata {
    pub generation: u64,
    pub request_id: u64,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct ProcessListRequest {
    pub metadata: RequestMetadata,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ProcessLogRequest {
    pub metadata: RequestMetadata,
    pub process_id: String,
    /// Byte cursor to send to `process_logs`. The first request always uses 0.
    pub cursor: i64,
}

/// Whether an asynchronous response was accepted by the current state.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum ResponseDisposition {
    Applied,
    /// The response no longer owns the active request, so no state was changed.
    Stale,
    /// The response owned the request but violated its cursor/process contract.
    Invalid,
}

/// What the owner should do with the recurring live-panel timer.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum NextPollDecision {
    /// No request is outstanding and another poll should be scheduled.
    Schedule,
    /// A list or log response is outstanding; do not schedule a duplicate poll.
    WaitForResponse,
    /// The selected process is terminal and all of its output has been read.
    StopTerminalEof,
}

/// Pure state for a bounded, incrementally updated managed-process panel.
///
/// This type deliberately performs no I/O and owns no timer. At most one list
/// request and one log request can be active. Selection is stored as a process
/// ID rather than a row index, so reordered snapshots do not move selection.
pub(crate) struct ProcessLiveState {
    processes: Vec<ManagedProcess>,
    selected_process_id: Option<String>,
    output: String,
    cursor: i64,
    terminal_eof: bool,
    generation: u64,
    next_request_id: u64,
    list_in_flight: Option<RequestMetadata>,
    log_in_flight: Option<ProcessLogRequest>,
}

impl Default for ProcessLiveState {
    fn default() -> Self {
        Self {
            processes: Vec::new(),
            selected_process_id: None,
            output: String::new(),
            cursor: 0,
            terminal_eof: false,
            generation: 0,
            next_request_id: 1,
            list_in_flight: None,
            log_in_flight: None,
        }
    }
}

impl ProcessLiveState {
    pub(crate) fn new() -> Self {
        Self::default()
    }

    pub(crate) fn processes(&self) -> &[ManagedProcess] {
        &self.processes
    }

    pub(crate) fn selected_process_id(&self) -> Option<&str> {
        self.selected_process_id.as_deref()
    }

    pub(crate) fn selected_process(&self) -> Option<&ManagedProcess> {
        let selected = self.selected_process_id()?;
        self.processes
            .iter()
            .find(|process| process.process_id == selected)
    }

    pub(crate) fn output(&self) -> &str {
        &self.output
    }

    pub(crate) fn cursor(&self) -> i64 {
        self.cursor
    }

    pub(crate) fn terminal_eof(&self) -> bool {
        self.terminal_eof
    }

    pub(crate) fn generation(&self) -> u64 {
        self.generation
    }

    pub(crate) fn list_request_in_flight(&self) -> Option<RequestMetadata> {
        self.list_in_flight
    }

    pub(crate) fn log_request_in_flight(&self) -> Option<&ProcessLogRequest> {
        self.log_in_flight.as_ref()
    }

    /// Selects an existing process by ID. Names are intentionally not stable
    /// identities and are never used here.
    pub(crate) fn select_process(&mut self, process_id: &str) -> bool {
        if self.selected_process_id() == Some(process_id) {
            return true;
        }
        if !self
            .processes
            .iter()
            .any(|process| process.process_id == process_id)
        {
            return false;
        }
        self.selected_process_id = Some(process_id.to_owned());
        self.invalidate_for_selection_change();
        true
    }

    /// Reconciles a fresh inventory while preserving selection by process ID.
    /// Returns true when the selected process changed.
    pub(crate) fn reconcile_processes(&mut self, processes: Vec<ManagedProcess>) -> bool {
        let previous = self.selected_process_id.clone();
        let selected_still_exists = previous.as_ref().is_some_and(|selected| {
            processes
                .iter()
                .any(|process| process.process_id == *selected)
        });
        let next = if selected_still_exists {
            previous.clone()
        } else {
            processes.first().map(|process| process.process_id.clone())
        };

        self.processes = processes;
        self.selected_process_id = next;
        let selection_changed = self.selected_process_id != previous;
        if selection_changed {
            self.invalidate_for_selection_change();
        } else if self
            .selected_process()
            .is_some_and(|process| process.status == "running")
        {
            // A refreshed running state permits polling again if a previous
            // terminal snapshot was superseded for the same process ID.
            self.terminal_eof = false;
        }
        selection_changed
    }

    /// Starts an inventory request unless one is already outstanding.
    pub(crate) fn start_list_request(&mut self) -> Option<ProcessListRequest> {
        if self.list_in_flight.is_some() {
            return None;
        }
        let metadata = self.next_metadata();
        self.list_in_flight = Some(metadata);
        Some(ProcessListRequest { metadata })
    }

    /// Applies an inventory only if `metadata` owns the current list request.
    pub(crate) fn apply_list_response(
        &mut self,
        metadata: RequestMetadata,
        processes: Vec<ManagedProcess>,
    ) -> ResponseDisposition {
        if self.list_in_flight != Some(metadata) || metadata.generation != self.generation {
            return ResponseDisposition::Stale;
        }
        self.list_in_flight = None;
        self.reconcile_processes(processes);
        ResponseDisposition::Applied
    }

    /// Completes a failed list request without changing the last good snapshot.
    pub(crate) fn finish_list_request(&mut self, metadata: RequestMetadata) -> ResponseDisposition {
        if self.list_in_flight != Some(metadata) || metadata.generation != self.generation {
            return ResponseDisposition::Stale;
        }
        self.list_in_flight = None;
        ResponseDisposition::Applied
    }

    /// Starts the next output page request for the selected process.
    pub(crate) fn start_log_request(&mut self) -> Option<ProcessLogRequest> {
        if self.log_in_flight.is_some() || self.terminal_eof {
            return None;
        }
        let process_id = self.selected_process_id.clone()?;
        let request = ProcessLogRequest {
            metadata: self.next_metadata(),
            process_id,
            cursor: self.cursor,
        };
        self.log_in_flight = Some(request.clone());
        Some(request)
    }

    /// Appends one cursor page if it belongs to the active selection/request.
    pub(crate) fn apply_log_response(
        &mut self,
        metadata: RequestMetadata,
        logs: ManagedProcessLogs,
    ) -> ResponseDisposition {
        let Some(request) = self.log_in_flight.as_ref() else {
            return ResponseDisposition::Stale;
        };
        if request.metadata != metadata || metadata.generation != self.generation {
            return ResponseDisposition::Stale;
        }

        let expected_process_id = request.process_id.clone();
        let expected_cursor = request.cursor;
        self.log_in_flight = None;
        if self.selected_process_id() != Some(expected_process_id.as_str())
            || logs.process_id != expected_process_id
            || logs.next_cursor < expected_cursor
        {
            return ResponseDisposition::Invalid;
        }

        if logs.omitted_bytes > 0 {
            self.append_output(&format!(
                "[... {} older output bytes omitted ...]\n",
                logs.omitted_bytes
            ));
        }
        self.append_output(&logs.output);
        self.cursor = logs.next_cursor;

        if let Some(process) = self
            .processes
            .iter_mut()
            .find(|process| process.process_id == logs.process_id)
            && !logs.status.is_empty()
        {
            process.status.clone_from(&logs.status);
        }
        let status = if logs.status.is_empty() {
            self.selected_process()
                .map(|process| process.status.as_str())
                .unwrap_or("")
        } else {
            logs.status.as_str()
        };
        self.terminal_eof = logs.eof && is_terminal_status(status);
        ResponseDisposition::Applied
    }

    /// Completes a failed log request without advancing output or its cursor.
    pub(crate) fn finish_log_request(&mut self, metadata: RequestMetadata) -> ResponseDisposition {
        if self.log_in_flight.as_ref().map(|request| request.metadata) != Some(metadata)
            || metadata.generation != self.generation
        {
            return ResponseDisposition::Stale;
        }
        self.log_in_flight = None;
        ResponseDisposition::Applied
    }

    pub(crate) fn next_poll_decision(&self) -> NextPollDecision {
        if self.terminal_eof {
            NextPollDecision::StopTerminalEof
        } else if self.list_in_flight.is_some() || self.log_in_flight.is_some() {
            NextPollDecision::WaitForResponse
        } else {
            NextPollDecision::Schedule
        }
    }

    /// Invalidates every outstanding response and clears all panel data.
    pub(crate) fn clear(&mut self) {
        self.bump_generation();
        self.processes.clear();
        self.selected_process_id = None;
        self.reset_output();
        self.list_in_flight = None;
    }

    fn next_metadata(&mut self) -> RequestMetadata {
        let metadata = RequestMetadata {
            generation: self.generation,
            request_id: self.next_request_id,
        };
        self.next_request_id = self.next_request_id.wrapping_add(1);
        if self.next_request_id == 0 {
            self.next_request_id = 1;
        }
        metadata
    }

    fn invalidate_for_selection_change(&mut self) {
        self.bump_generation();
        self.reset_output();
        // A selection change also makes an inventory response stale: applying
        // it could otherwise replace a newer user choice with an old snapshot.
        self.list_in_flight = None;
    }

    fn bump_generation(&mut self) {
        self.generation = self.generation.wrapping_add(1);
        self.log_in_flight = None;
    }

    fn reset_output(&mut self) {
        self.output.clear();
        self.output.shrink_to(PROCESS_LIVE_OUTPUT_LIMIT);
        self.cursor = 0;
        self.terminal_eof = false;
    }

    fn append_output(&mut self, addition: &str) {
        if addition.is_empty() {
            return;
        }
        if self.output.len().saturating_add(addition.len()) <= PROCESS_LIVE_OUTPUT_LIMIT {
            self.output.push_str(addition);
            return;
        }

        let tail_limit = PROCESS_LIVE_OUTPUT_LIMIT - PANEL_OMISSION_MARKER.len();
        let mut bounded = String::with_capacity(PROCESS_LIVE_OUTPUT_LIMIT);
        bounded.push_str(PANEL_OMISSION_MARKER);

        if addition.len() >= tail_limit {
            bounded.push_str(utf8_tail(addition, tail_limit));
        } else {
            let previous_limit = tail_limit - addition.len();
            bounded.push_str(utf8_tail(&self.output, previous_limit));
            bounded.push_str(addition);
        }
        self.output = bounded;
    }
}

fn is_terminal_status(status: &str) -> bool {
    !status.is_empty() && status != "running"
}

fn utf8_tail(value: &str, max_bytes: usize) -> &str {
    if value.len() <= max_bytes {
        return value;
    }
    let mut start = value.len() - max_bytes;
    while !value.is_char_boundary(start) {
        start += 1;
    }
    &value[start..]
}

#[cfg(test)]
mod tests {
    use super::*;

    fn process(id: &str, name: &str, status: &str) -> ManagedProcess {
        ManagedProcess {
            process_id: id.into(),
            name: name.into(),
            status: status.into(),
            ..ManagedProcess::default()
        }
    }

    fn logs(process_id: &str, status: &str, output: &str, next_cursor: i64) -> ManagedProcessLogs {
        ManagedProcessLogs {
            process_id: process_id.into(),
            status: status.into(),
            output: output.into(),
            next_cursor,
            ..ManagedProcessLogs::default()
        }
    }

    fn state_with_processes() -> ProcessLiveState {
        let mut state = ProcessLiveState::new();
        state.reconcile_processes(vec![
            process("one", "server", "running"),
            process("two", "watcher", "running"),
        ]);
        state
    }

    #[test]
    fn defaults_to_empty_with_initial_cursor_zero() {
        let state = ProcessLiveState::new();
        assert!(state.processes().is_empty());
        assert_eq!(state.selected_process_id(), None);
        assert_eq!(state.selected_process(), None);
        assert_eq!(state.output(), "");
        assert_eq!(state.cursor(), 0);
        assert!(!state.terminal_eof());
        assert_eq!(state.generation(), 0);
        assert_eq!(state.next_poll_decision(), NextPollDecision::Schedule);
    }

    #[test]
    fn reconciliation_selects_first_then_preserves_process_id_across_reordering() {
        let mut state = state_with_processes();
        assert_eq!(state.selected_process_id(), Some("one"));
        assert!(state.select_process("two"));
        let generation = state.generation();

        let changed = state.reconcile_processes(vec![
            process("two", "renamed", "running"),
            process("one", "server", "running"),
        ]);

        assert!(!changed);
        assert_eq!(state.generation(), generation);
        assert_eq!(state.selected_process_id(), Some("two"));
        assert_eq!(state.selected_process().unwrap().name, "renamed");
    }

    #[test]
    fn reconciliation_falls_back_when_selection_disappears_and_clears_when_empty() {
        let mut state = state_with_processes();
        let request = state.start_log_request().unwrap();
        assert_eq!(
            state.apply_log_response(request.metadata, logs("one", "running", "old", 3)),
            ResponseDisposition::Applied
        );

        assert!(state.reconcile_processes(vec![process("two", "watcher", "running")]));
        assert_eq!(state.selected_process_id(), Some("two"));
        assert_eq!(state.output(), "");
        assert_eq!(state.cursor(), 0);

        assert!(state.reconcile_processes(Vec::new()));
        assert_eq!(state.selected_process_id(), None);
        assert_eq!(state.cursor(), 0);
        assert!(state.start_log_request().is_none());
    }

    #[test]
    fn selection_accepts_only_ids_and_invalidates_old_requests() {
        let mut state = state_with_processes();
        let list = state.start_list_request().unwrap();
        let log = state.start_log_request().unwrap();
        let generation = state.generation();

        assert!(!state.select_process("watcher"));
        assert_eq!(state.generation(), generation);
        assert_eq!(state.selected_process_id(), Some("one"));

        assert!(state.select_process("two"));
        assert!(state.generation() > generation);
        assert_eq!(state.selected_process_id(), Some("two"));
        assert_eq!(state.list_request_in_flight(), None);
        assert_eq!(state.log_request_in_flight(), None);
        assert_eq!(
            state.apply_list_response(list.metadata, vec![process("stale", "", "running")]),
            ResponseDisposition::Stale
        );
        assert_eq!(
            state.apply_log_response(log.metadata, logs("one", "running", "stale", 5)),
            ResponseDisposition::Stale
        );
        assert_eq!(state.output(), "");
    }

    #[test]
    fn selecting_current_id_is_a_noop() {
        let mut state = state_with_processes();
        let generation = state.generation();
        let request = state.start_log_request().unwrap();
        assert!(state.select_process("one"));
        assert_eq!(state.generation(), generation);
        assert_eq!(state.log_request_in_flight(), Some(&request));
    }

    #[test]
    fn permits_only_one_request_of_each_kind_in_flight() {
        let mut state = state_with_processes();
        let list = state.start_list_request().unwrap();
        let log = state.start_log_request().unwrap();

        assert_eq!(list.metadata.generation, state.generation());
        assert_eq!(log.metadata.generation, state.generation());
        assert_ne!(list.metadata.request_id, log.metadata.request_id);
        assert_eq!(log.cursor, 0);
        assert!(state.start_list_request().is_none());
        assert!(state.start_log_request().is_none());
        assert_eq!(
            state.next_poll_decision(),
            NextPollDecision::WaitForResponse
        );
    }

    #[test]
    fn list_response_requires_exact_metadata_and_does_not_displace_current_request() {
        let mut state = state_with_processes();
        let request = state.start_list_request().unwrap();
        let stale = RequestMetadata {
            request_id: request.metadata.request_id + 1,
            ..request.metadata
        };

        assert_eq!(
            state.apply_list_response(stale, vec![process("stale", "", "running")]),
            ResponseDisposition::Stale
        );
        assert_eq!(state.list_request_in_flight(), Some(request.metadata));
        assert_eq!(state.selected_process_id(), Some("one"));
        assert_eq!(
            state.apply_list_response(request.metadata, vec![process("one", "updated", "running")]),
            ResponseDisposition::Applied
        );
        assert_eq!(state.selected_process().unwrap().name, "updated");
        assert_eq!(state.list_request_in_flight(), None);
        assert_eq!(
            state.apply_list_response(request.metadata, Vec::new()),
            ResponseDisposition::Stale
        );
    }

    #[test]
    fn failed_requests_clear_only_the_exact_active_request() {
        let mut state = state_with_processes();
        let list = state.start_list_request().unwrap();
        let log = state.start_log_request().unwrap();
        let wrong = RequestMetadata {
            request_id: 999,
            ..list.metadata
        };

        assert_eq!(state.finish_list_request(wrong), ResponseDisposition::Stale);
        assert!(state.list_request_in_flight().is_some());
        assert_eq!(
            state.finish_list_request(list.metadata),
            ResponseDisposition::Applied
        );
        assert_eq!(state.finish_log_request(wrong), ResponseDisposition::Stale);
        assert!(state.log_request_in_flight().is_some());
        assert_eq!(
            state.finish_log_request(log.metadata),
            ResponseDisposition::Applied
        );
        assert_eq!(state.next_poll_decision(), NextPollDecision::Schedule);
    }

    #[test]
    fn appends_cursor_pages_and_updates_selected_process_status() {
        let mut state = state_with_processes();
        let first = state.start_log_request().unwrap();
        assert_eq!(first.cursor, 0);
        assert_eq!(
            state.apply_log_response(first.metadata, logs("one", "running", "first\n", 6)),
            ResponseDisposition::Applied
        );
        let second = state.start_log_request().unwrap();
        assert_eq!(second.cursor, 6);
        assert_eq!(
            state.apply_log_response(second.metadata, logs("one", "exited", "second\n", 13)),
            ResponseDisposition::Applied
        );

        assert_eq!(state.output(), "first\nsecond\n");
        assert_eq!(state.cursor(), 13);
        assert_eq!(state.selected_process().unwrap().status, "exited");
        assert!(!state.terminal_eof());
    }

    #[test]
    fn inserts_reported_omission_marker_before_page_output() {
        let mut state = state_with_processes();
        let request = state.start_log_request().unwrap();
        let mut page = logs("one", "running", "retained\n", 2057);
        page.omitted_bytes = 2048;

        assert_eq!(
            state.apply_log_response(request.metadata, page),
            ResponseDisposition::Applied
        );
        assert_eq!(
            state.output(),
            "[... 2048 older output bytes omitted ...]\nretained\n"
        );
    }

    #[test]
    fn output_cap_keeps_utf8_safe_tail_and_a_single_panel_marker() {
        let mut state = state_with_processes();
        let huge = format!("prefix-{}TAIL", "é".repeat(PROCESS_LIVE_OUTPUT_LIMIT));
        let request = state.start_log_request().unwrap();
        assert_eq!(
            state.apply_log_response(
                request.metadata,
                logs("one", "running", &huge, huge.len() as i64)
            ),
            ResponseDisposition::Applied
        );

        assert!(state.output().is_char_boundary(state.output().len()));
        assert!(state.output().len() <= PROCESS_LIVE_OUTPUT_LIMIT);
        assert!(state.output().starts_with(PANEL_OMISSION_MARKER));
        assert!(state.output().ends_with("TAIL"));
        assert_eq!(state.output().matches(PANEL_OMISSION_MARKER).count(), 1);
        assert!(state.output.capacity() <= PROCESS_LIVE_OUTPUT_LIMIT);

        let request = state.start_log_request().unwrap();
        let cursor = request.cursor + 4;
        state.apply_log_response(request.metadata, logs("one", "running", "more", cursor));
        assert!(state.output().len() <= PROCESS_LIVE_OUTPUT_LIMIT);
        assert!(state.output().is_char_boundary(state.output().len()));
        assert_eq!(state.output().matches(PANEL_OMISSION_MARKER).count(), 1);
        assert!(state.output().ends_with("more"));
    }

    #[test]
    fn exact_output_limit_does_not_add_a_marker() {
        let mut state = state_with_processes();
        let output = "x".repeat(PROCESS_LIVE_OUTPUT_LIMIT);
        let request = state.start_log_request().unwrap();
        state.apply_log_response(
            request.metadata,
            logs("one", "running", &output, output.len() as i64),
        );
        assert_eq!(state.output().len(), PROCESS_LIVE_OUTPUT_LIMIT);
        assert!(!state.output().starts_with(PANEL_OMISSION_MARKER));
    }

    #[test]
    fn stale_log_response_cannot_mutate_output_cursor_or_active_request() {
        let mut state = state_with_processes();
        let request = state.start_log_request().unwrap();
        let wrong = RequestMetadata {
            request_id: request.metadata.request_id + 1,
            ..request.metadata
        };

        assert_eq!(
            state.apply_log_response(wrong, logs("one", "running", "stale", 5)),
            ResponseDisposition::Stale
        );
        assert_eq!(state.log_request_in_flight(), Some(&request));
        assert_eq!(state.output(), "");
        assert_eq!(state.cursor(), 0);

        assert_eq!(
            state.apply_log_response(request.metadata, logs("one", "running", "fresh", 5)),
            ResponseDisposition::Applied
        );
        assert_eq!(state.output(), "fresh");
        assert_eq!(
            state.apply_log_response(request.metadata, logs("one", "running", "duplicate", 9)),
            ResponseDisposition::Stale
        );
        assert_eq!(state.output(), "fresh");
    }

    #[test]
    fn invalid_process_or_regressing_cursor_is_rejected_without_mutation() {
        let mut state = state_with_processes();
        let request = state.start_log_request().unwrap();
        assert_eq!(
            state.apply_log_response(request.metadata, logs("two", "running", "wrong", 5)),
            ResponseDisposition::Invalid
        );
        assert_eq!(state.output(), "");
        assert_eq!(state.cursor(), 0);
        assert!(state.log_request_in_flight().is_none());

        let request = state.start_log_request().unwrap();
        state.apply_log_response(request.metadata, logs("one", "running", "valid", 5));
        let request = state.start_log_request().unwrap();
        assert_eq!(
            state.apply_log_response(request.metadata, logs("one", "running", "replay", 4)),
            ResponseDisposition::Invalid
        );
        assert_eq!(state.output(), "valid");
        assert_eq!(state.cursor(), 5);
    }

    #[test]
    fn only_terminal_eof_stops_polling() {
        let mut state = state_with_processes();
        let running = state.start_log_request().unwrap();
        let mut running_eof = logs("one", "running", "", 0);
        running_eof.eof = true;
        state.apply_log_response(running.metadata, running_eof);
        assert!(!state.terminal_eof());
        assert_eq!(state.next_poll_decision(), NextPollDecision::Schedule);
        assert!(state.start_log_request().is_some());

        let outstanding = state.log_request_in_flight().unwrap().clone();
        let mut terminal_not_eof = logs("one", "exited", "last", 4);
        terminal_not_eof.eof = false;
        state.apply_log_response(outstanding.metadata, terminal_not_eof);
        assert!(!state.terminal_eof());
        assert_eq!(state.next_poll_decision(), NextPollDecision::Schedule);

        let last = state.start_log_request().unwrap();
        let mut terminal_eof = logs("one", "exited", "", 4);
        terminal_eof.eof = true;
        state.apply_log_response(last.metadata, terminal_eof);
        assert!(state.terminal_eof());
        assert_eq!(
            state.next_poll_decision(),
            NextPollDecision::StopTerminalEof
        );
        assert!(state.start_log_request().is_none());
    }

    #[test]
    fn empty_log_status_uses_inventory_status_for_terminal_eof() {
        let mut state = state_with_processes();
        state.processes[0].status = "failed".into();
        let request = state.start_log_request().unwrap();
        let mut page = logs("one", "", "", 0);
        page.eof = true;
        state.apply_log_response(request.metadata, page);
        assert!(state.terminal_eof());
    }

    #[test]
    fn running_reconciliation_resumes_same_id_after_terminal_eof() {
        let mut state = state_with_processes();
        let request = state.start_log_request().unwrap();
        let mut page = logs("one", "exited", "", 0);
        page.eof = true;
        state.apply_log_response(request.metadata, page);
        assert!(state.terminal_eof());

        assert!(!state.reconcile_processes(vec![process("one", "server", "running")]));
        assert!(!state.terminal_eof());
        assert!(state.start_log_request().is_some());
    }

    #[test]
    fn clear_invalidates_responses_and_restores_initial_output_state() {
        let mut state = state_with_processes();
        let list = state.start_list_request().unwrap();
        let log = state.start_log_request().unwrap();
        let generation = state.generation();
        state.clear();

        assert!(state.generation() > generation);
        assert!(state.processes().is_empty());
        assert_eq!(state.selected_process_id(), None);
        assert_eq!(state.output(), "");
        assert_eq!(state.cursor(), 0);
        assert_eq!(state.next_poll_decision(), NextPollDecision::Schedule);
        assert_eq!(
            state.apply_list_response(list.metadata, vec![process("stale", "", "running")]),
            ResponseDisposition::Stale
        );
        assert_eq!(
            state.apply_log_response(log.metadata, logs("one", "running", "stale", 5)),
            ResponseDisposition::Stale
        );
    }
}
