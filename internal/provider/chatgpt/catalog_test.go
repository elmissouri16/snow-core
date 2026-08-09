package chatgpt

import (
	"context"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestModelsReturnsCodexCatalog(t *testing.T) {
	models, err := New().ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.3-codex-spark"}
	if len(models) != len(want) {
		t.Fatalf("catalog has %d models, want %d", len(models), len(want))
	}
	for i, model := range models {
		if model.Provider != ProviderID || model.ID != want[i] || !model.SupportsTools {
			t.Fatalf("invalid catalog model at %d: %+v", i, model)
		}
	}
}

func TestResolveRequiresChatGPTOAuth(t *testing.T) {
	if _, err := New().Resolve(context.Background(), auth.Credential{}); err == nil {
		t.Fatal("empty credential should fail")
	}
}

func TestChatRejectsMissingOAuth(t *testing.T) {
	stream, err := New().Chat(context.Background(), auth.Credential{}, protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, nextErr := stream.Next(context.Background())
	if nextErr != nil {
		t.Fatal(nextErr)
	}
	if ev.Type != protocol.EvStreamError {
		t.Fatalf("event type = %q, want stream error", ev.Type)
	}
}
