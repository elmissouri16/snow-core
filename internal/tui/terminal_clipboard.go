package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aymanbagabas/go-osc52/v2"
)

const terminalClipboardTimeout = 3 * time.Second
const maxClipboardTextBytes = 1 << 20

type clipboardOwner struct {
	target                          textareaTarget
	requestID, questionID, provider string
	generation                      uint64
	active                          bool
}

type terminalClipboardRequest struct {
	owner      clipboardOwner
	generation uint64
	canceled   bool
}

type localClipboardResultMsg struct {
	owner      clipboardOwner
	generation uint64
	text       string
	err        error
}

type terminalClipboardTimeoutMsg uint64

func (m *Model) clipboardOwner() clipboardOwner {
	if m.permPending {
		return clipboardOwner{}
	}
	if m.userInputPending {
		question := m.currentUserInputQuestion()
		if question == nil || !m.userInputEditing {
			return clipboardOwner{}
		}
		return clipboardOwner{target: textareaTargetUserInput, requestID: m.userInputRequest.ID, questionID: question.ID, active: true}
	}
	owner := clipboardOwner{generation: m.loginFieldGeneration, provider: m.loginProvider, active: true}
	switch {
	case m.loginMode:
		owner.target = textareaTargetLoginSecret
	case m.loginProfileMode:
		owner.target = textareaTargetLoginProfile
	case m.loginEndpointMode:
		owner.target = textareaTargetLoginEndpoint
	case m.composerCoveredByModal() || m.pluginScreenView() != nil:
		return clipboardOwner{}
	default:
		return clipboardOwner{target: textareaTargetComposer, generation: m.imagePasteGeneration, active: true}
	}
	return owner
}

// Composer paste generations change during normal reads, without a focus move.
func (m *Model) clipboardFocus() clipboardOwner {
	owner := m.clipboardOwner()
	if owner.target == textareaTargetComposer {
		owner.generation = 0
	}
	return owner
}

func remoteTerminal() bool { return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" }

func (m *Model) startClipboardTextRead() tea.Cmd {
	owner := m.clipboardOwner()
	if !owner.active {
		return nil
	}
	m.clipboardGeneration++
	generation := m.clipboardGeneration
	if remoteTerminal() {
		return m.requestTerminalClipboard(owner, generation)
	}
	read := m.readClipboardText
	if read == nil {
		read = readHostClipboardText
	}
	ctx := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, terminalClipboardTimeout)
		defer cancel()
		text, err := read(ctx)
		return localClipboardResultMsg{owner: owner, generation: generation, text: text, err: err}
	}
}

func (m *Model) requestTerminalClipboard(owner clipboardOwner, generation uint64) tea.Cmd {
	if m.terminalClipboard != nil {
		m.lastStatus = "clipboard request still pending · use terminal paste"
		return nil
	}
	m.terminalClipboard = &terminalClipboardRequest{owner: owner, generation: generation}
	m.lastStatus = "requesting terminal clipboard"
	read := tea.Cmd(tea.ReadClipboard)
	if wrappedTerminal() {
		read = tea.Raw(wrapTerminalClipboard(osc52.Query()))
	}
	return tea.Batch(read, tea.Tick(terminalClipboardTimeout, func(time.Time) tea.Msg { return terminalClipboardTimeoutMsg(generation) }))
}

func (m *Model) clipboardRequestCurrent(owner clipboardOwner, generation uint64) bool {
	return generation == m.clipboardGeneration && owner.active && owner == m.clipboardOwner()
}

func (m *Model) applyClipboardText(owner clipboardOwner, generation uint64, text string) tea.Cmd {
	if !m.clipboardRequestCurrent(owner, generation) {
		return nil
	}
	if len(text) > maxClipboardTextBytes {
		m.lastStatus = "clipboard text exceeds 1 MiB"
		return nil
	}
	return m.handlePaste(tea.PasteMsg{Content: text})
}

func (m *Model) applyLocalClipboard(msg localClipboardResultMsg) tea.Cmd {
	if !m.clipboardRequestCurrent(msg.owner, msg.generation) {
		return nil
	}
	if msg.err != nil {
		return m.requestTerminalClipboard(msg.owner, msg.generation)
	}
	return m.applyClipboardText(msg.owner, msg.generation, msg.text)
}

func (m *Model) applyTerminalClipboard(msg tea.ClipboardMsg) tea.Cmd {
	if msg.Selection != 'c' {
		return nil
	}
	request := m.terminalClipboard
	if request == nil {
		return nil
	}
	// OSC 52 has no request ID. Even a canceled request occupies this slot
	// until its reply is drained, so it can never be attributed to a later paste.
	m.terminalClipboard = nil
	if request.canceled {
		return nil
	}
	return m.applyClipboardText(request.owner, request.generation, msg.Content)
}

func (m *Model) expireTerminalClipboard(generation uint64) {
	if request := m.terminalClipboard; request != nil && request.generation == generation && !request.canceled {
		request.canceled = true
		m.lastStatus = "terminal clipboard timed out · use terminal paste"
	}
}

func (m *Model) cancelClipboardReads() {
	m.clipboardGeneration++
	m.imagePasteGeneration++
	if m.terminalClipboard != nil {
		m.terminalClipboard.canceled = true
	}
}

func wrappedTerminal() bool {
	return os.Getenv("TMUX") != "" || strings.HasPrefix(os.Getenv("TERM"), "screen")
}

func wrapTerminalClipboard(sequence osc52.Sequence) string {
	if os.Getenv("TMUX") != "" {
		return sequence.Tmux().String()
	}
	if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		return sequence.Screen().String()
	}
	return sequence.String()
}

func terminalClipboardWrite(text string) tea.Cmd {
	if wrappedTerminal() {
		return tea.Raw(transcriptSelectionClipboardSequence(text))
	}
	return tea.SetClipboard(text)
}

func readHostClipboardText(ctx context.Context) (string, error) {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbpaste"}}
	case "linux":
		candidates = [][]string{{"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"xsel", "--clipboard", "--output"}}
	default:
		return "", fmt.Errorf("host clipboard unsupported on %s", runtime.GOOS)
	}
	var failures []error
	for _, args := range candidates {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.WaitDelay = 250 * time.Millisecond
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := cmd.Start(); err != nil {
			failures = append(failures, err)
			continue
		}
		stop := context.AfterFunc(ctx, func() { _ = stdout.Close() })
		data, readErr := io.ReadAll(io.LimitReader(stdout, maxClipboardTextBytes+1))
		if len(data) > maxClipboardTextBytes || readErr != nil {
			_ = cmd.Process.Kill()
		}
		waitErr := cmd.Wait()
		stop()
		if len(data) > maxClipboardTextBytes {
			return "", errors.New("clipboard text exceeds 1 MiB")
		}
		if readErr == nil && waitErr == nil {
			return string(data), nil
		}
		failures = append(failures, errors.Join(readErr, waitErr))
		if ctx.Err() != nil {
			break
		}
	}
	return "", errors.Join(failures...)
}
