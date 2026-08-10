package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestPermissionModePicker(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	m.editor.SetValue("/permissions")
	m.refreshPalette()
	_, _ = m.handleKey(teaKeyEnter())
	if !m.pickPermissionMode {
		t.Fatal("/permissions should open the permission-mode picker")
	}
	_, _ = m.handleKey(teaKeyEnter())
	if m.app.Perm.Mode() != permission.ModeAllow {
		t.Fatalf("permission mode = %q, want allow", m.app.Perm.Mode())
	}

	m.editor.SetValue("/permissions")
	m.refreshPalette()
	_, _ = m.handleKey(teaKeyEnter())
	if !m.pickPermissionMode {
		t.Fatal("/permissions should open the permission-mode picker")
	}
	_, _ = m.handleKey(teaKeyEsc())
	if m.app.Perm.Mode() != permission.ModeAllow {
		t.Fatalf("Esc changed permission mode to %q", m.app.Perm.Mode())
	}
}

func TestStartupSessionHydratesTranscript(t *testing.T) {
	testHome(t)
	cwd := t.TempDir()
	idx := session.NewFileIndex(session.DefaultSessionsRoot())
	st, err := idx.Create(cwd)
	if err != nil {
		t.Fatal(err)
	}
	msg := protocol.NewUserMessage("startup-user", "root", "loaded on startup")
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	path := st.Path()
	_ = st.Close()

	a, err := app.New(context.Background(), app.Options{Provider: "fake", CWD: cwd, SessionPath: path, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	m := newModel(context.Background(), app.Options{})
	m.width = 100
	m.height = 30
	m.layout()
	_, _ = m.Update(doneMsg{app: a})
	if !strings.Contains(strings.Join(m.lines, "\n"), "loaded on startup") {
		t.Fatalf("startup transcript = %q", strings.Join(m.lines, "\n"))
	}
}

func TestSessionPickerResumeAndNew(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	old := m.app.Session.ID()

	idx := session.NewFileIndex(session.DefaultSessionsRoot())
	resume, err := idx.Create(m.app.CWD())
	if err != nil {
		t.Fatal(err)
	}
	resumeID := resume.ID()
	msg := protocol.NewUserMessage("user-1", "root", "resumed message")
	if err := resume.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	if err := resume.Close(); err != nil {
		t.Fatal(err)
	}

	// A session from another directory must not be resumable here.
	other, err := idx.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherMsg := protocol.NewUserMessage("other-user", "root", "other directory")
	if err := other.Append(session.Entry{Type: session.EntryMessage, ID: otherMsg.ID, Message: &otherMsg}); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}

	_, _ = m.startSessionPick()
	if !m.pickSession || len(m.sessions) != 1 {
		t.Fatalf("session picker = %v, sessions = %d", m.pickSession, len(m.sessions))
	}
	if m.sessionIndex != 0 {
		t.Fatalf("first resume entry index = %d, want 0", m.sessionIndex)
	}
	if strings.Contains(stripANSI(m.renderSessionPicker()), "New session") {
		t.Fatal("resume picker must not contain a New session row")
	}
	_, _ = m.handleSessionPick(teaKeyEnter())
	if m.app.Session.ID() != resumeID || m.app.Session.ID() == old {
		t.Fatal("resume did not switch sessions")
	}
	if !strings.Contains(strings.Join(m.lines, "\n"), "resumed message") {
		t.Fatalf("resumed transcript = %q", strings.Join(m.lines, "\n"))
	}

	previous := m.app.Session.ID()
	_, _ = m.startNewSession()
	if m.app.Session.ID() == previous || m.app.Session.Path() == "" {
		t.Fatal("/new did not create a persisted session")
	}
}

func TestTreePickerSelectsAndForksBranches(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	if err := m.app.Agent.Prompt(context.Background(), "branch base"); err != nil {
		t.Fatal(err)
	}
	messages, err := m.app.Agent.Messages()
	if err != nil || len(messages) == 0 {
		t.Fatalf("messages = %+v, err=%v", messages, err)
	}
	fork, err := m.app.Agent.Fork(messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fork.Active {
		t.Fatalf("fork = %+v", fork)
	}
	_, _ = m.startTreePick()
	if !m.pickTree || len(m.branches) != 2 {
		t.Fatalf("tree picker = %v, branches = %+v", m.pickTree, m.branches)
	}
	m.branchIndex = 0
	_, _ = m.handleTreePick(teaKeyEnter())
	if m.pickTree {
		t.Fatal("tree picker stayed open after select")
	}
	if m.app.Agent.IsRunning() {
		t.Fatal("branch selection marked agent running")
	}
}

func TestTreeAsyncNamedForkAndRenameActions(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.asyncIO = true
	if err := m.app.Agent.Prompt(context.Background(), "base"); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.startTreePick()
	if cmd == nil {
		t.Fatal("missing async list")
	}
	m.Update(cmd())
	_, _ = m.handleTreePick(teaKeyRunes('f'))
	for _, r := range "named" {
		_, _ = m.handleTreePick(teaKeyRunes(r))
	}
	_, cmd = m.handleTreePick(teaKeyEnter())
	if cmd == nil {
		t.Fatal("missing fork action")
	}
	m.Update(cmd())
	branches, err := m.app.Agent.Branches()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, branch := range branches {
		if branch.Name == "named" {
			found = true
		}
	}
	if !found {
		t.Fatalf("branches=%+v", branches)
	}
	_, cmd = m.startTreePick()
	m.Update(cmd())
	for i, branch := range m.branches {
		if branch.Active {
			m.branchIndex = i
		}
	}
	_, _ = m.handleTreePick(teaKeyRunes('r'))
	_, _ = m.handleTreePick(teaKeyRunes('-'))
	_, _ = m.handleTreePick(teaKeyRunes('x'))
	_, cmd = m.handleTreePick(teaKeyEnter())
	if cmd == nil {
		t.Fatal("missing rename action")
	}
	m.Update(cmd())
	branches, _ = m.app.Agent.Branches()
	found = false
	for _, branch := range branches {
		if branch.Name == "named-x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("renamed branches=%+v", branches)
	}
}

func TestSessionsPickerListsCurrentDirectoryAndNavigates(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.width = 100
	m.height = 30
	buildAppForTest(t, m)
	activeID := m.app.Session.ID()
	idx := session.NewFileIndex(session.DefaultSessionsRoot())

	current, err := idx.Create(m.app.CWD())
	if err != nil {
		t.Fatal(err)
	}
	currentID := current.ID()
	currentMsg := protocol.NewUserMessage("current-user", "root", "current directory")
	if err := current.Append(session.Entry{Type: session.EntryMessage, ID: currentMsg.ID, Message: &currentMsg}); err != nil {
		t.Fatal(err)
	}
	currentPath := current.Path()
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}

	other, err := idx.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherPath := other.Path()
	otherMsg := protocol.NewUserMessage("other-user", "root", "other directory")
	if err := other.Append(session.Entry{Type: session.EntryMessage, ID: otherMsg.ID, Message: &otherMsg}); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}

	_, _ = m.runCommand("/sessions")
	if !m.pickSession || len(m.sessions) != 1 {
		t.Fatalf("/sessions picker = %v, sessions = %d", m.pickSession, len(m.sessions))
	}
	view := stripANSI(m.renderSessionPicker())
	if !strings.Contains(view, shortSessionID(currentID)) || strings.Contains(view, currentPath) {
		t.Fatalf("sessions picker = %q, want compact current-session row", view)
	}
	if strings.Contains(view, otherPath) || strings.Contains(view, "other directory") {
		t.Fatalf("sessions picker leaked another directory: %q", view)
	}
	// The picker consumes arrow keys instead of moving the composer cursor.
	_, _ = m.handleSessionPick(teaKeyDown())
	_, _ = m.handleSessionPick(teaKeyUp())
	if m.sessionIndex != 0 {
		t.Fatalf("session index after navigation = %d, want 0", m.sessionIndex)
	}
	if m.app.Session.ID() != activeID {
		t.Fatal("/sessions switched the active session before Enter")
	}
	_, _ = m.handleSessionPick(teaKeyEnter())
	if m.app.Session.ID() != currentID {
		t.Fatal("/sessions Enter did not switch sessions")
	}
}

func TestResumeWithNoSessionsShowsEmptyState(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	_, _ = m.runCommand("/resume")
	if m.pickSession {
		t.Fatal("empty /resume should not open a picker")
	}
	if !strings.Contains(stripANSI(strings.Join(m.lines, "\n")), "no sessions to resume") {
		t.Fatalf("empty resume output = %q", strings.Join(m.lines, "\n"))
	}
	// Defensive: a stale picker event must not panic or create a session.
	_, _ = m.handleSessionPick(teaKeyEnter())
	if m.app.Session.Path() != "" {
		t.Fatal("empty resume event created a session")
	}
}

func TestRemovedSingularSessionAndPermissionCommands(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)

	for _, command := range []string{"/session", "/permission"} {
		_, _ = m.runCommand(command)
	}
	view := stripANSI(strings.Join(m.lines, "\n"))
	for _, command := range []string{"/session", "/permission"} {
		if !strings.Contains(view, "unknown command: "+command) {
			t.Fatalf("output = %q, want rejection for %s", view, command)
		}
	}
}

func TestResumeDirectPath(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	idx := session.NewFileIndex(session.DefaultSessionsRoot())
	st, err := idx.Create(m.app.CWD())
	if err != nil {
		t.Fatal(err)
	}
	msg := protocol.NewUserMessage("direct-user", "root", "direct resume")
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	path := st.Path()
	id := st.ID()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	_, _ = m.runCommand("/resume " + path)
	if m.app.Session.ID() != id || !strings.Contains(strings.Join(m.lines, "\n"), "direct resume") {
		t.Fatalf("direct resume did not open %q: id=%q lines=%q", path, m.app.Session.ID(), strings.Join(m.lines, "\n"))
	}
}

func TestSessionPickerScrollsAndFitsTerminal(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 80
	m.height = 20
	m.sessions = make([]session.SessionInfo, 20)
	for i := range m.sessions {
		m.sessions[i] = session.SessionInfo{
			ID:       fmt.Sprintf("session-%02d", i),
			Path:     fmt.Sprintf("/tmp/session-%02d.jsonl", i),
			Messages: i + 1,
		}
	}
	m.pickSession = true
	m.sessionIndex = 10
	m.layout()

	rows := m.sessionPickerRows()
	if rows > m.height-8 {
		t.Fatalf("session picker rows = %d, terminal budget = %d", rows, m.height-8)
	}
	start, end := m.sessionWindow()
	if end-start >= len(m.sessions) {
		t.Fatalf("session window should be bounded: %d:%d", start, end)
	}
	view := stripANSI(m.renderSessionPicker())
	if !strings.Contains(view, "sessions (20)") || !strings.Contains(view, "more sessions") {
		t.Fatalf("bounded session picker missing status/scroll marker: %q", view)
	}

	old := m.sessionIndex
	_, _ = m.handleSessionPick(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.sessionIndex <= old {
		t.Fatalf("PgDown did not advance session selection: %d -> %d", old, m.sessionIndex)
	}
	_, _ = m.handleSessionPick(tea.KeyMsg{Type: tea.KeyEnd})
	if m.sessionIndex != len(m.sessions)-1 {
		t.Fatalf("End index = %d, want %d", m.sessionIndex, len(m.sessions)-1)
	}
}

// Small key helpers keep this test focused on picker behavior without
// repeating Bubble Tea imports throughout the session assertions.
func teaKeyEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func teaKeyDown() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyDown} }
func teaKeyUp() tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyUp} }
func teaKeyEsc() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
