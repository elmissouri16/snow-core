package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func BenchmarkMailboxIngestion(b *testing.B) {
	for b.Loop() {
		q := newAgentEventMailbox()
		for range 10000 {
			q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "token "})
		}
		_ = q.popBatch(maxAgentEventsPerUpdate)
	}
}

func BenchmarkSessionHydration5000(b *testing.B) {
	home := b.TempDir()
	runtime, err := app.New(context.Background(), app.Options{
		Provider: "fake", Permission: "allow", CWD: home,
		SessionPath: filepath.Join(home, "session.db"),
		ConfigPath:  filepath.Join(home, "config.json"), AuthPath: filepath.Join(home, "auth.json"),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Close()
	payload := strings.Repeat("x", 2400)
	batch := make([]session.Entry, 5000)
	for i := range batch {
		message := protocol.NewAssistantMessage(fmt.Sprintf("message-%d", i), "", "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: payload}}, protocol.StopStop, nil)
		batch[i] = session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}
	}
	if err := runtime.Session.(session.BatchStore).AppendBatch(batch); err != nil {
		b.Fatal(err)
	}
	m := newModel(context.Background(), app.Options{})
	m.app = runtime
	m.width, m.height = 120, 40
	m.layout()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.hydrateSession()
	}
}

func BenchmarkSessionHydrationMixed5000(b *testing.B) {
	home := b.TempDir()
	runtime, err := app.New(context.Background(), app.Options{
		Provider: "fake", Permission: "allow", CWD: home,
		SessionPath: filepath.Join(home, "session.db"),
		ConfigPath:  filepath.Join(home, "config.json"), AuthPath: filepath.Join(home, "auth.json"),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Close()
	batch := make([]session.Entry, 0, 5000)
	assistantBody := strings.Repeat("x", 1200)
	userBody := strings.Repeat("u", 256)
	for i := range 1250 {
		user := protocol.NewUserMessage(fmt.Sprintf("mixed-user-%d", i), "", userBody)
		batch = append(batch, session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user})
		usage := &protocol.Usage{Input: 1000 + i, Output: 100, Total: 1100 + i}
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("mixed-assistant-%d", i), "", "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: assistantBody}}, protocol.StopStop, usage)
		batch = append(batch, session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant})
		callID := fmt.Sprintf("mixed-call-%d", i)
		call := protocol.NewAssistantMessage(fmt.Sprintf("mixed-call-message-%d", i), "", "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}}, protocol.StopToolUse, nil)
		batch = append(batch, session.Entry{Type: session.EntryMessage, ID: call.ID, Message: &call})
		result := protocol.NewToolResultMessage(fmt.Sprintf("mixed-result-%d", i), "", callID, "read", []protocol.ContentBlock{protocol.NewTextBlock("bounded output")}, false)
		if i%100 != 0 {
			result.ToolDisplay = &protocol.ToolDisplay{Started: true, StartMessage: "README.md", Output: "bounded output", DurationMS: 1}
		}
		batch = append(batch, session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result})
	}
	if err := runtime.Session.(session.BatchStore).AppendBatch(batch); err != nil {
		b.Fatal(err)
	}
	m := newModel(context.Background(), app.Options{})
	m.app = runtime
	m.width, m.height = 120, 40
	m.layout()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.hydrateSession()
	}
}

func BenchmarkBusySessionUpdateBurst12MB(b *testing.B) {
	home := b.TempDir()
	runtime, err := app.New(context.Background(), app.Options{
		Provider: "fake", Permission: "allow", CWD: home,
		SessionPath: filepath.Join(home, "session.db"),
		ConfigPath:  filepath.Join(home, "config.json"), AuthPath: filepath.Join(home, "auth.json"),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Close()
	payload := strings.Repeat("x", 2400)
	for i := range 5000 {
		message := protocol.NewAssistantMessage(fmt.Sprintf("message-%d", i), "", "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: payload}}, protocol.StopStop, nil)
		if err := runtime.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			b.Fatal(err)
		}
	}
	m := newModel(context.Background(), app.Options{})
	m.app = runtime
	m.busy = true
	events := make([]protocol.AgentEvent, 32)
	for i := range events {
		events[i] = protocol.AgentEvent{Type: protocol.EvSessionUpdated}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, event := range events {
			m.handleAgentEvent(event)
		}
	}
}

func BenchmarkPlanNudgeLongSession(b *testing.B) {
	home := b.TempDir()
	runtime, err := app.New(context.Background(), app.Options{
		Provider: "fake", Permission: "allow", CWD: home,
		SessionPath: filepath.Join(home, "session.db"),
		ConfigPath:  filepath.Join(home, "config.json"), AuthPath: filepath.Join(home, "auth.json"),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Close()
	payload := strings.Repeat("x", 2400)
	for i := range 5000 {
		message := protocol.NewAssistantMessage(fmt.Sprintf("message-%d", i), "", "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: payload}}, protocol.StopStop, nil)
		if err := runtime.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			b.Fatal(err)
		}
	}
	m := newModel(context.Background(), app.Options{})
	m.app = runtime
	m.editor.SetValue("make a plan first")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !m.planNudgeVisible() {
			b.Fatal("plan nudge unexpectedly hidden")
		}
	}
}

func BenchmarkSubagentFleetView32(b *testing.B) {
	m := newModel(context.Background(), app.Options{})
	m.width, m.height = 140, 42
	m.subagentFleetOpen = true
	m.subagentFleetList.ConcurrentLimit = 32
	for i := range 32 {
		state := fleetTestState(fmt.Sprintf("agent-%d", i), fmt.Sprintf("/root/agent_%d", i), protocol.AgentRunning)
		m.subagentFleetList.Agents = append(m.subagentFleetList.Agents, state)
		m.subagentFleetActivity[state.Agent.ThreadID] = []string{"12:00:00  tool ▶  grep", "12:00:01  thinking  inspecting files"}
	}
	m.subagentFleetList.Running = 32
	b.ReportAllocs()
	for b.Loop() {
		_ = m.renderSubagentFleetModal()
	}
}

func BenchmarkTranscriptRefresh10K(b *testing.B) {
	m := newModel(context.Background(), app.Options{})
	m.width, m.height = 120, 40
	m.layout()
	m.lines = make([]string, 10000)
	for i := range m.lines {
		m.lines[i] = fmt.Sprintf("line %d: stable transcript content", i)
	}
	for b.Loop() {
		m.transcriptBaseDirty = true
		m.transcriptDirty = true
		m.refreshTranscriptForced()
	}
}

func BenchmarkViewNormalAndNarrow(b *testing.B) {
	for _, width := range []int{40, 120} {
		b.Run(fmt.Sprintf("width-%d", width), func(b *testing.B) {
			m := newModel(context.Background(), app.Options{})
			m.width, m.height = width, 30
			m.lines = make([]string, 500)
			for i := range m.lines {
				m.lines[i] = "assistant output " + strings.Repeat("x", 24)
			}
			m.layout()
			m.refreshTranscriptForced()
			b.ResetTimer()
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}

func BenchmarkMentionMatching(b *testing.B) {
	files := make([]string, 2000)
	for i := range files {
		files[i] = fmt.Sprintf("internal/package-%03d/file.go", i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = matchMentionFiles(files, "file")
	}
}

func BenchmarkComposerBackspace(b *testing.B) {
	for _, size := range []int{256, 8 << 10, 64 << 10} {
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			m := newModel(context.Background(), app.Options{})
			m.width, m.height = 120, 40
			payload := strings.Repeat("word ", size/5+1)[:size]
			m.editor.SetValue(payload)
			m.editor.CursorEnd()
			m.layout()
			remaining := size
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if remaining == 0 {
					m.editor.SetValue(payload)
					m.editor.CursorEnd()
					remaining = size
				}
				_, _ = m.updateComposerEditor(tea.KeyMsg{Type: tea.KeyBackspace})
				m.layout()
				_ = m.View()
				remaining--
			}
		})
	}
}

func BenchmarkMentionDiscovery(b *testing.B) {
	root := b.TempDir()
	for i := range 200 {
		path := filepath.Join(root, fmt.Sprintf("pkg-%03d", i), "file.go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		_ = discoverMentionFiles(root)
	}
}
