package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/snow-core/snow/pkg/protocol"
	"testing"

	"github.com/snow-core/snow/internal/app"
)

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestRPCPropagatesShortWrites(t *testing.T) {
	var in bytes.Buffer
	in.WriteString(`{"id":"r1","type":"bogus"}` + "\n")
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := New(context.Background(), a, &in, shortWriter{}).Serve(context.Background()); err != io.ErrShortWrite {
		t.Fatalf("Serve error = %v, want %v", err, io.ErrShortWrite)
	}
}

func TestRPCPromptAndEvents(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer

	a, err := app.New(context.Background(), app.Options{
		Provider:   "fake",
		NoSession:  true,
		Permission: "allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := New(context.Background(), a, &in, &out)
	srv.app.Agent.Subscribe(func(ev protocol.AgentEvent) {
		b, _ := json.Marshal(ev)
		out.Write(append(b, '\n'))
	})

	in.WriteString("{\"id\":\"r1\",\"type\":\"prompt\",\"message\":\"hello\"}\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("no output")
	}
	// First line should be the prompt response.
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Command != "prompt" || !resp.Success {
		t.Fatalf("bad response: %+v", resp)
	}
	// The rest should be events including turn_done.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "turn_done") {
		t.Fatalf("expected turn_done event, got:\n%s", joined)
	}
}

func TestRPCSetThinkingAndSessionInfo(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	model := a.Agent.Model()
	model.SupportsThinking = true
	model.ThinkingLevels = []protocol.ThinkingLevel{protocol.ThinkingLow}
	if err := a.Agent.SetModel(model); err != nil {
		t.Fatal(err)
	}

	srv := New(context.Background(), a, &in, &out)
	in.WriteString(`{"id":"t1","type":"set_thinking","thinking":"low"}` + "\n")
	in.WriteString(`{"id":"i1","type":"session_info"}` + "\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	var set Response
	if err := json.Unmarshal([]byte(lines[0]), &set); err != nil {
		t.Fatal(err)
	}
	if set.Command != "set_thinking" || !set.Success {
		t.Fatalf("set response = %+v", set)
	}
	var info Response
	if err := json.Unmarshal([]byte(lines[1]), &info); err != nil {
		t.Fatal(err)
	}
	data, _ := info.Data.(map[string]any)
	if info.Command != "session_info" || !info.Success || data["thinking"] != "low" || fmt.Sprint(data["thinking_levels"]) != "[off low]" {
		t.Fatalf("session info = %+v", info)
	}
}

func TestRPCUnknownCommand(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := New(context.Background(), a, &in, &out)
	in.WriteString("{\"id\":\"r1\",\"type\":\"bogus\"}\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "unknown command") {
		t.Fatalf("expected failure response, got %+v", resp)
	}
}

func TestRPCInvalidJSON(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := New(context.Background(), a, &in, &out)
	in.WriteString("{not json}\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "invalid JSON") {
		t.Fatalf("expected invalid json response, got %q", out.String())
	}
}

func TestRPCUserInputReplyAndReject(t *testing.T) {
	newHarness := func(t *testing.T) (*app.App, *Server, *bytes.Buffer) {
		t.Helper()
		a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		out := &bytes.Buffer{}
		return a, New(context.Background(), a, &bytes.Buffer{}, out), out
	}
	request := protocol.UserInputRequest{ID: "ask-rpc", Questions: []protocol.UserInputQuestion{{ID: "choice", Header: "Choice", Question: "Choose?"}}}

	t.Run("reply", func(t *testing.T) {
		a, srv, out := newHarness(t)
		defer a.Close()
		resolved := make(chan protocol.UserInputResponse, 1)
		published := make(chan struct{})
		a.Agent.Subscribe(func(event protocol.AgentEvent) {
			if event.Type == protocol.EvUserInputRequest {
				close(published)
			}
		})
		go func() {
			response, _ := a.RequestUserInput(context.Background(), request)
			resolved <- response
		}()
		<-published
		params, _ := json.Marshal(protocol.UserInputResponse{RequestID: request.ID, Answers: []protocol.UserInputAnswer{{QuestionID: "choice", Answer: "A"}}})
		if err := srv.handle(context.Background(), Request{ID: "reply-1", Type: "user_input_reply", Params: params}); err != nil {
			t.Fatal(err)
		}
		if response := <-resolved; response.Answers[0].Answer != "A" {
			t.Fatalf("response = %+v", response)
		}
		if !strings.Contains(out.String(), `"command":"user_input_reply"`) || !strings.Contains(out.String(), `"success":true`) {
			t.Fatalf("output = %s", out)
		}
	})

	t.Run("reject", func(t *testing.T) {
		a, srv, out := newHarness(t)
		defer a.Close()
		resolved := make(chan error, 1)
		published := make(chan struct{})
		a.Agent.Subscribe(func(event protocol.AgentEvent) {
			if event.Type == protocol.EvUserInputRequest {
				close(published)
			}
		})
		go func() {
			_, err := a.RequestUserInput(context.Background(), request)
			resolved <- err
		}()
		<-published
		if err := srv.handle(context.Background(), Request{ID: "reject-1", Type: "user_input_reject", Params: json.RawMessage(`{"request_id":"ask-rpc"}`)}); err != nil {
			t.Fatal(err)
		}
		if err := <-resolved; err == nil || !strings.Contains(err.Error(), "declined") {
			t.Fatalf("error = %v", err)
		}
		if !strings.Contains(out.String(), `"command":"user_input_reject"`) {
			t.Fatalf("output = %s", out)
		}
	})
}

func TestRPCEOFReleasesPendingUserInput(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := New(context.Background(), a, &bytes.Buffer{}, &bytes.Buffer{})
	published := make(chan struct{})
	a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvUserInputRequest {
			close(published)
		}
	})
	resolved := make(chan error, 1)
	go func() {
		_, err := a.RequestUserInput(context.Background(), protocol.UserInputRequest{
			ID: "ask-eof", Questions: []protocol.UserInputQuestion{{ID: "choice", Header: "Choice", Question: "Choose?"}},
		})
		resolved <- err
	}()
	<-published
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-resolved; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("pending request error = %v", err)
	}
}
