package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/config"
)

func TestDebugCommandOpensSettingsAndPersistsToggle(t *testing.T) {
	home := testHome(t)
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir(), ConfigPath: filepath.Join(home, "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(context.Background(), app.Options{})
	m.app = a

	_, _ = m.runCommand("/debug")
	if !m.pickSettings || m.settingsIndex != settingsDebug {
		t.Fatalf("debug menu settings=%v index=%d", m.pickSettings, m.settingsIndex)
	}
	m.pickSettings = false
	_, _ = m.runCommand("/debug on")
	if !m.app.DebugStatus().Enabled {
		t.Fatal("/debug on did not enable runtime capture")
	}
	persisted, err := config.Load(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.Debug.Enabled {
		t.Fatal("/debug on did not persist enablement")
	}
	if len(m.lines) == 0 || !strings.Contains(stripANSI(m.lines[len(m.lines)-1]), "sensitive") {
		t.Fatalf("toggle did not warn about sensitivity: %v", m.lines)
	}

	_, clearCmd := m.runCommand("/debug clear")
	if clearCmd == nil {
		t.Fatal("/debug clear did not schedule asynchronous work")
	}
	clearMessage, ok := clearCmd().(debugClearDoneMsg)
	if !ok || clearMessage.err != nil {
		t.Fatalf("clear result=%#v", clearMessage)
	}
	_, _ = m.update(clearMessage)
	if !strings.Contains(strings.Join(m.lines, "\n"), "debug event capture cleared") {
		t.Fatalf("clear completion missing: %v", m.lines)
	}

	_, _ = m.runCommand("/debug off")
	persisted, err = config.Load(a.ConfigPath)
	if err != nil || persisted.Debug.Enabled || m.app.DebugStatus().Enabled {
		t.Fatalf("disabled persisted=%v runtime=%v err=%v", persisted.Debug.Enabled, m.app.DebugStatus().Enabled, err)
	}
}

func TestParseDebugDumpPath(t *testing.T) {
	for input, want := range map[string]string{`"path with spaces.json"`: "path with spaces.json", `'literal path.json'`: "literal path.json", "plain path.json": "plain path.json"} {
		got, err := parseDebugDumpPath(input)
		if err != nil || got != want {
			t.Fatalf("parse %q=%q err=%v want=%q", input, got, err, want)
		}
	}
	if _, err := parseDebugDumpPath(`"unterminated`); err == nil {
		t.Fatal("unterminated path unexpectedly accepted")
	}
}

func TestDebugDumpCommandIsAsyncAndReportsPath(t *testing.T) {
	testHome(t)
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(context.Background(), app.Options{})
	m.app = a
	path := filepath.Join(t.TempDir(), "tui-dump.json")
	_, cmd := m.runCommand("/debug dump " + path)
	if cmd == nil {
		t.Fatal("dump command did not schedule asynchronous work")
	}
	message, ok := cmd().(debugDumpDoneMsg)
	if !ok || message.err != nil || message.path != path {
		t.Fatalf("dump result=%#v", message)
	}
	_, _ = m.update(message)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	transcript := strings.Join(m.lines, "\n")
	if !strings.Contains(transcript, path) || !strings.Contains(transcript, "review before sharing") {
		t.Fatalf("dump result missing path/warning: %s", transcript)
	}
}
