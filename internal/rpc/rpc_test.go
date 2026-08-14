package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

type rpcQueueProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *rpcQueueProvider) ID() string { return "rpc-queue" }
func (p *rpcQueueProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *rpcQueueProvider) Resolve(_ context.Context, credential auth.Credential) (auth.Credential, error) {
	return credential, nil
}
func (p *rpcQueueProvider) Chat(_ context.Context, _ auth.Credential, _ protocol.ChatRequest) (protocol.EventStream, error) {
	first := false
	p.once.Do(func() {
		first = true
		close(p.started)
	})
	if first {
		return &rpcGateStream{release: p.release}, nil
	}
	return &rpcGateStream{}, nil
}

type rpcErrorProvider struct{}

func (*rpcErrorProvider) ID() string { return "rpc-error" }
func (*rpcErrorProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (*rpcErrorProvider) Resolve(_ context.Context, credential auth.Credential) (auth.Credential, error) {
	return credential, nil
}
func (*rpcErrorProvider) Chat(context.Context, auth.Credential, protocol.ChatRequest) (protocol.EventStream, error) {
	return nil, errors.New("fixture prompt failure")
}

type rpcGateStream struct {
	release <-chan struct{}
	done    bool
}

func (s *rpcGateStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.done {
		return protocol.StreamEvent{}, io.EOF
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return protocol.StreamEvent{}, ctx.Err()
		}
	}
	s.done = true
	return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}, nil
}
func (*rpcGateStream) Close() error { return nil }

type terminalErrorReader struct {
	data []byte
	err  error
}

func (r *terminalErrorReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

type shortWriter struct{}

func rpcFrame(t *testing.T, output, kind, id string) []byte {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var header struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal([]byte(line), &header); err != nil {
			t.Fatalf("decode RPC frame %q: %v", line, err)
		}
		if header.Type == kind && (id == "" || header.ID == id || header.RequestID == id) {
			return []byte(line)
		}
	}
	t.Fatalf("RPC frame type=%q id=%q not found in %q", kind, id, output)
	return nil
}

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

func TestRPCRejectsNegativeWaitTimeoutBeforeStartingWorker(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	server := New(context.Background(), a, strings.NewReader(""), io.Discard)
	err = server.handle(context.Background(), Request{Type: "subagent_wait", Params: json.RawMessage(`{"timeout_ms":-1}`)})
	if err == nil || !strings.Contains(err.Error(), "cannot be negative") {
		t.Fatalf("negative timeout error = %v", err)
	}
}

func TestRPCScannerErrorUsesOrderlyShutdown(t *testing.T) {
	sentinel := errors.New("input failed")
	reader := &terminalErrorReader{data: []byte(`{"id":"p1","type":"prompt","message":"hello"}` + "\n"), err: sentinel}
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	err = New(context.Background(), a, reader, &out).Serve(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Serve error = %v", err)
	}
	if a.Agent.IsRunning() {
		t.Fatal("Serve returned while prompt worker was still running")
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
		_ = srv.write(ev)
	})

	in.WriteString("{\"id\":\"r1\",\"type\":\"prompt\",\"message\":\"hello\"}\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("no output")
	}
	var ready protocol.RPCReady
	if err := json.Unmarshal([]byte(lines[0]), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Type != protocol.RPCTypeReady || ready.ProtocolVersion != protocol.RPCProtocolVersion {
		t.Fatalf("bad first frame: %+v", ready)
	}
	var resp Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "r1"), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Command != "prompt" || !resp.Success {
		t.Fatalf("bad response: %+v", resp)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "turn_done") || !strings.Contains(joined, protocol.RPCTypePromptCompleted) {
		t.Fatalf("expected turn_done and prompt completion, got:\n%s", joined)
	}
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, "r1"), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.RequestID != "r1" || completed.Status != protocol.RPCPromptCanceledStatus {
		t.Fatalf("completion = %+v", completed)
	}
}

func TestRPCSessionRenameAndInfo(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, CWD: t.TempDir(), Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	in.WriteString(`{"id":"r1","type":"session_rename","params":{"name":"RPC title"}}` + "\n")
	in.WriteString(`{"id":"i1","type":"session_info"}` + "\n")
	if err := New(context.Background(), a, &in, &out).Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	var info Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "i1"), &info); err != nil {
		t.Fatal(err)
	}
	data, _ := info.Data.(map[string]any)
	if !info.Success || data["name"] != "RPC title" {
		t.Fatalf("session info = %+v", info)
	}
}

func TestRPCSetThinkingAndSessionInfo(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	dir := t.TempDir()
	a, err := app.New(context.Background(), app.Options{Provider: "fake", SessionPath: filepath.Join(dir, "session.db"), CWD: dir, Permission: "allow"})
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
	goal, err := a.CreateGoal("priced goal", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Session.(session.ThreadGoalStore).AccountGoal(goal.GoalID, 10, 1, &protocol.Cost{Currency: "USD", Total: 0.02}); err != nil {
		t.Fatal(err)
	}

	srv := New(context.Background(), a, &in, &out)
	in.WriteString(`{"id":"t1","type":"set_thinking","thinking":"low"}` + "\n")
	in.WriteString(`{"id":"i1","type":"session_info"}` + "\n")
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	var set Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "t1"), &set); err != nil {
		t.Fatal(err)
	}
	if set.Command != "set_thinking" || !set.Success {
		t.Fatalf("set response = %+v", set)
	}
	var info Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "i1"), &info); err != nil {
		t.Fatal(err)
	}
	data, _ := info.Data.(map[string]any)
	pending, _ := data["pending_inputs"].(map[string]any)
	goalInfo, _ := data["goal"].(map[string]any)
	costs, _ := goalInfo["estimated_costs"].([]any)
	if info.Command != "session_info" || !info.Success || data["thinking"] != "low" || fmt.Sprint(data["thinking_levels"]) != "[off low]" || pending["total"] != float64(0) || len(costs) != 1 {
		t.Fatalf("session info = %+v", info)
	}
}

func TestRPCSecondPromptDoesNotCancelActivePrompt(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	active := make(chan struct{})
	srv.promptDone = active
	cancelled := false
	srv.cancel = func() { cancelled = true }
	err = srv.handlePrompt(context.Background(), Request{ID: "p2", Type: "prompt", Message: "replacement"})
	if err == nil || !strings.Contains(err.Error(), "use steer, follow_up, or abort") {
		t.Fatalf("second prompt error = %v", err)
	}
	if cancelled {
		t.Fatal("second prompt implicitly cancelled active work")
	}
}

func TestRPCPromptCompletionPreservesMissingID(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	if err := srv.handlePrompt(context.Background(), Request{Type: "prompt", Message: "idless"}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	done := srv.promptDone
	srv.mu.Unlock()
	<-done
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, ""), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.RequestID != "" || completed.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion = %+v", completed)
	}
}

func TestRPCPromptFailureKeepsLegacyResponseBeforeTerminalCompletion(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	provider := &rpcErrorProvider{}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	a.Agent.Subscribe(func(event protocol.AgentEvent) { _ = srv.write(event) })
	if err := srv.handlePrompt(context.Background(), Request{ID: "p1", Type: "prompt", Message: "fail"}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	done := srv.promptDone
	srv.mu.Unlock()
	<-done

	failureIndex := -1
	completionIndex := -1
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for i, line := range lines {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		if frame["type"] == "response" && frame["id"] == "p1" && frame["success"] == false {
			failureIndex = i
		}
		if frame["type"] == protocol.RPCTypePromptCompleted && frame["request_id"] == "p1" {
			completionIndex = i
			if frame["status"] != string(protocol.RPCPromptFailedStatus) || !strings.Contains(fmt.Sprint(frame["error"]), "fixture prompt failure") {
				t.Fatalf("completion = %+v", frame)
			}
		}
	}
	if failureIndex < 0 || completionIndex != len(lines)-1 || failureIndex >= completionIndex {
		t.Fatalf("legacy failure=%d completion=%d frames=%q", failureIndex, completionIndex, lines)
	}
}

func TestRPCAbortEmitsTerminalCanceledCompletion(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	a.Agent.Subscribe(func(event protocol.AgentEvent) { _ = srv.write(event) })
	if err := srv.handlePrompt(context.Background(), Request{ID: "p1", Type: "prompt", Message: "wait"}); err != nil {
		t.Fatal(err)
	}
	<-provider.started
	if err := srv.handle(context.Background(), Request{ID: "a1", Type: "abort"}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	done := srv.promptDone
	srv.mu.Unlock()
	if done != nil {
		<-done
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, "p1"), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Type != protocol.RPCTypePromptCompleted || completed.RequestID != "p1" || completed.Status != protocol.RPCPromptCanceledStatus || completed.Error != "" {
		t.Fatalf("completion = %+v; frames = %q", completed, lines)
	}
}

func TestRPCQueueCommandsAcceptActiveRunAndReportCounts(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	provider := &rpcQueueProvider{started: make(chan struct{}), release: make(chan struct{})}
	model := a.Agent.Model()
	model.Provider = provider.ID()
	if err := a.Agent.SetProviderAndModel(provider, model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	queueEvents := 0
	a.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvQueueUpdated {
			queueEvents++
		}
	})
	done := make(chan error, 1)
	go func() { done <- a.Agent.Prompt(context.Background(), "initial") }()
	<-provider.started
	if err := srv.handle(context.Background(), Request{ID: "s", Type: "steer", Message: "steer"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(context.Background(), Request{ID: "f", Type: "follow_up", Message: "follow"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(context.Background(), Request{ID: "i", Type: "session_info"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses = %q", lines)
	}
	var info Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "i"), &info); err != nil {
		t.Fatal(err)
	}
	data := info.Data.(map[string]any)
	pending := data["pending_inputs"].(map[string]any)
	if pending["steering"] != float64(1) || pending["follow_up"] != float64(1) || pending["total"] != float64(2) {
		t.Fatalf("pending counts = %+v", pending)
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if queueEvents < 4 {
		t.Fatalf("queue events = %d", queueEvents)
	}
}

func TestRPCQueueCommandsRejectIdleAndEmpty(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	for _, req := range []Request{
		{ID: "s", Type: "steer", Message: "later"},
		{ID: "f", Type: "follow_up", Message: "later"},
		{ID: "e", Type: "steer"},
	} {
		if err := srv.handle(context.Background(), req); err == nil {
			t.Fatalf("idle/empty %s accepted", req.Type)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("rejected queue commands wrote success: %q", out.String())
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
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "r1"), &resp); err != nil {
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

func TestRPCReadyAnnouncementIsFirstAndUnique(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := NewWithOptions(context.Background(), a, strings.NewReader(""), &out, ServerOptions{SnowVersion: "test-version"})
	if err := srv.announceReady(); err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("ready frames = %q", lines)
	}
	var ready protocol.RPCReady
	if err := json.Unmarshal([]byte(lines[0]), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Type != protocol.RPCTypeReady || ready.ProtocolVersion != protocol.RPCProtocolVersion || ready.SnowVersion != "test-version" || ready.MaxInputBytes != protocol.RPCMaxInputBytes {
		t.Fatalf("ready = %+v", ready)
	}
}

func TestRPCModelDiscovery(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	if err := srv.handle(context.Background(), Request{ID: "m1", Type: "models_list"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(context.Background(), Request{ID: "sm1", Type: "subagent_models"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %q", lines)
	}
	var active struct {
		Data protocol.RPCModelList `json:"data"`
	}
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "m1"), &active); err != nil {
		t.Fatal(err)
	}
	if active.Data.Provider != "fake" || active.Data.Current != "fake-1" || len(active.Data.Models) != 1 || active.Data.Models[0].ID != "fake-1" {
		t.Fatalf("active models = %+v", active.Data)
	}
	var children struct {
		Data protocol.RPCModelList `json:"data"`
	}
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "sm1"), &children); err != nil {
		t.Fatal(err)
	}
	if children.Data.Enabled == nil || *children.Data.Enabled || len(children.Data.Models) == 0 {
		t.Fatalf("subagent models = %+v", children.Data)
	}
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
