package web

import (
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageRegenerateSavedFirstMiddleLatestSameChat(t *testing.T) {
	for selected := range 3 {
		t.Run([]string{"first", "middle", "latest"}[selected], func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "regenerate-normal")
			p := projects[0]
			before, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			replies := []RuntimeMessage{}
			for _, row := range before.Messages {
				if row.CanRegenerate {
					replies = append(replies, row)
				}
			}
			if len(replies) != 3 {
				t.Fatalf("saved terminal eligibility: %+v", before.Messages)
			}
			_, sub, err := m.Subscribe(p.ID, before.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			prepared, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, replies[selected].ID)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(prepared)
			if strings.Contains(string(encoded), "text") || strings.Contains(string(encoded), "source") || prepared.ProjectID != p.ID || prepared.SessionID != before.SessionID || prepared.InstanceID != before.InstanceID || prepared.MessageID != replies[selected].ID {
				t.Fatalf("public preparation: %s", encoded)
			}
			unchanged, _ := m.Snapshot(p.ID)
			if !slices.EqualFunc(before.Messages, unchanged.Messages, func(a, b RuntimeMessage) bool {
				return a.ID == b.ID && a.Text == b.Text && a.CanRegenerate == b.CanRegenerate
			}) {
				t.Fatal("prepare changed source")
			}
			after, err := m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, prepared.EditToken)
			if err != nil {
				t.Fatal(err)
			}
			if after.InstanceID == before.InstanceID || after.SessionID != before.SessionID || after.SessionName != before.SessionName || after.Status != "idle" {
				t.Fatalf("same chat branch: %+v", after)
			}
			if _, valid := sub.Snapshot(); valid {
				t.Fatal("old stream retained authority")
			}
			count := 0
			for _, row := range after.Messages {
				if row.Role == "user" {
					count++
				}
				if row.SourceID == replies[selected].SourceID {
					t.Fatal("selected old reply remains")
				}
			}
			if count != selected+1 || len(after.Messages) != 2*(selected+1) || after.Messages[len(after.Messages)-2].Text != "identical" || after.Messages[len(after.Messages)-1].Text != "regenerated" || !after.Messages[len(after.Messages)-1].CanRegenerate {
				t.Fatalf("replacement did not rerun one exact original: %+v", after.Messages)
			}
			if _, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, replies[selected].ID); err == nil {
				t.Fatal("old tab can prepare against new branch")
			}
			commands, _ := os.ReadFile(log)
			if strings.Count(string(commands), "session_create") != 1 || strings.Contains(string(commands), "session_open") || !strings.Contains(string(commands), "source:"+replies[selected].SourceID+":") {
				t.Fatalf("wrong identity/worker operation: %s", commands)
			}
		})
	}
}

func TestMessageRegenerateLiveFinalOnlyAfterCompletion(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "regenerate-hold-live")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "identical"); err != nil {
		t.Fatal(err)
	}
	running := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool {
		return s.Status == "running" && len(s.Messages) > 0 && s.Messages[len(s.Messages)-1].Text == "live answer"
	})
	for _, row := range running.Messages[len(before.Messages):] {
		if row.CanRegenerate {
			t.Fatal("streaming text prematurely eligible")
		}
		if row.Role == "assistant" && row.SourceTurnID != "live-root-turn" {
			t.Fatalf("assistant creation lacks exact root identity: %+v", row)
		}
	}
	if _, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, running.Messages[len(running.Messages)-1].ID); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("active turn prepared: %v", err)
	}
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	completed := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	eligible := 0
	for _, row := range completed.Messages[len(before.Messages):] {
		if row.CanRegenerate {
			eligible++
		}
	}
	reply := completed.Messages[len(completed.Messages)-1]
	if eligible != 1 || !reply.CanRegenerate || reply.SourceID != "" || reply.SourceTurnID != "live-root-turn" {
		t.Fatalf("tool-preface/final mapping: %+v", completed.Messages)
	}
	for _, row := range completed.Messages[len(before.Messages) : len(completed.Messages)-1] {
		if row.Role == "assistant" {
			if _, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, row.ID); err == nil {
				t.Fatal("intermediate tool preface prepared")
			}
		}
	}
	prepared, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, reply.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, prepared.EditToken)
	if err != nil {
		t.Fatal(err)
	}
	if after.Messages[len(after.Messages)-1].Text != "regenerated" || len(after.Activities) != 0 {
		t.Fatalf("old live tool suffix retained: %+v", after)
	}
	commands, _ := os.ReadFile(log)
	if !strings.Contains(string(commands), "source::live-root-turn") || strings.Contains(string(commands), "source:"+reply.ID) {
		t.Fatalf("local ID confused with durable reply: %s", commands)
	}
}

func TestMessageRegenerateActionTokensAndStalePreparations(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "regenerate-normal")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	edit, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, before.Messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, edit.EditToken); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("edit token crossed action: %v", err)
	}
	first, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, before.Messages[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, first.EditToken, "browser replacement"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("regenerate token crossed action: %v", err)
	}
	second, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, before.Messages[3].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, first.EditToken); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale preparation accepted: %v", err)
	}
	if _, err := m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, second.EditToken); err != nil {
		t.Fatal(err)
	}
}

func TestMessageRegenerateFailureSharesEditOutcomeRules(t *testing.T) {
	for _, mode := range []string{"regenerate-reject", "regenerate-ambiguous", "regenerate-exit", "regenerate-fail"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, mode)
			p := projects[0]
			before, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := m.PrepareMessageRegenerate(t.Context(), p.ID, before.InstanceID, before.Messages[1].ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.CommitMessageRegenerate(t.Context(), p.ID, before.InstanceID, prepared.EditToken)
			after, _ := m.Snapshot(p.ID)
			switch mode {
			case "regenerate-reject":
				if !errors.Is(err, ErrRuntimeInvalid) || after.Status != "idle" || after.InstanceID != before.InstanceID || len(after.Messages) != len(before.Messages) {
					t.Fatalf("precommit rejection: %+v %v", after, err)
				}
			case "regenerate-fail":
				if err != nil || after.Status != "idle" || len(after.Messages) != 2 || after.Messages[0].Text != "identical" || after.Messages[1].CanRegenerate || after.Recovery.State != RecoveryFailed {
					t.Fatalf("postcommit failure: %+v %v", after, err)
				}
			default:
				if !errors.Is(err, ErrRuntimeUnavailable) || after.Status != "failed" {
					t.Fatalf("ambiguous commit resumed controls: %+v %v", after, err)
				}
			}
			if _, err := m.CommitMessageRegenerate(t.Context(), p.ID, after.InstanceID, prepared.EditToken); err == nil {
				t.Fatal("consumed preparation retried")
			}
		})
	}
}

func TestMessageRegenerateHistoryEligibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message protocol.Message
		want    bool
	}{
		{name: "terminal", message: protocol.Message{StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "reply"}}}, want: true},
		{name: "length", message: protocol.Message{StopReason: protocol.StopLength, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "reply"}}}, want: true},
		{name: "tool-preface", message: protocol.Message{StopReason: protocol.StopToolUse, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "preface"}}}},
		{name: "unknown-terminal", message: protocol.Message{Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "reply"}}}},
		{name: "plan", message: protocol.Message{StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "preface"}, {Type: protocol.BlockPlan, Text: "plan"}, {Type: protocol.BlockText, Text: "after plan"}}}},
		{name: "private-only", message: protocol.Message{StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "PRIVATE"}}}},
		{name: "private-with-public", message: protocol.Message{StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "PRIVATE"}, {Type: protocol.BlockText, Text: "reply"}}}, want: true},
		{name: "truncated", message: protocol.Message{StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: strings.Repeat("x", runtimeMessageBytes+1)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.message.ID = "saved-assistant"
			tc.message.Role = protocol.RoleAssistant
			r := &liveRuntime{assistant: -1, plan: -1}
			r.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{tc.message}})
			eligible := 0
			for _, row := range r.snapshot.Messages {
				if row.CanRegenerate {
					eligible++
					if row.Role != "assistant" || row.SourceID != "saved-assistant" || row.SourceTurnID != "" {
						t.Fatal("wrong history source")
					}
				}
			}
			if (eligible == 1) != tc.want || eligible > 1 {
				t.Fatalf("eligible=%d rows=%+v", eligible, r.snapshot.Messages)
			}
			encoded, _ := json.Marshal(r.snapshot)
			if strings.Contains(string(encoded), "PRIVATE") {
				t.Fatal("private history leak")
			}
		})
	}
}

func TestMessageRegenerateFailureAndRetiredCompletionNeverPromoteLiveReply(t *testing.T) {
	for _, status := range []protocol.RPCPromptStatus{protocol.RPCPromptFailedStatus, protocol.RPCPromptCanceledStatus} {
		r := &liveRuntime{ctx: t.Context(), cancel: func() {}, busy: true, assistant: 0, turnID: "root", promptID: "8", snapshot: RuntimeSnapshot{Status: "running", Messages: []RuntimeMessage{{ID: "row", Role: "assistant", SourceTurnID: "root", Text: "partial"}}}}
		r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "8", Status: status}})
		if r.snapshot.Messages[0].CanRegenerate {
			t.Fatal("failed/canceled partial became eligible")
		}
	}
	r := &liveRuntime{ctx: t.Context(), cancel: func() {}, busy: true, assistant: 0, turnID: "new", promptID: "8", messageEdit: runtimeMessageEditState{committedTurn: "new"}, snapshot: RuntimeSnapshot{Status: "running", Messages: []RuntimeMessage{{Role: "assistant", SourceTurnID: "new", Text: "reply"}}}}
	r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "7", Status: protocol.RPCPromptCompletedStatus}})
	if r.snapshot.Messages[0].CanRegenerate || !r.busy {
		t.Fatal("retired completion promoted new reply")
	}
}

func TestMessageRegenerateLivePlanBearingReplyStaysIneligibleAcrossRows(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		for _, boundary := range []string{"none", "new-tool-step", "duplicate-old-tool-step"} {
			t.Run(boundary+map[bool]string{true: "/buffered", false: "/live"}[buffered], func(t *testing.T) {
				root := func(e protocol.AgentEvent) clientrpc.Event {
					e.TurnID = "root"
					e.RootEpoch = 1
					e.TurnSequence = 1
					return clientrpc.Event{AgentEvent: &e}
				}
				tool := func(typ protocol.AgentEventType, id string) clientrpc.Event {
					return root(protocol.AgentEvent{Type: typ, ToolCallID: id, ToolName: "read"})
				}
				events := []clientrpc.Event{}
				if boundary == "duplicate-old-tool-step" {
					events = append(events, tool(protocol.EvToolStart, "old-call"), tool(protocol.EvToolEnd, "old-call"))
				}
				events = append(events, root(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Intro"}), root(protocol.AgentEvent{Type: protocol.EvPlanStarted, Plan: &protocol.PlanItem{ID: "plan"}}), root(protocol.AgentEvent{Type: protocol.EvPlanDelta, Plan: &protocol.PlanItem{ID: "plan"}, Text: "Plan"}), root(protocol.AgentEvent{Type: protocol.EvPlanCompleted, Plan: &protocol.PlanItem{ID: "plan", Text: "Plan"}}), root(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Outro"}))
				switch boundary {
				case "new-tool-step":
					events = append(events, tool(protocol.EvToolStart, "new-call"), tool(protocol.EvToolEnd, "new-call"), root(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "Final plain reply"}))
				case "duplicate-old-tool-step":
					events = append(events, tool(protocol.EvToolStart, "old-call"), tool(protocol.EvToolEnd, "old-call"))
				}
				events = append(events, clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
				r := &liveRuntime{ctx: t.Context(), cancel: func() {}, busy: true, assistant: -1, plan: -1, promptID: "42", snapshot: RuntimeSnapshot{Status: "running"}}
				if buffered {
					r.messageEdit.pending = true
					for _, event := range events {
						if stored, valid := r.bufferMessageEditEventLocked(event); !stored || !valid {
							t.Fatal("buffer rejected public plan lifecycle")
						}
					}
					c := protocol.RPCMessageEditCommitted{TurnID: "root", History: protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "original-user-copy", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "original user"}}}}}}
					if _, err := r.publishMessageEdit("42", c); err != nil {
						t.Fatal(err)
					}
				} else {
					for _, event := range events {
						r.consumeEvent(event)
					}
				}
				eligible := 0
				for _, message := range r.snapshot.Messages {
					if message.CanRegenerate {
						eligible++
						if message.Text != "Final plain reply" {
							t.Fatalf("plan-bearing response advertised: %+v", message)
						}
					}
				}
				want := 0
				if boundary == "new-tool-step" {
					want = 1
				}
				if eligible != want || r.snapshot.Status != "idle" {
					t.Fatalf("eligibility=%d want=%d snapshot=%+v", eligible, want, r.snapshot)
				}
			})
		}
	}
}

func TestMessageRegenerateEarlyCompletionWaitsForCorrelatedAdmission(t *testing.T) {
	for _, ack := range []string{"42", "43"} {
		t.Run(ack, func(t *testing.T) {
			r := &liveRuntime{ctx: t.Context(), cancel: func() {}, busy: true, assistant: 0, turnID: "root", snapshot: RuntimeSnapshot{Status: "running", Recovery: RecoveryHint{State: RecoveryAdmissionUnknown}, Messages: []RuntimeMessage{{ID: "exact-reply", Role: "assistant", SourceTurnID: "root", Text: "reply"}}}}
			r.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
			if r.snapshot.Messages[0].CanRegenerate || r.pendingRegenerateReplyID != "exact-reply" {
				t.Fatal("uncorrelated completion exposed reply authority")
			}
			err := r.promptAcknowledged(protocol.RPCResponse{ID: ack, Success: true}, nil, false)
			if ack == "42" {
				if err != nil || !r.snapshot.Messages[0].CanRegenerate {
					t.Fatalf("correlated completion lost: %v %+v", err, r.snapshot)
				}
			} else if !errors.Is(err, ErrRuntimeUnavailable) || r.snapshot.Messages[0].CanRegenerate {
				t.Fatalf("uncorrelated admission promoted reply: %v %+v", err, r.snapshot)
			}
		})
	}
}

func TestMessageRegenerateWhitespaceOnlyReplyNeverEligible(t *testing.T) {
	for _, text := range []string{"", " \t\n", "\u00a0\u2003\n"} {
		t.Run(text, func(t *testing.T) {
			saved := &liveRuntime{assistant: -1, plan: -1}
			saved.projectHistory(protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "saved-reply", Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: text}}}}})
			for _, row := range saved.snapshot.Messages {
				if row.CanRegenerate {
					t.Fatalf("blank saved reply eligible: %+v", row)
				}
			}
			for _, early := range []bool{false, true} {
				live := &liveRuntime{ctx: t.Context(), cancel: func() {}, busy: true, assistant: -1, plan: -1, promptID: "42", snapshot: RuntimeSnapshot{Status: "running"}}
				if early {
					live.promptID = ""
				}
				live.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "root", RootEpoch: 1, TurnSequence: 1, Text: text}})
				live.consumeEvent(clientrpc.Event{PromptCompleted: &protocol.RPCPromptCompleted{RequestID: "42", Status: protocol.RPCPromptCompletedStatus}})
				if live.pendingRegenerateReplyID != "" {
					t.Fatal("blank reply staged for admission eligibility")
				}
				if early {
					if err := live.promptAcknowledged(protocol.RPCResponse{ID: "42", Success: true}, nil, false); err != nil {
						t.Fatal(err)
					}
				}
				for _, row := range live.snapshot.Messages {
					if row.CanRegenerate {
						t.Fatalf("blank live reply eligible: %+v", row)
					}
				}
			}
		})
	}
}
