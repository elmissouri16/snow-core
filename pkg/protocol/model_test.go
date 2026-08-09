package protocol

import (
	"strings"
	"testing"
)

func TestThinkingLevelParsingAndNormalization(t *testing.T) {
	for _, want := range KnownThinkingLevels() {
		got, err := ParseThinkingLevel(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseThinkingLevel(%q) = %q, %v", want, got, err)
		}
	}
	if got, err := ParseThinkingLevel(""); err != nil || got != ThinkingOff {
		t.Fatalf("empty level = %q, %v, want off", got, err)
	}
	if _, err := ParseThinkingLevel("xhigh"); err == nil || !strings.Contains(err.Error(), "off|minimal|low|medium|high") {
		t.Fatalf("invalid level error = %v", err)
	}
}

func TestResponseSettingParsingAndDefaults(t *testing.T) {
	if got := NormalizeReasoningSummary(""); got != ReasoningSummaryAuto {
		t.Fatalf("empty reasoning summary = %q, want auto", got)
	}
	for _, want := range KnownReasoningSummaries() {
		got, err := ParseReasoningSummary(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseReasoningSummary(%q) = %q, %v", want, got, err)
		}
	}
	if _, err := ParseReasoningSummary("full"); err == nil || !strings.Contains(err.Error(), "off|auto|concise|detailed") {
		t.Fatalf("invalid summary error = %v", err)
	}
	if got := NormalizeTextVerbosity(""); got != TextVerbosityLow {
		t.Fatalf("empty verbosity = %q, want low", got)
	}
	for _, want := range KnownTextVerbosities() {
		got, err := ParseTextVerbosity(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseTextVerbosity(%q) = %q, %v", want, got, err)
		}
	}
	if _, err := ParseTextVerbosity("maximum"); err == nil || !strings.Contains(err.Error(), "low|medium|high") {
		t.Fatalf("invalid verbosity error = %v", err)
	}
}

func TestModelCloneIsDeep(t *testing.T) {
	summarySupported := true
	original := Model{
		ID: "m", ThinkingLevels: []ThinkingLevel{ThinkingHigh},
		SupportsReasoningSummary: &summarySupported,
		Upgrade:                  &ModelUpgrade{Model: "next", Message: "move"},
		Pricing:                  &ModelPricing{InputPerMillion: 1},
	}
	clone := original.Clone()
	clone.ThinkingLevels[0] = ThinkingLow
	*clone.SupportsReasoningSummary = false
	clone.Upgrade.Model = "changed"
	clone.Pricing.InputPerMillion = 2
	if original.ThinkingLevels[0] != ThinkingHigh || !*original.SupportsReasoningSummary || original.Upgrade.Model != "next" || original.Pricing.InputPerMillion != 1 {
		t.Fatalf("clone aliases source: original=%+v clone=%+v", original, clone)
	}

	eventClone := (AgentEvent{Model: &original}).Clone()
	*eventClone.Model.SupportsReasoningSummary = false
	eventClone.Model.Upgrade.Model = "event-changed"
	if !*original.SupportsReasoningSummary || original.Upgrade.Model != "next" {
		t.Fatalf("event clone aliases source model: %+v", original)
	}
}

func TestModelThinkingCapabilitiesAreConservative(t *testing.T) {
	model := Model{
		ID:               "m",
		SupportsThinking: true,
		ThinkingLevels:   []ThinkingLevel{ThinkingLow, ThinkingHigh, ThinkingLow, "future"},
	}
	got := model.SupportedThinkingLevels()
	want := []ThinkingLevel{ThinkingOff, ThinkingLow, ThinkingHigh}
	if len(got) != len(want) {
		t.Fatalf("supported levels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("supported levels = %v, want %v", got, want)
		}
	}
	if !model.SupportsThinkingLevel(ThinkingOff) || !model.SupportsThinkingLevel(ThinkingLow) || model.SupportsThinkingLevel(ThinkingMedium) {
		t.Fatalf("unexpected membership for %+v", model)
	}

	unknown := Model{ID: "unknown", SupportsThinking: true}
	if got := unknown.SupportedThinkingLevels(); len(got) != 1 || got[0] != ThinkingOff || unknown.SupportsThinkingLevel(ThinkingHigh) {
		t.Fatalf("unknown model advertised an effort: %v", got)
	}
	noThinking := Model{ID: "no-thinking", ThinkingLevels: []ThinkingLevel{ThinkingHigh}}
	if noThinking.SupportsThinkingLevel(ThinkingHigh) {
		t.Fatal("model without thinking support accepted high")
	}
}
