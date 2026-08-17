package opencodego

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestBuildBodySendsOnlyLatestCheckpointAsUserInput(t *testing.T) {
	provider := &Provider{defaultModel: "m", providerID: "opencode-go"}
	checkpoint := protocol.Message{
		ID:      "compaction-latest",
		Role:    protocol.RoleCustom,
		Content: []protocol.ContentBlock{protocol.NewTextBlock("Working-state checkpoint for compacted history:\nLATEST-CHECKPOINT-SENTINEL")},
	}
	body, err := provider.buildBody(protocol.ChatRequest{
		Model: protocol.Model{ID: "m"},
		Messages: []protocol.Message{
			checkpoint,
			protocol.NewUserMessage("recall", checkpoint.ID, "Recall the latest checkpoint."),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var request openAIChatRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "user" {
		t.Fatalf("wire messages=%+v", request.Messages)
	}
	if strings.Count(string(body), "LATEST-CHECKPOINT-SENTINEL") != 1 || strings.Contains(string(body), "STALE-CHECKPOINT-SENTINEL") {
		t.Fatalf("wire body contains wrong checkpoint: %s", body)
	}
}

func TestMapMessageRendersCompactionCheckpointAsUserInput(t *testing.T) {
	message := protocol.Message{
		Role:    protocol.RoleCustom,
		Content: []protocol.ContentBlock{protocol.NewTextBlock("Working-state checkpoint for compacted history:\nlatest facts")},
	}
	mapped, ok := mapMessage(message)
	if !ok {
		t.Fatal("custom checkpoint was not representable")
	}
	if mapped.Role != "user" {
		t.Fatalf("checkpoint role=%q, want user", mapped.Role)
	}
	if text, ok := mapped.Content.(string); !ok || text != "Working-state checkpoint for compacted history:\nlatest facts" {
		t.Fatalf("checkpoint content=%#v", mapped.Content)
	}
}
