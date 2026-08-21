package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestFooterAlwaysShowsContextUsageOnRight(t *testing.T) {
	m := &Model{
		app:           &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width:         100,
		contextTokens: 1234,
	}
	footer := stripANSI(m.renderFooter())
	if !strings.Contains(footer, "context: 1.2k/128k") {
		t.Fatalf("footer = %q", footer)
	}
	if !strings.HasSuffix(strings.TrimRight(footer, " "), "context: 1.2k/128k") {
		t.Fatalf("context usage is not right aligned: %q", footer)
	}
	for _, hint := range []string{"ctrl+c abort", "@ files", "/help", "/model", "/login", "pgup/pgdn"} {
		if strings.Contains(footer, hint) {
			t.Fatalf("footer contains removed hint %q: %q", hint, footer)
		}
	}
}

func TestFooterShowsLatestKnownCacheHitRateBesideContext(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width: 100,
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{
		Input: 1000, Output: 50, CacheRead: 750, CacheReadKnown: true, Total: 1050,
	}})
	footer := stripANSI(m.renderFooter())
	if !strings.Contains(footer, "CH75.0% · context: 1.1k/128k") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestFooterCacheHitUsesLatestRequestNotTurnAggregate(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width: 100,
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{
		Input: 1000, CacheRead: 250, CacheReadKnown: true, Total: 1000,
	}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, Usage: &protocol.Usage{
		Input: 4000, CacheRead: 3000, CacheReadKnown: true, Total: 4000, Requests: 2,
	}})
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "CH25.0% · context:") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestFooterTreatsPositiveLegacyCacheReadAsKnown(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width: 100,
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{
		Input: 1000, CacheRead: 500, Total: 1000,
	}})
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "CH50.0% · context:") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestFooterOmitsUnknownCacheHitRate(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width: 100,
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{Input: 1000, Total: 1000}})
	if footer := stripANSI(m.renderFooter()); strings.Contains(footer, "CH") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestFooterShowsKnownCacheMissAsZeroPercent(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 128000}},
		width: 100,
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{
		Input: 1000, CacheReadKnown: true, Total: 1000,
	}})
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "CH0.0% · context:") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestHeaderUsesSnowLogo(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	m.width = 80

	if header := stripANSI(m.renderHeader("ready")); !strings.Contains(header, "❄ snow") {
		t.Fatalf("header missing Snow logo: %q", header)
	}
}

func TestHeaderShowsThinkingImmediatelyAfterModel(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 160

	model := m.app.Agent.Model()
	want := m.app.ProviderID + "/" + model.ID + "  ·  thinking:" + string(m.app.Agent.Thinking())
	if header := stripANSI(m.renderHeader("ready")); !strings.Contains(header, want) {
		t.Fatalf("header does not place thinking after model; want %q in %q", want, header)
	}
}

func TestGoalTokenUsageIsCompactInHeaderAndFooter(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 220
	budget := int64(5_000_000)
	m.goal = &protocol.ThreadGoal{Status: protocol.GoalComplete, TokensUsed: 2_121_170, TokenBudget: &budget, EstimatedCosts: []protocol.Cost{{Currency: "USD", Total: 0.018279814}}}
	want := "goal:complete 2.1m/5m tks · est. $0.0183"
	for surface, rendered := range map[string]string{
		"header": stripANSI(m.renderHeader("ready")),
		"footer": stripANSI(m.renderFooter()),
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("%s missing formatted goal usage %q: %q", surface, want, rendered)
		}
		if strings.Contains(rendered, "2121170") || strings.Contains(rendered, "5000000") {
			t.Fatalf("%s contains unformatted goal usage: %q", surface, rendered)
		}
	}
}

func TestGoalUpdateRefreshesFooterUsageDuringActiveTurn(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.width = 160
	m.busy = true
	m.activeTurnID = "goal-turn"
	m.goal = &protocol.ThreadGoal{GoalID: "goal", Status: protocol.GoalActive, TokensUsed: 100}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvThreadGoalUpdated, TurnID: "goal-turn", GoalContinuing: true,
		ThreadGoal: &protocol.ThreadGoalUpdate{Goal: &protocol.ThreadGoal{GoalID: "goal", Status: protocol.GoalActive, TokensUsed: 2400}}})
	footer := stripANSI(m.renderFooter())
	if !strings.Contains(footer, "goal:active 2.4k tks") {
		t.Fatalf("footer did not refresh live goal usage: %q", footer)
	}
	if !m.busy {
		t.Fatal("live goal update unlocked active turn")
	}
}

func TestEstimatedGoalCostFormattingPreservesCurrency(t *testing.T) {
	goal := &protocol.ThreadGoal{TokensUsed: 10, EstimatedCosts: []protocol.Cost{{Currency: "EUR", Total: 1.25}, {Currency: "USD", Total: 0.00001}}}
	if got, want := formatGoalTokenUsage(goal), "10 tks · est. EUR 1.25 + <$0.0001"; got != want {
		t.Fatalf("goal usage=%q want %q", got, want)
	}
}

func TestContextUsageColorBands(t *testing.T) {
	tests := []struct {
		current int
		want    string
	}{
		{0, "healthy"},
		{49, "healthy"},
		{50, "notice"},
		{69, "notice"},
		{70, "warning"},
		{89, "warning"},
		{90, "critical"},
		{125, "critical"},
	}
	for _, test := range tests {
		if got := contextUsageBand(test.current, 100); got != test.want {
			t.Fatalf("context band at %d%%=%q want %q", test.current, got, test.want)
		}
	}
	if got := contextUsageBand(10, 0); got != "unknown" {
		t.Fatalf("unknown context band=%q", got)
	}
}

func TestFooterShowsZeroContextBeforeFirstTurn(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 400000}},
		width: 80,
	}
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "context: 0/400k") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestInlineFooterDropsCacheHitWithoutRestoringLongGoalPrefix(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	buildAppForTest(t, m)
	m.inlineTranscript = true
	m.width = 70
	m.goal = &protocol.ThreadGoal{Status: protocol.GoalActive, TokensUsed: 2400}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{
		Input: 1000, CacheRead: 500, CacheReadKnown: true, Total: 1000,
	}})
	footer := stripANSI(m.renderFooter())
	if strings.Contains(footer, "CH50.0%") {
		t.Fatalf("narrow footer retained cache hit: %q", footer)
	}
	if !strings.Contains(footer, "fake-1 · default/off · context:") {
		t.Fatalf("narrow footer lost compact runtime prefix: %q", footer)
	}
}

func TestFooterTruncatesAtNarrowWidths(t *testing.T) {
	m := &Model{
		app:           &app.App{Model: protocol.Model{ContextWindow: 400000}},
		width:         40,
		contextTokens: 123456,
	}
	footer := stripANSI(m.renderFooter())
	if len([]rune(footer)) > 40 {
		t.Fatalf("footer exceeds terminal width: %q (%d runes)", footer, len([]rune(footer)))
	}
}

func TestBusySessionUpdateDoesNotRescanContext(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	usage := &protocol.Usage{Input: 8000, Output: 1000, Total: 9000}
	assistant := protocol.NewAssistantMessage("usage", "", "fake", "fake-model", nil, protocol.StopStop, usage)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: assistant.ID, Message: &assistant}); err != nil {
		t.Fatal(err)
	}
	m.contextTokens = 123
	m.busy = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated})
	if m.contextTokens != 123 {
		t.Fatalf("busy session update context=%d want unchanged 123", m.contextTokens)
	}
	m.busy = false
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvSessionUpdated, TurnID: "aborted-turn"})
	if m.contextTokens != 123 {
		t.Fatalf("attributed idle session update context=%d want unchanged 123", m.contextTokens)
	}
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvAborted, TurnID: "aborted-turn"})
	cmd := m.scheduleContextUsageRefresh()
	if cmd == nil {
		t.Fatal("usage-less abort did not schedule context refresh")
	}
	msg := cmd()
	if _, ok := msg.(contextUsageRefreshMsg); !ok {
		t.Fatalf("refresh command returned %T", msg)
	}
	_, _ = m.update(msg)
	if m.contextTokens != 9000 {
		t.Fatalf("terminal refresh context=%d want 9000", m.contextTokens)
	}
}

func TestAsyncContextRefreshDoesNotOverwriteNewUsage(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.contextRefreshNeeded = true
	cmd := m.scheduleContextUsageRefresh()
	if cmd == nil {
		t.Fatal("context refresh was not scheduled")
	}
	msg := cmd()
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvUsage, Usage: &protocol.Usage{Total: 77}})
	_, _ = m.update(msg)
	if m.contextTokens != 77 {
		t.Fatalf("stale refresh overwrote context: %d", m.contextTokens)
	}
}

func TestOverlappingUsageLessBoundariesRejectOlderRefresh(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTurnDone, TurnID: "first"})
	first := m.scheduleContextUsageRefresh()
	if first == nil {
		t.Fatal("first refresh was not scheduled")
	}
	firstMsg := first().(contextUsageRefreshMsg)
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvAborted, TurnID: "second"})
	m.contextTokens = 77
	_, next := m.update(firstMsg)
	if m.contextTokens != 77 {
		t.Fatalf("older refresh overwrote newer boundary state: %d", m.contextTokens)
	}
	if next == nil {
		t.Fatal("newer boundary did not schedule a replacement refresh")
	}
}

func TestUsageEventTracksCurrentContextNotTurnAggregate(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 200000}},
		width: 80,
	}
	m.handleAgentEvent(protocol.AgentEvent{
		Type:  protocol.EvUsage,
		Usage: &protocol.Usage{Input: 1200, Output: 50, Total: 1250},
	})
	m.handleAgentEvent(protocol.AgentEvent{
		Type:  protocol.EvTurnDone,
		Usage: &protocol.Usage{Input: 5000, Output: 500, Total: 5500, Requests: 3},
	})
	if m.contextTokens != 1250 || m.contextEstimated {
		t.Fatalf("context = %d estimated=%v, want exact 1250", m.contextTokens, m.contextEstimated)
	}
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "context: 1.2k/200k") {
		t.Fatalf("footer = %q", footer)
	}
}
