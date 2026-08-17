package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/internal/compact"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/provider"
	"github.com/snow-core/snow/internal/session"
	skillspkg "github.com/snow-core/snow/internal/skills"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/internal/tools/builtin"
	"github.com/snow-core/snow/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Scripted provider used in tests
// ---------------------------------------------------------------------------

func TestToolStartMessageIncludesBashCommand(t *testing.T) {
	command := `printf '%s\\n' "hello world"`
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	if got := toolStartMessage("bash", args); got != command {
		t.Fatalf("toolStartMessage() = %q, want %q", got, command)
	}
}

func TestToolStartMessageBoundsFilePath(t *testing.T) {
	path := strings.Repeat("é", 3000)
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	got := toolStartMessage("edit", args)
	if len(got) > 2*1024+len("\n… [tool output preview truncated]") || !strings.HasSuffix(got, "… [tool output preview truncated]") || !utf8.ValidString(got) {
		t.Fatalf("bounded tool path bytes=%d valid=%t suffix=%q", len(got), utf8.ValidString(got), got[max(0, len(got)-50):])
	}
}

func TestFirstPromptCreatesDeterministicSessionTitle(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: st,
		Permission: permission.NewService(permission.ModeAllow, nil),
		Model:      protocol.Model{Provider: p.ID(), ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(context.Background(), "  ## Review\n session naming behavior  "); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.SessionTitle(); got != "Review session naming behavior" {
		t.Fatalf("session title = %q", got)
	}
}

func TestQuietSessionTransactionPreservesLatestTurnUntilCommit(t *testing.T) {
	oldStore := session.NewMemoryStore(session.Options{})
	newStore := session.NewMemoryStore(session.Options{})
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: oldStore,
		Permission: permission.NewService(permission.ModeAllow, nil),
		Model:      protocol.Model{Provider: p.ID(), ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(context.Background(), "establish reconciliation identity"); err != nil {
		t.Fatal(err)
	}
	wantOrigin, wantID := a.LatestTurn()
	if wantID == "" {
		t.Fatal("prompt did not retain latest turn identity")
	}
	wantSequence := a.TurnSequenceWatermark()
	if wantSequence == 0 {
		t.Fatal("prompt turn sequence is zero")
	}

	unlock := a.LockAdmission()
	if err := a.SetSessionQuietAdmitted(newStore); err != nil {
		unlock()
		t.Fatal(err)
	}
	if err := a.SetSessionQuietAdmitted(oldStore); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	if origin, id := a.LatestTurn(); origin != wantOrigin || id != wantID {
		t.Fatalf("rollback identity = %q/%q, want %q/%q", origin, id, wantOrigin, wantID)
	}
	if sequence := a.TurnSequenceWatermark(); sequence != wantSequence {
		t.Fatalf("rollback sequence = %d; want %d", sequence, wantSequence)
	}

	if err := a.SetSession(newStore); err != nil {
		t.Fatal(err)
	}
	if origin, id := a.LatestTurn(); origin != "" || id != "" {
		t.Fatalf("committed session retained identity %q/%q", origin, id)
	}
	if sequence := a.TurnSequenceWatermark(); sequence != wantSequence {
		t.Fatalf("session commit changed monotonic sequence to %d; want %d", sequence, wantSequence)
	}
}

func TestMalformedToolArgumentsRemainPersistableInSQLite(t *testing.T) {
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "session.db"), t.TempDir(), session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	malformed := json.RawMessage(`{"bad"`)
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "call-1", ToolName: "read", Arguments: malformed}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: st,
		Permission: permission.NewService(permission.ModeDeny, nil),
		Model:      protocol.Model{Provider: p.ID(), ID: "m", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "read"); err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		if message.Role == protocol.RoleTool && message.IsError && strings.Contains(messageTextForTest(message), "not valid JSON") {
			found = true
		}
	}
	if !found {
		t.Fatalf("malformed-argument tool result missing: %+v", messages)
	}
}

func TestUsageEventMarksPositiveLegacyCacheReadKnown(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 100, CacheRead: 40}},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{}),
		Permission: permission.NewService(permission.ModeAllow, nil),
		Model:      protocol.Model{Provider: p.ID(), ID: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var usage *protocol.Usage
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvUsage {
			usage = ev.Usage
		}
	})
	if err := a.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if usage == nil || !usage.CacheReadKnown || usage.CacheRead != 40 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestTurnEventsCarryOneCorrelatedIdentity(t *testing.T) {
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamThinkingDelta, Text: "reason"},
		{Type: protocol.EvStreamTextDelta, Text: "answer"},
		{Type: protocol.EvStreamUsage, Usage: &protocol.Usage{Input: 3, Output: 2}},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{}),
		Permission: permission.NewService(permission.ModeAllow, nil),
		Model: protocol.Model{Provider: p.ID(), ID: "m", SupportsThinking: true,
			ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var events []protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) { events = append(events, ev) })
	if err := a.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	turnID := ""
	var turnSequence, rootEpoch uint64
	for _, ev := range events {
		if ev.TurnID == "" {
			t.Fatalf("turn event %s has no turn ID: %+v", ev.Type, ev)
		}
		if turnID == "" {
			turnID = ev.TurnID
			turnSequence = ev.TurnSequence
			rootEpoch = ev.RootEpoch
		}
		if ev.TurnID != turnID || ev.TurnOrigin != "user" || ev.TurnSequence == 0 || ev.TurnSequence != turnSequence || ev.RootEpoch == 0 || ev.RootEpoch != rootEpoch {
			t.Fatalf("event identity=%q/%q want user/%q for %s", ev.TurnOrigin, ev.TurnID, turnID, ev.Type)
		}
	}
	if turnID == "" {
		t.Fatal("no turn events")
	}
}

func messageTextForTest(message protocol.Message) string {
	var out strings.Builder
	for _, block := range message.Content {
		out.WriteString(block.Text)
	}
	return out.String()
}

func TestRequestAffinityKeyIsStableScopedAndOpaque(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	a := &Agent{opts: Options{Session: st}}
	turn := a.requestAffinityKey("turn")
	if len(turn) != 64 {
		t.Fatalf("turn affinity length=%d", len(turn))
	}
	if _, err := hex.DecodeString(turn); err != nil {
		t.Fatalf("turn affinity is not safe hex: %q", turn)
	}
	if turn != a.requestAffinityKey("turn") {
		t.Fatal("turn affinity changed within one branch")
	}
	if strings.Contains(turn, st.ID()) {
		t.Fatal("raw session id leaked into affinity")
	}
	if compact := a.requestAffinityKey("compaction"); compact == turn {
		t.Fatal("compaction reused ordinary turn affinity")
	}
	oldBranch := st.ActiveBranchID()
	if _, err := st.ForkBranch(st.BranchTip()); err != nil {
		t.Fatal(err)
	}
	if st.ActiveBranchID() == oldBranch {
		t.Fatal("fork did not change active branch")
	}
	if forked := a.requestAffinityKey("turn"); forked == turn || strings.Contains(forked, st.ActiveBranchID()) {
		t.Fatalf("fork affinity not independently opaque: %q", forked)
	}
}

type scriptedProvider struct {
	mu         sync.Mutex
	scripts    [][]protocol.StreamEvent // per Chat call
	call       int
	resolveErr error
	models     []protocol.Model
	requests   []protocol.ChatRequest
}

func TestActivatedSkillsRestoredIntoSystemContext(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	activation := "<skill_content name=\"review\">\nfollow review workflow\n</skill_content>"
	msg := protocol.NewToolResultMessage("skill-result", "", "call-1", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: st,
		Permission:   permission.NewService(permission.ModeDeny, nil),
		SystemPrompt: "base", Model: protocol.Model{Provider: "scripted", ID: "m1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	requests := p.requests
	if len(requests) != 1 || !strings.Contains(requests[0].System, "follow review workflow") || !strings.Contains(requests[0].System, "active_agent_skills") {
		t.Fatalf("system = %q", requests[0].System)
	}
}

func TestRestoredSkillsHonorCurrentPolicy(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	activation := "<skill_content name=\"review\">\nfollow review workflow\n</skill_content>"
	msg := protocol.NewToolResultMessage("skill-result", "", "call-1", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{
		Provider: p, Registry: tools.NewRegistry(), Session: st,
		Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{},
		Model: protocol.Model{Provider: "scripted", ID: "m1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 || strings.Contains(p.requests[0].System, "follow review workflow") {
		t.Fatalf("disabled skill leaked into system prompt: %q", p.requests[0].System)
	}
}

func TestRestoredSkillUsesCurrentCatalogContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Current review skill.\n---\ncurrent trusted instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := skillspkg.Discover(skillspkg.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	defer catalog.Close()
	registry := tools.NewRegistry()
	if err := skillspkg.RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	st := session.NewMemoryStore(session.Options{})
	old := "<skill_content name=\"review\">\nstale untrusted project instructions\n</skill_content>"
	msg := protocol.NewToolResultMessage("skill-result", "", "call-1", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(old)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{Provider: p, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{"review": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 || !strings.Contains(p.requests[0].System, "current trusted instructions") || strings.Contains(p.requests[0].System, "stale untrusted") {
		t.Fatalf("restored current skill system = %q", p.requests[0].System)
	}
}

func TestHistoricalMentionWithoutSuccessMarkerDoesNotActivate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Current review skill.\n---\nmust not activate retroactively\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := skillspkg.Discover(skillspkg.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	defer catalog.Close()
	registry := tools.NewRegistry()
	if err := skillspkg.RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	st := session.NewMemoryStore(session.Options{})
	user := protocol.NewUserMessage("historical", "", "$review this predates installation")
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
		t.Fatal(err)
	}
	p := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, err := New(Options{Provider: p, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{"review": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 || strings.Contains(p.requests[0].System, "must not activate retroactively") {
		t.Fatalf("historical mention activated without marker: %q", p.requests[0].System)
	}
}

func TestFailedExplicitSkillActivationPersistsTranscript(t *testing.T) {
	registry := tools.NewRegistry()
	schema := protocol.ToolSchema{
		Name:        "activate_skill",
		Description: "activate",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","enum":["review"]}},"required":["name"]}`),
	}
	tool := &testTool{name: "activate_skill", schema: schema, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.ErrorResult(errors.New("skill activation failed"))
	}}
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: tool, Source: tools.SourceBuiltin, Owner: "skills", Risk: permission.RiskRead}); err != nil {
		t.Fatal(err)
	}
	st := session.NewMemoryStore(session.Options{})
	provider := &scriptedProvider{}
	a, err := New(Options{Provider: provider, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SkillNames: map[string]bool{"review": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "$review inspect this"); err == nil || !strings.Contains(err.Error(), "skill activation failed") {
		t.Fatalf("Prompt error = %v, want activation failure", err)
	}
	entries, err := st.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type != session.EntryMeta || entry.Key != session.MetaToolTranscript {
			continue
		}
		var transcript protocol.ToolTranscript
		if json.Unmarshal([]byte(entry.Value), &transcript) == nil && transcript.ToolName == "activate_skill" && transcript.IsError && transcript.Display.Started && strings.Contains(transcript.Display.Output, "skill activation failed") {
			return
		}
	}
	t.Fatalf("failed activation transcript missing: %+v", entries)
}

func TestExplicitSkillDirectiveParsing(t *testing.T) {
	candidates := []string{"review", "docs"}
	for _, test := range []struct {
		text string
		want []string
	}{
		{"$review do work", []string{"review"}},
		{"  $review $docs do work", []string{"review", "docs"}},
		{"Use $review in pasted prose", nil},
		{"quoted text\n$review", nil},
		{"$reviewer", nil},
		{"$review $review once", []string{"review"}},
	} {
		if got := explicitSkillNames(test.text, candidates); !slices.Equal(got, test.want) {
			t.Errorf("explicitSkillNames(%q) = %v, want %v", test.text, got, test.want)
		}
	}
}

func TestExplicitSkillMentionActivatesAndRestoresWithoutModelToolCall(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "skills")
	dir := filepath.Join(root, "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code.\n---\nfollow explicit review workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := skillspkg.Discover(skillspkg.Options{Home: t.TempDir(), SnowHome: t.TempDir(), ExtraDirs: []string{root}})
	defer catalog.Close()
	registry := tools.NewRegistry()
	if err := skillspkg.RegisterTools(registry, catalog); err != nil {
		t.Fatal(err)
	}
	st := session.NewMemoryStore(session.Options{})
	firstProvider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	first, err := New(Options{Provider: firstProvider, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{"review": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	var eventMu sync.Mutex
	var skillEvents []protocol.AgentEventType
	first.Subscribe(func(event protocol.AgentEvent) {
		if event.ToolName == "activate_skill" && (event.Type == protocol.EvToolStart || event.Type == protocol.EvToolEnd) {
			eventMu.Lock()
			skillEvents = append(skillEvents, event.Type)
			eventMu.Unlock()
		}
	})
	if err := first.Prompt(context.Background(), "$review Use this skill for the change."); err != nil {
		t.Fatal(err)
	}
	if len(firstProvider.requests) != 1 || len(firstProvider.requests[0].Messages) != 1 || firstProvider.requests[0].Messages[0].Role != protocol.RoleUser || !strings.Contains(firstProvider.requests[0].System, "follow explicit review workflow") {
		t.Fatalf("explicit activation system prompt = %q", firstProvider.requests[0].System)
	}
	entries, err := st.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	markerFound := false
	transcriptFound := false
	for _, entry := range entries {
		markerFound = markerFound || entry.Type == session.EntryMeta && entry.Key == skillActivationMeta && entry.Value == "review"
		if entry.Type == session.EntryMeta && entry.Key == session.MetaToolTranscript {
			var transcript protocol.ToolTranscript
			if json.Unmarshal([]byte(entry.Value), &transcript) == nil && transcript.ToolName == "activate_skill" && transcript.Display.Started && transcript.Display.Output == "activated skill review" {
				transcriptFound = true
			}
		}
	}
	if !markerFound || !transcriptFound {
		t.Fatalf("direct activation metadata missing: marker=%t transcript=%t entries=%+v", markerFound, transcriptFound, entries)
	}
	eventMu.Lock()
	observedEvents := append([]protocol.AgentEventType(nil), skillEvents...)
	eventMu.Unlock()
	if !slices.Equal(observedEvents, []protocol.AgentEventType{protocol.EvToolStart, protocol.EvToolEnd}) {
		t.Fatalf("direct activation lifecycle events = %v", observedEvents)
	}

	secondProvider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	resumed, err := New(Options{Provider: secondProvider, Registry: registry, Session: st, Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base", SkillNames: map[string]bool{"review": true}, Model: protocol.Model{Provider: "scripted", ID: "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(secondProvider.requests) != 1 || !strings.Contains(secondProvider.requests[0].System, "follow explicit review workflow") {
		t.Fatalf("restored explicit activation system prompt = %q", secondProvider.requests[0].System)
	}
}

func (p *scriptedProvider) ID() string { return "scripted" }

type namedScriptedProvider struct {
	*scriptedProvider
	id string
}

func (p *namedScriptedProvider) ID() string { return p.id }

func (p *scriptedProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return p.models, nil
}

func (p *scriptedProvider) Resolve(ctx context.Context, creds auth.Credential) (auth.Credential, error) {
	return creds, p.resolveErr
}

func (p *scriptedProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.resolveErr != nil {
		return nil, p.resolveErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var evs []protocol.StreamEvent
	if len(p.scripts) > 0 {
		if p.call < len(p.scripts) {
			evs = p.scripts[p.call]
		} else {
			// Repeat the last script so tool loops can be driven until capped.
			evs = p.scripts[len(p.scripts)-1]
		}
	}
	p.requests = append(p.requests, req)
	p.call++
	return &sliceStream{evs: evs, ctx: ctx}, nil
}

type sliceStream struct {
	evs []protocol.StreamEvent
	i   int
	ctx context.Context
}

type blockingSummaryProvider struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSummaryProvider) ID() string { return "blocking-summary" }
func (*blockingSummaryProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (*blockingSummaryProvider) Resolve(_ context.Context, credential auth.Credential) (auth.Credential, error) {
	return credential, nil
}
func (p *blockingSummaryProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	return &blockingSummaryStream{started: p.started, release: p.release}, nil
}

type blockingSummaryStream struct {
	started chan struct{}
	release chan struct{}
	step    int
}

func (s *blockingSummaryStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	switch s.step {
	case 0:
		s.step++
		close(s.started)
		select {
		case <-s.release:
			return protocol.StreamEvent{Type: protocol.EvStreamTextDelta, Text: "summary"}, nil
		case <-ctx.Done():
			return protocol.StreamEvent{}, ctx.Err()
		}
	case 1:
		s.step++
		return protocol.StreamEvent{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}, nil
	default:
		return protocol.StreamEvent{}, io.EOF
	}
}
func (*blockingSummaryStream) Close() error { return nil }

func (s *sliceStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	if s.i >= len(s.evs) {
		return protocol.StreamEvent{}, io.EOF
	}
	ev := s.evs[s.i]
	s.i++
	return ev, nil
}

func (s *sliceStream) Close() error { return nil }

// ---------------------------------------------------------------------------
// Test tool
// ---------------------------------------------------------------------------

type testTool struct {
	name    string
	schema  protocol.ToolSchema
	runFunc func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult
}

func (t *testTool) Schema() protocol.ToolSchema { return t.schema }

func (t *testTool) Run(ctx context.Context, args json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	return t.runFunc(ctx, args, host), nil
}

// ---------------------------------------------------------------------------
// Host
// ---------------------------------------------------------------------------

type testHost struct {
	cwd    string
	perm   permission.Service
	events []tools.ToolProgressEvent
}

func (h *testHost) CWD() string                    { return h.cwd }
func (h *testHost) Roots() []string                { return []string{h.cwd} }
func (h *testHost) Permission() permission.Service { return h.perm }
func (h *testHost) EmitProgress(e tools.ToolProgressEvent) {
	h.events = append(h.events, e)
}
func (h *testHost) Environ() []string { return nil }

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func setup(t *testing.T, prov provider.Provider, reg *tools.SimpleRegistry, permMode permission.Mode) (*Agent, *session.MemoryStore) {
	t.Helper()
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	if reg == nil {
		reg = tools.NewRegistry()
	}
	perm := permission.NewService(permMode, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "test system",
		Model:        protocol.Model{Provider: prov.ID(), ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, st
}

func toolCallBlock(id, name string, args map[string]any) protocol.ContentBlock {
	raw, _ := json.Marshal(args)
	return protocol.ContentBlock{
		Type:       protocol.BlockToolCall,
		ToolCallID: id,
		Name:       name,
		Arguments:  raw,
	}
}

func TestManualCompactUsesSummaryAndPreservesHistory(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "model summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("msg-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, compact.WorkingStateTitle) || !strings.Contains(result.Summary, "model summary") || result.UsedFallback || result.SummarizedMessages == 0 {
		t.Fatalf("compact result = %+v", result)
	}
	if len(prov.requests) != 1 || len(prov.requests[0].Tools) != 0 || len(prov.requests[0].Messages) != result.SummarizedMessages {
		t.Fatalf("summary request = %+v", prov.requests)
	}
	full, err := st.Messages()
	if err != nil || len(full) != 6 {
		t.Fatalf("full history = %d, err=%v", len(full), err)
	}
	projected, err := st.ContextMessages()
	if err != nil || len(projected) != result.RetainedMessages+1 || projected[0].Role != protocol.RoleCustom {
		t.Fatalf("projected context = %+v, result=%+v, err=%v", projected, result, err)
	}
}

func TestManualCompactPublishesSessionUpdateBeforeTerminalDone(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "model summary"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("ordered-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	var events []protocol.AgentEvent
	a.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvSessionUpdated || event.Type == protocol.EvCompactionDone {
			events = append(events, event.Clone())
		}
	})
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.bus.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != protocol.EvSessionUpdated || events[1].Type != protocol.EvCompactionDone {
		t.Fatalf("compaction lifecycle order=%+v", events)
	}
	if events[0].TurnID == "" || events[0].TurnID != events[1].TurnID || events[1].TurnOrigin != "compact" {
		t.Fatalf("compaction lifecycle identity=%+v", events)
	}
}

func TestManualCompactPersistsMailboxArrivingDuringSummary(t *testing.T) {
	provider := &blockingSummaryProvider{started: make(chan struct{}), release: make(chan struct{})}
	a, store := setup(t, provider, nil, permission.ModeDeny)
	for i := 0; i < 6; i++ {
		message := protocol.NewUserMessage(fmt.Sprintf("mailbox-%d", i), "", fmt.Sprintf("message %d", i))
		if err := store.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	completed := make(chan error, 1)
	go func() {
		_, err := a.Compact(context.Background())
		completed <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("compaction summary did not start")
	}
	envelope := protocol.AgentMessage{ID: "mail-during-compact", Author: protocol.AgentPath("/root/child"), Recipient: protocol.RootAgentPath, Kind: protocol.AgentMessageNormal, Content: "important mail", CreatedAt: time.Now().UnixMilli()}
	if err := a.EnqueueMailbox(envelope); err != nil {
		t.Fatal(err)
	}
	close(provider.release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != protocol.RoleAgent || !strings.Contains(messages[len(messages)-1].Content[0].Text, "important mail") {
		t.Fatalf("mail was not persisted after compaction: %+v", messages)
	}
}

func TestManualCompactConfiguredGuidanceAndBudgetReachProvider(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 333, Fallback: "error", Guidance: "Preserve ticket IDs."}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("configured-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 1 || prov.requests[0].MaxTokens != 333 || !strings.Contains(prov.requests[0].System, "Preserve ticket IDs") {
		t.Fatalf("request=%+v", prov.requests)
	}
}

func TestRepeatedCompactionSummarizesProjectedContext(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamTextDelta, Text: "first summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}, {{Type: protocol.EvStreamTextDelta, Text: "second summary"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("repeat-a-%d", i), "", fmt.Sprintf("first %d", i))
		_ = st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg})
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("repeat-b-%d", i), "", fmt.Sprintf("second %d", i))
		_ = st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg})
	}
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 2 || len(prov.requests[1].Messages) >= 10 || prov.requests[1].Messages[0].Role != protocol.RoleCustom || !strings.Contains(prov.requests[1].Messages[0].Content[0].Text, "first summary") {
		t.Fatalf("second request=%+v", prov.requests)
	}
}

func TestRepeatedCompactionSendsLatestCheckpointToNextTurn(t *testing.T) {
	first := compact.WorkingStateTitle + "\n\n## Objective and constraints\n- first checkpoint sentinel"
	second := compact.WorkingStateTitle + "\n\n## Objective and constraints\n- latest checkpoint sentinel"
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamTextDelta, Text: first}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: second}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
		{{Type: protocol.EvStreamTextDelta, Text: "recalled latest"}, {Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "local"}
	memoryValues := []string{"ALPHA-17", "BRAVO-29", "COBALT-31", "DELTA-43", "EMBER-59", "FROST-61", "GARNET-73", "HARBOR-89"}
	appendUsers := func(prefix string, count int) {
		t.Helper()
		for i := 0; i < count; i++ {
			text := fmt.Sprintf("%s message %d", prefix, i)
			if prefix == "first" && i == 2 {
				text = "Facts available only in this old turn: " + strings.Join(memoryValues, ", ")
			}
			msg := protocol.NewUserMessage(fmt.Sprintf("%s-%d", prefix, i), "", text)
			if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
				t.Fatal(err)
			}
		}
	}
	appendUsers("first", 6)
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	appendUsers("second", 4)
	if _, err := a.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "Recall the latest checkpoint."); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 3 {
		t.Fatalf("provider requests=%d, want 3", len(prov.requests))
	}
	request := prov.requests[2]
	custom := 0
	for _, message := range request.Messages {
		if message.Role != protocol.RoleCustom {
			continue
		}
		custom++
		text := messageTextBlocks(message)
		if !strings.Contains(text, "latest checkpoint sentinel") {
			t.Fatalf("active checkpoint does not contain latest summary:\n%s", text)
		}
		for _, value := range memoryValues {
			if !strings.Contains(text, value) {
				t.Fatalf("active checkpoint lost old fact %q after two compactions:\n%s", value, text)
			}
		}
	}
	if custom != 1 || request.Messages[0].Role != protocol.RoleCustom {
		t.Fatalf("latest request custom checkpoints=%d messages=%+v", custom, request.Messages)
	}
}

func TestManualCompactRejectsMalformedSummaryWhenFallbackIsError(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: `<｜DSML｜tool_calls><｜DSML｜invoke name="bash">`},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	a.opts.Compaction = CompactionOptions{RetainTokens: 1, MinRetainedTurns: 2, SummaryMaxTokens: 500, Fallback: "error"}
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("malformed-%d", i), "", fmt.Sprintf("objective message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Compact(context.Background()); err == nil || !strings.Contains(err.Error(), "tool-protocol markup") {
		t.Fatalf("malformed summary error=%v", err)
	}
	projected, err := st.ContextMessages()
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range projected {
		if message.Role == protocol.RoleCustom {
			t.Fatalf("failed quality validation persisted a compaction marker: %+v", projected)
		}
	}
}

func TestManualCompactFallsBackWhenProviderFails(t *testing.T) {
	prov := &scriptedProvider{resolveErr: errors.New("summary unavailable")}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	for i := 0; i < 6; i++ {
		msg := protocol.NewUserMessage(fmt.Sprintf("fallback-%d", i), "", fmt.Sprintf("message %d", i))
		if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsedFallback || result.Summary == "" {
		t.Fatalf("fallback result = %+v", result)
	}
}

// TestSingleTextTurn: provider returns one text delta then done.
func TestSingleTextTurn(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamTextDelta, Text: "hello "},
		{Type: protocol.EvStreamTextDelta, Text: "world"},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
	}}}
	a, st := setup(t, prov, nil, permission.ModeDeny)

	var got string
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvTextDelta {
			got += ev.Text
		}
	})

	if err := a.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
	msgs, _ := st.Messages()
	if len(msgs) != 2 { // user + assistant
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Role != protocol.RoleAssistant || msgs[1].StopReason != protocol.StopStop {
		t.Fatalf("bad assistant message: %+v", msgs[1])
	}
}

func TestSearchToolsAreReadOnly(t *testing.T) {
	for _, name := range []string{"read", "grep", "glob"} {
		if got := riskFor(name); got != permission.RiskRead {
			t.Fatalf("riskFor(%q) = %q, want read", name, got)
		}
	}
}

// TestToolRoundTrip: tool_use -> tool result -> final text.
func TestToolRoundTrip(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("file contents here")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(readTool); err != nil {
		t.Fatal(err)
	}

	_ = toolCallBlock("call-1", "read", map[string]any{"path": "a.txt"})
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDelta, ToolCallID: "call-1", ToolName: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "call-1", ToolName: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done reading"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)

	var toolResults int
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvToolEnd {
			toolResults++
		}
	})

	if err := a.Prompt(context.Background(), "read a.txt"); err != nil {
		t.Fatal(err)
	}
	if toolResults != 1 {
		t.Fatalf("expected 1 tool end, got %d", toolResults)
	}
	msgs, _ := st.Messages()
	if len(msgs) != 4 { // user, assistant(tool_use), tool_result, assistant(final)
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	// Message 2 is the tool result.
	tr := msgs[2]
	if tr.Role != protocol.RoleTool || tr.ToolCallID != "call-1" {
		t.Fatalf("bad tool result message: %+v", tr)
	}
	if tr.IsError {
		t.Fatal("tool result should not be error")
	}
	// Final assistant contains "done reading".
	if msgs[3].Content[0].Text != "done reading" {
		t.Fatalf("final assistant text wrong: %+v", msgs[3].Content)
	}
}

func TestToolEventsExposeOutputProgressAndTiming(t *testing.T) {
	tool := &testTool{
		name:   "progress_tool",
		schema: protocol.ToolSchema{Name: "progress_tool", Description: "progress", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			host.EmitProgress(tools.ToolProgressEvent{ToolCallID: "spoofed-call", Name: "spoofed-tool", Message: "halfway"})
			return tools.TextResult("tool output")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "progress-1", ToolName: "progress_tool", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeAllow)
	var progress, end protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) {
		switch ev.Type {
		case protocol.EvToolProgress:
			progress = ev
		case protocol.EvToolEnd:
			end = ev
		}
	})
	if err := a.Prompt(context.Background(), "run tool"); err != nil {
		t.Fatal(err)
	}
	if progress.ToolProgress == nil || progress.ToolProgress.Message != "halfway" || progress.ToolCallID != "progress-1" || progress.ToolName != "progress_tool" || progress.ToolProgress.ToolCallID != "progress-1" || progress.ToolProgress.Name != "progress_tool" {
		t.Fatalf("progress event = %+v", progress)
	}
	if end.ToolOutput != "tool output" || end.ToolDurationMS < 0 {
		t.Fatalf("tool end = %+v", end)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 3 || messages[2].Role != protocol.RoleTool || messages[2].ToolDisplay == nil {
		t.Fatalf("persisted tool result missing display metadata: %+v", messages)
	}
	display := messages[2].ToolDisplay
	if !display.Started || !slices.Equal(display.Progress, []string{"halfway"}) || display.Output != end.ToolOutput || display.DurationMS != end.ToolDurationMS {
		t.Fatalf("persisted display=%+v, event=%+v", display, end)
	}
}

func TestBashToolStartIncludesCommandForUI(t *testing.T) {
	const command = `printf '%s\\n' "hello world"`
	tool := &testTool{
		name:   "bash",
		schema: protocol.ToolSchema{Name: "bash", Description: "shell", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
			return tools.TextResult("hello world")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "bash-1", ToolName: "bash", Arguments: args},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, _ := setup(t, prov, reg, permission.ModeAllow)
	var start protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvToolStart {
			start = ev
		}
	})
	if err := a.Prompt(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}
	if start.Message != command {
		t.Fatalf("bash tool start message = %q, want %q", start.Message, command)
	}
}

func TestToolEditDetailsBecomeUIToolPreview(t *testing.T) {
	tool := &testTool{
		name:   "edit",
		schema: protocol.ToolSchema{Name: "edit", Description: "edit", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.ToolResult{
				Content: []protocol.ContentBlock{protocol.NewTextBlock("updated")},
				Details: tools.DiffDetails{Diff: "-1 old\n+1 new"},
			}
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "edit-1", ToolName: "edit", Arguments: json.RawMessage(`{"path":"docs/sessions.md"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeAllow)
	var start, end protocol.AgentEvent
	a.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvToolStart {
			start = ev
		}
		if ev.Type == protocol.EvToolEnd {
			end = ev
		}
	})
	if err := a.Prompt(context.Background(), "edit it"); err != nil {
		t.Fatal(err)
	}
	if start.Message != "docs/sessions.md" {
		t.Fatalf("tool start message = %q, want edit path", start.Message)
	}
	if end.ToolOutput != "-1 old\n+1 new" {
		t.Fatalf("tool output = %q, want private diff preview", end.ToolOutput)
	}
	messages, err := st.Messages()
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 3 || messages[2].ToolDisplay == nil || messages[2].ToolDisplay.Output != end.ToolOutput || messages[2].ToolDisplay.StartMessage != start.Message {
		t.Fatalf("persisted edit display does not match live events: %+v", messages)
	}
	for requestIndex, request := range prov.requests {
		for messageIndex, message := range request.Messages {
			if message.ToolDisplay != nil {
				t.Fatalf("private tool display reached provider request %d message %d: %+v", requestIndex, messageIndex, message.ToolDisplay)
			}
		}
	}
}

// TestToolDenied: permission deny produces error tool result and loop still finishes.
func TestToolDenied(t *testing.T) {
	writeTool := &testTool{
		name:   "write",
		schema: protocol.ToolSchema{Name: "write", Description: "write", Parameters: json.RawMessage(`{"type":"object"}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("SHOULD NOT RUN")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(writeTool)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "write", Arguments: json.RawMessage(`{"path":"x"}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "denied ok"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)

	if err := a.Prompt(context.Background(), "write x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	tr := msgs[2]
	if !tr.IsError {
		t.Fatalf("expected denied tool result to be error: %+v", tr)
	}
}

// TestChildBashPermissionAndExecution exercises the real builtin bash tool
// through a child-attributed agent and proves denial happens before process start.
func TestChildBashPermissionAndExecution(t *testing.T) {
	command := "printf started > started; sleep 0.02; printf child-shell"

	for _, tc := range []struct {
		name  string
		mode  permission.Mode
		allow bool
	}{
		{name: "allow", mode: permission.ModeAllow, allow: true},
		{name: "deny", mode: permission.ModeDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			bash := builtin.NewBash()
			bash.Timeout = time.Second
			reg := tools.NewRegistry()
			if err := reg.Register(bash); err != nil {
				t.Fatal(err)
			}
			prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
				{
					{Type: protocol.EvStreamToolCallDone, ToolCallID: "bash-1", ToolName: "bash", Arguments: json.RawMessage(fmt.Sprintf(`{"command":%q}`, command))},
					{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
				},
				{
					{Type: protocol.EvStreamTextDelta, Text: "shell turn complete"},
					{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
				},
			}}
			st := session.NewMemoryStore(session.Options{CWD: cwd})
			perm := permission.NewService(tc.mode, nil)
			a, err := New(Options{
				Provider: prov, Registry: reg, Session: st, Permission: perm,
				ToolHost: &testHost{cwd: cwd, perm: perm},
				Model:    protocol.Model{Provider: prov.ID(), ID: "m1", SupportsTools: true},
				Identity: &protocol.AgentRef{ThreadID: "child", ParentThreadID: "root", Path: "/root/child", ParentPath: "/root", Role: "general", Depth: 1},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Prompt(context.Background(), "run the bounded shell check"); err != nil {
				t.Fatal(err)
			}

			var toolResult *protocol.Message
			msgs, err := st.Messages()
			if err != nil {
				t.Fatal(err)
			}
			for i := range msgs {
				if msgs[i].Role == protocol.RoleTool && msgs[i].ToolName == "bash" {
					toolResult = &msgs[i]
					break
				}
			}
			if toolResult == nil {
				t.Fatalf("bash result missing: %+v", msgs)
			}
			started := false
			if _, err := os.Stat(cwd + string(os.PathSeparator) + "started"); err == nil {
				started = true
			}
			if tc.allow {
				if toolResult.IsError || len(toolResult.Content) == 0 || !strings.Contains(toolResult.Content[0].Text, "child-shell") {
					t.Fatalf("allowed bash result = %+v", toolResult)
				}
				if !started {
					t.Fatal("allowed bash did not execute")
				}
			} else {
				if !toolResult.IsError || len(toolResult.Content) == 0 || !strings.Contains(toolResult.Content[0].Text, "Permission denied") {
					t.Fatalf("denied bash result = %+v", toolResult)
				}
				if started {
					t.Fatal("denied bash started a process")
				}
			}
			a.Close()
		})
	}
}

func TestUnknownTool(t *testing.T) {
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "nonexistent", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "recovered"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if !msgs[2].IsError {
		t.Fatal("expected error result for unknown tool")
	}
}

// TestMalformedArguments: invalid JSON args produce error tool result.
func TestMalformedArguments(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("ran")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(readTool)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{"bad json`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "done"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, st := setup(t, prov, reg, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if !msgs[2].IsError {
		t.Fatal("expected error result for malformed args")
	}
}

// TestMaxTurns: loop stops at turn cap.
func TestMaxTurns(t *testing.T) {
	loop := &testTool{
		name:   "loop",
		schema: protocol.ToolSchema{Name: "loop", Description: "l", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("loop")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(loop)

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "loop", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
	}}
	// Same script for all calls -> infinite tool loop, capped by MaxTurns=2.
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	perm := permission.NewService(permission.ModeDeny, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "s",
		Model:        protocol.Model{Provider: "scripted", ID: "m1"},
		MaxTurns:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = a.Prompt(context.Background(), "x")
	if err == nil {
		t.Fatal("expected max turns error")
	}
	if err.Error() != "agent: max turns reached" {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestAbort: context cancellation aborts mid-stream and records aborted assistant.
func TestAbort(t *testing.T) {
	prov := &blockingProvider{}
	a, st := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	if err := a.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + aborted assistant), got %d", len(msgs))
	}
	asst := msgs[1]
	if asst.Role != protocol.RoleAssistant || asst.StopReason != protocol.StopAborted {
		t.Fatalf("expected aborted assistant: %+v", asst)
	}
}

// blockingProvider returns a stream that never yields, so ctx cancellation
// surfaces as the stream error path.
type blockingProvider struct {
	started chan struct{} // closed when Chat is entered
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{})}
}

func (p *blockingProvider) ID() string { return "blocking" }
func (p *blockingProvider) ListModels(ctx context.Context) ([]protocol.Model, error) {
	return nil, nil
}
func (p *blockingProvider) Resolve(ctx context.Context, creds auth.Credential) (auth.Credential, error) {
	return creds, nil
}
func (p *blockingProvider) Chat(ctx context.Context, req protocol.ChatRequest) (protocol.EventStream, error) {
	if p.started != nil {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
	}
	return &blockingStream{ctx: ctx}, nil
}

type blockingStream struct{ ctx context.Context }

func (s *blockingStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	<-ctx.Done()
	return protocol.StreamEvent{}, ctx.Err()
}
func (s *blockingStream) Close() error { return nil }

// TestEventOrder: tool_start before tool_end; turn_done last.
func TestEventOrder(t *testing.T) {
	readTool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			return tools.TextResult("ok")
		},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(readTool)
	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDelta, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "final"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	a, _ := setup(t, prov, reg, permission.ModeDeny)

	var order []string
	a.Subscribe(func(ev protocol.AgentEvent) {
		switch ev.Type {
		case protocol.EvToolStart:
			order = append(order, "tool_start")
		case protocol.EvToolEnd:
			order = append(order, "tool_end")
		case protocol.EvTurnDone:
			order = append(order, "turn_done")
		}
	})
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	want := []string{"tool_start", "tool_end", "turn_done"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("event order = %v, want %v", order, want)
	}
}

// TestEmptyPrompt rejected.
func TestEmptyPrompt(t *testing.T) {
	prov := &scriptedProvider{}
	a, _ := setup(t, prov, nil, permission.ModeDeny)
	if err := a.Prompt(context.Background(), "   "); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

// TestAlreadyRunning: a second Prompt while a turn is in flight errors out.
func TestAlreadyRunning(t *testing.T) {
	prov := newBlockingProvider()
	a, _ := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Prompt(ctx, "first")
	}()
	// Wait until the provider Chat is entered, then the running flag is set.
	<-prov.started
	// Give the agent a moment to publish the running state.
	err := a.Prompt(context.Background(), "second")
	cancel()
	if err == nil {
		t.Fatal("expected already-running error")
	}
	if err2 := <-done; err2 != nil {
		t.Fatalf("first prompt should abort cleanly, got %v", err2)
	}
}

// TestConcurrentPromptNoGhostMessage: a second Prompt while a turn is in
// flight must fail with "already running" WITHOUT persisting a ghost user
// message that would never be processed.
func TestConcurrentPromptNoGhostMessage(t *testing.T) {
	prov := newBlockingProvider()
	a, st := setup(t, prov, nil, permission.ModeDeny)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.Prompt(ctx, "first")
	}()
	<-prov.started // first turn claimed the running flag

	err := a.Prompt(context.Background(), "second")
	if err == nil {
		t.Fatal("expected already-running error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("wrong error: %v", err)
	}

	// No ghost: exactly the first user message is persisted.
	msgs, _ := st.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 user message (no ghost), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != protocol.RoleUser || msgs[0].Content[0].Text != "first" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}

	// First turn aborts cleanly on cancel.
	cancel()
	if err2 := <-done; err2 != nil {
		t.Fatalf("first prompt should abort cleanly, got %v", err2)
	}
}

// TestToolLoopCancelStopsRemaining: cancelling the context during the first
// tool execution must stop the remaining tool calls (no second run) and
// surface the context error from Prompt.
func TestToolLoopCancelStopsRemaining(t *testing.T) {
	runCount := 0
	var once sync.Once
	cancelFirst := func() {}

	tool := &testTool{
		// Named "read" so the permission gate (RiskRead) always allows it
		// under deny mode; the loop-cancel behavior is what we exercise.
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			runCount++
			once.Do(func() {
				cancelFirst()
			})
			return tools.TextResult("ran")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{{
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamToolCallDone, ToolCallID: "c2", ToolName: "read", Arguments: json.RawMessage(`{}`)},
		{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
	}}}
	a, st := setup(t, prov, reg, permission.ModeDeny)

	ctx, cancel := context.WithCancel(context.Background())
	cancelFirst = cancel

	err := a.Prompt(ctx, "run tools")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prompt = %v, want context.Canceled", err)
	}
	if runCount != 1 {
		t.Fatalf("tool ran %d times, want exactly 1 (second call must be skipped)", runCount)
	}
	msgs, msgErr := st.Messages()
	if msgErr != nil {
		t.Fatal(msgErr)
	}
	if len(msgs) != 5 || msgs[3].ToolCallID != "c2" || !msgs[3].IsError || msgs[4].StopReason != protocol.StopAborted {
		t.Fatalf("cancelled tool calls = %+v, want synthetic error result and aborted boundary", msgs)
	}
}

// TestToolCallLimitEmitsErrorResults: when CallLimit is exceeded, the skipped
// tool call must still get an error tool_result so no tool_call is left
// dangling without a result.
func TestToolCallLimitEmitsErrorResults(t *testing.T) {
	ran := 0
	tool := &testTool{
		name:   "read",
		schema: protocol.ToolSchema{Name: "read", Description: "r", Parameters: json.RawMessage(`{}`)},
		runFunc: func(ctx context.Context, args json.RawMessage, host tools.ToolHost) tools.ToolResult {
			ran++
			return tools.TextResult("ok")
		},
	}
	reg := tools.NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c1", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamToolCallDone, ToolCallID: "c2", ToolName: "read", Arguments: json.RawMessage(`{}`)},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse},
		},
		{
			{Type: protocol.EvStreamTextDelta, Text: "finished"},
			{Type: protocol.EvStreamDone, StopReason: protocol.StopStop},
		},
	}}
	st := session.NewMemoryStore(session.Options{CWD: t.TempDir()})
	perm := permission.NewService(permission.ModeDeny, nil)
	host := &testHost{cwd: t.TempDir(), perm: perm}
	a, err := New(Options{
		Provider:     prov,
		Registry:     reg,
		Session:      st,
		Permission:   perm,
		ToolHost:     host,
		SystemPrompt: "s",
		Model:        protocol.Model{Provider: "scripted", ID: "m1"},
		CallLimit:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("tool ran %d times, want 1 (limit)", ran)
	}
	msgs, _ := st.Messages()
	// user, assistant(tool_use), tool_result(c1), tool_result(c2 error), assistant(final)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(msgs), msgs)
	}
	// Both tool calls must have results (no dangling tool_call).
	if msgs[2].Role != protocol.RoleTool || msgs[2].ToolCallID != "c1" {
		t.Fatalf("msgs[2] = %+v, want tool_result for c1", msgs[2])
	}
	if msgs[3].Role != protocol.RoleTool || msgs[3].ToolCallID != "c2" {
		t.Fatalf("msgs[3] = %+v, want tool_result for c2", msgs[3])
	}
	// The skipped (limited) call must be an error result.
	if !msgs[3].IsError {
		t.Fatalf("skipped call result should be IsError: %+v", msgs[3])
	}
	// The executed call is a normal result.
	if msgs[2].IsError {
		t.Fatalf("executed call result should not be IsError: %+v", msgs[2])
	}
	if msgs[4].Role != protocol.RoleAssistant || msgs[4].Content[0].Text != "finished" {
		t.Fatalf("final assistant message wrong: %+v", msgs[4])
	}
}
