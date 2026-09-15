package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestMessageEditFirstMiddleLatestSameChat(t *testing.T) {
	for selected := range 3 {
		t.Run([]string{"first", "middle", "latest"}[selected], func(t *testing.T) {
			m, projects, log := runtimeTestManager(t, "edit-normal")
			project := projects[0]
			before, err := m.Open(t.Context(), project, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			users := []RuntimeMessage{}
			for _, message := range before.Messages {
				if message.Role == "user" {
					users = append(users, message)
				}
			}
			source := users[selected]
			_, sub, err := m.Subscribe(project.ID, before.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			prepared, err := m.PrepareMessageEdit(t.Context(), project.ID, before.InstanceID, source.ID)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, _ := m.Snapshot(project.ID)
			if !slices.EqualFunc(before.Messages, unchanged.Messages, func(a, b RuntimeMessage) bool { return a.ID == b.ID && a.Text == b.Text }) || before.InstanceID != unchanged.InstanceID {
				t.Fatal("prepare mutated visible source")
			}
			if prepared.Text != "identical" || prepared.MessageID != source.ID {
				t.Fatalf("prepare: %+v", prepared)
			}
			after, err := m.CommitMessageEdit(t.Context(), project.ID, before.InstanceID, prepared.EditToken, "replacement")
			if err != nil {
				t.Fatal(err)
			}
			if after.InstanceID == before.InstanceID || after.SessionID != before.SessionID || after.SessionName != before.SessionName || after.Status != "idle" {
				t.Fatalf("same-chat commit: %+v", after)
			}
			if _, valid := sub.Snapshot(); valid {
				t.Fatal("old SSE binding survived")
			}
			texts, userCount := []string{}, 0
			for _, message := range after.Messages {
				texts = append(texts, message.Text)
				if message.Role == "user" {
					userCount++
				}
			}
			if userCount != selected+1 || texts[len(texts)-2] != "replacement" || texts[len(texts)-1] != "regenerated" || len(after.Activities) != 0 {
				t.Fatalf("projection: %+v", after.Messages)
			}
			for _, message := range after.Messages {
				if message.SourceID == source.SourceID || message.Text == "plan "+string(rune('0'+selected)) {
					t.Fatal("edited suffix retained")
				}
			}
			if _, err := m.CommitMessageEdit(t.Context(), project.ID, before.InstanceID, prepared.EditToken, "again"); err == nil {
				t.Fatal("old instance retained mutation authority")
			}
			commands, _ := os.ReadFile(log)
			if strings.Count(string(commands), "session_create") != 1 || strings.Contains(string(commands), "session_open") || !strings.Contains(string(commands), "source:"+source.SourceID+":") {
				t.Fatalf("worker operations: %s", commands)
			}
			encoded, _ := json.Marshal(after)
			if strings.Contains(string(encoded), "PRIVATE") || strings.Contains(string(encoded), "STALE") || strings.Contains(string(encoded), "source_turn_id") {
				t.Fatalf("nonpublic projection: %s", encoded)
			}
		})
	}
}

func TestMessageEditLiveUserOwnsOnlyExactAcceptedTurn(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "edit-normal")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "identical"); err != nil {
		t.Fatal(err)
	}
	live := runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "idle" })
	source := live.Messages[len(live.Messages)-2]
	if source.SourceID != "" || source.SourceTurnID != "live-root-turn" || !source.CanEdit {
		t.Fatalf("live identity: %+v", source)
	}
	for _, message := range live.Messages[:len(live.Messages)-2] {
		if message.SourceTurnID != "" {
			t.Fatal("identical prior user associated with new root")
		}
	}
	if _, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, source.ID); err != nil {
		t.Fatal(err)
	}
	commands, _ := os.ReadFile(log)
	if !strings.Contains(string(commands), "source::live-root-turn") || strings.Contains(string(commands), "source:"+source.ID) {
		t.Fatalf("local ID used as durable identity: %s", commands)
	}
}

func TestMessageEditRejectsStalePreparationAndInvalidText(t *testing.T) {
	m, projects, _ := runtimeTestManager(t, "edit-normal")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	id := before.Messages[0].ID
	first, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, first.EditToken, "replacement"); !errors.Is(err, ErrRuntimeInvalid) {
		t.Fatalf("stale prep: %v", err)
	}
	for _, text := range []string{"", " \t\n", "\xff", "a\x00b", strings.Repeat("a", (64<<10)+1)} {
		if _, err := m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, second.EditToken, text); !errors.Is(err, ErrRuntimeInvalid) {
			t.Fatalf("invalid text admitted: %v", err)
		}
	}
	after, err := m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, second.EditToken, strings.Repeat("é", 32<<10))
	if err != nil || after.InstanceID == before.InstanceID {
		t.Fatalf("64KiB valid boundary: %v", err)
	}
}

func TestMessageEditFailureEvidenceAndNoRetry(t *testing.T) {
	for _, mode := range []string{"edit-reject", "edit-exit", "edit-ambiguous", "edit-fail"} {
		t.Run(mode, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, mode)
			p := projects[0]
			before, err := m.Open(t.Context(), p, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, before.Messages[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.CommitMessageEdit(t.Context(), p.ID, before.InstanceID, prepared.EditToken, "changed")
			after, _ := m.Snapshot(p.ID)
			switch mode {
			case "edit-reject":
				if !errors.Is(err, ErrRuntimeInvalid) || after.Status != "idle" || len(after.Messages) != len(before.Messages) {
					t.Fatalf("rejection changed branch: %+v %v", after, err)
				}
			case "edit-exit", "edit-ambiguous":
				if !errors.Is(err, ErrRuntimeUnavailable) || after.Status != "failed" {
					t.Fatalf("unknown admission reopened: %+v %v", after, err)
				}
				if err := m.Prompt(t.Context(), p.ID, after.InstanceID, "retry"); err == nil {
					t.Fatal("ambiguous state permitted mutation")
				}
			case "edit-fail":
				if err != nil || after.Status != "idle" || len(after.Messages) != 2 || after.Messages[0].Text != "changed" || after.Error == "" || after.Recovery.State != RecoveryFailed {
					t.Fatalf("provider failure restored original: %+v %v", after, err)
				}
			}
			if _, err := m.CommitMessageEdit(t.Context(), p.ID, after.InstanceID, prepared.EditToken, "retry"); err == nil {
				t.Fatal("consumed token retried")
			}
		})
	}
}

func TestMessageEditBrowserCancellationCannotUndoAdmittedCommit(t *testing.T) {
	m, projects, log := runtimeTestManager(t, "edit-hold")
	p := projects[0]
	before, err := m.Open(t.Context(), p, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := m.PrepareMessageEdit(t.Context(), p.ID, before.InstanceID, before.Messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := m.CommitMessageEdit(ctx, p.ID, before.InstanceID, prepared.EditToken, "changed")
		done <- err
	}()
	runtimeWait(t, m, p.ID, func(s RuntimeSnapshot) bool { return s.Status == "switching" })
	cancel()
	if err := m.Prompt(t.Context(), p.ID, before.InstanceID, "overlap"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("pending commit admitted prompt: %v", err)
	}
	if err := m.CancelTurn(t.Context(), p.ID, before.InstanceID, before.CancelToken); err == nil {
		t.Fatal("retired cancel token reached replacement")
	}
	if err := os.WriteFile(log+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not finish")
	}
	after, _ := m.Snapshot(p.ID)
	if after.InstanceID == before.InstanceID || after.Messages[0].Text != "changed" {
		t.Fatal("browser cancellation reverted admitted commit")
	}
}

func TestMessageEditPublicBufferBoundsAndOldEventAuthority(t *testing.T) {
	r := &liveRuntime{messageEdit: runtimeMessageEditState{pending: true}}
	e := clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "replacement", RootEpoch: 2, TurnSequence: 1, Text: "answer", Message: "PRIVATE"}}
	for range messageEditEventCount {
		if consumed, valid := r.bufferMessageEditEventLocked(e); !consumed || !valid {
			t.Fatal("bounded event refused")
		}
	}
	if _, valid := r.bufferMessageEditEventLocked(e); valid {
		t.Fatal("event-count overflow accepted")
	}
	data, _ := json.Marshal(r.messageEdit.events)
	if strings.Contains(string(data), "PRIVATE") {
		t.Fatal("buffer retained private payload")
	}
	r.messageEdit.events = nil
	r.messageEdit.bytes = messageEditEventBytes
	if _, valid := r.bufferMessageEditEventLocked(e); valid {
		t.Fatal("byte overflow accepted")
	}
	r.messageEdit = runtimeMessageEditState{committedTurn: "replacement"}
	r.busy, r.rootEpoch, r.turnID = true, 2, "replacement"
	if r.acceptEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, TurnID: "old-root", RootEpoch: 99, TurnSequence: 99}) || r.rootEpoch != 2 {
		t.Fatal("old root changed authority")
	}
	if !r.acceptEvent(*e.AgentEvent) {
		t.Fatal("correct replacement rejected")
	}
}

func TestMessageEditKnownCommitAfterLifetimeFailureStillReplacesHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &liveRuntime{ctx: ctx, cancel: cancel, instanceID: "old", snapshot: RuntimeSnapshot{InstanceID: "old", SessionID: "same", SessionName: "title", Status: "failed", Error: "connection failed", Messages: []RuntimeMessage{{Role: "user", Text: "old suffix"}}}}
	committed := protocol.RPCMessageEditCommitted{TurnID: "new-turn", History: protocol.RPCMessagesPage{Messages: []protocol.Message{{ID: "new-user", Role: protocol.RoleUser, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "replacement"}}}}}}
	if _, err := r.publishMessageEdit("5", committed); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("late lifetime failure: %v", err)
	}
	if r.snapshot.Status != "failed" || r.snapshot.Error != "connection failed" || r.snapshot.InstanceID == "old" || len(r.snapshot.Messages) != 1 || r.snapshot.Messages[0].Text != "replacement" || r.snapshot.SessionID != "same" || r.snapshot.SessionName != "title" {
		t.Fatalf("known committed branch restored or revived: %+v", r.snapshot)
	}
}

func TestMessageEditOptimisticBindingRejectsChildOldAndUnknownSources(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &liveRuntime{ctx: ctx, cancel: cancel, busy: true, instanceID: "instance", pendingUserID: "optimistic", turnSequence: 4, rootEpoch: 2, assistant: -1, plan: -1, snapshot: RuntimeSnapshot{Status: "running", Messages: []RuntimeMessage{{ID: "prior", Role: "user", Text: "same"}, {ID: "optimistic", Role: "user", Text: "same"}}}}
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "child", RootEpoch: 2, TurnSequence: 5, TurnID: "child-turn", Agent: &protocol.AgentRef{ThreadID: "child", Path: "/root/child", Depth: 1, ParentPath: protocol.RootAgentPath}}})
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "old", RootEpoch: 1, TurnSequence: 100, TurnID: "old-turn"}})
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvThinkingDelta, RootEpoch: 2, TurnSequence: 4, TurnID: "completed-old-turn"}})
	if r.snapshot.Messages[0].SourceTurnID != "" || r.snapshot.Messages[1].SourceTurnID != "" {
		t.Fatal("non-authoritative turn bound optimistic user")
	}
	r.consumeEvent(clientrpc.Event{AgentEvent: &protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "answer", RootEpoch: 2, TurnSequence: 5, TurnID: "accepted-root"}})
	if r.snapshot.Messages[0].SourceTurnID != "" || r.snapshot.Messages[1].SourceTurnID != "accepted-root" || !r.snapshot.Messages[1].CanEdit {
		t.Fatalf("exact optimistic ownership: %+v", r.snapshot.Messages)
	}
	r.addMessage(RuntimeMessage{Role: "user", Text: "unbound"})
	r.addMessage(RuntimeMessage{Role: "user", SourceID: "saved", Text: strings.Repeat("x", runtimeMessageBytes+1)})
	if r.snapshot.Messages[len(r.snapshot.Messages)-1].CanEdit || r.snapshot.Messages[len(r.snapshot.Messages)-2].CanEdit {
		t.Fatal("truncated/unbound source advertises edit")
	}
}
