package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compactionRPCApp(t *testing.T, seed bool) (*app.App, *goalRunRPCProvider, protocol.RPCCompactionStartParams) {
	t.Helper()
	a, p, _ := goalRunRPCApp(t)
	if seed {
		for i := range 12 {
			msg := protocol.NewUserMessage(fmt.Sprintf("compact-message-%d", i), a.Session.BranchTip(), strings.Repeat("fixture context ", 128))
			if err := a.Session.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, ParentID: msg.ParentID, Message: &msg}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return a, p, protocol.RPCCompactionStartParams{SessionID: a.Session.ID(), BranchID: a.Session.(session.ActiveBranchStore).ActiveBranchID(), ExpectedTipID: a.Session.BranchTip()}
}

type compactionRPCWriter struct {
	mu       sync.Mutex
	output   bytes.Buffer
	provider *goalRunRPCProvider
	app      *app.App
	fail     bool
	early    bool
}

func (*compactionRPCWriter) RPCWriteBounded() bool { return true }
func (w *compactionRPCWriter) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if bytes.Contains(raw, []byte(`"command":"compaction_start"`)) && bytes.Contains(raw, []byte(`"success":true`)) {
		if w.provider.calls.Load() != 0 {
			w.early = true
			return 0, errors.New("provider before ACK")
		}
		if w.fail {
			return 0, errors.New("PRIVATE failed transport")
		}
	}
	if bytes.Contains(raw, []byte(`"type":"compaction_completed"`)) && w.app.Agent.IsRunning() {
		w.early = true
	}
	return w.output.Write(raw)
}
func (w *compactionRPCWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.output.Bytes())
}

func TestCompactionRPCACKBeforeProviderAndOneCleanCompletion(t *testing.T) {
	for _, seed := range []bool{false, true} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			a, p, params := compactionRPCApp(t, seed)
			w := &compactionRPCWriter{provider: p, app: a}
			srv := New(t.Context(), a, strings.NewReader(""), w)
			raw, _ := json.Marshal(params)
			if err := srv.handle(t.Context(), Request{ID: "compact-request", Type: "compaction_start", Params: raw}); err != nil {
				t.Fatal(err)
			}
			srv.promptWG.Wait()
			frames := bytes.Split(bytes.TrimSpace(w.bytes()), []byte{'\n'})
			if len(frames) != 2 || w.early {
				t.Fatalf("early=%v frames=%s", w.early, w.bytes())
			}
			var ack struct {
				Data protocol.RPCCompactionAccepted `json:"data"`
			}
			var completed protocol.RPCCompactionCompleted
			if json.Unmarshal(frames[0], &ack) != nil || json.Unmarshal(frames[1], &completed) != nil {
				t.Fatal("invalid frames")
			}
			if completed.Type != protocol.RPCTypeCompactionCompleted || completed.RequestID != "compact-request" || completed.CompactionID != ack.Data.CompactionID || completed.TurnID != ack.Data.TurnID || completed.RootEpoch != ack.Data.RootEpoch || completed.SessionID != params.SessionID || completed.BranchID != params.BranchID {
				t.Fatalf("ack=%+v completion=%+v", ack, completed)
			}
			if !seed && (completed.Status != "noop" || p.calls.Load() != 0) {
				t.Fatalf("noop=%+v calls=%d", completed, p.calls.Load())
			}
			if seed && p.calls.Load() == 0 {
				t.Fatal("seed never exercised provider")
			}
			if srv.promptDone != nil || srv.cancel != nil || bytes.Contains(w.bytes(), []byte("PRIVATE")) {
				t.Fatal("leaked lifecycle/private data")
			}
		})
	}
}

func TestCompactionRPCLostACKCancelsJoinsWithoutProvider(t *testing.T) {
	a, p, params := compactionRPCApp(t, true)
	w := &compactionRPCWriter{provider: p, app: a, fail: true}
	srv := New(t.Context(), a, strings.NewReader(""), w)
	raw, _ := json.Marshal(params)
	err := srv.handle(t.Context(), Request{ID: "lost", Type: "compaction_start", Params: raw})
	if !errors.Is(err, app.ErrCompactionOutcomeUnknown) || errors.Is(err, app.ErrCompactionRejected) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("unsafe outcome=%v", err)
	}
	if p.calls.Load() != 0 || a.Agent.IsRunning() || srv.promptDone != nil || a.Session.BranchTip() != params.ExpectedTipID {
		t.Fatal("failed ACK performed work or leaked owner")
	}
	if bytes.Contains(w.bytes(), []byte(protocol.RPCTypeCompactionCompleted)) {
		t.Fatal("unacknowledged run published terminal")
	}
}

func TestCompactionRPCServeResponsiveToAbortDuringSummary(t *testing.T) {
	a, p, params := compactionRPCApp(t, true)
	p.block = true
	w := &compactionRPCWriter{provider: p, app: a}
	input, send := io.Pipe()
	defer send.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srv := New(ctx, a, input, w)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx) }()
	raw, _ := json.Marshal(Request{ID: "blocked", Type: "compaction_start", Params: mustCompactionParams(t, params)})
	if _, err := send.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("summary never started")
	}
	if _, err := io.WriteString(send, "{\"id\":\"stop\",\"type\":\"abort\"}\n"); err != nil {
		t.Fatal(err)
	}
	_ = send.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("RPC failed to process Abort while provider blocked")
	}
	output := w.bytes()
	if bytes.Count(output, []byte(`"type":"compaction_completed"`)) != 1 || !bytes.Contains(output, []byte(`"status":"canceled"`)) || !bytes.Contains(output, []byte(`"command":"abort","success":true`)) || a.Agent.IsRunning() {
		t.Fatalf("output=%s", output)
	}
}
func mustCompactionParams(t *testing.T, p protocol.RPCCompactionStartParams) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCompactionRPCRejectsStaleBindingWithoutMutation(t *testing.T) {
	a, p, params := compactionRPCApp(t, true)
	for name, change := range map[string]func(*protocol.RPCCompactionStartParams){
		"session":             func(p *protocol.RPCCompactionStartParams) { p.SessionID = "stale" },
		"branch":              func(p *protocol.RPCCompactionStartParams) { p.BranchID = "stale" },
		"tip":                 func(p *protocol.RPCCompactionStartParams) { p.ExpectedTipID = "stale" },
		"empty tip assertion": func(p *protocol.RPCCompactionStartParams) { p.ExpectedTipID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := params
			change(&bad)
			var output bytes.Buffer
			srv := New(t.Context(), a, strings.NewReader(""), &output)
			err := srv.handle(t.Context(), Request{ID: "stale", Type: "compaction_start", Params: mustCompactionParams(t, bad)})
			if !errors.Is(err, app.ErrCompactionRejected) || p.calls.Load() != 0 || a.Session.BranchTip() != params.ExpectedTipID || srv.promptDone != nil || output.Len() != 0 {
				t.Fatalf("stale mutated: %v", err)
			}
		})
	}
}

type rpcCompactionCleanupStore struct {
	*session.MemoryStore
	started, release chan struct{}
	once             sync.Once
}

func (s *rpcCompactionCleanupStore) AppendBatch(entries []session.Entry) error {
	for _, entry := range entries {
		if entry.Message != nil && entry.Message.Role == protocol.RoleAgent {
			s.once.Do(func() { close(s.started) })
			<-s.release
			break
		}
	}
	return s.MemoryStore.AppendBatch(entries)
}

func TestCompactionRPCCompletionWaitsForMailboxCleanup(t *testing.T) {
	a, p, _ := compactionRPCApp(t, false)
	p.block = true
	store := &rpcCompactionCleanupStore{MemoryStore: session.NewMemoryStore(session.Options{}), started: make(chan struct{}), release: make(chan struct{})}
	if err := a.SetSession(store); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		msg := protocol.NewUserMessage(fmt.Sprintf("cleanup-%d", i), store.BranchTip(), strings.Repeat("context ", 128))
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, ParentID: msg.ParentID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	params := protocol.RPCCompactionStartParams{SessionID: store.ID(), BranchID: store.ActiveBranchID(), ExpectedTipID: store.BranchTip()}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	release := sync.OnceFunc(func() { close(store.release) })
	defer release()
	w := &compactionRPCWriter{provider: p, app: a}
	srv := New(ctx, a, strings.NewReader(""), w)
	if err := srv.handle(ctx, Request{ID: "cleanup", Type: "compaction_start", Params: mustCompactionParams(t, params)}); err != nil {
		t.Fatal(err)
	}
	defer func() { release(); cancel(); srv.promptWG.Wait() }()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("provider did not start")
	}
	if err := a.Agent.EnqueueMailbox(protocol.AgentMessage{ID: "cleanup-mail", Author: protocol.AgentPath("/root/test"), Recipient: protocol.RootAgentPath, Kind: protocol.AgentMessageNormal, Content: "pending mailbox", CreatedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	cancelRun := srv.cancel
	done := srv.promptDone
	srv.mu.Unlock()
	cancelRun()
	select {
	case <-store.started:
	case <-ctx.Done():
		t.Fatal("mailbox cleanup did not start")
	}
	if bytes.Contains(w.bytes(), []byte(protocol.RPCTypeCompactionCompleted)) {
		t.Fatal("terminal before mailbox persistence")
	}
	select {
	case <-done:
		t.Fatal("RPC lifecycle released before cleanup")
	default:
	}
	if err := srv.handle(ctx, Request{ID: "replacement", Type: "compaction_start", Params: mustCompactionParams(t, params)}); !errors.Is(err, app.ErrCompactionRejected) {
		t.Fatalf("cleanup owner replaced: %v", err)
	}
	release()
	srv.promptWG.Wait()
	if bytes.Count(w.bytes(), []byte(`"type":"compaction_completed"`)) != 1 || !bytes.Contains(w.bytes(), []byte(`"request_id":"cleanup"`)) || !bytes.Contains(w.bytes(), []byte(`"status":"canceled"`)) {
		t.Fatalf("terminal=%s", w.bytes())
	}
}

func TestCompactionRPCDrainsNativeEventsBeforeTerminal(t *testing.T) {
	a, p, params := compactionRPCApp(t, true)
	w := &compactionRPCWriter{provider: p, app: a}
	srv := New(t.Context(), a, strings.NewReader(""), w)
	nativeDone := make(chan struct{})
	releaseNative := make(chan struct{})
	var once sync.Once
	release := sync.OnceFunc(func() { close(releaseNative) })
	defer release()
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvCompactionDone {
			once.Do(func() { close(nativeDone) })
			<-releaseNative
		}
	})
	defer unsubscribe()
	stopEvents := srv.forwardAgentEvents()
	defer func() {
		release()
		if err := stopEvents(); err != nil {
			t.Error(err)
		}
	}()
	if err := srv.handle(t.Context(), Request{ID: "drain", Type: "compaction_start", Params: mustCompactionParams(t, params)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-nativeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("no native compaction_done")
	}
	if bytes.Contains(w.bytes(), []byte(protocol.RPCTypeCompactionCompleted)) {
		t.Fatal("terminal escaped native event drain")
	}
	release()
	srv.promptWG.Wait()
	output := w.bytes()
	nativeIndex := bytes.Index(output, []byte(`"type":"compaction_done"`))
	terminalIndex := bytes.Index(output, []byte(`"type":"compaction_completed"`))
	if nativeIndex < 0 || terminalIndex < nativeIndex || bytes.Count(output, []byte(`"type":"compaction_completed"`)) != 1 {
		t.Fatalf("bad event ordering: %s", output)
	}
}
