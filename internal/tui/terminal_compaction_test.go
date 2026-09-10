package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTerminalManualCompactionAnnouncesCompletion(t *testing.T) {
	m := prepareScrollableModel(t)
	m.Update(tea.BlurMsg{})
	events := make(chan protocol.AgentEvent, 32)
	unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	cmd := m.startCompact()
	result := cmd()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	seen := false
	for !seen {
		select {
		case ev := <-events:
			m.Update(agentEventMsg{ev: ev})
			seen = ev.Type == protocol.EvCompactionDone
		case <-deadline.C:
			t.Fatal("no core compaction completion event")
		}
	}
	m.Update(result)
	if title := m.View().WindowTitle; !strings.HasSuffix(title, " · Done") {
		t.Errorf("successful manual compaction title=%q, want Done", title)
	}
	if alert := terminalTestAlert(m); alert == "" {
		t.Error("manual compaction completed unfocused without a completion alert")
	}
}

func TestTerminalManualCompactionFailureReportsFailed(t *testing.T) {
	m := prepareScrollableModel(t)
	oldAgent := m.app.Agent
	t.Cleanup(func() { oldAgent.Close() })
	provider := fake.New([]fake.Step{{Kind: fake.StepError, Err: errors.New("audit summary unavailable")}})
	a, err := agent.New(agent.Options{
		Provider: provider, Registry: m.app.Registry, Session: m.app.Session, Permission: m.app.Perm,
		Model:      m.app.Model,
		Compaction: agent.CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app.Agent = a
	for i := range 6 {
		msg := protocol.NewUserMessage(fmt.Sprintf("audit-%d", i), m.app.Session.BranchTip(), fmt.Sprintf("message %d", i))
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	m.Update(tea.BlurMsg{})
	events := make(chan protocol.AgentEvent, 32)
	unsubscribe := a.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	result := m.startCompact()().(compactDoneMsg)
	if result.err == nil {
		t.Fatal("fixture did not fail compaction")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	seen := false
	for !seen {
		select {
		case ev := <-events:
			m.Update(agentEventMsg{ev: ev})
			seen = ev.Type == protocol.EvCompactionDone
		case <-deadline.C:
			t.Fatal("no core compaction completion event")
		}
	}
	m.Update(result)
	if title := m.View().WindowTitle; !strings.HasSuffix(title, " · Failed") {
		t.Errorf("failed manual compaction title=%q, want Failed; error=%v", title, result.err)
	}
	if alert := terminalTestAlert(m); alert == "" {
		t.Error("failed compaction did not alert unfocused terminal")
	}
}

func TestTerminalCompactionResultBeforeEventsSettlesOnce(t *testing.T) {
	m := prepareScrollableModel(t)
	m.Update(tea.BlurMsg{})
	events := make(chan protocol.AgentEvent, 32)
	unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	result := m.startCompact()().(compactDoneMsg)
	m.Update(result)
	if m.terminalActivity() != terminalDone || terminalTestAlert(m) == "" {
		t.Fatal("result did not complete compaction")
	}
	m.Update(tea.FocusMsg{})
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for done := false; !done; {
		select {
		case ev := <-events:
			m.Update(agentEventMsg{ev: ev})
			done = ev.Type == protocol.EvCompactionDone
		case <-deadline.C:
			t.Fatal("no core compaction completion event")
		}
	}
	m.Update(result)
	if m.busy || m.compacting || m.terminalActivity() != terminalIdle || terminalTestAlert(m) != "" {
		t.Fatalf("delayed compaction reopened acknowledged state: activity=%v busy=%v", m.terminalActivity(), m.busy)
	}
}

func TestTerminalCompactionPreAdmissionFailureAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		m := prepareScrollableModel(t)
		m.Update(tea.BlurMsg{})
		_ = m.startCompact()
		err := errors.New("admission unavailable")
		if canceled {
			err = context.Canceled
		}
		m.Update(compactDoneMsg{generation: m.compactGeneration, runGeneration: m.runGeneration, err: err})
		want := terminalFailed
		if canceled {
			want = terminalAborted
		}
		if m.terminalActivity() != want || m.busy {
			t.Fatalf("activity=%v busy=%v", m.terminalActivity(), m.busy)
		}
		if got := terminalTestAlert(m); (got == "") != canceled {
			t.Fatalf("canceled=%v alert=%q", canceled, got)
		}
	}
}

func TestTerminalCompactionAbortAndAutomaticPhase(t *testing.T) {
	m := prepareScrollableModel(t)
	_ = m.startCompact()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: "manual", TurnOrigin: "compact"})
	m.requestAbort()
	m.Update(tea.BlurMsg{})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "manual", TurnOrigin: "compact", IsError: true})
	if m.terminalActivity() != terminalAborted || terminalTestAlert(m) != "" || m.busy {
		t.Fatal("canceled compaction announced failure")
	}
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: "auto"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "auto", Compaction: &protocol.CompactionResult{Automatic: true}})
	if m.terminalActivity() != terminalRunning || terminalTestAlert(m) != "" {
		t.Fatal("automatic intermediate phase announced completion")
	}
}

func TestOldCompactionResultCannotSettleNewPrompt(t *testing.T) {
	m := prepareScrollableModel(t)
	_ = m.startCompact()
	old := compactDoneMsg{generation: m.compactGeneration, runGeneration: m.runGeneration, err: errors.New("late")}
	m.setRunIdle()
	m.beginOptimisticRun()
	m.Update(old)
	if !m.busy || m.terminalActivity() != terminalRunning {
		t.Fatal("old compaction result settled new prompt")
	}
}
