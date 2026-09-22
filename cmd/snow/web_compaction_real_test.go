//go:build darwin || linux

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/web"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const compactionRealEnv = "SNOW_WEB_COMPACTION_REAL_DIR"

func init() {
	if directory := os.Getenv(compactionRealEnv); directory != "" && slices.Contains(os.Args, "--mode") {
		code := runCompactionRealWorker(directory)
		_ = os.WriteFile(filepath.Join(directory, "worker-exit"), []byte(strconv.Itoa(code)), 0600)
		os.Exit(code)
	}
}

// Only the provider and its pipe transport are substituted. CLI options,
// exact native operation handles, RPC lifecycle, durable SQLite, HTTP authority,
// manager refresh and cancellation all use production implementations.
func runCompactionRealWorker(directory string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 90
	}
	if _, err := permissionFixtureProject(directory, cwd); err != nil {
		return 91
	}
	if os.Getenv("HOME") != filepath.Join(directory, "home") || os.Getenv("SNOW_HOME") != filepath.Join(directory, "home", ".snow") || os.Getenv("SNOW_SESSIONS_DIR") != filepath.Join(directory, "sessions") {
		return 92
	}
	opts, startup, err := managerExecutionOptions(os.Args[1:])
	if err != nil {
		return 93
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if startup == "catalog" {
		if rpc.CatalogMain(ctx, cwd, session.DefaultSessionsRoot(), "compaction-real") != nil {
			return 94
		}
		return 0
	}
	if startup != "eager" || opts.Provider != "fake" || opts.Model != "fake-1" || !opts.ManagedExplicitGoals || !opts.NoSession || !opts.NoPlugins || opts.NoMCP || !opts.NoSkills || opts.Subagents == nil || !*opts.Subagents {
		return 95
	}
	a, err := app.New(ctx, opts)
	if err != nil {
		return 96
	}
	defer a.Close()
	mode, _ := os.ReadFile(filepath.Join(directory, "fixture-mode"))
	p := &compactionRealProvider{Provider: fake.New(nil), directory: directory, mode: string(mode)}
	a.Providers["fake"] = p
	if err := a.SetProvider("fake"); err != nil {
		return 97
	}
	output := &compactionRealOutput{out: &permissionFixtureOutput{out: os.Stdout}, directory: directory, mode: string(mode), ctx: ctx}
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			cancel()
			return
		}
		if event.TurnOrigin == "compact" {
			log, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				cancel()
				return
			}
			_, err = log.Write(append(slices.Clone(data), '\n'))
			_ = log.Close()
			if err != nil {
				cancel()
				return
			}
		}
		if _, err := output.Write(append(data, '\n')); err != nil {
			cancel()
			return
		}
		if event.Type == protocol.EvCompactionDone && string(mode) == "progress" {
			if err := permissionFixtureGate(ctx, filepath.Join(directory, "release-native-drain")); err != nil {
				cancel()
			}
		}
	})
	defer unsubscribe()
	// Preserve real pipe descriptors, rather than the descriptor-hiding wrapper
	// used by some older fixtures. Neither existing fixture is modified here.
	if err := rpc.New(ctx, a, compactionRealInput{os.Stdin}, output).Serve(ctx); err != nil {
		return 98
	}
	return 0
}

type compactionRealInput struct{ *os.File }

func (compactionRealInput) InterruptsReadOnClose() bool { return true }

type compactionRealOutput struct {
	out             *permissionFixtureOutput
	directory, mode string
	ctx             context.Context
	mu              sync.Mutex
	heldACK         []byte
}

func (*compactionRealOutput) RPCWriteBounded() bool { return true }
func (*compactionRealOutput) Fd() uintptr           { return os.Stdout.Fd() }
func (w *compactionRealOutput) Write(data []byte) (int, error) {
	var frame struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return 0, err
	}
	if frame.Type == "response" && frame.Command == "compaction_start" && frame.Success {
		if err := os.WriteFile(filepath.Join(w.directory, "native-ack"), []byte("captured"), 0600); err != nil {
			return 0, err
		}
		if w.mode == "preack" {
			if err := permissionFixtureGate(w.ctx, filepath.Join(w.directory, "release-ack")); err != nil {
				return 0, err
			}
		}
		if w.mode == "early" {
			// A transport scheduling adversary buffers the genuine native ACK, then
			// delivers genuine completion first. No frame or native outcome is invented.
			w.mu.Lock()
			w.heldACK = slices.Clone(data)
			w.mu.Unlock()
			return len(data), nil
		}
	}
	n, err := w.out.Write(data)
	if err != nil {
		return n, err
	}
	if frame.Type == protocol.RPCTypeCompactionCompleted && w.mode == "early" {
		w.mu.Lock()
		ack := w.heldACK
		w.heldACK = nil
		w.mu.Unlock()
		if len(ack) == 0 {
			return 0, errors.New("native completion preceded acceptance")
		}
		if _, err := w.out.Write(ack); err != nil {
			return 0, err
		}
	}
	return n, nil
}

type compactionRealProvider struct {
	*fake.Provider
	directory, mode string
	mu              sync.Mutex
}

func (p *compactionRealProvider) Chat(ctx context.Context, request protocol.ChatRequest) (protocol.EventStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, _ := os.ReadFile(filepath.Join(p.directory, "provider-count"))
	call, _ := strconv.Atoi(string(raw))
	call++
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(p.directory, fmt.Sprintf("request-%d.json", call)), data, 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(p.directory, "provider-count"), []byte(strconv.Itoa(call)), 0600); err != nil {
		return nil, err
	}
	summary := strings.HasPrefix(request.System, "Create a factual working-state checkpoint")
	if summary {
		if _, err := os.Stat(filepath.Join(p.directory, "native-ack")); err != nil {
			return nil, errors.New("provider work before captured native acknowledgment")
		}
		if len(request.Tools) != 0 {
			return nil, errors.New("summary request exposed tools")
		}
	}
	text := "Fictional next reply"
	if summary {
		text = compact.WorkingStateTitle
		for _, section := range compact.WorkingStateSections {
			text += "\n## " + section + "\nFictional retained state; no outstanding action.\n"
		}
	}
	stream, err := fake.New([]fake.Step{{Kind: fake.StepText, Text: text}, {Kind: fake.StepDone, Stop: protocol.StopStop}}).Chat(ctx, request)
	if err != nil {
		return nil, err
	}
	return &compactionRealStream{EventStream: stream, directory: p.directory, call: call, gate: summary && p.mode != "early", cleanup: summary && (p.mode == "cleanup" || p.mode == "transport-loss"), loseTransport: summary && p.mode == "transport-loss"}, nil
}

type compactionRealStream struct {
	protocol.EventStream
	directory     string
	call          int
	gate, cleanup bool
	loseTransport bool
}

func (s *compactionRealStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.gate {
		s.gate = false
		if err := permissionFixtureGate(ctx, filepath.Join(s.directory, fmt.Sprintf("release-provider-%d", s.call))); err != nil {
			return protocol.StreamEvent{}, err
		}
	}
	return s.EventStream.Next(ctx)
}
func (s *compactionRealStream) Close() error {
	if s.cleanup {
		if err := os.WriteFile(filepath.Join(s.directory, "cleanup-entered"), nil, 0600); err != nil {
			return err
		}
		// Real stream cleanup deliberately outlives the canceled provider context.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if s.loseTransport {
			// This stream exists only in the private child. Close its real RPC
			// output pipe while native provider cleanup still owns the operation.
			if err := permissionFixtureGate(ctx, filepath.Join(s.directory, "drop-event-stream")); err != nil {
				return err
			}
			if err := os.Stdout.Close(); err != nil {
				return err
			}
		}
		if err := permissionFixtureGate(ctx, filepath.Join(s.directory, "release-cleanup")); err != nil {
			return err
		}
	}
	return s.EventStream.Close()
}

type compactionRealSeed struct {
	sessionID, path string
	messages        []protocol.Message
	goal            *protocol.ThreadGoal
	deferred        bool
}

func newCompactionRealHTTP(t *testing.T, mode string) (*managerExecutionHTTP, compactionRealSeed, web.RuntimeSnapshot) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	// RuntimeManager captures the launcher environment during construction.
	// Register this child selector before creating the real HTTP server.
	t.Setenv(compactionRealEnv, directory)
	f := newManagerExecutionHTTP(t, directory)
	t.Setenv(managerExecutionEnv, "")
	f.write("fixture-mode", mode)
	cfg := config.Default()
	cfg.DefaultProvider = "fake"
	cfg.DefaultModel = "fake-1"
	cfg.Compaction.RetainTokens = 1
	cfg.Compaction.MinRetainedTurns = 2
	cfg.Compaction.AutoThresholdPercent = 0
	cfg.Compaction.ToolHistoryBudgetPercent = 0
	if err := config.Save(filepath.Join(f.directory, "home", ".snow", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(t.Context(), app.Options{CWD: filepath.Join(f.directory, "project-a"), Provider: "fake", Model: "fake-1", Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, Subagents: new(false), Debug: new(false), ManagedExplicitGoals: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	seed := compactionRealSeed{sessionID: a.Session.ID(), path: a.Session.Path()}
	appendMessage := func(m protocol.Message) {
		t.Helper()
		m.ParentID = a.Session.BranchTip()
		if err := a.Session.Append(session.Entry{ID: m.ID, Type: session.EntryMessage, Message: &m}); err != nil {
			t.Fatal(err)
		}
		seed.messages = append(seed.messages, m)
	}
	for i := range 5 {
		appendMessage(protocol.NewUserMessage(fmt.Sprintf("fictional-user-%d", i), "", fmt.Sprintf("fictional historical request %d", i)))
		if i == 0 {
			appendMessage(protocol.Message{ID: "fictional-tool-owner", Role: protocol.RoleAssistant, StopReason: protocol.StopToolUse, Content: []protocol.ContentBlock{{Type: protocol.BlockProviderData, Data: []byte("PRIVATE-COMPACTION-CONTINUITY")}, {Type: protocol.BlockToolCall, ToolCallID: "old-write", Name: "write", Arguments: []byte(`{"path":"fictional-must-not-exist.txt","content":"must-not-replay"}`)}}})
			appendMessage(protocol.Message{ID: "fictional-tool-result", Role: protocol.RoleTool, ToolCallID: "old-write", ToolName: "write", Content: []protocol.ContentBlock{protocol.NewTextBlock("fictional old result")}, PublicToolResult: &protocol.ToolResultPreview{Text: "fictional public result"}})
		}
		appendMessage(protocol.Message{ID: fmt.Sprintf("fictional-assistant-%d", i), Role: protocol.RoleAssistant, StopReason: protocol.StopStop, Content: []protocol.ContentBlock{protocol.NewTextBlock(fmt.Sprintf("fictional old answer %d", i))}})
	}
	if kind, ok := strings.CutPrefix(mode, "goal-"); ok {
		created, err := a.Goal.Create("Fictional existing nonterminal objective", new(int64(12345)), false)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "active":
		case "deferred":
			if err := a.Goal.Defer(true); err != nil {
				t.Fatal(err)
			}
		case "paused", "blocked":
			if _, err := a.Goal.SetStatus(created.GoalID, protocol.ThreadGoalStatus(kind), false); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unknown fixture goal status: %s", kind)
		}
		seed.goal, err = a.Goal.Get()
		if err != nil {
			t.Fatal(err)
		}
		seed.deferred, err = a.Goal.Deferred()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	s := f.inspect(f.open(seed.sessionID))
	if !s.CompactionEnabled || s.Mode != "plan" || s.Status != "idle" || f.count() != 0 {
		t.Fatalf("invalid initial native scope: %+v", s)
	}
	return f, seed, s
}
func compactionRealForm(s web.RuntimeSnapshot) url.Values {
	return url.Values{"instance_id": {s.InstanceID}, "session_id": {s.SessionID}, "branch_id": {s.Goal.BranchID}, "expected_tip_id": {s.Goal.TipID}, "expected_revision": {strconv.FormatUint(s.Revision, 10)}}
}
func compactionRealFile(directory, name string) bool {
	_, err := os.Stat(filepath.Join(directory, name))
	return err == nil
}
func compactionRealStart(f *managerExecutionHTTP, s web.RuntimeSnapshot) web.RuntimeSnapshot {
	f.t.Helper()
	var result web.RuntimeSnapshot
	f.runtime("compaction-start", compactionRealForm(s), &result)
	if result.CompactionACK == nil || result.CompactionACK.CompactionID == "" || result.CompactionACK.SessionID != s.SessionID || result.CompactionACK.BranchID != s.Goal.BranchID || result.CompactionACK.TurnOrigin != "compact" {
		f.t.Fatal("missing immutable captured native receipt")
	}
	return result
}
func compactionRealRejectPrompt(f *managerExecutionHTTP, s web.RuntimeSnapshot) {
	f.t.Helper()
	code, _ := f.request("POST", "/projects/"+f.project+"/runtime/prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"must not be admitted"}})
	if code != http.StatusConflict {
		f.t.Fatalf("busy manual owner accepted prompt: %d", code)
	}
}
func compactionRealAssertHistory(f *managerExecutionHTTP, seed compactionRealSeed, next bool) {
	f.t.Helper()
	f.runtime("close", url.Values{"instance_id": {f.snapshot().InstanceID}}, nil)
	store, err := session.OpenSQLiteStore(seed.path, filepath.Join(f.directory, "project-a"), session.Options{})
	if err != nil {
		f.t.Fatal(err)
	}
	defer store.Close()
	history, err := store.Messages()
	if err != nil {
		f.t.Fatal(err)
	}
	want := len(seed.messages)
	if next {
		want += 2
	}
	if len(history) != want || !reflect.DeepEqual(history[:len(seed.messages)], seed.messages) {
		f.t.Fatal("manual compaction rewrote parent-linked history, fabricated a user, or replayed tools")
	}
	if _, err := os.Stat(filepath.Join(f.directory, "project-a", "fictional-must-not-exist.txt")); !errors.Is(err, os.ErrNotExist) {
		f.t.Fatal("historical tool executed")
	}
	if mode, err := store.CollaborationMode(); err != nil || mode != protocol.ModePlan {
		f.t.Fatal("manual work changed persisted Plan Mode")
	}
}
func compactionRealNextPrompt(f *managerExecutionHTTP, s web.RuntimeSnapshot) {
	f.t.Helper()
	before := f.count()
	f.runtime("prompt", url.Values{"instance_id": {s.InstanceID}, "text": {"fictional explicit next request"}}, nil)
	done := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && f.count() == before+1 })
	if done.Mode != "plan" || done.CancelToken != "" || done.CancelRequested {
		f.t.Fatal("next turn lost Plan Mode or retained cancellation ownership")
	}
	data, err := os.ReadFile(filepath.Join(f.directory, fmt.Sprintf("request-%d.json", before+1)))
	if err != nil {
		f.t.Fatal(err)
	}
	var request protocol.ChatRequest
	if err := json.Unmarshal(data, &request); err != nil {
		f.t.Fatal(err)
	}
	if !slices.ContainsFunc(request.Messages, func(m protocol.Message) bool {
		return m.Role == protocol.RoleUser && slices.ContainsFunc(m.Content, func(b protocol.ContentBlock) bool {
			return b.Type == protocol.BlockText && b.Text == "fictional explicit next request"
		})
	}) {
		f.t.Fatal("next native prompt did not reach provider")
	}
}

func compactionRealAssertNative(f *managerExecutionHTTP, ack protocol.RPCCompactionAccepted) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.directory, "native-events.jsonl"))
	if err != nil {
		f.t.Fatal(err)
	}
	started, progress := 0, 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var event protocol.AgentEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			f.t.Fatal(err)
		}
		if event.TurnID != ack.TurnID || event.TurnOrigin != "compact" || event.RootEpoch != ack.RootEpoch || event.TurnSequence != ack.TurnSequence {
			f.t.Fatal("native manual event escaped captured turn identity")
		}
		switch event.Type {
		case protocol.EvCompactionStarted:
			started++
		case protocol.EvCompactionDone:
			progress++
		case protocol.EvTextDelta, protocol.EvToolStart, protocol.EvToolEnd:
			f.t.Fatal("manual native operation replayed conversational or tool output")
		}
	}
	if started != 1 || progress != 1 {
		f.t.Fatalf("native compaction lifecycle: starts=%d progress=%d", started, progress)
	}
}

func TestWebCompactionRealWorkerProgressAndNextPrompt(t *testing.T) {
	f, seed, before := newCompactionRealHTTP(t, "progress")
	for key, value := range map[string]string{"session_id": "stale-session", "branch_id": "stale-branch", "expected_tip_id": "stale-tip", "expected_revision": "0", "text": "must never become a user message"} {
		bad := compactionRealForm(before)
		bad.Set(key, value)
		code, _ := f.request("POST", "/projects/"+f.project+"/runtime/compaction-start", bad)
		if code != http.StatusConflict || f.count() != 0 {
			t.Fatalf("invalid %s authority reached native execution", key)
		}
	}
	accepted := compactionRealStart(f, before)
	ack := *accepted.CompactionACK
	running := f.wait(func(s web.RuntimeSnapshot) bool {
		return f.count() == 1 && s.Compaction != nil && s.Compaction.State == "running"
	})
	if running.Status != "running" || running.Mode != "plan" || running.CancelToken == "" {
		t.Fatal("ACK was mistaken for completion")
	}
	compactionRealRejectPrompt(f, running)
	f.write("release-provider-1", "release")
	progress := f.wait(func(s web.RuntimeSnapshot) bool { return s.Compaction != nil && s.Compaction.ProgressDone })
	if progress.Status != "running" || progress.Compaction.State != "running" || progress.CancelToken == "" {
		t.Fatal("native compaction_done released browser ownership before event drain")
	}
	compactionRealRejectPrompt(f, progress)
	f.write("release-native-drain", "release")
	done := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	if done.Compaction == nil || done.Compaction.State != "completed" || done.Compaction.SummarizedMessages == 0 || done.Compaction.CompactionID != ack.CompactionID || done.Mode != "plan" || done.CancelToken != "" || f.count() != 1 {
		t.Fatalf("manual completion did not preserve native scope: %+v", done.Compaction)
	}
	compactionRealAssertNative(f, ack)
	if *accepted.CompactionACK != ack {
		t.Fatal("later completion changed acceptance receipt")
	}
	for _, m := range done.Messages {
		if strings.Contains(m.Text, "Working State Checkpoint") || strings.Contains(m.Text, "PRIVATE-COMPACTION") {
			t.Fatal("provider-only summary/private data entered browser history")
		}
	}
	f.inspect(done)
	f.snapshot()
	if f.count() != 1 {
		t.Fatal("read-only refresh retried compaction")
	}
	compactionRealNextPrompt(f, done)
	compactionRealAssertHistory(f, seed, true)
}

func TestWebCompactionRealWorkerStopBeforeACK(t *testing.T) {
	f, seed, before := newCompactionRealHTTP(t, "preack")
	result := make(chan web.RuntimeSnapshot, 1)
	go func() { result <- compactionRealStart(f, before) }()
	pending := f.wait(func(s web.RuntimeSnapshot) bool {
		return s.Compaction != nil && s.Compaction.State == "pending" && compactionRealFile(f.directory, "native-ack")
	})
	if pending.CancelToken == "" || f.count() != 0 {
		t.Fatal("provider started before native ACK release or pending Stop missing")
	}
	f.runtime("cancel", url.Values{"instance_id": {pending.InstanceID}, "cancel_token": {pending.CancelToken}}, nil)
	latched := f.snapshot()
	if !latched.CancelRequested || latched.Status != "running" || f.count() != 0 {
		t.Fatal("pre-ACK Stop lost intent or claimed completion")
	}
	compactionRealRejectPrompt(f, latched)
	f.write("release-ack", "release")
	select {
	case accepted := <-result:
		if accepted.CompactionACK == nil {
			t.Fatal("Stop erased accepted receipt")
		}
	case <-time.After(12 * time.Second):
		t.Fatal("native ACK did not return")
	}
	done := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if done.Compaction.State != "canceled" || done.CancelToken != "" || f.count() > 1 {
		t.Fatalf("pre-ACK cancellation retried or fabricated outcome: %+v", done.Compaction)
	}
	compactionRealNextPrompt(f, done)
	compactionRealAssertHistory(f, seed, true)
}

func TestWebCompactionRealWorkerStopWaitsForSummaryCleanup(t *testing.T) {
	f, seed, before := newCompactionRealHTTP(t, "cleanup")
	accepted := compactionRealStart(f, before)
	running := f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 1 && s.Status == "running" })
	f.runtime("cancel", url.Values{"instance_id": {running.InstanceID}, "cancel_token": {running.CancelToken}}, nil)
	cleaning := f.wait(func(s web.RuntimeSnapshot) bool {
		return s.CancelRequested && compactionRealFile(f.directory, "cleanup-entered")
	})
	if cleaning.Status != "running" || cleaning.Compaction.State != "running" || cleaning.Compaction.ProgressDone {
		t.Fatal("cancel ACK released owner before actual summary stream cleanup")
	}
	compactionRealRejectPrompt(f, cleaning)
	f.write("release-cleanup", "release")
	done := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" && !s.CancelRequested })
	if done.Compaction.State != "canceled" || done.Compaction.CompactionID != accepted.CompactionACK.CompactionID || f.count() != 1 {
		t.Fatal("summary Stop lost captured native ownership")
	}
	compactionRealNextPrompt(f, done)
	compactionRealAssertHistory(f, seed, true)
}

func TestWebCompactionRealWorkerCompletionBeforeTransportACK(t *testing.T) {
	f, seed, before := newCompactionRealHTTP(t, "early")
	accepted := compactionRealStart(f, before)
	if accepted.Compaction == nil || accepted.CompactionACK == nil || accepted.Compaction.CompactionID != accepted.CompactionACK.CompactionID || f.count() != 1 {
		t.Fatalf("genuine early completion lost immutable receipt: status=%s compaction=%+v", accepted.Status, accepted.Compaction)
	}
	ack := *accepted.CompactionACK
	// Wire delivery is completion-before-ACK, but the independent reader/event
	// drain goroutines may project completion before OR after the HTTP receipt.
	// Both are legal; neither may lose completion or replay provider work.
	done := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "idle" })
	if done.Compaction.State != "completed" || done.Compaction.CompactionID != ack.CompactionID || done.CompactionACK != nil || *accepted.CompactionACK != ack || f.count() != 1 {
		t.Fatal("early native terminal was lost, retried, or mutated its acceptance receipt")
	}
	compactionRealAssertNative(f, ack)
	compactionRealNextPrompt(f, done)
	compactionRealAssertHistory(f, seed, true)
}

func TestWebCompactionRealWorkerTransportLossDuringCleanup(t *testing.T) {
	f, seed, before := newCompactionRealHTTP(t, "transport-loss")
	accepted := compactionRealStart(f, before)
	running := f.wait(func(s web.RuntimeSnapshot) bool { return f.count() == 1 && s.Status == "running" })
	f.runtime("cancel", url.Values{"instance_id": {running.InstanceID}, "cancel_token": {running.CancelToken}}, nil)
	cleaning := f.wait(func(s web.RuntimeSnapshot) bool {
		return s.CancelRequested && compactionRealFile(f.directory, "cleanup-entered")
	})
	if cleaning.Status != "running" || cleaning.Compaction.State != "running" || cleaning.Compaction.ProgressDone {
		t.Fatal("native ownership released before held cleanup and transport loss")
	}
	f.write("drop-event-stream", "close only the worker event pipe")
	failed := f.wait(func(s web.RuntimeSnapshot) bool { return s.Status == "failed" })
	if failed.Compaction == nil || failed.Compaction.State != "uncertain" || failed.Compaction.CompactionID != accepted.CompactionACK.CompactionID || f.count() != 1 {
		t.Fatalf("lost native completion claimed terminal success or replayed work: %+v", failed.Compaction)
	}
	compactionRealRejectPrompt(f, failed)
	code, _ := f.request("POST", "/projects/"+f.project+"/runtime/compaction-start", compactionRealForm(failed))
	if code != http.StatusConflict {
		t.Fatalf("failed runtime admitted new compaction: %d", code)
	}
	// Cleanup may finish after the stream is lost. It cannot retroactively prove
	// a terminal outcome to this failed browser generation or revive admission.
	f.write("release-cleanup", "release")
	for range 3 {
		current := f.snapshot()
		if current.Status != "failed" || current.Compaction.State != "uncertain" || current.Compaction.CompactionID != accepted.CompactionACK.CompactionID {
			t.Fatal("read-only reconciliation guessed lost native outcome")
		}
	}
	code, _ = f.request("GET", "/?project="+url.QueryEscape(f.project), nil)
	if code != http.StatusOK || f.count() != 1 {
		t.Fatal("page reload replayed the unknown operation")
	}
	compactionRealRejectPrompt(f, f.snapshot())
	compactionRealAssertHistory(f, seed, false)
	if f.count() != 1 {
		t.Fatal("transport failure automatically retried provider work")
	}
}

func TestWebCompactionRealWorkerRejectsExistingNonterminalGoals(t *testing.T) {
	for _, kind := range []string{"active", "deferred", "paused", "blocked"} {
		t.Run(kind, func(t *testing.T) {
			f, seed, before := newCompactionRealHTTP(t, "goal-"+kind)
			if seed.goal == nil || before.Goal == nil || before.Goal.GoalID != seed.goal.GoalID || before.Goal.Status != string(seed.goal.Status) || before.Goal.Deferred != seed.deferred || before.Goal.Running || before.CancelToken != "" {
				t.Fatalf("fixture did not retain the exact nonterminal goal at admission: %+v", before.Goal)
			}
			code, _ := f.request("POST", "/projects/"+f.project+"/runtime/compaction-start", compactionRealForm(before))
			if code != http.StatusConflict || f.count() != 0 {
				t.Fatalf("goal %s allowed provider compaction: status=%d calls=%d", kind, code, f.count())
			}
			after := f.inspect(f.snapshot())
			if !reflect.DeepEqual(before.Goal, after.Goal) || after.Status != "idle" || after.Compaction != nil || after.CancelToken != "" || after.Mode != before.Mode || !reflect.DeepEqual(before.Messages, after.Messages) {
				t.Fatal("rejected context work stopped, deferred, edited, or replaced existing goal/history")
			}
			if compactionRealFile(f.directory, "native-ack") || compactionRealFile(f.directory, "native-events.jsonl") || f.count() != 0 {
				t.Fatal("read-only rejection started a native manual operation")
			}
			compactionRealAssertHistory(f, seed, false)
			// Reopen the actual durable store after worker shutdown, not merely the web
			// projection, to prove rejection never rewrote goal metadata or deferral.
			store, err := session.OpenSQLiteStore(seed.path, filepath.Join(f.directory, "project-a"), session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			actual, err := store.Goal()
			if err != nil {
				t.Fatal(err)
			}
			deferred, err := store.GoalContinuationDeferred()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, seed.goal) || deferred != seed.deferred || f.count() != 0 {
				t.Fatal("nonterminal goal rejection mutated persisted state or started provider work")
			}
		})
	}
}
