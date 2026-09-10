package tui

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Exercise Bubble Tea's real RawMsg/renderer/cleanup path without credentials
// or a desktop. The fixture only replaces asynchronous application bootstrap.
type terminalProgramFixture struct{ *Model }

func (f terminalProgramFixture) Init() tea.Cmd { return f.syncTerminalStatus() }

func (f terminalProgramFixture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := f.Model.Update(msg)
	return f, cmd
}

type terminalOutputCapture struct {
	mu sync.Mutex
	bytes.Buffer
	changed chan struct{}
}

func (o *terminalOutputCapture) Write(p []byte) (int, error) {
	o.mu.Lock()
	n, err := o.Buffer.Write(p)
	o.mu.Unlock()
	select {
	case o.changed <- struct{}{}:
	default:
	}
	return n, err
}

func (o *terminalOutputCapture) snapshot() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.Buffer.String()
}

func TestTerminalRendererHeartbeatAndShutdown(t *testing.T) {
	for _, cancelProgram := range []bool{false, true} {
		t.Run(map[bool]string{false: "quit", true: "cancel"}[cancelProgram], func(t *testing.T) {
			m := modelPickerTestModel(t, 80, 24)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			m.ctx = ctx
			m.busy, m.permPending, m.terminal.unfocused = true, true, true
			m.permRequest = &protocol.PermissionRequest{ID: "pending", Tool: "bash"}
			output := &terminalOutputCapture{changed: make(chan struct{}, 1)}
			program := tea.NewProgram(terminalProgramFixture{m},
				tea.WithInput(nil), tea.WithOutput(output), tea.WithWindowSize(80, 24),
				tea.WithContext(ctx), tea.WithEnvironment([]string{"TERM=xterm-ghostty"}),
				tea.WithoutSignalHandler(),
				tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg { return terminalStatusFilter(m, msg) }),
			)
			done := make(chan error, 1)
			go func() { _, err := program.Run(); done <- err }()
			t.Cleanup(func() {
				cancel()
				program.Wait()
			})
			deadline := time.NewTimer(6 * time.Second)
			defer deadline.Stop()
			for strings.Count(output.snapshot(), ansi.SetWarningProgressBar(0)) < 3 {
				select {
				case <-output.changed:
				case err := <-done:
					t.Fatalf("program exited before heartbeats: %v", err)
				case <-deadline.C:
					t.Fatalf("progress was not refreshed through the renderer: %q", output.snapshot())
				}
			}
			pulses := strings.Split(output.snapshot(), ansi.SetWarningProgressBar(0))
			if between := pulses[len(pulses)-2]; between != "" {
				t.Fatalf("heartbeat redrew terminal content between pulses: %q", between)
			}
			if cancelProgram {
				cancel()
			} else {
				program.Send(tea.QuitMsg{})
			}
			select {
			case err := <-done:
				if err != nil && !cancelProgram {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("terminal shutdown blocked")
			}
			text := output.snapshot()
			if !strings.Contains(text, "Snow · ") || !strings.Contains(text, "\a"+ansi.Notify("Snow: Waiting for approval")) {
				t.Fatal("real program did not emit title and attention/desktop notification")
			}
			if strings.LastIndex(text, ansi.ResetProgressBar) < strings.LastIndex(text, ansi.SetWarningProgressBar(0)) {
				t.Fatal("progress remained active after shutdown")
			}
			if !strings.Contains(text, ansi.SetWindowTitle("")) {
				t.Fatal("title was not cleared by Bubble Tea shutdown")
			}
		})
	}
}
