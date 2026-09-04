package chatgpt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestStaticCatalogDoesNotGuessThinkingLevels(t *testing.T) {
	models, err := New().ListModels(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("static catalog is empty")
	}
	for _, model := range models {
		if model.SupportsThinking || model.DefaultThinking != "" || len(model.ThinkingLevels) != 0 {
			t.Fatalf("static model guessed backend thinking metadata: %+v", model)
		}
		levels := model.SupportedThinkingLevels()
		if len(levels) != 1 || levels[0] != protocol.ThinkingOff {
			t.Fatalf("static model %q levels = %v, want [off]", model.ID, levels)
		}
		if model.SupportsReasoningSummary != nil {
			t.Fatalf("bundled model should preserve legacy summary behavior: %+v", model)
		}
	}
}

func TestBuildResponsesBodyThinkingMapping(t *testing.T) {
	model := protocol.Model{
		Provider:         ProviderID,
		ID:               "gpt-test",
		SupportsThinking: true,
		ThinkingLevels: []protocol.ThinkingLevel{
			protocol.ThinkingMinimal,
			protocol.ThinkingLow,
			protocol.ThinkingMedium,
			protocol.ThinkingHigh,
			protocol.ThinkingXHigh,
			protocol.ThinkingMax,
		},
	}
	want := map[protocol.ThinkingLevel]string{
		protocol.ThinkingOff:     "",
		protocol.ThinkingMinimal: "minimal",
		protocol.ThinkingLow:     "low",
		protocol.ThinkingMedium:  "medium",
		protocol.ThinkingHigh:    "high",
		protocol.ThinkingXHigh:   "xhigh",
		protocol.ThinkingMax:     "max",
	}
	for level, native := range want {
		body, err := buildRequestBody(protocol.ChatRequest{Model: model, Thinking: level})
		if err != nil {
			t.Fatalf("buildRequestBody(%q): %v", level, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		reasoning, ok := decoded["reasoning"].(map[string]any)
		if native == "" {
			if ok {
				t.Fatalf("level %q serialized reasoning=%v, want omitted", level, reasoning)
			}
		} else if !ok || reasoning["effort"] != native || reasoning["summary"] != "auto" {
			t.Fatalf("level %q reasoning=%v, want effort=%q summary=auto", level, reasoning, native)
		}
	}
}

func TestChatRejectsCatalogOnlyUltraEffort(t *testing.T) {
	stream, err := New().Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{
		Model: protocol.Model{
			Provider:         ProviderID,
			ID:               "catalog-ultra",
			SupportsThinking: true,
			ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingUltra},
		},
		Thinking: protocol.ThinkingUltra,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EvStreamError || event.Err == nil || !strings.Contains(event.Err.Error(), "catalog-only ultra") {
		t.Fatalf("event = %+v, want catalog-only ultra error", event)
	}
}

func TestBuildResponsesBodyResponseSettings(t *testing.T) {
	model := protocol.Model{
		Provider: ProviderID, ID: "gpt-test", SupportsThinking: true, SupportsVerbosity: true,
		ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingHigh},
	}
	for _, tc := range []struct {
		name          string
		summary       protocol.ReasoningSummary
		verbosity     protocol.TextVerbosity
		wantSummary   string
		wantVerbosity string
	}{
		{name: "defaults", wantSummary: "auto", wantVerbosity: "low"},
		{name: "explicit", summary: protocol.ReasoningSummaryDetailed, verbosity: protocol.TextVerbosityHigh, wantSummary: "detailed", wantVerbosity: "high"},
		{name: "summary-off", summary: protocol.ReasoningSummaryOff, verbosity: protocol.TextVerbosityMedium, wantVerbosity: "medium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildRequestBody(protocol.ChatRequest{
				Model: model, Thinking: protocol.ThinkingHigh,
				ReasoningSummary: tc.summary, TextVerbosity: tc.verbosity,
			})
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			reasoning := decoded["reasoning"].(map[string]any)
			if got, _ := reasoning["summary"].(string); got != tc.wantSummary {
				t.Fatalf("summary = %q, want %q; body=%s", got, tc.wantSummary, body)
			}
			text := decoded["text"].(map[string]any)
			if got := text["verbosity"]; got != tc.wantVerbosity {
				t.Fatalf("verbosity = %v, want %q", got, tc.wantVerbosity)
			}
		})
	}

	body, err := buildRequestBody(protocol.ChatRequest{
		Model: model, Thinking: protocol.ThinkingOff,
		ReasoningSummary: protocol.ReasoningSummaryDetailed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"reasoning"`) {
		t.Fatalf("thinking off serialized reasoning: %s", body)
	}

	summaryUnsupported := false
	model.SupportsReasoningSummary = &summaryUnsupported
	body, err = buildRequestBody(protocol.ChatRequest{
		Model: model, Thinking: protocol.ThinkingHigh,
		ReasoningSummary: protocol.ReasoningSummaryDetailed,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	reasoning := decoded["reasoning"].(map[string]any)
	if _, ok := reasoning["summary"]; ok || reasoning["effort"] != "high" {
		t.Fatalf("explicitly unsupported summary parameter body = %s", body)
	}
}

func TestBuildResponsesBodyRejectsUnsupportedThinking(t *testing.T) {
	_, err := buildRequestBody(protocol.ChatRequest{
		Model: protocol.Model{
			Provider:         ProviderID,
			ID:               "low-only",
			SupportsThinking: true,
			ThinkingLevels:   []protocol.ThinkingLevel{protocol.ThinkingLow},
		},
		Thinking: protocol.ThinkingHigh,
	})
	if err == nil || !strings.Contains(err.Error(), "low-only") || !strings.Contains(err.Error(), "off|low") {
		t.Fatalf("error = %v, want actionable unsupported-effort error", err)
	}
}
