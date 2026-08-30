package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestModelHydratesPersistedToolTranscript(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	assistant := protocol.NewAssistantMessage(
		"assistant-tool",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "Inspecting files"},
			{Type: protocol.BlockToolCall, ToolCallID: "call-1", Name: "grep", Arguments: json.RawMessage(`{"pattern":"needle"}`)},
		},
		protocol.StopToolUse,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	result := protocol.NewToolResultMessage(
		"tool-result",
		m.app.Session.BranchTip(),
		"call-1",
		"grep",
		[]protocol.ContentBlock{protocol.NewTextBlock("main.go:7: needle")},
		false,
	)
	result.ToolDisplay = &protocol.ToolDisplay{
		Started:      true,
		StartMessage: "running",
		Progress:     []string{"scanning files"},
		Output:       "main.go:7: needle",
		DurationMS:   12,
	}
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result}); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	wants := []string{"think: Inspecting files", "✔ grep  (12ms)", "main.go:7: needle"}
	position := -1
	for _, want := range wants {
		next := strings.Index(plain, want)
		if next < 0 {
			t.Fatalf("hydrated tool transcript missing %q: %q", want, plain)
		}
		if next <= position {
			t.Fatalf("hydrated tool row %q is out of order: %q", want, plain)
		}
		position = next
	}

	live := newModel(context.Background(), app.Options{})
	buildAppForTest(t, live)
	live.width = 100
	live.height = 30
	live.layout()
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThinkingDelta, Text: "Inspecting files"})
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "call-1", ToolName: "grep", Message: "running"})
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, ToolCallID: "call-1", ToolName: "grep", Message: "scanning files"})
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "call-1", ToolName: "grep", ToolOutput: "main.go:7: needle", ToolDurationMS: 12})
	if livePlain := stripANSI(strings.Join(live.lines, "\n")); plain != livePlain {
		t.Fatalf("resumed tool transcript differs from live transcript:\nresume: %q\nlive:   %q", plain, livePlain)
	}
}

type legacyHydrationStore struct {
	session.Store
	branch  session.BranchEntryStore
	context session.ContextStore
}

func (s legacyHydrationStore) BranchEntries() ([]session.Entry, error) {
	return s.branch.BranchEntries()
}

func (s legacyHydrationStore) ContextMessages() ([]protocol.Message, error) {
	return s.context.ContextMessages()
}

type paginatedHydrationSpy struct {
	session.Store
	hydration session.BranchHydrationStore
	lookup    session.BranchEntryLookup
	calls     int
	maxPage   int
	totalIDs  int
}

func (s *paginatedHydrationSpy) BranchHydration() (session.BranchHydrationSnapshot, error) {
	return s.hydration.BranchHydration()
}

func (s *paginatedHydrationSpy) BranchEntriesByID(ids []string) ([]session.Entry, error) {
	s.calls++
	s.maxPage = max(s.maxPage, len(ids))
	s.totalIDs += len(ids)
	return s.lookup.BranchEntriesByID(ids)
}

func TestPaginatedHydrationBoundsMessageBlobPages(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	for i := range 5000 {
		message := protocol.NewAssistantMessage(fmt.Sprintf("bounded-%04d", i), m.app.Session.BranchTip(), "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "visible row"}}, protocol.StopStop, nil)
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	spy := &paginatedHydrationSpy{
		Store:     m.app.Session,
		hydration: m.app.Session.(session.BranchHydrationStore),
		lookup:    m.app.Session.(session.BranchEntryLookup),
	}
	originalSession := m.app.Session
	m.app.Session = spy
	m.hydrateSession()
	m.app.Session = originalSession
	if spy.calls == 0 || spy.maxPage > sessionHydrationPageSize {
		t.Fatalf("lookup calls=%d max page=%d", spy.calls, spy.maxPage)
	}
	if spy.totalIDs != maxTranscriptEntries-1 {
		t.Fatalf("decoded message blobs=%d want=%d", spy.totalIDs, maxTranscriptEntries-1)
	}
	if len(m.lines) != maxTranscriptEntries {
		t.Fatalf("transcript rows=%d want=%d", len(m.lines), maxTranscriptEntries)
	}
}

func TestPaginatedHydrationSkipsDurableToolLookbehind(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.layout()
	call := protocol.NewAssistantMessage("display-call", m.app.Session.BranchTip(), "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "displayed", Name: "bash", Arguments: json.RawMessage(`{"command":"echo displayed"}`)}}, protocol.StopToolUse, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: call.ID, Message: &call}); err != nil {
		t.Fatal(err)
	}
	result := protocol.NewToolResultMessage("display-result", m.app.Session.BranchTip(), "displayed", "bash", []protocol.ContentBlock{protocol.NewTextBlock("displayed")}, false)
	result.ToolDisplay = &protocol.ToolDisplay{Started: true, StartMessage: "echo displayed", Output: "displayed"}
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result}); err != nil {
		t.Fatal(err)
	}
	spy := &paginatedHydrationSpy{
		Store:     m.app.Session,
		hydration: m.app.Session.(session.BranchHydrationStore),
		lookup:    m.app.Session.(session.BranchEntryLookup),
	}
	originalSession := m.app.Session
	m.app.Session = spy
	m.hydrateSession()
	m.app.Session = originalSession
	if spy.totalIDs != 1 {
		t.Fatalf("decoded ids=%d want only durable tool result", spy.totalIDs)
	}
	if plain := stripANSI(strings.Join(m.lines, "\n")); !strings.Contains(plain, "echo displayed") {
		t.Fatalf("durable tool label missing: %q", plain)
	}
}

func TestPaginatedHydrationMatchesLegacyProjection(t *testing.T) {
	fast := newModel(context.Background(), app.Options{})
	buildAppForTest(t, fast)
	fast.width, fast.height = 100, 30
	fast.layout()

	oldUser := protocol.NewUserMessage("old-user", fast.app.Session.BranchTip(), "  old input\nkept exactly  ")
	if err := fast.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: oldUser.ID, Message: &oldUser}); err != nil {
		t.Fatal(err)
	}
	oldPlan := protocol.NewAssistantMessage("old-plan", fast.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{
		{Type: protocol.BlockThinking, Text: "old thought"},
		{Type: protocol.BlockPlan, Text: "plan outside visible suffix"},
	}, protocol.StopStop, nil)
	if err := fast.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: oldPlan.ID, Message: &oldPlan}); err != nil {
		t.Fatal(err)
	}
	call := protocol.NewAssistantMessage("old-call", fast.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "paged-call", Name: "bash", Arguments: json.RawMessage(`{"command":"paged command"}`)}}, protocol.StopToolUse, nil)
	if err := fast.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: call.ID, Message: &call}); err != nil {
		t.Fatal(err)
	}
	for i := range maxTranscriptEntries + 25 {
		message := protocol.NewAssistantMessage(fmt.Sprintf("row-%04d", i), fast.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: fmt.Sprintf("row %04d", i)}}, protocol.StopStop, nil)
		if err := fast.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
			t.Fatal(err)
		}
	}
	result := protocol.NewToolResultMessage("paged-result", fast.app.Session.BranchTip(), "paged-call", "bash", []protocol.ContentBlock{protocol.NewTextBlock("paged output")}, false)
	if err := fast.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result}); err != nil {
		t.Fatal(err)
	}

	legacy := newModel(context.Background(), app.Options{})
	legacy.width, legacy.height = fast.width, fast.height
	legacy.layout()
	originalSession := fast.app.Session
	legacyStore := legacyHydrationStore{
		Store:   originalSession,
		branch:  originalSession.(session.BranchEntryStore),
		context: originalSession.(session.ContextStore),
	}
	legacy.app = fast.app

	fast.hydrateSession()
	fast.app.Session = legacyStore
	legacy.hydrateSession()
	fast.app.Session = originalSession
	if got, want := stripANSI(strings.Join(fast.lines, "\n")), stripANSI(strings.Join(legacy.lines, "\n")); got != want {
		t.Fatalf("paginated transcript differs from legacy\nfast:   %q\nlegacy: %q", got, want)
	}
	if !slices.Equal(fast.inputHistory, legacy.inputHistory) {
		t.Fatalf("input history fast=%q legacy=%q", fast.inputHistory, legacy.inputHistory)
	}
	if fast.latestPlan != legacy.latestPlan || fast.latestPlan != "plan outside visible suffix" {
		t.Fatalf("latest plan fast=%q legacy=%q", fast.latestPlan, legacy.latestPlan)
	}
	if fast.transcriptDropped != legacy.transcriptDropped || fast.contextTokens != legacy.contextTokens || fast.contextEstimated != legacy.contextEstimated {
		t.Fatalf("state fast=(dropped=%d tokens=%d estimated=%t) legacy=(dropped=%d tokens=%d estimated=%t)", fast.transcriptDropped, fast.contextTokens, fast.contextEstimated, legacy.transcriptDropped, legacy.contextTokens, legacy.contextEstimated)
	}
}

func TestPaginatedHydrationContextUsageMatchesMessageProjection(t *testing.T) {
	cases := []struct {
		name       string
		compaction bool
		usage      bool
	}{
		{name: "estimated"},
		{name: "latest usage plus tail", usage: true},
		{name: "compacted", usage: true, compaction: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := session.NewMemoryStore(session.Options{})
			defer store.Close()
			firstUsage := (*protocol.Usage)(nil)
			if tc.usage {
				firstUsage = &protocol.Usage{Input: 80, Output: 20, Total: 100}
			}
			first := protocol.NewAssistantMessage("first", store.BranchTip(), "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "measured text"}}, protocol.StopStop, firstUsage)
			if err := store.Append(session.Entry{Type: session.EntryMessage, ID: first.ID, Message: &first}); err != nil {
				t.Fatal(err)
			}
			if tc.compaction {
				if err := store.Append(session.Entry{Type: session.EntryCompaction, ID: "compact", Summary: "summary", CompactedThrough: first.ID}); err != nil {
					t.Fatal(err)
				}
			}
			tail := protocol.NewAssistantMessage("tail", store.BranchTip(), "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: "tail text"}}, protocol.StopStop, nil)
			if err := store.Append(session.Entry{Type: session.EntryMessage, ID: tail.ID, Message: &tail}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := store.BranchHydration()
			if err != nil {
				t.Fatal(err)
			}
			projected, err := store.ContextMessages()
			if err != nil {
				t.Fatal(err)
			}
			want := projectedContextUsage(projected, tc.compaction)
			got := hydrationContextUsage(snapshot.ContextUsage)
			if got.tokens != want.tokens || got.estimated != want.estimated || !reflect.DeepEqual(got.usage, want.usage) {
				t.Fatalf("usage mismatch got=%+v want=%+v", got, want)
			}
		})
	}
}

func TestModelHydrationPairsReusedToolCallIDsWithNearestCall(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	for i, command := range []string{"first-command", "second-command"} {
		assistantID := fmt.Sprintf("assistant-%d", i)
		assistant := protocol.NewAssistantMessage(
			assistantID,
			m.app.Session.BranchTip(),
			"fake",
			"fake-model",
			[]protocol.ContentBlock{{Type: protocol.BlockToolCall, ToolCallID: "reused-call", Name: "bash", Arguments: json.RawMessage(fmt.Sprintf(`{"command":%q}`, command))}},
			protocol.StopToolUse,
			nil,
		)
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, ParentID: assistant.ParentID, Message: &assistant}); err != nil {
			t.Fatal(err)
		}
		result := protocol.NewToolResultMessage(fmt.Sprintf("result-%d", i), m.app.Session.BranchTip(), "reused-call", "bash", []protocol.ContentBlock{protocol.NewTextBlock(command + " output")}, false)
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, ParentID: result.ParentID, Message: &result}); err != nil {
			t.Fatal(err)
		}
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	first := strings.Index(plain, "✓ first-command")
	second := strings.Index(plain, "✓ second-command")
	if first < 0 || second <= first || strings.Count(plain, "✓ first-command") != 1 || strings.Count(plain, "✓ second-command") != 1 {
		t.Fatalf("reused tool-call IDs paired incorrectly: %q", plain)
	}
}

func TestModelHydratesExplicitSkillToolTranscript(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	user := protocol.NewUserMessage("skill-user", m.app.Session.BranchTip(), "$review check this")
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(protocol.ToolTranscript{
		ToolName: "activate_skill",
		Display: protocol.ToolDisplay{
			Started:      true,
			StartMessage: "activating explicitly requested skill review",
			Output:       "activated skill review",
			DurationMS:   9,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMeta, ID: "skill-transcript", Key: session.MetaToolTranscript, Value: string(encoded)}); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	for _, want := range []string{"$review check this", "✔ activate_skill activating explicitly requested skill review  (9ms)", "skill instructions loaded"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("resumed explicit skill transcript missing %q: %q", want, plain)
		}
	}
}

func TestModelHydratesFailedExplicitSkillLikeLive(t *testing.T) {
	resumed := newModel(context.Background(), app.Options{})
	buildAppForTest(t, resumed)
	resumed.width = 100
	resumed.height = 30
	resumed.layout()

	encoded, err := json.Marshal(protocol.ToolTranscript{
		ToolName: "activate_skill",
		IsError:  true,
		Display: protocol.ToolDisplay{
			Started:      true,
			StartMessage: "activating explicitly requested skill review",
			Output:       "skill activation failed",
			DurationMS:   9,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.app.Session.Append(session.Entry{Type: session.EntryMeta, ID: "failed-skill-transcript", Key: session.MetaToolTranscript, Value: string(encoded)}); err != nil {
		t.Fatal(err)
	}
	resumed.hydrateSession()

	live := newModel(context.Background(), app.Options{})
	buildAppForTest(t, live)
	live.width = 100
	live.height = 30
	live.layout()
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "skill-call", ToolName: "activate_skill", Message: "activating explicitly requested skill review"})
	live.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "skill-call", ToolName: "activate_skill", Message: "skill activation failed", ToolOutput: "skill activation failed", ToolDurationMS: 9, IsError: true})

	resumedPlain := stripANSI(strings.Join(resumed.lines, "\n"))
	livePlain := stripANSI(strings.Join(live.lines, "\n"))
	if resumedPlain != livePlain {
		t.Fatalf("failed skill transcript differs after resume:\nresume: %q\nlive:   %q", resumedPlain, livePlain)
	}
}

func TestModelHydratesLegacyInterruptedToolTurn(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	assistant := protocol.NewAssistantMessage(
		"legacy-assistant-tool",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "Reading the repository"},
			{Type: protocol.BlockToolCall, ToolCallID: "legacy-call", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)},
		},
		protocol.StopToolUse,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	result := protocol.NewToolResultMessage(
		"legacy-tool-result",
		m.app.Session.BranchTip(),
		"legacy-call",
		"read",
		[]protocol.ContentBlock{protocol.NewTextBlock("README contents")},
		false,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result}); err != nil {
		t.Fatal(err)
	}
	aborted := protocol.NewAssistantMessage("legacy-aborted", m.app.Session.BranchTip(), "fake", "fake-model", nil, protocol.StopAborted, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: aborted.ID, Message: &aborted}); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	for _, want := range []string{"think: Reading the repository", "✔ read", "README contents", "aborted"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("legacy interrupted turn missing %q after resume: %q", want, plain)
		}
	}
}

func TestModelHydrationLimitsRenderedRowsNotRawMessages(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	user := protocol.NewUserMessage("old-user", m.app.Session.BranchTip(), "keep this visible")
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: user.ID, Message: &user}); err != nil {
		t.Fatal(err)
	}
	assistant := protocol.NewAssistantMessage("old-answer", m.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{protocol.NewTextBlock("durable answer")}, protocol.StopStop, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	for i := range maxTranscriptEntries + 10 {
		result := protocol.NewToolResultMessage(fmt.Sprintf("hidden-tool-%d", i), m.app.Session.BranchTip(), fmt.Sprintf("call-%d", i), "spawn_agent", []protocol.ContentBlock{protocol.NewTextBlock("hidden")}, false)
		result.ToolDisplay = &protocol.ToolDisplay{Started: true, Output: `{"path":"/root/child"}`}
		if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: result.ID, Message: &result}); err != nil {
			t.Fatal(err)
		}
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	for _, want := range []string{"keep this visible", "durable answer"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("renderable history was discarded before hidden tool messages: missing %q", want)
		}
	}
	if strings.Contains(plain, "older transcript entries omitted") {
		t.Fatalf("non-rendered raw messages consumed the transcript row limit: %q", plain)
	}
}

func TestModelHydrationRetainsExactBoundaryRowsAndLatestOmittedPlan(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	boundary := protocol.NewAssistantMessage("boundary", m.app.Session.BranchTip(), "fake", "fake-model", []protocol.ContentBlock{
		{Type: protocol.BlockText, Text: "drop-boundary-one"},
		{Type: protocol.BlockPlan, Text: "omitted latest plan"},
		{Type: protocol.BlockText, Text: "drop-boundary-three"},
		{Type: protocol.BlockText, Text: "keep-boundary-four"},
	}, protocol.StopStop, nil)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: boundary.ID, Message: &boundary}); err != nil {
		t.Fatal(err)
	}
	batch := make([]session.Entry, maxTranscriptEntries-2)
	for i := range batch {
		message := protocol.NewUserMessage(fmt.Sprintf("recent-%d", i), "", fmt.Sprintf("recent row %d", i))
		batch[i] = session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}
	}
	if err := m.app.Session.(session.BatchStore).AppendBatch(batch); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	plain := stripANSI(strings.Join(m.lines, "\n"))
	if !strings.Contains(plain, "── 3 older transcript entries omitted ──") {
		t.Fatalf("omission marker changed: %q", plain[:min(len(plain), 500)])
	}
	if strings.Contains(plain, "drop-boundary-one") || strings.Contains(plain, "drop-boundary-three") || strings.Contains(plain, "omitted latest plan") {
		t.Fatal("rows before the retained boundary remained visible")
	}
	if !strings.Contains(plain, "keep-boundary-four") || !strings.Contains(plain, "recent row 1997") {
		t.Fatal("retained hydration suffix is incomplete")
	}
	if m.latestPlan != "omitted latest plan" {
		t.Fatalf("latestPlan=%q", m.latestPlan)
	}
}

func TestModelHydratesPersistedThinking(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width = 100
	m.height = 30
	m.layout()

	assistant := protocol.NewAssistantMessage(
		"assistant-thinking",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "**Planning changes**"},
			{Type: protocol.BlockText, Text: "Done."},
		},
		protocol.StopStop,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	m.hydrateSession()
	plain := stripANSI(m.transcript.View())
	for _, want := range []string{"think: Planning changes", "Done."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("hydrated transcript missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "**") {
		t.Fatalf("hydrated thinking exposed Markdown markers: %q", plain)
	}
}

// TestModelAbortOnCtrlC verifies the busy-abort path returns no quit cmd.
func TestModelAbortOnCtrlC(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.busy = true
	_, quit := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit != nil {
		t.Fatal("ctrl+c while busy should abort, not quit")
	}
	// busy is cleared when the EvAborted event arrives from the agent; a
	// second ctrl+c while idle should quit.
	m.busy = false
	_, quit = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit == nil {
		t.Fatal("ctrl+c while idle should quit")
	}
}

func TestModelAbortOnEscDuringActiveRun(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.width, m.height = 100, 30
	m.busy = true
	m.runStartedAt = time.Unix(100, 0)
	cancelled, cancel := context.WithCancel(t.Context())
	m.cancelRun = cancel
	m.layout()

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc while running returned an unexpected command")
	}
	select {
	case <-cancelled.Done():
	default:
		t.Fatal("esc did not cancel the active run")
	}
	plain := stripANSI(strings.Join(m.lines, "\n"))
	if strings.Count(plain, "aborted") != 1 || strings.Contains(plain, "aborting") {
		t.Fatalf("abort status should be one simple line: %q", plain)
	}

	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvAborted})
	plain = stripANSI(strings.Join(m.lines, "\n"))
	if strings.Count(plain, "aborted") != 1 {
		t.Fatalf("terminal abort event duplicated status: %q", plain)
	}
}

func TestActiveToolProgressBufferReleasedAtCompletion(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolStart, ToolCallID: "call", ToolName: "read", Message: "running"})
	for range 1000 {
		m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolProgress, ToolCallID: "call", ToolName: "read", Message: strings.Repeat("x", 256)})
	}
	if m.activeToolRows != 1001 || m.activeToolText.Len() == 0 {
		t.Fatalf("active progress rows=%d bytes=%d", m.activeToolRows, m.activeToolText.Len())
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvToolEnd, ToolCallID: "call", ToolName: "read"})
	if m.activeToolRows != 0 || m.activeToolText.Len() != 0 || m.activeToolText.Cap() != 0 {
		t.Fatalf("completed progress buffer rows=%d len=%d cap=%d", m.activeToolRows, m.activeToolText.Len(), m.activeToolText.Cap())
	}
}

func TestFormatRunElapsed(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "0s",
		16*time.Second + 900*time.Millisecond: "16s",
		76 * time.Second:                      "1m 16s",
		time.Hour + 2*time.Minute + 3*time.Second: "1h 2m 3s",
	}
	for elapsed, want := range tests {
		if got := formatRunElapsed(elapsed); got != want {
			t.Fatalf("formatRunElapsed(%s) = %q, want %q", elapsed, got, want)
		}
	}
}
