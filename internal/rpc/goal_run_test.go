package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type goalRunRPCProvider struct {
	provider.Provider
	calls   atomic.Int32
	block   bool
	started chan struct{}
}

func (p *goalRunRPCProvider) Chat(ctx context.Context, _ protocol.ChatRequest) (protocol.EventStream, error) {
	if p.calls.Add(1) == 1 && p.started != nil {
		close(p.started)
	}
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &rpcEventStream{events: []protocol.StreamEvent{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}, nil
}
func goalRunRPCApp(t *testing.T) (*app.App, *goalRunRPCProvider, protocol.RPCGoalRunParams) {
	t.Helper()
	a, err := app.New(t.Context(), app.Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "deny", ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "goal-run.db"), a.CWD(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSession(st); err != nil {
		t.Fatal(err)
	}
	p := &goalRunRPCProvider{Provider: a.Provider, started: make(chan struct{})}
	if err := a.Agent.SetProvider(p); err != nil {
		t.Fatal(err)
	}
	return a, p, protocol.RPCGoalRunParams{Action: "create", SessionID: st.ID(), BranchID: st.ActiveBranchID(), ExpectedTipID: st.BranchTip(), Objective: "explicit objective", TokenBudget: new(int64(1000))}
}

type goalRunACKWriter struct {
	bytes.Buffer
	provider *goalRunRPCProvider
	fail     bool
	sawACK   bool
}

func (*goalRunACKWriter) RPCWriteBounded() bool { return true }

func (w *goalRunACKWriter) Write(raw []byte) (int, error) {
	if bytes.Contains(raw, []byte(`"command":"goal_run"`)) {
		if w.provider.calls.Load() != 0 {
			return 0, errors.New("provider executed before ACK")
		}
		w.sawACK = true
		if w.fail {
			return 0, errors.New("ACK transport lost")
		}
	}
	return w.Buffer.Write(raw)
}
func TestGoalRunRPCACKAndOneCompletionAcrossNativeTurns(t *testing.T) {
	a, p, params := goalRunRPCApp(t)
	writer := &goalRunACKWriter{provider: p}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	raw, _ := json.Marshal(params)
	if err := srv.handleGoalRun(t.Context(), Request{ID: "owned-request", Type: "goal_run", Params: raw}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	if !writer.sawACK || p.calls.Load() != 3 {
		t.Fatalf("ACK=%v native calls=%d", writer.sawACK, p.calls.Load())
	}
	frames := bytes.Split(bytes.TrimSpace(writer.Bytes()), []byte{'\n'})
	if len(frames) != 2 {
		t.Fatalf("frames=%s", writer.Bytes())
	}
	var accepted struct {
		Data protocol.RPCGoalRunAccepted `json:"data"`
	}
	if err := json.Unmarshal(frames[0], &accepted); err != nil {
		t.Fatal(err)
	}
	var completed protocol.RPCGoalRunCompleted
	if err := json.Unmarshal(frames[1], &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Type != protocol.RPCTypeGoalRunCompleted || completed.RequestID != "owned-request" || completed.GoalRunID != accepted.Data.GoalRunID || completed.GoalID != accepted.Data.GoalID || completed.Status != "finished" || completed.GoalStatus != protocol.GoalPaused {
		t.Fatalf("completion=%+v", completed)
	}
	if srv.promptDone != nil || srv.cancel != nil {
		t.Fatal("RPC lifecycle leaked")
	}
}
func TestGoalRunRPCLostACKCancelsAndJoins(t *testing.T) {
	a, p, params := goalRunRPCApp(t)
	writer := &goalRunACKWriter{provider: p, fail: true}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	raw, _ := json.Marshal(params)
	if err := srv.handleGoalRun(t.Context(), Request{ID: "lost", Type: "goal_run", Params: raw}); !errors.Is(err, app.ErrGoalRunOutcomeUnknown) || errors.Is(err, app.ErrGoalRunRejected) {
		t.Fatalf("lost ACK did not require authoritative refresh: %v", err)
	}
	if p.calls.Load() != 0 || a.Agent.GoalRunID() != "" || srv.promptDone != nil {
		t.Fatal("lost ACK leaked provider work/ownership")
	}
	deferred, err := a.Goal.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
}
func TestGoalRunRPCAbortUsesSharedLifecycle(t *testing.T) {
	a, p, params := goalRunRPCApp(t)
	p.block = true
	writer := &goalRunACKWriter{provider: p}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	raw, _ := json.Marshal(params)
	if err := srv.handleGoalRun(t.Context(), Request{ID: "stop", Type: "goal_run", Params: raw}); err != nil {
		t.Fatal(err)
	}
	<-p.started
	if err := srv.handle(t.Context(), Request{ID: "abort", Type: "abort"}); err != nil {
		t.Fatal(err)
	}
	srv.promptWG.Wait()
	if a.Agent.GoalRunID() != "" {
		t.Fatal("abort did not join owner")
	}
	count := 0
	for _, frame := range bytes.Split(bytes.TrimSpace(writer.Bytes()), []byte{'\n'}) {
		var completed protocol.RPCGoalRunCompleted
		if err := json.Unmarshal(frame, &completed); err != nil {
			t.Fatal(err)
		}
		if completed.Type == protocol.RPCTypeGoalRunCompleted {
			count++
			if completed.Status != "canceled" || completed.RequestID != "stop" {
				t.Fatalf("completion=%+v", completed)
			}
		}
	}
	if count != 1 {
		t.Fatalf("completion count=%d", count)
	}
}
func TestGoalRunRPCRejectsMissingAssertionsAndResumeMutationFields(t *testing.T) {
	a, p, _ := goalRunRPCApp(t)
	for _, params := range []string{
		`{}`, `{"action":"create","session_id":"s","branch_id":"b","objective":"x","token_budget":1}`,
		`{"action":"resume","session_id":"s","branch_id":"b","expected_tip_id":"","expected_goal_id":"g","objective":""}`,
		`{"action":"resume","session_id":"s","branch_id":"b","expected_tip_id":"","expected_goal_id":"g","token_budget":null}`,
		`{"action":"create","session_id":"s","branch_id":"b","expected_tip_id":null,"expected_goal_id":"","objective":"x","token_budget":1}`,
	} {
		var out bytes.Buffer
		srv := New(t.Context(), a, strings.NewReader(""), &out)
		if err := srv.handleGoalRun(t.Context(), Request{ID: "reject", Type: "goal_run", Params: []byte(params)}); err == nil {
			t.Fatalf("accepted %s", params)
		}
	}
	if p.calls.Load() != 0 {
		t.Fatal("rejected operation executed")
	}
}

func TestGoalRunRPCContextLossJoinsSharedLifecycle(t *testing.T) {
	a, p, params := goalRunRPCApp(t)
	p.block = true
	writer := &goalRunACKWriter{provider: p}
	srv := New(t.Context(), a, strings.NewReader(""), writer)
	ctx, cancel := context.WithCancel(t.Context())
	raw, _ := json.Marshal(params)
	if err := srv.handleGoalRun(ctx, Request{ID: "eof", Type: "goal_run", Params: raw}); err != nil {
		t.Fatal(err)
	}
	// Serve's EOF path cancels this same parent context before waiting on the
	// shared promptDone slot. Exercise both the active provider and its join.
	<-p.started
	srv.mu.Lock()
	done := srv.promptDone
	srv.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("context loss did not join run")
	}
	srv.promptWG.Wait()
	if a.Agent.GoalRunID() != "" {
		t.Fatal("context loss leaked owner")
	}
	deferred, err := a.Goal.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
	if bytes.Count(writer.Bytes(), []byte(`"type":"goal_run_completed"`)) != 1 {
		t.Fatalf("completion stream=%s", writer.Bytes())
	}
}

func TestGoalRunRPCServeEOFJoinsOwnedRun(t *testing.T) {
	a, p, params := goalRunRPCApp(t)
	p.block = true
	raw, _ := json.Marshal(params)
	frame, _ := json.Marshal(Request{ID: "eof-run", Type: "goal_run", Params: raw})
	writer := &goalRunACKWriter{provider: p}
	srv := New(t.Context(), a, strings.NewReader(string(frame)+"\n"), writer)
	if err := srv.Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !writer.sawACK || a.Agent.GoalRunID() != "" {
		t.Fatal("EOF did not admit then join the owned run")
	}
	if bytes.Count(writer.Bytes(), []byte(`"type":"goal_run_completed"`)) != 1 {
		t.Fatalf("completion stream=%s", writer.Bytes())
	}
	if !bytes.Contains(writer.Bytes(), []byte(`"status":"canceled"`)) {
		t.Fatalf("EOF not canceled: %s", writer.Bytes())
	}
	deferred, err := a.Goal.Deferred()
	if err != nil || !deferred {
		t.Fatalf("deferred=%v err=%v", deferred, err)
	}
}
