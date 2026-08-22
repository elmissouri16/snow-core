package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestStreamingAgentTextSanitizesTerminalControls(t *testing.T) {
	const unsafe = "visible\x1b[2J\x1b]52;c;Y2xpcGJvYXJk\x07\u009b31m"
	for _, eventType := range []protocol.AgentEventType{
		protocol.EvTextDelta,
		protocol.EvThinkingDelta,
		protocol.EvPlanDelta,
	} {
		t.Run(string(eventType), func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			m.handleAgentEvent(protocol.AgentEvent{Type: eventType, Text: unsafe})
			rendered := m.liveText()
			for _, control := range []string{"\x1b[2J", "\x1b]52", "\x07", "\u009b"} {
				if strings.Contains(rendered, control) {
					t.Fatalf("stream retained terminal control %q: %q", control, rendered)
				}
			}
			if !strings.Contains(rendered, "visible") {
				t.Fatalf("stream lost ordinary text: %q", rendered)
			}
		})
	}
}

func TestPlanAndErrorRenderingSanitizesTerminalControls(t *testing.T) {
	const unsafe = "unsafe\x1b[2J\x1b]2;spoofed\x07"
	m := newModel(context.Background(), app.Options{})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvPlanUpdate, PlanUpdate: &protocol.PlanUpdate{
		Explanation: unsafe,
		Plan:        []protocol.PlanStep{{Step: unsafe, Status: protocol.PlanStepPending}},
	}})
	m.handleAgentEvent(protocol.AgentEvent{Type: protocol.EvError, Message: unsafe})

	rendered := strings.Join(m.lines, "\n")
	for _, control := range []string{"\x1b[2J", "\x1b]2", "\x07"} {
		if strings.Contains(rendered, control) {
			t.Fatalf("durable transcript retained terminal control %q: %q", control, rendered)
		}
	}
}

func TestCompactAgentTextSanitizesTerminalControls(t *testing.T) {
	got := compactAgentText("child\x1b[2J output\x07", 100)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\x07") {
		t.Fatalf("subagent text retained terminal controls: %q", got)
	}
	if got != "child[2J output" {
		t.Fatalf("sanitized subagent text = %q", got)
	}
}
