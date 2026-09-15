package web

import (
	"context"
	"encoding/json/v2"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/agentclient/process"
	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Exercise the real client decoder and runtime drain without a provider or
// replacing existing process-worker fixtures owned by other tests.
func runtimeActivityStream(t *testing.T) (*RuntimeManager, *liveRuntime, net.Conn) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	local, peer := net.Pipe()
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() {
		data, err := json.Marshal(protocol.NewRPCReady("activity-test"))
		if err == nil {
			_, err = peer.Write(append(data, '\n'))
		}
		ready <- err
	}()
	client, err := clientrpc.New(ctx, local, clientrpc.Options{})
	if err != nil {
		cancel()
		_ = peer.Close()
		t.Fatal(err)
	}
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	r := &liveRuntime{ctx: ctx, cancel: cancel, worker: &process.Worker{Client: client}, drained: make(chan struct{}), busy: true, promptID: "prompt", assistant: -1, snapshot: RuntimeSnapshot{ProjectID: "project", Status: "running"}}
	m := &RuntimeManager{workers: map[string]*liveRuntime{"project": r}}
	go r.drain()
	t.Cleanup(func() {
		cancel()
		_ = peer.Close()
		_ = client.Close()
		select {
		case <-r.drained:
		case <-time.After(5 * time.Second):
			t.Error("activity drain did not stop")
		}
	})
	return m, r, peer
}

func runtimeActivityEmit(t *testing.T, peer net.Conn, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeActivityRootStreamProjection(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		name := "legacy"
		var root *protocol.AgentRef
		if tagged {
			name = "root-metadata"
			root = &protocol.AgentRef{ThreadID: "root-thread", Path: protocol.RootAgentPath, Role: "root"}
		}
		t.Run(name, func(t *testing.T) {
			m, r, peer := runtimeActivityStream(t)
			start := protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "call", TurnID: "turn", ToolName: "read", Agent: root, Message: "SECRET-ARGS", Text: "SECRET-THINKING"}
			runtimeActivityEmit(t, peer, start)
			snapshot := runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return len(s.Activities) == 1 })
			if snapshot.Activities[0].Status != "running" {
				t.Fatalf("start not running: %+v", snapshot.Activities)
			}
			id := snapshot.Activities[0].ID
			for _, ref := range []*protocol.AgentRef{
				{ThreadID: "child", ParentThreadID: "root-thread", Path: "/root/child", ParentPath: protocol.RootAgentPath, Depth: 1},
				{},
				{ThreadID: "bad-depth", Path: protocol.RootAgentPath, Depth: 1},
				{ThreadID: "bad-parent", Path: protocol.RootAgentPath, ParentThreadID: "other"},
			} {
				bad := start
				bad.ToolCallID, bad.ToolName, bad.Agent = "child", "SECRET-NONROOT", ref
				runtimeActivityEmit(t, peer, bad)
				bad.Type, bad.ToolOutput = protocol.EvToolEnd, "SECRET-NONROOT-OUTPUT"
				runtimeActivityEmit(t, peer, bad)
			}
			runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvToolProgress, ToolCallID: "call", Agent: root, Text: "SECRET-PROGRESS", ToolOutput: "SECRET-PREVIEW"})
			runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvThinkingDelta, Agent: root, Text: "SECRET-THINKING"})
			runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvTurnDone, Agent: root})
			runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvTextDelta, Agent: root, Text: "barrier"})
			snapshot = runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return len(s.Messages) == 2 })
			if len(snapshot.Activities) != 1 || snapshot.Activities[0].Status != "running" || snapshot.Messages[0].HTML != "" || snapshot.Messages[0].Role != "tool_activity" || snapshot.Activities[0].MessageID != snapshot.Messages[0].ID || snapshot.Messages[1].Text != "barrier" {
				t.Fatalf("non-tool-end changed lifecycle: %+v", snapshot)
			}
			end := start
			end.Type, end.ToolOutput = protocol.EvToolEnd, "SECRET-PRIVATE-DETAIL-PREVIEW"
			end.ToolResult = &protocol.ToolResultPreview{Text: "public tool text"}
			runtimeActivityEmit(t, peer, end)
			snapshot = runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return s.Activities[0].Status == "completed" })
			if snapshot.Activities[0].ID != id || snapshot.Activities[0].Output != "public tool text" {
				t.Fatalf("bad completion projection: %+v", snapshot)
			}
			data, err := json.Marshal(snapshot)
			if err != nil || strings.Contains(string(data), "SECRET") {
				t.Fatalf("private event fields escaped: %s, %v", data, err)
			}
			runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: "prompt", Status: protocol.RPCPromptCompletedStatus})
			runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return s.Status == "idle" })
			start.ToolCallID = "idle-event"
			runtimeActivityEmit(t, peer, start)
			_ = peer.Close()
			<-r.drained
			final, _ := m.Snapshot("project")
			if len(final.Activities) != 1 || final.Activities[0].Status != "completed" {
				t.Fatal("idle event/disconnection changed completed tool")
			}
		})
	}
}

func TestRuntimeActivityPendingFinalization(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "canceled", "aborted-event", "disconnect", "connection-failure"} {
		t.Run(terminal, func(t *testing.T) {
			m, r, peer := runtimeActivityStream(t)
			runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "pending", ToolName: "bash"})
			started := runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return len(s.Activities) == 1 })
			switch terminal {
			case "disconnect":
				_ = peer.Close()
			case "connection-failure":
				r.fail()
			case "aborted-event":
				runtimeActivityEmit(t, peer, protocol.AgentEvent{Type: protocol.EvAborted})
			default:
				runtimeActivityEmit(t, peer, protocol.RPCPromptCompleted{Type: protocol.RPCTypePromptCompleted, RequestID: "prompt", Status: protocol.RPCPromptStatus(terminal)})
			}
			want := "canceled"
			if terminal == "disconnect" || terminal == "connection-failure" {
				want = "unknown"
			}
			snapshot := runtimeWait(t, m, "project", func(s RuntimeSnapshot) bool { return s.Activities[0].Status == want })
			if snapshot.Activities[0].IsError {
				t.Fatal("cancellation incorrectly became tool failure")
			}
			assertRuntimeTimeline(t, snapshot, []string{"tool_activity"}, []string{""})
			if snapshot.Activities[0].MessageID != started.Messages[0].ID || snapshot.Messages[0].ID != started.Messages[0].ID {
				t.Fatal("terminal event moved the pending tool group")
			}
		})
	}
}

func TestRuntimeActivityAbortAndCloseControls(t *testing.T) {
	for _, closeProject := range []bool{false, true} {
		name := "abort"
		if closeProject {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			m, projects, _ := runtimeTestManager(t, "")
			project := projects[0]
			snapshot, err := m.Open(t.Context(), project, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prompt(t.Context(), project.ID, snapshot.InstanceID, "hold"); err != nil {
				t.Fatal(err)
			}
			r, err := m.runtime(project.ID, snapshot.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			r.projectActivity(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "pending", ToolName: "bash"})
			r.mu.Unlock()
			if closeProject {
				err = m.CloseProject(t.Context(), project.ID, snapshot.InstanceID)
			} else {
				err = m.Abort(t.Context(), project.ID, snapshot.InstanceID)
			}
			if err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			activity := r.snapshot.Activities[0]
			r.mu.Unlock()
			want := "canceled"
			if closeProject {
				want = "unknown" // Closing is not proof of tool completion/cancellation.
			}
			if activity.Status != want {
				t.Fatalf("pending after %s: %+v", name, activity)
			}
		})
	}
}
