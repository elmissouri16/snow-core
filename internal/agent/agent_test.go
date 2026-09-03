package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/session"
	skillspkg "github.com/elmissouri16/snow-core/internal/skills"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	conversation := a.conversationAffinityKey()
	turn := a.requestAffinityKey("turn")
	if len(conversation) != 64 {
		t.Fatalf("conversation affinity length=%d", len(conversation))
	}
	if _, err := hex.DecodeString(conversation); err != nil {
		t.Fatalf("conversation affinity is not safe hex: %q", conversation)
	}
	if conversation != a.conversationAffinityKey() {
		t.Fatal("conversation affinity changed within one branch")
	}
	if len(turn) != 64 {
		t.Fatalf("turn affinity length=%d", len(turn))
	}
	if _, err := hex.DecodeString(turn); err != nil {
		t.Fatalf("turn affinity is not safe hex: %q", turn)
	}
	if turn != a.requestAffinityKey("turn") {
		t.Fatal("turn affinity changed within one branch")
	}
	if strings.Contains(turn, st.ID()) || strings.Contains(conversation, st.ID()) {
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
	if forked := a.conversationAffinityKey(); forked == conversation || strings.Contains(forked, st.ActiveBranchID()) {
		t.Fatalf("fork conversation affinity not independently opaque: %q", forked)
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

func TestClearActiveSkillsPersistsAcrossResume(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	activation := "<skill_content name=\"review\">\nfollow review workflow\n</skill_content>"
	msg := protocol.NewToolResultMessage("skill-result", "", "call-1", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}); err != nil {
		t.Fatal(err)
	}
	newAgent := func(p *scriptedProvider) *Agent {
		a, err := New(Options{
			Provider: p, Registry: tools.NewRegistry(), Session: st,
			Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base",
			Model: protocol.Model{Provider: "scripted", ID: "m1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	a := newAgent(&scriptedProvider{})
	if system := a.requestSystemPrompt(); !strings.Contains(system, "follow review workflow") {
		t.Fatalf("activated skill missing before clear: %q", system)
	}
	cleared, err := a.ClearActiveSkills()
	if err != nil || cleared != 1 {
		t.Fatalf("ClearActiveSkills() = %d, %v", cleared, err)
	}
	if system := a.requestSystemPrompt(); strings.Contains(system, "follow review workflow") {
		t.Fatalf("cleared skill remained active: %q", system)
	}
	a.Close()

	resumed := newAgent(&scriptedProvider{})
	if system := resumed.requestSystemPrompt(); strings.Contains(system, "follow review workflow") {
		t.Fatalf("cleared skill restored after resume: %q", system)
	}
	resumed.Close()
	entries, err := st.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		found = found || entry.Type == session.EntryMeta && entry.Key == skillDeactivationMeta && entry.Value == skillDeactivationAll
	}
	if !found {
		t.Fatalf("missing durable skill clear marker: %+v", entries)
	}

	reactivation := protocol.NewToolResultMessage("skill-result-2", "", "call-2", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: reactivation.ID, Message: &reactivation}); err != nil {
		t.Fatal(err)
	}
	reactivated := newAgent(&scriptedProvider{})
	defer reactivated.Close()
	if system := reactivated.requestSystemPrompt(); !strings.Contains(system, "follow review workflow") {
		t.Fatalf("later activation did not override clear marker: %q", system)
	}
}

func TestDeactivateSkillToolRemovesActiveSkillAndPersists(t *testing.T) {
	st := session.NewMemoryStore(session.Options{})
	activation := "<skill_content name=\"review\">\nfollow review workflow\n</skill_content>"
	activated := protocol.NewToolResultMessage("skill-result", "", "activate-call", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := st.Append(session.Entry{Type: session.EntryMessage, ID: activated.ID, Message: &activated}); err != nil {
		t.Fatal(err)
	}

	registry := tools.NewRegistry()
	schema := protocol.ToolSchema{
		Name:        "deactivate_skill",
		Description: "deactivate",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","enum":["review","*"]}},"required":["name"]}`),
	}
	tool := &testTool{name: "deactivate_skill", schema: schema, runFunc: func(_ context.Context, args json.RawMessage, _ tools.ToolHost) tools.ToolResult {
		var input struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return tools.ErrorResult(err)
		}
		return tools.ToolResult{
			Content: []protocol.ContentBlock{protocol.NewTextBlock("deactivated skill " + input.Name)},
			Details: tools.SkillDeactivationDetails{Name: input.Name},
		}
	}}
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: tool, Source: tools.SourceBuiltin, Owner: "skills", Risk: permission.RiskRead}); err != nil {
		t.Fatal(err)
	}
	observeSchema := protocol.ToolSchema{Name: "observe", Description: "observe", Parameters: json.RawMessage(`{"type":"object"}`)}
	observe := &testTool{name: "observe", schema: observeSchema, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		return tools.TextResult("observed after deactivation")
	}}
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: observeSchema, Tool: observe, Source: tools.SourceBuiltin, Owner: "test", Risk: permission.RiskRead}); err != nil {
		t.Fatal(err)
	}

	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "deactivate-call", ToolName: "deactivate_skill", Arguments: json.RawMessage(`{"name":"review"}`)}, {Type: protocol.EvStreamToolCallDone, ToolCallID: "observe-call", ToolName: "observe", Arguments: json.RawMessage(`{}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: st,
		Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base",
		Model: protocol.Model{Provider: "scripted", ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prompt(context.Background(), "switch to implementation"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want tool call and continuation", len(provider.requests))
	}
	if !strings.Contains(provider.requests[0].System, "follow review workflow") {
		t.Fatalf("first request missing active skill: %q", provider.requests[0].System)
	}
	if strings.Contains(provider.requests[1].System, "follow review workflow") {
		t.Fatalf("deactivated skill remained in continuation: %q", provider.requests[1].System)
	}
	a.Close()

	entries, err := st.BranchEntries()
	if err != nil {
		t.Fatal(err)
	}
	markerFound := false
	for _, entry := range entries {
		markerFound = markerFound || entry.Type == session.EntryMeta && entry.Key == skillDeactivationMeta && entry.Value == "review"
	}
	if !markerFound {
		t.Fatalf("named deactivation marker missing: %+v", entries)
	}

	resumedProvider := &scriptedProvider{scripts: [][]protocol.StreamEvent{{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}}}}
	resumed, err := New(Options{
		Provider: resumedProvider, Registry: registry, Session: st,
		Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base",
		Model: protocol.Model{Provider: "scripted", ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err := resumed.Prompt(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if len(resumedProvider.requests) != 1 || strings.Contains(resumedProvider.requests[0].System, "follow review workflow") {
		t.Fatalf("named deactivation did not survive resume: %q", resumedProvider.requests[0].System)
	}
}

type baseOnlySessionStore struct{ session.Store }

func TestDeactivateSkillToolRequiresAtomicBranchStore(t *testing.T) {
	memory := session.NewMemoryStore(session.Options{})
	activation := "<skill_content name=\"review\">\nfollow review workflow\n</skill_content>"
	activated := protocol.NewToolResultMessage("skill-result", "", "activate-call", "activate_skill", []protocol.ContentBlock{protocol.NewTextBlock(activation)}, false)
	if err := memory.Append(session.Entry{Type: session.EntryMessage, ID: activated.ID, Message: &activated}); err != nil {
		t.Fatal(err)
	}
	st := &baseOnlySessionStore{Store: memory}
	registry := tools.NewRegistry()
	schema := protocol.ToolSchema{Name: "deactivate_skill", Description: "deactivate", Parameters: json.RawMessage(`{"type":"object"}`)}
	called := false
	tool := &testTool{name: "deactivate_skill", schema: schema, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		called = true
		return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock("deactivated skill review")}, Details: tools.SkillDeactivationDetails{Name: "review"}}
	}}
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: schema, Tool: tool, Source: tools.SourceBuiltin, Owner: "skills", Risk: permission.RiskRead}); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{scripts: [][]protocol.StreamEvent{
		{{Type: protocol.EvStreamToolCallDone, ToolCallID: "deactivate-call", ToolName: "deactivate_skill", Arguments: json.RawMessage(`{"name":"review"}`)}, {Type: protocol.EvStreamDone, StopReason: protocol.StopToolUse}},
		{{Type: protocol.EvStreamDone, StopReason: protocol.StopStop}},
	}}
	a, err := New(Options{
		Provider: provider, Registry: registry, Session: st,
		Permission: permission.NewService(permission.ModeDeny, nil), SystemPrompt: "base",
		Model: protocol.Model{Provider: "scripted", ID: "m1", SupportsTools: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Prompt(context.Background(), "switch to implementation"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("deactivate_skill ran without atomic branch-aware storage")
	}
	if len(provider.requests) != 2 || !strings.Contains(provider.requests[1].System, "follow review workflow") {
		t.Fatalf("active skill changed after rejected deactivation: requests=%d system=%q", len(provider.requests), provider.requests[1].System)
	}
	messages, err := memory.Messages()
	if err != nil {
		t.Fatal(err)
	}
	foundError := false
	for _, message := range messages {
		foundError = foundError || message.Role == protocol.RoleTool && message.ToolName == "deactivate_skill" && message.IsError && strings.Contains(toolResultText(message.Content), "atomic branch-aware")
	}
	if !foundError {
		t.Fatalf("missing deactivation capability error: %+v", messages)
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
		{"Use $review to inspect this", []string{"review"}},
		{"Use $review and $docs together", []string{"review", "docs"}},
		{"quoted text\n$review", []string{"review"}},
		{"Use\u2003$docs", []string{"docs"}},
		{"Use `$review` as an example", nil},
		{"Use $reviewer", nil},
		{"Use $review then $review once", []string{"review"}},
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
	if err := first.Prompt(context.Background(), "Use $review for this change."); err != nil {
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
	observedEvents := slices.Clone(skillEvents)
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
