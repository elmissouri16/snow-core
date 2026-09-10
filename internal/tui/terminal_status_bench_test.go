package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func terminalBenchmarkModel(b *testing.B) *Model {
	b.Helper()
	dir := b.TempDir()
	a, err := app.New(b.Context(), app.Options{
		Provider: "fake", NoSession: true, Permission: "deny", CWD: dir,
		ConfigPath: filepath.Join(dir, "config.json"), AuthPath: filepath.Join(dir, "auth.json"),
		NoMCP: true, NoSkills: true, NoPlugins: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	m := newModel(b.Context(), app.Options{})
	m.app = a
	b.Cleanup(func() { _ = m.Close() })
	m.width, m.height, m.busy = 120, 40, true
	m.lines = make([]string, 10000)
	for i := range m.lines {
		m.lines[i] = "assistant output " + strings.Repeat("x", 24)
	}
	m.layout()
	m.refreshTranscriptForced()
	m.syncTerminalStatus()
	m.View()
	return m
}

func BenchmarkTerminalStatusFrame(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			m := terminalBenchmarkModel(b)
			m.app.Cfg.TUI.TerminalTitle, m.app.Cfg.TUI.TerminalProgress = enabled, enabled
			b.ReportAllocs()
			for b.Loop() {
				m.syncTerminalStatus()
				_ = m.View()
			}
		})
	}
}

func BenchmarkTerminalHeartbeat(b *testing.B) {
	m := terminalBenchmarkModel(b)
	tick := terminalProgressTick(m.terminal.generation)
	b.ReportAllocs()
	for b.Loop() {
		msg := terminalStatusFilter(m, tick)
		m.Update(msg)
		_ = m.View()
	}
}
