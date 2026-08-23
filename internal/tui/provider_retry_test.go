package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestProviderRetryRendersAsNonterminalProgress(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.busy = true
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "partial"})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvProviderRetry, ProviderRetry: &protocol.ProviderRetry{
		Provider: "test", Kind: "rate_limit", Phase: "recovery", Attempt: 2, MaxAttempts: 12, DelayMS: 1500, MaxElapsedMS: 300000,
	}})
	rendered := strings.Join(m.lines, "\n")
	if !strings.Contains(rendered, "provider rate limited") || !strings.Contains(rendered, "retry 2/12") || !strings.Contains(rendered, "1.5s") {
		t.Fatalf("rendered=%q", rendered)
	}
	if !m.busy || m.lastErrorText != "" {
		t.Fatalf("retry changed terminal state: busy=%v lastError=%q", m.busy, m.lastErrorText)
	}
}
