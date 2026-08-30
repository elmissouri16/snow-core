package responsesapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func testStream(capacity int) *codexStream {
	return &codexStream{ch: make(chan protocol.StreamEvent, capacity), done: make(chan struct{}), ctx: context.Background(), provider: "test"}
}

func TestReasoningAccumReconstructsManyFragments(t *testing.T) {
	accum := newReasoningAccum()
	for range 4096 {
		if err := accum.canAppend("item", "x"); err != nil {
			t.Fatal(err)
		}
		accum.append("item", "x")
	}
	if got := accum.text("item"); got != strings.Repeat("x", 4096) {
		t.Fatalf("reconstructed length=%d", len(got))
	}
}

func TestStreamRejectsAggregateSSEEvent(t *testing.T) {
	fragment := strings.Repeat("x", 3<<20)
	body := "data: " + fragment + "\ndata: " + fragment + "\ndata: " + fragment + "\n"
	s := testStream(4)
	s.body = io.NopCloser(strings.NewReader(body))
	s.read()
	ev := <-s.ch
	if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "SSE event exceeds size limit") {
		t.Fatalf("event = %+v", ev)
	}
}

func TestStreamRejectsTooManyEmptySSEFragments(t *testing.T) {
	s := testStream(4)
	s.body = io.NopCloser(strings.NewReader(strings.Repeat("data:\n", maxCodexSSEEventFragments+1)))
	s.read()
	ev := <-s.ch
	if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "SSE event exceeds size limit") {
		t.Fatalf("event = %+v", ev)
	}
}

func TestStreamBoundsToolCallsAndArguments(t *testing.T) {
	t.Run("per-call arguments", func(t *testing.T) {
		s := testStream(2)
		stopped := s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "one", "delta": strings.Repeat("x", maxCodexToolArgumentBytes+1)}, map[string]*toolAccum{}, newReasoningAccum(), &codexStreamBounds{}, new(protocol.StopReason), new(bool))
		if !stopped {
			t.Fatal("oversized arguments did not stop stream")
		}
		ev := <-s.ch
		if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "per-call size limit") {
			t.Fatalf("event = %+v", ev)
		}
	})
	t.Run("completed snapshot is authoritative", func(t *testing.T) {
		s := testStream(8)
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		if s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "item", "delta": `{"old":true}`}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("delta stopped")
		}
		if s.processEvent(map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "function_call", "id": "item", "arguments": `{"new":true}`}}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("output item stopped")
		}
		var lastDone protocol.StreamEvent
		for len(s.ch) > 0 {
			ev := <-s.ch
			if ev.Type == protocol.EvStreamToolCallDone {
				lastDone = ev
			}
		}
		if string(lastDone.Arguments) != `{"new":true}` {
			t.Fatalf("last done arguments = %s", lastDone.Arguments)
		}
	})
	t.Run("malformed snapshot remains persistable", func(t *testing.T) {
		s := testStream(4)
		if s.processEvent(map[string]any{"type": "response.function_call_arguments.done", "item_id": "item", "call_id": "call", "name": "read", "arguments": `{"bad"`}, map[string]*toolAccum{}, newReasoningAccum(), &codexStreamBounds{}, new(protocol.StopReason), new(bool)) {
			t.Fatal("malformed arguments unexpectedly stopped stream")
		}
		ev := <-s.ch
		want, _ := json.Marshal(`{"bad"`)
		if ev.Type != protocol.EvStreamToolCallDone || string(ev.Arguments) != string(want) || !json.Valid(ev.Arguments) {
			t.Fatalf("event = %+v", ev)
		}
	})
	t.Run("completed snapshots contribute to aggregate", func(t *testing.T) {
		s := testStream(16)
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		for i := range 4 {
			id := fmt.Sprintf("snapshot-%d", i)
			if s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": id, "delta": "x"}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
				t.Fatalf("delta %d stopped early", i)
			}
			if s.processEvent(map[string]any{"type": "response.function_call_arguments.done", "item_id": id, "arguments": strings.Repeat("x", maxCodexToolArgumentBytes)}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
				t.Fatalf("snapshot %d stopped early", i)
			}
		}
		if !s.processEvent(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "overflow", "delta": "x"}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("aggregate snapshot growth did not stop later arguments")
		}
	})
	t.Run("tool count", func(t *testing.T) {
		s := testStream(2)
		calls := make(map[string]*toolAccum)
		bounds := &codexStreamBounds{}
		for i := 0; i <= maxCodexStreamToolCalls; i++ {
			stopped := s.processEvent(map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "function_call"}}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool))
			if stopped != (i == maxCodexStreamToolCalls) {
				t.Fatalf("event %d stopped = %v", i, stopped)
			}
		}
		ev := <-s.ch
		if ev.Type != protocol.EvStreamError || ev.Err == nil || !strings.Contains(ev.Err.Error(), "tool-call count") {
			t.Fatalf("event = %+v", ev)
		}
	})
}

func TestStreamBoundsCompletedReasoningItems(t *testing.T) {
	s := testStream(maxCodexReasoningItems + 2)
	calls := make(map[string]*toolAccum)
	bounds := &codexStreamBounds{}
	for i := 0; i <= maxCodexReasoningItems; i++ {
		stopped := s.processEvent(map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "reasoning", "id": fmt.Sprintf("reasoning-%d", i), "summary": []any{}}}, calls, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool))
		if stopped != (i == maxCodexReasoningItems) {
			t.Fatalf("item %d stopped = %v", i, stopped)
		}
	}
	var last protocol.StreamEvent
	for len(s.ch) > 0 {
		last = <-s.ch
	}
	if last.Type != protocol.EvStreamError || last.Err == nil || !strings.Contains(last.Err.Error(), "completed reasoning") {
		t.Fatalf("last event = %+v", last)
	}
}

func TestReasoningAccumSeparatesDistinctSummaryItems(t *testing.T) {
	reasoning := newReasoningAccum()
	chunks := []string{
		reasoning.append("summary:item-1:0", "**Planning tasks**"),
		reasoning.append("summary:item-1:0", " carefully"),
		reasoning.append("summary:item-2:0", "**Designing workers**"),
		reasoning.append("text:item-3:0", "\nMonitoring execution"),
	}
	got := strings.Join(chunks, "")
	want := "**Planning tasks** carefully\n\n**Designing workers**\n\nMonitoring execution"
	if got != want || reasoning.text("summary:item-2:0") != "**Designing workers**" {
		t.Fatalf("reasoning stream = %q", got)
	}
}

func TestMissingReasoningSuffixIsMonotonic(t *testing.T) {
	for _, tc := range []struct{ streamed, completed, want string }{
		{"", "complete", "complete"}, {"com", "complete", "plete"}, {"complete", "complete", ""}, {"complete", "com", ""}, {"first", "second", ""},
	} {
		if got := missingReasoningSuffix(tc.streamed, tc.completed); got != tc.want {
			t.Fatalf("missingReasoningSuffix(%q, %q) = %q", tc.streamed, tc.completed, got)
		}
	}
}

func TestStreamCloseReleasesResponseBody(t *testing.T) {
	reader, writer := io.Pipe()
	stream := NewStream(context.Background(), &http.Response{Body: reader}, "test")
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("data: {}\n\n")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after stream close error = %v, want closed pipe", err)
	}
	_ = writer.Close()
}

func TestStreamTerminalEventEndsKeepaliveConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	stream := NewStream(context.Background(), resp, "test")
	defer stream.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	first, err := stream.Next(ctx)
	if err != nil || first.Type != protocol.EvStreamUsage {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	second, err := stream.Next(ctx)
	if err != nil || second.Type != protocol.EvStreamDone || second.StopReason != protocol.StopStop {
		t.Fatalf("second = %+v err=%v", second, err)
	}
}

func TestStreamBoundsTextAndFailureMessage(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		s := testStream(2)
		bounds := &codexStreamBounds{responseTextBytes: maxResponseTextBytes}
		if !s.processEvent(map[string]any{"type": "response.output_text.delta", "delta": "x"}, map[string]*toolAccum{}, newReasoningAccum(), bounds, new(protocol.StopReason), new(bool)) {
			t.Fatal("text overflow did not stop")
		}
	})
	t.Run("failure", func(t *testing.T) {
		s := &codexStream{ch: make(chan protocol.StreamEvent, 2), done: make(chan struct{}), ctx: context.Background(), provider: "test", secrets: []string{"secret"}}
		message := "secret" + strings.Repeat("é", 600)
		if !s.processEvent(map[string]any{"type": "response.failed", "message": message}, map[string]*toolAccum{}, newReasoningAccum(), &codexStreamBounds{}, new(protocol.StopReason), new(bool)) {
			t.Fatal("failure did not stop")
		}
		ev := <-s.ch
		if ev.Err == nil || strings.Contains(ev.Err.Error(), "secret") || !strings.Contains(ev.Err.Error(), "[redacted]") || len(ev.Err.Error()) > maxStreamErrorBytes+20 || !utf8.ValidString(ev.Err.Error()) {
			t.Fatalf("error = %v", ev.Err)
		}
	})
}
