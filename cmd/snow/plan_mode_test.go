package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestCLIJSONEmitsInitialCollaborationModeSnapshot(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"cli-model"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	oldArgs := os.Args
	os.Args = []string{"snow", "--provider", "opencode-go", "--model", "cli-model", "--api-key", "key", "--base-url", server.URL, "--permission", "allow", "--no-session", "--mode", "json", "--collaboration-mode", "plan", "-p", "design"}
	t.Cleanup(func() { os.Args = oldArgs })
	output, err := captureStdout(t, run)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	if !scanner.Scan() {
		t.Fatalf("no JSON events: %q", output)
	}
	var event protocol.AgentEvent
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EvModeChanged || event.Mode == nil || event.Mode.Mode != protocol.ModePlan {
		t.Fatalf("first event = %+v", event)
	}
}

func TestCLIPlanPrintSuppressesTagsAndPrintsPlanOnce(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"cli-model","supports_reasoning":false}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"<proposed_plan>\\n# Ship\\n\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"- test\\n</proposed_plan>\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	oldArgs := os.Args
	os.Args = []string{"snow", "--provider", "opencode-go", "--model", "cli-model", "--api-key", "key", "--base-url", server.URL, "--permission", "allow", "--no-session", "--mode", "print", "--collaboration-mode", "plan", "-p", "design"}
	t.Cleanup(func() { os.Args = oldArgs })
	output, err := captureStdout(t, run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "proposed_plan") || strings.Count(output, "# Ship") != 1 || !strings.Contains(output, "- test") {
		t.Fatalf("output = %q", output)
	}
}
