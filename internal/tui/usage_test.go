package tui

import (
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
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

func TestFooterShowsZeroContextBeforeFirstTurn(t *testing.T) {
	m := &Model{
		app:   &app.App{Model: protocol.Model{ContextWindow: 400000}},
		width: 80,
	}
	if footer := stripANSI(m.renderFooter()); !strings.Contains(footer, "context: 0/400k") {
		t.Fatalf("footer = %q", footer)
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
