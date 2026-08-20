package chatgpt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestStaticCatalogAdvertisesNormalizedThinkingLevels(t *testing.T) {
	models, err := New().ListModels(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("static catalog is empty")
	}
	for _, model := range models {
		if !model.SupportsThinking {
			t.Fatalf("model does not advertise thinking: %+v", model)
		}
		if model.SupportsReasoningSummary != nil {
			t.Fatalf("bundled model should preserve legacy summary behavior: %+v", model)
		}
		levels := model.SupportedThinkingLevels()
		wantLen := 4
		if model.ID == "gpt-5.6-sol" {
			wantLen = 7
		}
		if len(levels) != wantLen || levels[0] != protocol.ThinkingOff || levels[1] != protocol.ThinkingLow || levels[3] != protocol.ThinkingHigh {
			t.Fatalf("model %q levels = %v", model.ID, levels)
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
			protocol.ThinkingUltra,
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
		protocol.ThinkingUltra:   "ultra",
	}
	for level, native := range want {
		body, err := buildResponsesBody(protocol.ChatRequest{Model: model, Thinking: level})
		if err != nil {
			t.Fatalf("buildResponsesBody(%q): %v", level, err)
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
			body, err := buildResponsesBody(protocol.ChatRequest{
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

	body, err := buildResponsesBody(protocol.ChatRequest{
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
	body, err = buildResponsesBody(protocol.ChatRequest{
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
	_, err := buildResponsesBody(protocol.ChatRequest{
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
