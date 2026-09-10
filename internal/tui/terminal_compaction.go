package tui

import "github.com/elmissouri16/snow-core/pkg/protocol"

// Manual compaction has its own terminal boundary, without turn_done. Retain
// its identity after acknowledgement so delayed events/results cannot announce
// the same operation twice or reopen its terminal status.
type terminalCompactionSettlement struct {
	id         string
	epoch      uint64
	generation uint64
	valid      bool
}

func (m *Model) settledCompactionEvent(ev protocol.AgentEvent) bool {
	s := m.terminal.compaction
	return ev.Agent == nil && s.valid && s.id != "" && ev.TurnID == s.id && ev.RootEpoch == s.epoch
}

func (m *Model) settleTerminalCompaction(id string, epoch uint64, failed, canceled bool) {
	s := terminalCompactionSettlement{id: id, epoch: epoch, generation: m.compactGeneration, valid: true}
	previous := m.terminal.compaction
	if previous.valid && ((id != "" && previous.id == id && previous.epoch == epoch) ||
		(id == "" && previous.generation == s.generation)) {
		return
	}
	m.terminal.compaction = s
	if canceled {
		m.resetTerminalRun()
		m.terminal.outcome = terminalAborted
		return
	}
	m.terminal.failed = failed
	if failed {
		m.terminal.outcome = terminalFailed
		m.queueTerminalAlert("Snow compaction failed")
	} else {
		m.terminal.outcome = terminalDone
		m.queueTerminalAlert("Snow compaction finished")
	}
}
