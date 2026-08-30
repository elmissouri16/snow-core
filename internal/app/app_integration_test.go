package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// TestAppDefaultModelPrefersProviderDefault verifies that with no configured
// default model, the app pins the provider's documented default (kimi-k2.6)
// instead of taking whatever the live catalog lists first (minimax-m3).
func TestAppDefaultModelPrefersProviderDefault(t *testing.T) {
	// Isolate from the developer's real ~/.snow (never read or write it).
	t.Setenv("SNOW_HOME", t.TempDir())

	// Catalog lists minimax-m3 FIRST; kimi-k2.6 is present but later.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"minimax-m3"},{"id":"kimi-k2.6"},{"id":"glm-5.2"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	ctx := t.Context()
	a, err := New(ctx, Options{
		Provider:   "opencode-go",
		NoSession:  true,
		Permission: "allow",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		CWD:        t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if a.Model.ID != "kimi-k2.6" {
		t.Fatalf("default model = %q, want kimi-k2.6 (provider pinned default, not catalog[0])", a.Model.ID)
	}
}

// TestAppOpenCodeGoEndToEnd wires the real opencode-go provider against a
// local OpenAI-compatible mock and verifies a tool-call round trip through
// the agent loop: text → tool_call → tool result → final text.
func TestAppOpenCodeGoEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET /models returns the catalog; POST /chat/completions streams.
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"kimi-k2.6"}]}`)
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = fmt.Fprint(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
		if n == 1 {
			// Model wants to read a file.
			write("data: {\"choices\":[{\"delta\":{\"content\":\"Let me read that file\"}}]}\n\n")
			write("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\": \\\"existing.txt\\\"}\"}}]}}]}\n\n")
			write("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			write("data: {\"choices\":[{\"delta\":{\"content\":\"Found it: hello world\"}}]}\n\n")
			write("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		write("data: [DONE]\n\n")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/existing.txt", []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(ctx, Options{
		Provider:   "opencode-go",
		NoSession:  true,
		Permission: "allow",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	var text strings.Builder
	var toolEnds int
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		switch ev.Type {
		case protocol.EvTextDelta:
			text.WriteString(ev.Text)
		case protocol.EvToolEnd:
			toolEnds++
		}
	})

	if err := a.Agent.Prompt(ctx, "read existing.txt"); err != nil {
		t.Fatal(err)
	}

	if toolEnds != 1 {
		t.Fatalf("expected 1 tool execution, got %d", toolEnds)
	}
	final := text.String()
	if !strings.Contains(final, "Found it") {
		t.Fatalf("final text = %q, want to contain %q", final, "Found it")
	}
	msgs, err := a.Session.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 { // user, assistant(tool_use), tool_result, assistant(final)
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[2].Role != protocol.RoleTool || msgs[2].IsError {
		t.Fatalf("tool result message wrong: %+v", msgs[2])
	}
	if !strings.Contains(msgs[2].Content[0].Text, "hello world") {
		t.Fatalf("tool result content = %+v, want to contain file contents", msgs[2].Content)
	}
}
