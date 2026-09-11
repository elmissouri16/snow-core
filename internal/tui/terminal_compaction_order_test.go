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

func TestTerminalCompactionResultsFenceAllCompletedOperations(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("later_rejection=%v", rejected), func(t *testing.T) {
			m := prepareScrollableModel(t)
			m.app.Cfg.TUI.Notifications = "always"
			events := make(chan protocol.AgentEvent, 64)
			unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
			defer unsubscribe()
			var previousSequence uint64
			for range 2 {
				result := m.startCompact()().(compactDoneMsg)
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.sequence <= previousSequence || result.epoch == 0 {
					t.Fatalf("missing monotonic identity: %+v", result)
				}
				previousSequence = result.sequence
				m.Update(result)
				if m.terminalActivity() != terminalDone || terminalTestAlert(m) == "" {
					t.Fatal("command result did not settle completion")
				}
			}
			if rejected {
				_ = m.startCompact()
				m.Update(compactDoneMsg{generation: m.compactGeneration, runGeneration: m.runGeneration, err: errors.New("admission unavailable")})
				if terminalTestAlert(m) == "" {
					t.Fatal("rejection did not alert")
				}
			}
			m.Update(tea.FocusMsg{})
			for range 2 {
				for _, ev := range collectCompactionEvents(t, events) {
					m.Update(agentEventMsg{ev: ev})
					if m.busy || m.compacting || m.terminalActivity() != terminalIdle || terminalTestAlert(m) != "" {
						t.Fatalf("delayed %s reopened acknowledged state: activity=%v busy=%v", ev.Type, m.terminalActivity(), m.busy)
					}
				}
			}
		})
	}
}

func TestTerminalCompactionFencePreservesNewEpochsAndChildEvents(t *testing.T) {
	m := prepareScrollableModel(t)
	m.fenceTerminalCompaction("settled", 3, 10)
	for _, tc := range []struct {
		ev   protocol.AgentEvent
		want bool
	}{
		{ev: protocol.AgentEvent{TurnID: "older", RootEpoch: 3, TurnSequence: 9}, want: true},
		{ev: protocol.AgentEvent{TurnID: "settled", RootEpoch: 3, TurnSequence: 10}, want: true},
		{ev: protocol.AgentEvent{TurnID: "newer", RootEpoch: 3, TurnSequence: 11}},
		{ev: protocol.AgentEvent{TurnID: "new-branch", RootEpoch: 4, TurnSequence: 1}},
		{ev: protocol.AgentEvent{TurnID: "legacy"}},
		{ev: protocol.AgentEvent{RootEpoch: 3}},
		{ev: protocol.AgentEvent{TurnID: "child", RootEpoch: 3, TurnSequence: 2, Agent: &protocol.AgentRef{Path: "/root/child"}}},
	} {
		if got := m.settledCompactionEvent(tc.ev); got != tc.want {
			t.Fatalf("fenced=%v want=%v event=%+v", got, tc.want, tc.ev)
		}
	}
}

func TestTerminalCompactionFinalMailboxErrorWinsEitherDeliveryOrder(t *testing.T) {
	for _, resultFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("result_first=%v", resultFirst), func(t *testing.T) {
			m := prepareScrollableModel(t)
			oldAgent := m.app.Agent
			t.Cleanup(func() { oldAgent.Close() })
			failure := errors.New("fixture mailbox persistence failed")
			store := &compactionMailboxFailureStore{Store: m.app.Session, failure: failure}
			p := &compactionMailboxProvider{Provider: fake.New([]fake.Step{{Kind: fake.StepText, Text: "fixture summary"}})}
			a, err := agent.New(agent.Options{
				Provider: p, Registry: m.app.Registry, Session: store, Permission: m.app.Perm, Model: m.app.Model,
				Compaction: agent.CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"},
			})
			if err != nil {
				t.Fatal(err)
			}
			m.app.Agent = a
			p.beforeChat = func() error {
				return a.EnqueueMailbox(protocol.AgentMessage{
					ID: "mail-during-compact", Author: "/root/child", Recipient: protocol.RootAgentPath,
					Kind: protocol.AgentMessageNormal, Content: "fixture child result", CreatedAt: time.Now().UnixMilli(),
				})
			}
			for i := range 6 {
				msg := protocol.NewUserMessage(fmt.Sprintf("fixture-%d", i), store.BranchTip(), fmt.Sprintf("message %d", i))
				if err := store.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
					t.Fatal(err)
				}
			}
			m.Update(tea.BlurMsg{})
			events := make(chan protocol.AgentEvent, 64)
			unsubscribe := a.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
			defer unsubscribe()
			result := m.startCompact()().(compactDoneMsg)
			if !errors.Is(result.err, failure) || result.result.SummarizedMessages == 0 || !a.PendingMailbox() {
				t.Fatalf("fixture did not fail final mailbox cleanup: %+v", result)
			}
			stream := collectCompactionEvents(t, events)
			if stream[len(stream)-1].IsError {
				t.Fatal("fixture must report stream success before final cleanup failure")
			}
			if resultFirst {
				m.Update(result)
			}
			for _, ev := range stream {
				m.Update(agentEventMsg{ev: ev})
			}
			if !resultFirst {
				if terminalTestAlert(m) != "" {
					t.Fatal("stream sent provisional success alert before final result")
				}
				m.Update(result)
			}
			if m.terminalActivity() != terminalFailed || m.busy {
				t.Fatalf("final persistence failure lost: activity=%v busy=%v", m.terminalActivity(), m.busy)
			}
			if alert := terminalTestAlert(m); !strings.Contains(alert, "Snow compaction failed") || strings.Contains(alert, failure.Error()) {
				t.Fatalf("missing generic failure notification: %q", alert)
			}
			m.Update(tea.FocusMsg{})
			m.Update(result)
			for _, ev := range stream {
				m.Update(agentEventMsg{ev: ev})
			}
			if m.terminalActivity() != terminalIdle || terminalTestAlert(m) != "" {
				t.Fatal("duplicate settlement undid acknowledgement")
			}
		})
	}
}

func TestTerminalCompactionFinalResultPreservesAcknowledgementAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		m := prepareScrollableModel(t)
		m.app.Cfg.TUI.Notifications = "always"
		_ = m.startCompact()
		m.Update(agentEventMsg{ev: protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "fixture", TurnOrigin: "compact"}})
		if terminalTestAlert(m) != "" {
			t.Fatal("provisional completion alerted")
		}
		m.Update(tea.FocusMsg{})
		var err error
		want := terminalIdle
		if canceled {
			err, want = context.Canceled, terminalAborted
		}
		result := compactDoneMsg{generation: m.compactGeneration, runGeneration: m.runGeneration, turnID: "fixture", err: err}
		m.Update(result)
		if m.terminalActivity() != want || terminalTestAlert(m) != "" {
			t.Fatalf("final result changed acknowledged/canceled state: canceled=%v activity=%v", canceled, m.terminalActivity())
		}
	}
}

func TestTerminalCompactionDeferredAlertCannotLeakIntoNewRun(t *testing.T) {
	for _, nextCompact := range []bool{false, true} {
		t.Run(fmt.Sprintf("next_compact=%v", nextCompact), func(t *testing.T) {
			m := prepareScrollableModel(t)
			m.Update(tea.BlurMsg{})
			events := make(chan protocol.AgentEvent, 64)
			unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
			defer unsubscribe()
			old := m.startCompact()().(compactDoneMsg)
			for _, ev := range collectCompactionEvents(t, events) {
				m.Update(agentEventMsg{ev: ev})
			}
			if m.busy || terminalTestAlert(m) != "" {
				t.Fatal("stream must settle without notifying before final result")
			}
			var next tea.Cmd
			if nextCompact {
				next = m.startCompact()
			} else {
				m.beginOptimisticRun()
			}
			m.Update(old)
			want := terminalRunning
			if nextCompact {
				want = terminalCompacting
			}
			if !m.busy || m.terminalActivity() != want || terminalTestAlert(m) != "" {
				t.Fatal("old result settled or notified during newer work")
			}
			if nextCompact {
				m.Update(next())
			} else {
				m.setRunIdle()
				// An externally requested compaction has no local command result.
				// The abandoned old command must not defer its notification.
				m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "external", TurnOrigin: "compact", RootEpoch: old.epoch, TurnSequence: old.sequence + 1})
			}
			if m.terminalActivity() != terminalDone || terminalTestAlert(m) == "" {
				t.Fatal("old pending result stranded newer completion notification")
			}
		})
	}
}

func TestTerminalCompactionExternalSuccessorDoesNotInheritLocalResult(t *testing.T) {
	m := prepareScrollableModel(t)
	m.Update(tea.BlurMsg{})
	events := make(chan protocol.AgentEvent, 64)
	unsubscribe := m.app.Agent.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	old := m.startCompact()().(compactDoneMsg)
	for _, ev := range collectCompactionEvents(t, events) {
		m.Update(agentEventMsg{ev: ev})
	}
	if m.busy || terminalTestAlert(m) != "" {
		t.Fatal("local stream must settle without a provisional notification")
	}
	// No optimistic TUI run separates these operations, so generations alone
	// cannot associate pending command results with the right operation.
	if _, err := m.app.Agent.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, ev := range collectCompactionEvents(t, events) {
		m.Update(agentEventMsg{ev: ev})
	}
	if m.terminalActivity() != terminalDone || terminalTestAlert(m) == "" {
		t.Fatal("external successor inherited the withheld local result")
	}
	m.Update(tea.FocusMsg{})
	old.err = errors.New("late old command failure")
	m.Update(old)
	if m.busy || m.terminalActivity() != terminalIdle || terminalTestAlert(m) != "" {
		t.Fatal("old command overwrote acknowledged external successor")
	}
}

func TestTerminalCompactionPreAdmissionFailureAfterBranchFence(t *testing.T) {
	m := prepareScrollableModel(t)
	m.fenceRootTurnProjection()
	m.Update(tea.BlurMsg{})
	m.app.Agent.Close()
	result := m.startCompact()().(compactDoneMsg)
	if result.err == nil || result.turnID != "" || result.epoch != 0 {
		t.Fatalf("fixture must reject before admission: %+v", result)
	}
	m.Update(result)
	if m.busy || m.compacting || m.terminalActivity() != terminalFailed || terminalTestAlert(m) == "" {
		t.Fatal("event fence discarded a current command rejection without a lifecycle event")
	}
}

func collectCompactionEvents(t *testing.T, events <-chan protocol.AgentEvent) []protocol.AgentEvent {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	var collected []protocol.AgentEvent
	for {
		select {
		case ev := <-events:
			collected = append(collected, ev)
			if ev.Type == protocol.EvCompactionDone {
				return collected
			}
		case <-deadline.C:
			t.Fatal("missing core compaction completion")
		}
	}
}

// Fault only the real final mailbox append, not summarization or checkpoint
// persistence, so the command's final error differs from the stream outcome.
type compactionMailboxFailureStore struct {
	session.Store
	failure error
}

func (s *compactionMailboxFailureStore) Append(entry session.Entry) error {
	if entry.Message != nil && entry.Message.Role == protocol.RoleAgent {
		return s.failure
	}
	return s.Store.Append(entry)
}

type compactionMailboxProvider struct {
	*fake.Provider
	beforeChat func() error
}

func (p *compactionMailboxProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if err := p.beforeChat(); err != nil {
		return nil, err
	}
	return p.Provider.Chat(ctx, req)
}
