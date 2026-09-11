package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/elmissouri16/snow-core/internal/agent"
	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func terminalGoalFailurePhase(m *Model, id string) {
	m.beginOptimisticRun()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: id, TurnOrigin: "compact"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: id, TurnOrigin: "compact", IsError: true,
		Compaction: &protocol.CompactionResult{Automatic: true}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: id, Message: "goal auto-compaction: private fixture error"})
}

func terminalBlockedGoalEvent(id string) protocol.AgentEvent {
	return protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, TurnID: id,
		ThreadGoal: &protocol.ThreadGoalUpdate{Goal: &protocol.ThreadGoal{
			GoalID: "goal-alert", Status: protocol.GoalBlocked, BlockedReason: "Automatic compaction failed: private fixture error",
		}}}
}

func TestTerminalGoalFailureBoundaryNotifiesOnce(t *testing.T) {
	for _, id := range []string{"", "automatic-compaction"} {
		t.Run("identity="+id, func(t *testing.T) {
			m := modelPickerTestModel(t, 80, 24)
			m.Update(tea.BlurMsg{})
			terminalGoalFailurePhase(m, id)
			if !m.busy || terminalTestAlert(m) != "" {
				t.Fatal("compaction phase announced a terminal failure before goal boundary")
			}
			ev := terminalBlockedGoalEvent(id)
			m.handleAgentEvent(ev)
			if m.busy || m.compacting || m.terminalActivity() != terminalFailed {
				t.Fatalf("goal boundary busy=%v compacting=%v activity=%v", m.busy, m.compacting, m.terminalActivity())
			}
			if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow encountered an error") {
				t.Fatalf("goal failure notification=%q", got)
			}
			for range 3 {
				m.handleAgentEvent(ev)
				ev.Snapshot = true
				m.handleAgentEvent(ev)
				if got := terminalTestAlert(m); got != "" {
					t.Fatalf("repeated goal snapshot notified again: %q", got)
				}
			}
			m.Update(tea.FocusMsg{})
			m.Update(tea.BlurMsg{})
			m.handleAgentEvent(ev)
			if m.busy || m.terminalActivity() != terminalIdle || terminalTestAlert(m) != "" {
				t.Fatal("snapshot resurrected acknowledged failure")
			}
			// A later run may fail for the same goal and must get its own alert.
			terminalGoalFailurePhase(m, "next-compaction")
			m.handleAgentEvent(terminalBlockedGoalEvent("next-compaction"))
			if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow encountered an error") {
				t.Fatalf("new run inherited old failure suppression: %q", got)
			}
		})
	}
}

func TestTerminalGoalFailureFocusAndNotificationPolicy(t *testing.T) {
	for _, mode := range []string{"off", "unfocused", "always"} {
		for _, focused := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/focused=%v", mode, focused), func(t *testing.T) {
				m := modelPickerTestModel(t, 80, 24)
				m.app.Cfg.TUI.Notifications = mode
				m.terminal.unfocused = !focused
				terminalGoalFailurePhase(m, "compact")
				m.handleAgentEvent(terminalBlockedGoalEvent("compact"))
				want := mode == "always" || mode == "unfocused" && !focused
				if got := terminalTestAlert(m); (got != "") != want {
					t.Fatalf("notification=%q, want alert=%v", got, want)
				}
				m.app.Cfg.TUI.Notifications = "always"
				m.Update(tea.BlurMsg{})
				m.handleAgentEvent(terminalBlockedGoalEvent("compact"))
				if terminalTestAlert(m) != "" {
					t.Fatal("policy change replayed a consumed terminal failure")
				}
			})
		}
	}
	for _, change := range []string{"focus", "disable", "new run"} {
		t.Run("queued/"+change, func(t *testing.T) {
			m := modelPickerTestModel(t, 80, 24)
			m.Update(tea.BlurMsg{})
			terminalGoalFailurePhase(m, "compact")
			m.handleAgentEvent(terminalBlockedGoalEvent("compact"))
			queued := terminalAlertMsg(m.terminal.alertID)
			switch change {
			case "focus":
				m.Update(tea.FocusMsg{})
			case "disable":
				m.app.Cfg.TUI.Notifications = "off"
			case "new run":
				m.beginOptimisticRun()
			}
			if terminalStatusFilter(m, queued) != nil {
				t.Fatal("stale queued alert escaped current policy or run generation")
			}
		})
	}
}

func TestTerminalGoalAutomaticSuccessAndCancellationStayQuiet(t *testing.T) {
	for _, boundary := range []string{"success", "aborted", "requested abort"} {
		t.Run(boundary, func(t *testing.T) {
			m := modelPickerTestModel(t, 80, 24)
			m.Update(tea.BlurMsg{})
			if boundary == "success" {
				m.beginOptimisticRun()
				m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionStarted, TurnID: "compact"})
				m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "compact", Compaction: &protocol.CompactionResult{Automatic: true}})
				if !m.busy || terminalTestAlert(m) != "" {
					t.Fatal("automatic success announced or unlocked intermediate phase")
				}
			} else {
				terminalGoalFailurePhase(m, "compact")
				if boundary == "aborted" {
					m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvAborted, TurnID: "compact"})
				} else {
					m.requestAbort()
				}
			}
			ev := terminalBlockedGoalEvent("compact")
			for range 2 {
				m.handleAgentEvent(ev)
				if terminalTestAlert(m) != "" || m.busy {
					t.Fatal("goal stop announced an intermediate success or cancellation")
				}
			}
		})
	}
}

// Distinguish the ordinary goal request from the summarizer while retaining a
// real provider stream and the core's automatic boundary/error/goal ordering.
type terminalGoalSummaryFailureProvider struct {
	*fake.Provider
	summaryCalls atomic.Int64
}

func (p *terminalGoalSummaryFailureProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if strings.Contains(req.System, "working-state checkpoint") {
		p.summaryCalls.Add(1)
		return fake.New([]fake.Step{{Kind: fake.StepError, Err: errors.New("private summary outage")}}).Chat(ctx, req)
	}
	return p.Provider.Chat(ctx, req)
}

func TestTerminalGoalFailureFromAutomaticSummarizerStream(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	oldAgent := m.app.Agent
	t.Cleanup(func() { oldAgent.Close() })
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "goal-alert.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.app.Session = store
	controller, err := goalpkg.New(m.app.Session, testHome(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	for _, tool := range goalpkg.Tools(controller) {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	p := &terminalGoalSummaryFailureProvider{Provider: fake.New([]fake.Step{
		{Kind: fake.StepText, Text: "goal progress"},
		{Kind: fake.StepUsage, Usage: &protocol.Usage{Input: 90000, Output: 1, Total: 90001}},
		{Kind: fake.StepDone, Stop: protocol.StopStop},
	})}
	runtime, err := agent.New(agent.Options{
		Provider: p, Registry: registry, Session: m.app.Session, Permission: m.app.Perm,
		Model: protocol.Model{Provider: "fake", ID: "goal-alert", ContextWindow: 100000, SupportsTools: true}, Goal: controller,
		Compaction: agent.CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 128, Fallback: "error", AutoThresholdPercent: 90},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app.Agent, m.app.Goal = runtime, controller
	controller.SetEmitter(runtime.Publish)
	for i := range 6 {
		user := protocol.NewUserMessage(fmt.Sprintf("old-user-%d", i), m.app.Session.BranchTip(), "older goal work")
		assistant := protocol.NewAssistantMessage(fmt.Sprintf("old-assistant-%d", i), user.ID, "fake", "goal-alert", []protocol.ContentBlock{protocol.NewTextBlock("older goal progress")}, protocol.StopStop, nil)
		for _, message := range []protocol.Message{user, assistant} {
			if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := controller.Create("continue until finished", nil, false); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.BlurMsg{})
	events := make(chan protocol.AgentEvent, 128)
	unsubscribe := runtime.Subscribe(func(ev protocol.AgentEvent) { events <- ev })
	defer unsubscribe()
	runtime.ContinueGoal()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	var compactFailed, diagnosed, blocked bool
	for !blocked {
		select {
		case ev := <-events:
			m.Update(agentEventMsg{ev: ev})
			if ev.Type == protocol.EvCompactionDone {
				compactFailed = ev.IsError && ev.Compaction != nil && ev.Compaction.Automatic
			}
			if ev.Type == protocol.EvError {
				diagnosed = true
			}
			blocked = ev.Type == protocol.EvThreadGoalUpdated && ev.ThreadGoal != nil && ev.ThreadGoal.Goal != nil && ev.ThreadGoal.Goal.Status == protocol.GoalBlocked
			if !blocked && terminalTestAlert(m) != "" {
				t.Fatalf("event %s announced before terminal goal boundary", ev.Type)
			}
		case <-deadline.C:
			t.Fatal("automatic goal worker did not reach failed compaction boundary")
		}
	}
	if !compactFailed || !diagnosed || p.summaryCalls.Load() != 1 || p.CallCount() != 1 {
		t.Fatalf("wrong core path: compactFailed=%v diagnosed=%v summaries=%d goalCalls=%d", compactFailed, diagnosed, p.summaryCalls.Load(), p.CallCount())
	}
	if runtime.IsRunning() || m.busy || m.terminalActivity() != terminalFailed {
		t.Fatalf("failed automatic worker not settled: coreRunning=%v busy=%v activity=%v", runtime.IsRunning(), m.busy, m.terminalActivity())
	}
	if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow encountered an error") {
		t.Fatalf("real automatic failure notification=%q", got)
	}
	m.handleAgentEvent(terminalBlockedGoalEvent(""))
	if terminalTestAlert(m) != "" {
		t.Fatal("real goal failure announced twice")
	}
}

func TestTerminalGoalSnapshotDoesNotReannounceManualCompactionFailure(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	m.Update(tea.BlurMsg{})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvCompactionDone, TurnID: "manual", IsError: true})
	if terminalTestAlert(m) == "" {
		t.Fatal("manual failure fixture did not alert")
	}
	m.handleAgentEvent(terminalBlockedGoalEvent(""))
	if terminalTestAlert(m) != "" {
		t.Fatal("goal metadata reannounced an already settled manual failure")
	}
}

func TestTerminalGoalFailureWaitsForAdmittedCoreTurn(t *testing.T) {
	m := modelPickerTestModel(t, 80, 24)
	oldAgent := m.app.Agent
	t.Cleanup(func() { oldAgent.Close() })
	p := newBoundaryGoalProvider()
	runtime, err := agent.New(agent.Options{
		Provider: p, Registry: m.app.Registry, Session: m.app.Session, Permission: m.app.Perm,
		Model: protocol.Model{Provider: p.ID(), ID: "goal-alert", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.app.Agent = runtime
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Prompt(ctx, "hold this admitted turn") }()
	call := waitBoundaryCall(t, p, 0)
	_, id, running := runtime.ActiveTurn()
	if !running {
		t.Fatal("fixture did not admit a core turn")
	}
	m.Update(tea.BlurMsg{})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, TurnID: id, Message: "private provider failure"})
	m.handleAgentEvent(terminalBlockedGoalEvent(id))
	if !m.busy || terminalTestAlert(m) != "" {
		t.Fatal("blocked metadata prematurely settled a running core turn")
	}
	p.release(call)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("core turn did not finish")
	}
	// Normal failed goal turns have turn_done; unlike the inter-turn automatic
	// compaction failure, the earlier blocked snapshot is not their boundary.
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: id})
	if got := terminalTestAlert(m); got != "\a"+ansi.Notify("Snow encountered an error") {
		t.Fatalf("turn terminal boundary lost pending error: %q", got)
	}
	m.handleAgentEvent(terminalBlockedGoalEvent(id))
	if terminalTestAlert(m) != "" {
		t.Fatal("late goal snapshot duplicated the turn's failure alert")
	}
}
