package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/config"
)

type debugDumpDoneMsg struct {
	path string
	err  error
}

type debugClearDoneMsg struct{ err error }

func (m *Model) setDebugEnabled(enabled bool) error {
	candidate, err := m.persistConfig(func(latest *config.Config) error {
		latest.Debug.Enabled = enabled
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist debug diagnostics: %w", err)
	}
	m.app.PersistedCfg = candidate
	m.app.SetDebugEnabled(enabled)
	return nil
}

func (m *Model) startDebugClear() (tea.Model, tea.Cmd) {
	m.lastStatus = "clearing diagnostic capture…"
	appRuntime := m.app
	clearContext := m.ctx
	if clearContext == nil {
		clearContext = context.Background()
	}
	return m, func() tea.Msg {
		return debugClearDoneMsg{err: appRuntime.ClearDebugEvents(clearContext)}
	}
}

func (m *Model) startDebugDump(path string) (tea.Model, tea.Cmd) {
	if m.busy || m.app.Agent.IsRunning() {
		m.pushLine(styleError.Render("debug: wait for the current turn to finish"))
		return m, nil
	}
	m.lastStatus = "creating sensitive diagnostic dump…"
	appRuntime := m.app
	dumpContext := m.ctx
	if dumpContext == nil {
		dumpContext = context.Background()
	}
	return m, func() tea.Msg {
		resolved, err := appRuntime.CreateDebugDump(dumpContext, path)
		return debugDumpDoneMsg{path: resolved, err: err}
	}
}

func (m *Model) debugStatusLine() string {
	status := m.app.DebugStatus()
	state := onOff(status.Enabled)
	return fmt.Sprintf("debug diagnostics %s · %d events · %s retained · %d dropped", state, status.EventCount, debugBytes(status.RetainedBytes), status.DroppedEvents)
}

func parseDebugDumpPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", fmt.Errorf("debug: unterminated quoted dump path")
		}
		return value[1 : len(value)-1], nil
	}
	if value[0] == '"' || value[0] == '`' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("debug: invalid quoted dump path: %w", err)
		}
		return unquoted, nil
	}
	return value, nil
}

func debugBytes(value int) string {
	if value < 1<<10 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(value)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/(1<<20))
}
