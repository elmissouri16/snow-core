package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const terminalFragmentTimeout = 80 * time.Millisecond

type terminalInputState struct {
	raw  string
	keys []tea.KeyMsg
}

type decodedTerminalInput struct {
	messages   []tea.Msg
	remainder  string
	incomplete bool
	recognized bool
}

type terminalPrefixStatus uint8

const (
	terminalPrefixInvalid terminalPrefixStatus = iota
	terminalPrefixIncomplete
	terminalPrefixComplete
)

// normalizeTerminalKey repairs escape sequences that Bubble Tea v1 can expose
// as ordinary KeyRunes when a terminal splits one SGR report across reads. It
// returns true when the original key must not reach the textarea.
func (m *Model) normalizeTerminalKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.terminalInput.raw == "" {
		raw, candidate := terminalCandidate(msg)
		if !candidate {
			return false, nil
		}
		m.terminalInput = terminalInputState{raw: raw, keys: []tea.KeyMsg{msg}}
	} else {
		// Escape followed by Return is the split macOS Option+Return form.
		if m.terminalInput.raw == "\x1b" && msg.Type == tea.KeyEnter {
			m.clearTerminalInput()
			msg.Alt = true
			return true, m.replayTerminalInputNow(tea.Msg(msg))
		}
		if m.terminalInput.raw == "\x1b" && msg.Type == tea.KeyShiftTab {
			m.clearTerminalInput()
			return true, m.replayTerminalInputNow(tea.Msg(msg))
		}
		part, ok := terminalKeyPayload(msg)
		if !ok {
			messages := m.pendingTerminalKeys(msg)
			m.clearTerminalInput()
			return true, m.replayTerminalInputNow(messages...)
		}
		m.terminalInput.raw += part
		m.terminalInput.keys = append(m.terminalInput.keys, msg)
	}

	decoded := decodeTerminalInput(m.terminalInput.raw)
	if !decoded.recognized {
		messages := m.pendingTerminalKeys()
		m.clearTerminalInput()
		return true, m.replayTerminalInputNow(messages...)
	}
	if decoded.incomplete {
		if len(decoded.messages) > 0 {
			m.terminalInput = terminalInputState{
				raw:  decoded.remainder,
				keys: []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune(decoded.remainder)}},
			}
		}
		prefixCmd := m.replayTerminalInputNow(decoded.messages...)
		return true, tea.Batch(prefixCmd, m.scheduleTerminalInputTimeout())
	}

	m.clearTerminalInput()
	messages := decoded.messages
	if decoded.remainder != "" {
		messages = append(messages, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(decoded.remainder)})
	}
	return true, m.replayTerminalInputNow(messages...)
}

func terminalCandidate(msg tea.KeyMsg) (string, bool) {
	if msg.Paste {
		return "", false
	}
	if msg.Type == tea.KeyEsc {
		return "\x1b", true
	}
	if msg.Type != tea.KeyRunes {
		return "", false
	}
	text := string(msg.Runes)
	if msg.Alt && text == "[" {
		return "\x1b[", true
	}
	if strings.HasPrefix(text, "[<") || strings.HasPrefix(text, "\x1b[<") {
		return text, true
	}
	return "", false
}

func terminalKeyPayload(msg tea.KeyMsg) (string, bool) {
	if msg.Paste || msg.Type != tea.KeyRunes {
		return "", false
	}
	text := string(msg.Runes)
	if msg.Alt {
		return "\x1b" + text, true
	}
	return text, true
}

func (m *Model) scheduleTerminalInputTimeout() tea.Cmd {
	m.metaEnterSeq++
	seq := m.metaEnterSeq
	m.metaEnterPending = strings.HasPrefix(m.terminalInput.raw, "\x1b")
	return tea.Tick(terminalFragmentTimeout, func(time.Time) tea.Msg {
		return clearMetaEnterMsg(seq)
	})
}

func (m *Model) pendingTerminalKeys(extra ...tea.KeyMsg) []tea.Msg {
	messages := make([]tea.Msg, 0, len(m.terminalInput.keys)+len(extra))
	for _, key := range m.terminalInput.keys {
		messages = append(messages, key)
	}
	for _, key := range extra {
		messages = append(messages, key)
	}
	return messages
}

func (m *Model) expireTerminalInput(seq uint64) []tea.Msg {
	if seq != m.metaEnterSeq {
		return nil
	}
	messages := m.pendingTerminalKeys()
	m.clearTerminalInput()
	return messages
}

func (m *Model) clearTerminalInput() {
	m.terminalInput = terminalInputState{}
	m.metaEnterPending = false
}

func decodeTerminalInput(input string) decodedTerminalInput {
	result := decodedTerminalInput{}
	remaining := input
	for remaining != "" {
		status, msg, consumed := decodeTerminalPrefix(remaining)
		switch status {
		case terminalPrefixComplete:
			result.recognized = true
			result.messages = append(result.messages, msg)
			remaining = remaining[consumed:]
		case terminalPrefixIncomplete:
			result.recognized = true
			result.incomplete = true
			result.remainder = remaining
			return result
		default:
			result.remainder = remaining
			return result
		}
	}
	return result
}

func decodeTerminalPrefix(input string) (terminalPrefixStatus, tea.Msg, int) {
	if input == "" {
		return terminalPrefixIncomplete, nil, 0
	}
	i := 0
	escape := input[0] == '\x1b'
	if escape {
		i++
		if i == len(input) {
			return terminalPrefixIncomplete, nil, 0
		}
	}
	if input[i] != '[' {
		return terminalPrefixInvalid, nil, 0
	}
	i++
	if i == len(input) {
		return terminalPrefixIncomplete, nil, 0
	}
	if escape && input[i] == 'Z' {
		return terminalPrefixComplete, tea.KeyMsg{Type: tea.KeyShiftTab}, i + 1
	}
	if input[i] != '<' {
		return terminalPrefixInvalid, nil, 0
	}
	i++

	values := [3]int{}
	for part := 0; part < len(values); part++ {
		start := i
		for i < len(input) && input[i] >= '0' && input[i] <= '9' {
			i++
		}
		if start == i {
			if i == len(input) {
				return terminalPrefixIncomplete, nil, 0
			}
			return terminalPrefixInvalid, nil, 0
		}
		value, err := strconv.Atoi(input[start:i])
		if err != nil {
			return terminalPrefixInvalid, nil, 0
		}
		values[part] = value
		if part < len(values)-1 {
			if i == len(input) {
				return terminalPrefixIncomplete, nil, 0
			}
			if input[i] != ';' {
				return terminalPrefixInvalid, nil, 0
			}
			i++
		}
	}
	if i == len(input) {
		return terminalPrefixIncomplete, nil, 0
	}
	if input[i] != 'M' && input[i] != 'm' {
		return terminalPrefixInvalid, nil, 0
	}
	return terminalPrefixComplete, sgrMouseMsg(values[0], values[1], values[2], input[i] == 'm'), i + 1
}

func sgrMouseMsg(code, x, y int, release bool) tea.MouseMsg {
	event := tea.MouseEvent{
		X:      max(0, x-1),
		Y:      max(0, y-1),
		Shift:  code&4 != 0,
		Alt:    code&8 != 0,
		Ctrl:   code&16 != 0,
		Action: tea.MouseActionPress,
	}
	button := code & 3
	switch {
	case code&64 != 0:
		event.Button = tea.MouseButtonWheelUp + tea.MouseButton(button)
	case button == 3:
		event.Button = tea.MouseButtonNone
		event.Action = tea.MouseActionRelease
	default:
		event.Button = tea.MouseButtonLeft + tea.MouseButton(button)
		if code&32 != 0 {
			event.Action = tea.MouseActionMotion
		}
	}
	if release && code&64 == 0 && event.Action != tea.MouseActionMotion {
		event.Action = tea.MouseActionRelease
	}
	return tea.MouseMsg(event)
}
