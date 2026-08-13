package compact

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/snow-core/snow/pkg/protocol"
)

func TestPruneHistoricalToolResultsPreservesDurableInputAndMetadata(t *testing.T) {
	original := strings.Repeat("h", 50) + strings.Repeat("m", 200) + strings.Repeat("t", 50)
	message := protocol.NewToolResultMessage("result", "call", "call-1", "bash",
		[]protocol.ContentBlock{protocol.NewTextBlock(original)}, true)
	pruned := PruneHistoricalToolResults([]protocol.Message{message}, 160, 50, 50)
	if got := message.Content[0].Text; got != original {
		t.Fatal("input message was mutated")
	}
	if len(pruned) != 1 || pruned[0].ToolCallID != "call-1" || pruned[0].ToolName != "bash" || !pruned[0].IsError {
		t.Fatalf("metadata changed: %+v", pruned)
	}
	got := pruned[0].Content[0].Text
	if !strings.HasPrefix(got, strings.Repeat("h", 50)) || !strings.Contains(got, "historical tool result middle pruned") || !strings.HasSuffix(got, strings.Repeat("t", 50)) {
		t.Fatalf("unexpected projection: %q", got)
	}
	if len(got) > 160 || len(got) >= len(original) {
		t.Fatalf("projection size=%d original=%d", len(got), len(original))
	}
}

func TestPruneHistoricalToolResultsKeepsUTF8AndSkipsRichBlocks(t *testing.T) {
	value := strings.Repeat("界", 100)
	plain := protocol.NewToolResultMessage("plain", "", "call", "read", []protocol.ContentBlock{protocol.NewTextBlock(value)}, false)
	rich := protocol.NewToolResultMessage("rich", "", "call-2", "read", []protocol.ContentBlock{
		protocol.NewTextBlock(value), {Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1}},
	}, false)
	out := PruneHistoricalToolResults([]protocol.Message{plain, rich}, 180, 60, 30)
	if !utf8.ValidString(out[0].Content[0].Text) || !strings.Contains(out[0].Content[0].Text, "middle pruned") {
		t.Fatalf("invalid UTF-8 projection: %q", out[0].Content[0].Text)
	}
	if out[1].Content[0].Text != value || len(out[1].Content) != 2 {
		t.Fatalf("rich result was changed: %+v", out[1].Content)
	}
}
