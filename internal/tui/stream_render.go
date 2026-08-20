package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	streamFlushFast     = 33 * time.Millisecond
	streamFlushMedium   = 75 * time.Millisecond
	streamFlushSlow     = 150 * time.Millisecond
	streamFlushVerySlow = 300 * time.Millisecond
)

func (m *Model) scheduleTranscriptFlush() tea.Cmd {
	if m.transcriptFlushPending || !m.transcriptDirty {
		return nil
	}
	// Do not repeatedly rebuild a snapshot the user intentionally scrolled
	// away from. Mouse/PageDown/End catches it up when the old bottom is reached.
	if m.transcriptContent != "" && !m.transcript.AtBottom() {
		return nil
	}
	m.transcriptFlushPending = true
	m.transcriptFlushSeq++
	seq := m.transcriptFlushSeq
	delay := m.transcriptFlushDelay()
	return tea.Tick(delay, func(time.Time) tea.Msg { return transcriptFlushMsg(seq) })
}

func (m *Model) transcriptFlushDelay() time.Duration {
	liveBytes := m.assistantBuf.Len() + m.thinkingBuf.Len() + m.planBuf.Len()
	switch {
	case liveBytes > 1024*1024:
		return streamFlushVerySlow
	case liveBytes > 256*1024:
		return streamFlushSlow
	case liveBytes > 64*1024:
		return streamFlushMedium
	default:
		return streamFlushFast
	}
}

func (m *Model) flushTranscriptImmediately() {
	m.transcriptFlushSeq++
	m.transcriptFlushPending = false
	m.refreshTranscript()
}

func (m *Model) catchUpTranscriptAtBottom() {
	if !m.transcript.AtBottom() || !m.transcriptDirty {
		return
	}
	m.flushTranscriptImmediately()
}

func (m *Model) applyMouse(msg tea.MouseMsg) tea.Cmd {
	if handled, cmd := m.applyTranscriptSelectionMouse(msg); handled {
		return cmd
	}
	var cmd tea.Cmd
	m.transcript, cmd = m.transcript.Update(msg)
	m.catchUpTranscriptAtBottom()
	return cmd
}

func eventNeedsImmediateTranscript(kind protocol.AgentEventType) bool {
	switch kind {
	case protocol.EvCompactionStarted,
		protocol.EvCompactionDone,
		protocol.EvSessionUpdated,
		protocol.EvPlanCompleted,
		protocol.EvPlanUpdate,
		protocol.EvToolStart,
		protocol.EvToolEnd,
		protocol.EvPermissionRequest,
		protocol.EvUserInputRequest,
		protocol.EvQueueUpdated,
		protocol.EvError,
		protocol.EvTurnDone,
		protocol.EvAborted:
		return true
	default:
		return false
	}
}

func (m *Model) replayTerminalInputNow(messages ...tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	m.replayTerminalMessages(messages, &cmds)
	m.layout()
	return tea.Batch(cmds...)
}

func (m *Model) replayTerminalMessages(messages []tea.Msg, cmds *[]tea.Cmd) {
	if len(messages) == 0 {
		return
	}
	m.replayingInput = true
	defer func() { m.replayingInput = false }()
	for _, message := range messages {
		switch msg := message.(type) {
		case tea.MouseMsg:
			if cmd := m.applyMouse(msg); cmd != nil {
				*cmds = append(*cmds, cmd)
			}
		case tea.KeyMsg:
			_, cmd := m.handleKey(msg)
			if cmd != nil {
				*cmds = append(*cmds, cmd)
			}
		}
	}
}
