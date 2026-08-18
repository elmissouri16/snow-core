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
func (p *rpcQueueProvider) Chat(_ context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
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
func (*rpcErrorProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
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

func TestRPCBranchAndDetachedSessionFork(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	var in bytes.Buffer
	var out bytes.Buffer
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, CWD: t.TempDir(), Permission: "allow", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	in.WriteString(`{"id":"b1","type":"branch_fork","params":{"from_entry_id":"root","name":"branch child"}}` + "\n")
	in.WriteString(`{"id":"s1","type":"session_fork","params":{"from_entry_id":"root","name":"session child"}}` + "\n")
	if err := New(context.Background(), a, &in, &out).Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var branch Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "b1"), &branch); err != nil {
		t.Fatal(err)
	}
	branchData, _ := branch.Data.(map[string]any)
	if !branch.Success || branchData["name"] != "branch child" {
		t.Fatalf("branch response = %+v", branch)
	}
	var fork Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "s1"), &fork); err != nil {
		t.Fatal(err)
	}
	forkData, _ := fork.Data.(map[string]any)
	path, _ := forkData["session_path"].(string)
	if !fork.Success || forkData["name"] != "session child" || path == "" {
		t.Fatalf("session fork response = %+v", fork)
	}
	if err := session.ValidateSQLiteSession(path); err != nil {
		t.Fatalf("forked session is invalid: %v", err)
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

func TestRPCContentPromptRequiresValidAttachments(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)

	cases := []struct {
		name    string
		request Request
		wantSub string
	}{
		{"empty content", Request{Type: "prompt", Content: []protocol.ContentBlock{}}, "content must not be empty"},
		{"text-only without message", Request{Type: "prompt", Content: []protocol.ContentBlock{{Type: protocol.BlockText, Text: "hi"}}}, "requires a message"},
		{"provider data block", Request{Type: "prompt", Message: "x", Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Text: "opaque"}}}, "not allowed"},
		{"thinking block", Request{Type: "prompt", Message: "x", Content: []protocol.ContentBlock{{Type: protocol.BlockThinking, Text: "think"}}}, "not allowed"},
		{"image without mime", Request{Type: "prompt", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, Data: []byte{1}}}}, "requires mime_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := srv.handlePrompt(context.Background(), tc.request)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("handlePrompt = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestRPCContentPromptValidatesModelVisionAndRuns(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	model := a.Agent.Model()
	model.SupportsVision = false
	if err := a.Agent.SetProviderAndModel(a.Providers["fake"], model); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	err = srv.handlePrompt(context.Background(), Request{Type: "prompt", Message: "look", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1}}}})
	if err == nil || !strings.Contains(err.Error(), "does not support image input") {
		t.Fatalf("handlePrompt = %v, want vision rejection", err)
	}

	model2 := a.Agent.Model()
	model2.SupportsVision = true
	if err := a.Agent.SetProviderAndModel(a.Providers["fake"], model2); err != nil {
		t.Fatal(err)
	}
	if err := srv.handlePrompt(context.Background(), Request{ID: "mm1", Type: "prompt", Message: "look", Content: []protocol.ContentBlock{{Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1, 2}}}}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	done := srv.promptDone
	srv.mu.Unlock()
	if done != nil {
		<-done
	}
	var completed protocol.RPCPromptCompleted
	if err := json.Unmarshal(rpcFrame(t, out.String(), protocol.RPCTypePromptCompleted, ""), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != protocol.RPCPromptCompletedStatus {
		t.Fatalf("completion = %+v", completed)
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
	if resp.Success || !strings.Contains(resp.Error, "unknown command") || resp.ErrorCode != "invalid" {
		t.Fatalf("expected classified failure response, got %+v", resp)
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

func TestRPCControlAndInspectionCommands(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	var in bytes.Buffer
	var out bytes.Buffer
	dir := t.TempDir()
	a, err := app.New(context.Background(), app.Options{Provider: "fake", SessionPath: filepath.Join(dir, "session.db"), CWD: dir, Permission: "allow", NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	srv := New(context.Background(), a, &in, &out)
	input := strings.Join([]string{
		`{"id":"c0","type":"set_reasoning_summary","reasoning_summary":"concise"}`,
		`{"id":"c1","type":"set_text_verbosity","text_verbosity":"high"}`,
		`{"id":"i1","type":"session_info"}`,
		`{"id":"u1","type":"usage"}`,
		`{"id":"m1","type":"messages_list"}`,
		`{"id":"d1","type":"diagnostics"}`,
		`{"id":"l1","type":"branches_list"}`,
		`{"id":"r1","type":"branch_rename","params":{"branch_id":"main","name":"renamed main"}}`,
		`{"id":"s1","type":"branch_select","params":{"branch_id":"main"}}`,
		`{"id":"p1","type":"pending_inputs"}`,
		`{"id":"p2","type":"pending_inputs_clear"}`,
		`{"id":"p3","type":"pending_inputs"}`,
	}, "\n") + "\n"
	in.WriteString(input)
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}

	must := func(id string) Response {
		t.Helper()
		var resp Response
		if err := json.Unmarshal(rpcFrame(t, out.String(), "response", id), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success {
			t.Fatalf("%s failed: %+v", id, resp)
		}
		return resp
	}
	var info Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "i1"), &info); err != nil {
		t.Fatal(err)
	}
	infoData := info.Data.(map[string]any)
	if infoData["reasoning_summary"] != "concise" || infoData["text_verbosity"] != "high" {
		t.Fatalf("session info controls = %+v", infoData)
	}
	var usageResp Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "u1"), &usageResp); err != nil {
		t.Fatal(err)
	}
	usageData, ok := usageResp.Data.(map[string]any)
	if !ok || usageData["input"] == nil {
		t.Fatalf("usage data = %+v", usageResp.Data)
	}
	var messages Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "m1"), &messages); err != nil {
		t.Fatal(err)
	}
	messagesData, ok := messages.Data.(map[string]any)
	if !ok || messagesData["messages"] == nil {
		t.Fatalf("messages data = %+v", messages.Data)
	}
	diagnostics := must("d1")
	if _, ok := diagnostics.Data.(map[string]any)["diagnostics"]; !ok {
		t.Fatalf("diagnostics data = %+v", diagnostics.Data)
	}
	branchList := must("l1")
	branches, ok := branchList.Data.(map[string]any)["branches"].([]any)
	if !ok || len(branches) == 0 {
		t.Fatalf("branch list data = %+v", branchList.Data)
	}
	renameResp := must("r1")
	_, ok = renameResp.Data.(map[string]any)["name"].(string)
	if !ok {
		t.Fatalf("branch rename data = %+v", renameResp.Data)
	}
	must("s1")
	pending := must("p1")
	items, _ := pending.Data.(map[string]any)["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("pending items before clear = %+v", pending.Data)
	}
	must("p2")
	must("p3")

}

func TestPublicMessagesRemoveProviderContinuityData(t *testing.T) {
	input := []protocol.Message{{
		ID: "assistant-1", Role: protocol.RoleAssistant, Timestamp: 1,
		Content: []protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "summary"},
			{Type: protocol.BlockProviderData, Name: "opaque", Data: []byte(`{"secret":true}`)},
			{Type: protocol.BlockText, Text: "answer"},
		},
	}}
	got := publicMessages(input)
	if len(got) != 1 || len(got[0].Content) != 2 {
		t.Fatalf("public messages = %+v", got)
	}
	for _, block := range got[0].Content {
		if block.Type == protocol.BlockProviderData {
			t.Fatalf("provider continuity block escaped: %+v", block)
		}
	}
	if len(input[0].Content) != 3 {
		t.Fatalf("source messages mutated: %+v", input)
	}
	if empty := publicMessages(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("nil messages = %#v, want non-nil empty slice", empty)
	}
}

func TestRPCInvalidNewCommandsRejected(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	for _, req := range []Request{
		{ID: "a1", Type: "set_reasoning_summary"},
		{ID: "a2", Type: "set_reasoning_summary", ReasoningSummary: "bogus"},
		{ID: "a3", Type: "set_text_verbosity"},
		{ID: "a4", Type: "set_text_verbosity", TextVerbosity: "bogus"},
		{ID: "a5", Type: "branch_select"},
		{ID: "a6", Type: "branch_select", Params: json.RawMessage(`{"branch_id":""}`)},
		{ID: "a7", Type: "branch_delete", Params: json.RawMessage(`{"branch_id":"missing"}`)},
		{ID: "a8", Type: "branch_rename", Params: json.RawMessage(`{"branch_id":""}`)},
	} {
		if err := srv.handle(context.Background(), req); err == nil {
			t.Fatalf("%s (%q) unexpectedly accepted", req.Type, req.ReasoningSummary)
		}
	}
}

func TestRPCCompactOnIdleSession(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	if err := srv.handle(context.Background(), Request{ID: "b1", Type: "compact"}); err != nil {
		t.Fatalf("compact on idle session failed: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(rpcFrame(t, out.String(), "response", "b1"), &resp); err != nil {
		t.Fatal(err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok || data["summarized_messages"] == nil || data["retained_messages"] == nil {
		t.Fatalf("compact data = %+v", resp.Data)
	}
}

func TestRPCBranchDeletesRejectMissingBranch(t *testing.T) {
	a, err := app.New(context.Background(), app.Options{Provider: "fake", SessionPath: filepath.Join(t.TempDir(), "session.db"), CWD: t.TempDir(), Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	srv := New(context.Background(), a, strings.NewReader(""), &out)
	err = srv.handle(context.Background(), Request{ID: "d1", Type: "branch_delete", Params: json.RawMessage(`{"branch_id":"missing-branch"}`)})
	if err == nil {
		t.Fatal("branch_delete accepted a missing branch")
	}
}
