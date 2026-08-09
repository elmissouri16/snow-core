package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func completePlanForModal(t *testing.T, m *Model) {
	t.Helper()
	if err := m.app.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: "p"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "# Ship\n- test\n", Plan: &protocol.PlanItem{ID: "p"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: &protocol.PlanItem{ID: "p", Text: "# Ship\n- test\n"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if !m.planPrompt {
		t.Fatal("plan modal did not open")
	}
}

func TestPlanModalRequiresSuccessfulNonEmptyCompletion(t *testing.T) {
	for _, terminal := range []protocol.AgentEventType{protocol.EvError, protocol.EvAborted} {
		t.Run(string(terminal), func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			buildAppForTest(t, m)
			_ = m.app.Agent.SetMode(protocol.ModePlan)
			m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: "p"}})
			m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanDelta, Text: "partial", Plan: &protocol.PlanItem{ID: "p"}})
			m.handleAgentEvent(protocol.AgentEvent{Type: terminal, Message: "failed"})
			m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
			if m.planPrompt || m.completedPlanThisTurn || m.sawPlanThisTurn {
				t.Fatalf("partial failed plan opened modal or retained flags: %+v", m)
			}
		})
	}
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	_ = m.app.Agent.SetMode(protocol.ModePlan)
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: "empty"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: &protocol.PlanItem{ID: "empty", Text: " \n"}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if m.planPrompt {
		t.Fatal("empty plan opened modal")
	}
}

func TestPlanImplementationModalChoices(t *testing.T) {
	t.Run("first current context", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		completePlanForModal(t, m)
		id := m.app.Session.ID()
		_, cmd := m.handlePlanImplementationKey(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("missing prompt command")
		}
		_ = cmd()
		if m.app.Session.ID() != id || m.app.Agent.Mode() != protocol.ModeDefault {
			t.Fatalf("session=%q mode=%q", m.app.Session.ID(), m.app.Agent.Mode())
		}
	})

	t.Run("fresh ephemeral", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		completePlanForModal(t, m)
		id := m.app.Session.ID()
		m.planPromptChoice = 1
		_, cmd := m.handlePlanImplementationKey(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("missing prompt command")
		}
		if m.app.Session.ID() == id || m.app.Session.Path() != "" {
			t.Fatalf("fresh session id=%q path=%q", m.app.Session.ID(), m.app.Session.Path())
		}
		_ = cmd()
	})

	t.Run("fresh persisted", func(t *testing.T) {
		testHome(t)
		m := newModel(context.Background(), app.Options{})
		a, err := app.New(context.Background(), app.Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		m.app = a
		t.Cleanup(func() { _ = a.Close() })
		completePlanForModal(t, m)
		id := m.app.Session.ID()
		m.planPromptChoice = 1
		_, cmd := m.handlePlanImplementationKey(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("missing prompt command")
		}
		if m.app.Session.ID() == id || m.app.Session.Path() == "" {
			t.Fatalf("fresh session id=%q path=%q", m.app.Session.ID(), m.app.Session.Path())
		}
		_ = cmd()
	})

	t.Run("stay plan", func(t *testing.T) {
		m := newModel(context.Background(), app.Options{})
		buildAppForTest(t, m)
		completePlanForModal(t, m)
		id := m.app.Session.ID()
		m.planPromptChoice = 2
		_, cmd := m.handlePlanImplementationKey(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || m.app.Session.ID() != id || m.app.Agent.Mode() != protocol.ModePlan {
			t.Fatalf("cmd=%v session=%q mode=%q", cmd != nil, m.app.Session.ID(), m.app.Agent.Mode())
		}
	})
}

func TestPlanCommandExpandsFileMentions(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	path := filepath.Join(m.app.CWD(), "notes.md")
	if err := os.WriteFile(path, []byte("mention contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mentionFiles = []string{"notes.md"}
	_, cmd := m.runCommand("/plan inspect @notes.md")
	if cmd == nil {
		t.Fatal("missing plan prompt command")
	}
	_ = cmd()
	messages, err := m.app.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || !strings.Contains(sessionMessageText(messages[0]), "mention contents") {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestCommittedPlanSurvivesResize(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 80, 30
	m.layout()
	m.planBuf.WriteString("# Resize Plan\n- preserve this committed source content across a narrower terminal")
	m.finalizePlan()
	before := stripANSI(m.transcriptContent)
	m.width = 36
	m.layout()
	m.refreshTranscript()
	after := stripANSI(m.transcriptContent)
	if !strings.Contains(after, "Resize Plan") || !strings.Contains(after, "preserve this committed") || strings.Count(after, "Resize Plan") != 1 {
		t.Fatalf("before=%q after=%q", before, after)
	}
	if m.transcriptBaseWidth != m.transcript.Width {
		t.Fatalf("base width=%d transcript width=%d", m.transcriptBaseWidth, m.transcript.Width)
	}
}
