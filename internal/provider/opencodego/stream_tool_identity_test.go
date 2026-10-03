package opencodego

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestChatToolCallIdentityAndReplay(t *testing.T) {
	type callCase struct {
		firstID string
		laterID string
		wantID  string // Empty permits any nonempty, unique collision replacement.
		path    string
	}
	for _, tc := range []struct {
		name  string
		calls []callCase
	}{
		{name: "deferred", calls: []callCase{{laterID: "wire-call", wantID: "tc-0", path: "a.txt"}}},
		{name: "omitted", calls: []callCase{{wantID: "tc-0", path: "a.txt"}}},
		{name: "initial", calls: []callCase{{firstID: "wire-call", laterID: "wire-call", wantID: "wire-call", path: "a.txt"}}},
		{name: "wire-collides-with-fallback", calls: []callCase{
			{laterID: "late-wire", wantID: "tc-0", path: "a.txt"},
			{firstID: "tc-0", laterID: "tc-0", path: "b.txt"},
		}},
		{name: "fallback-collides-with-wire", calls: []callCase{
			{firstID: "tc-1", laterID: "tc-1", wantID: "tc-1", path: "a.txt"},
			{laterID: "late-wire", path: "b.txt"},
		}},
		{name: "collision-replacement-also-in-use", calls: []callCase{
			{firstID: "tc-2", wantID: "tc-2", path: "a.txt"},
			{firstID: "tc-2-1", wantID: "tc-2-1", path: "b.txt"},
			{path: "c.txt"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req openAIChatRequest
				if err := json.UnmarshalRead(r.Body, &req); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if len(req.Messages) > 0 {
					if len(req.Messages) != 1+len(tc.calls) || req.Messages[0].Role != "assistant" || len(req.Messages[0].ToolCalls) != len(tc.calls) {
						t.Errorf("replay messages=%+v, want assistant calls paired with tool messages", req.Messages)
					} else {
						seen := make(map[string]bool)
						for i, want := range tc.calls {
							call, result := req.Messages[0].ToolCalls[i], req.Messages[i+1]
							wantArgs := fmt.Sprintf(`{"path":%q}`, want.path)
							if call.ID == "" || seen[call.ID] || want.wantID != "" && call.ID != want.wantID || result.Role != "tool" || result.ToolCallID != call.ID || call.Function.Arguments != wantArgs {
								t.Errorf("incoherent replay: messages=%+v", req.Messages)
							}
							seen[call.ID] = true
						}
					}
					_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"content": "replayed"}, "stop", nil)))
				} else {
					var first, second []any
					for i, call := range tc.calls {
						// The first fragment publishes the identity; the second may
						// introduce a deferred wire ID for the same index.
						start := map[string]any{"index": i, "function": map[string]any{"name": "read", "arguments": `{"path":`}}
						if call.firstID != "" {
							start["id"] = call.firstID
						}
						end := map[string]any{"index": i, "function": map[string]any{"arguments": fmt.Sprintf(`%q}`, call.path)}}
						if call.laterID != "" {
							end["id"] = call.laterID
						}
						first = append(first, start)
						second = append(second, end)
					}
					_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"tool_calls": first}, "", nil)))
					_, _ = fmt.Fprint(w, sseChunk(chunkWith(map[string]any{"tool_calls": second}, "tool_calls", nil)))
				}
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()

			p := mustNew(t, server.URL, "k")
			request := protocol.ChatRequest{Model: protocol.Model{ID: "m"}}
			stream, err := p.Chat(t.Context(), auth.Credential{Key: "k"}, request)
			if err != nil {
				t.Fatal(err)
			}
			events := drain(t, stream, t.Context())
			n := len(tc.calls)
			if len(events) != 3*n+1 || events[3*n].Type != protocol.EvStreamDone || events[3*n].StopReason != protocol.StopToolUse {
				t.Fatalf("events=%+v, want two deltas and completion per call, then tool-use stop", events)
			}
			assistant := protocol.Message{Role: protocol.RoleAssistant}
			var results []protocol.Message
			seen := make(map[string]bool)
			for i, call := range tc.calls {
				first, second, done := events[i], events[n+i], events[2*n+i]
				if first.Type != protocol.EvStreamToolCallDelta || second.Type != protocol.EvStreamToolCallDelta || done.Type != protocol.EvStreamToolCallDone {
					t.Fatalf("call %d events=%+v, want two deltas followed by completion", i, events)
				}
				if first.ToolCallID == "" || seen[first.ToolCallID] || call.wantID != "" && first.ToolCallID != call.wantID || second.ToolCallID != first.ToolCallID || done.ToolCallID != first.ToolCallID || first.ToolName != "read" || second.ToolName != "read" || done.ToolName != "read" {
					t.Fatalf("call %d identity is not stable and unique: events=%+v", i, events)
				}
				seen[first.ToolCallID] = true
				if string(done.Arguments) != fmt.Sprintf(`{"path":%q}`, call.path) || string(first.Arguments)+string(second.Arguments) != string(done.Arguments) {
					t.Fatalf("arguments not coherently accumulated: events=%+v", events)
				}
				assistant.Content = append(assistant.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, ToolCallID: done.ToolCallID, Name: done.ToolName, Arguments: done.Arguments})
				results = append(results, protocol.Message{Role: protocol.RoleTool, ToolCallID: done.ToolCallID, Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "result"}}})
			}
			request.Messages = append([]protocol.Message{assistant}, results...)
			stream, err = p.Chat(t.Context(), auth.Credential{Key: "k"}, request)
			if err != nil {
				t.Fatal(err)
			}
			events = drain(t, stream, t.Context())
			if len(events) != 2 || events[0].Text != "replayed" || events[1].Type != protocol.EvStreamDone {
				t.Fatalf("replay events=%+v", events)
			}
		})
	}
}
