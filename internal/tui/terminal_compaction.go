package tui

import "github.com/elmissouri16/snow-core/pkg/protocol"

// Manual compaction has its own terminal boundary, without turn_done. Retain
// both the latest outcome and a monotonic fence: another command result must
// not make an older, already acknowledged operation's events live again.
type terminalCompactionSettlement struct {
	id                   string
	epoch                uint64
	generation           uint64
	valid                bool
	failed               bool
	canceled             bool
	pendingRunGeneration uint64
	deferredAlert        bool
	settledEpoch         uint64
	settledSequence      uint64
}

func (m *Model) beginTerminalCompaction() {
	m.terminal.compaction.pendingRunGeneration = m.runGeneration
	m.terminal.compaction.deferredAlert = false
}

func (m *Model) fenceTerminalCompaction(id string, epoch, sequence uint64) {
	if id == "" || epoch == 0 || sequence == 0 {
		return
	}
	s := &m.terminal.compaction
	if epoch > s.settledEpoch || (epoch == s.settledEpoch && sequence > s.settledSequence) {
		s.settledEpoch, s.settledSequence = epoch, sequence
	}
}

func (m *Model) settledCompactionEvent(ev protocol.AgentEvent) bool {
	if ev.Agent != nil || ev.TurnID == "" {
		return false
	}
	s := m.terminal.compaction
	if s.valid && s.id == ev.TurnID && s.epoch == ev.RootEpoch {
		return true
	}
	return ev.RootEpoch != 0 && ev.TurnSequence != 0 && s.settledSequence != 0 &&
		(ev.RootEpoch < s.settledEpoch || (ev.RootEpoch == s.settledEpoch && ev.TurnSequence <= s.settledSequence))
}

func (m *Model) staleCompactionResult(msg compactDoneMsg) bool {
	// Rejections have no admitted identity or lifecycle event. Their command
	// generations remain authoritative even after a session/branch event fence.
	if msg.turnID == "" {
		return false
	}
	ev := protocol.AgentEvent{TurnID: msg.turnID, RootEpoch: msg.epoch, TurnSequence: msg.sequence}
	if m.staleRootEvent(ev) {
		return true
	}
	s := m.terminal.compaction
	// Unlike duplicate stream events, a matching result is allowed to upgrade
	// provisional success. Only a strictly newer settled operation fences it.
	return msg.turnID != "" && msg.epoch != 0 && msg.sequence != 0 && s.settledSequence != 0 &&
		(msg.epoch < s.settledEpoch || (msg.epoch == s.settledEpoch && msg.sequence < s.settledSequence))
}

func (m *Model) settleTerminalCompaction(id string, epoch uint64, failed, canceled bool) {
	previous := m.terminal.compaction
	same := previous.valid && ((id != "" && previous.id == id && previous.epoch == epoch) ||
		(id == "" && previous.generation == m.compactGeneration))
	if same {
		// A final command error may follow a successful stream boundary (for
		// example, mailbox persistence in deferred core cleanup). Upgrade that
		// outcome, but never turn cancellation into failure or replay success.
		if previous.canceled || (!canceled && (!failed || previous.failed)) {
			m.flushTerminalCompactionAlert()
			return
		}
	}
	pendingRun := previous.pendingRunGeneration
	if previous.valid && previous.generation == m.compactGeneration && !same {
		// A successor admitted outside the composer has no local result to
		// await. Do not transfer the previous operation's notification deferral.
		pendingRun = 0
	}
	m.terminal.compaction = terminalCompactionSettlement{
		id: id, epoch: epoch, generation: m.compactGeneration, valid: true,
		failed: failed, canceled: canceled, pendingRunGeneration: pendingRun,
		settledEpoch: previous.settledEpoch, settledSequence: previous.settledSequence,
	}
	if canceled {
		m.terminal.compaction.pendingRunGeneration = 0
		m.resetTerminalRun()
		m.terminal.outcome = terminalAborted
		return
	}
	m.terminal.failed = failed
	m.terminal.outcome = terminalDone
	if failed {
		m.terminal.outcome = terminalFailed
	}
	m.terminal.compaction.deferredAlert = true
	m.flushTerminalCompactionAlert()
}

// Locally requested compaction waits for its command result before notifying:
// the stream boundary precedes final mailbox cleanup. An external operation
// without a pending command still settles from its lifecycle event alone.
func (m *Model) flushTerminalCompactionAlert() {
	s := &m.terminal.compaction
	pending := s.pendingRunGeneration != 0 && s.pendingRunGeneration == m.runGeneration
	if pending || !s.deferredAlert {
		return
	}
	s.deferredAlert = false
	// A focus/key acknowledgement after the stream settled must survive a
	// delayed, identical command result, including notifications=always.
	if s.canceled || m.terminal.outcome == terminalIdle {
		return
	}
	if s.failed {
		m.queueTerminalAlert("Snow compaction failed")
	} else {
		m.queueTerminalAlert("Snow compaction finished")
	}
}
