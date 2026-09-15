package web

import (
	"encoding/json/v2"
	"strings"
	"testing"

	clientrpc "github.com/elmissouri16/snow-core/pkg/agentclient/rpc"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestGoalACKOrderNeverRetainsPrivateToolOrProviderPayloads(t *testing.T) {
	const publicOutput = "explicit public tool output"
	private := []string{"SECRET-RAW-ARGUMENTS", "SECRET-PROVIDER-CONTINUITY", "SECRET-THINKING", "SECRET-PRIVATE-PREVIEW", "SECRET-NIL-PROVENANCE-RESULT", "SECRET-PLUGIN-VIEW", "SECRET-PROGRESS", "SECRET-ERROR"}
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "ACK-first", true: "events-first"}[early], func(t *testing.T) {
			r := goalProjectionRuntime(t)
			start := goalProjectionEvent(protocol.EvToolStart, "run", "turn", 1, "SECRET-THINKING")
			start.AgentEvent.ToolCallID, start.AgentEvent.ToolName = "private-result", "read"
			// Message can carry raw tool arguments; neither it nor the legacy preview
			// carries explicit-public-text provenance.
			start.AgentEvent.Message = `{"arguments":{"credential":"SECRET-RAW-ARGUMENTS"}}`
			start.AgentEvent.ToolOutput = "SECRET-PRIVATE-PREVIEW"
			start.AgentEvent.PluginView = &protocol.PluginNode{Type: "text", Text: "SECRET-PLUGIN-VIEW"}
			end := clientrpc.Event{AgentEvent: new(start.AgentEvent.Clone())}
			end.AgentEvent.Type = protocol.EvToolEnd
			end.AgentEvent.Text = "SECRET-NIL-PROVENANCE-RESULT"
			end.AgentEvent.ToolResult = nil
			end.AgentEvent.ToolOutput = "SECRET-PRIVATE-PREVIEW SECRET-NIL-PROVENANCE-RESULT"
			progress := goalProjectionEvent(protocol.EvToolProgress, "run", "turn", 1, "SECRET-PROGRESS")
			progress.AgentEvent.ToolProgress = &protocol.ToolProgress{ToolCallID: "private-result", Name: "read", Message: "SECRET-PROGRESS"}
			thinking := goalProjectionEvent(protocol.EvThinkingDelta, "run", "turn", 1, "SECRET-THINKING")
			// Core intentionally exposes no provider-private continuity field in
			// AgentEvent. Future/unknown typed events still must not create a fallback
			// Text projection or survive the public allowlist.
			continuity := goalProjectionEvent(protocol.AgentEventType("provider_private_continuity"), "run", "turn", 1, "SECRET-PROVIDER-CONTINUITY")
			continuity.AgentEvent.Message = `{"provider_data":"SECRET-PROVIDER-CONTINUITY"}`
			publicStart := clientrpc.Event{AgentEvent: new(start.AgentEvent.Clone())}
			publicStart.AgentEvent.ToolCallID = "public-result"
			publicEnd := clientrpc.Event{AgentEvent: new(end.AgentEvent.Clone())}
			publicEnd.AgentEvent.ToolCallID = "public-result"
			publicEnd.AgentEvent.ToolResult = &protocol.ToolResultPreview{Text: publicOutput}
			events := []clientrpc.Event{start, progress, thinking, continuity, end, publicStart, publicEnd, goalProjectionEvent(protocol.EvTextDelta, "run", "turn", 1, "visible answer"), goalProjectionCompletion("42", "run")}
			source, err := json.Marshal(events)
			if err != nil {
				t.Fatal(err)
			}
			for _, sentinel := range private {
				if !strings.Contains(string(source), sentinel) {
					t.Fatalf("fixture omitted %s", sentinel)
				}
			}
			assertPrivateAbsent := func(label string, value any) {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				for _, sentinel := range private {
					if strings.Contains(string(data), sentinel) {
						t.Fatalf("%s retained %s", label, sentinel)
					}
				}
			}
			if !early {
				goalProjectionACK(t, r)
			}
			for _, event := range events {
				// Exercise the same buffer-or-project boundary as the live event drain,
				// inspecting retained state after every event, not merely after completion.
				r.eventMu.Lock()
				r.mu.Lock()
				buffered, valid := r.bufferGoalEventLocked(event)
				r.mu.Unlock()
				if !valid {
					r.eventMu.Unlock()
					t.Fatal("public allowlist rejected fixture")
				}
				if !buffered {
					r.consumeEvent(event)
				}
				r.eventMu.Unlock()
				assertPrivateAbsent("retained preACK buffer", r.goal.events)
				assertPrivateAbsent("intermediate snapshot", r.snapshot)
			}
			if early {
				if len(r.snapshot.Messages) != 0 || len(r.snapshot.Activities) != 0 {
					t.Fatal("private/public events projected before ACK")
				}
				retained, err := json.Marshal(r.goal.events)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(retained), publicOutput) {
					t.Fatal("allowlist dropped explicit public output")
				}
				ack := goalProjectionACK(t, r)
				assertPrivateAbsent("returned ACK snapshot", ack)
			}
			assertPrivateAbsent("retired buffer", r.goal.events)
			assertPrivateAbsent("completed snapshot", r.snapshot)
			if len(r.goal.events) != 0 || r.busy || r.snapshot.Status != "idle" {
				t.Fatal("correlated completion did not retire private-buffer lifecycle")
			}
			if len(r.snapshot.Activities) != 2 || r.snapshot.Activities[0].Output != "" || r.snapshot.Activities[1].Output != publicOutput {
				t.Fatalf("nil provenance fell back to private result or public output was lost: %+v", r.snapshot.Activities)
			}
		})
	}
}
