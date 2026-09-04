package compact

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
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
	if !strings.HasPrefix(got, strings.Repeat("h", 50)) || !strings.Contains(got, "200 bytes omitted") || !strings.HasSuffix(got, strings.Repeat("t", 50)) {
		t.Fatalf("unexpected projection: %q", got)
	}
	if len(got) > 160 || len(got) >= len(original) {
		t.Fatalf("projection size=%d original=%d", len(got), len(original))
	}
}

func TestOwnedPruningMatchesDefensiveProjection(t *testing.T) {
	original := strings.Repeat("head", 100)
	message := protocol.NewToolResultMessage("result", "call", "call-1", "bash", []protocol.ContentBlock{protocol.NewTextBlock(original)}, false)
	defensive := PruneHistoricalToolResultsWithRefs([]protocol.Message{message}, 160, 50, 50, func(protocol.Message, string) string { return "artifact-00000000000000000000000000000000" })
	ownedInput := []protocol.Message{message.Clone()}
	owned := PruneHistoricalToolResultsOwnedWithRefs(ownedInput, 160, 50, 50, func(protocol.Message, string) string { return "artifact-00000000000000000000000000000000" })
	if len(owned) != 1 || owned[0].Content[0].Text != defensive[0].Content[0].Text {
		t.Fatalf("owned projection=%+v defensive=%+v", owned, defensive)
	}
	if &owned[0] != &ownedInput[0] {
		t.Fatal("owned pruning replaced the caller slice")
	}
	if message.Content[0].Text != original {
		t.Fatal("owned projection test mutated durable input")
	}
}

func TestNeedsHistoricalToolResultPruning(t *testing.T) {
	plain := protocol.NewToolResultMessage("plain", "", "call", "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", 161))}, false)
	rich := protocol.NewToolResultMessage("rich", "", "call-2", "read", []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", 200)), {Type: protocol.BlockImage, Data: []byte{1}}}, false)
	if !NeedsHistoricalToolResultPruning([]protocol.Message{plain}, 160) {
		t.Fatal("oversized plain result was not detected")
	}
	if NeedsHistoricalToolResultPruning([]protocol.Message{rich}, 160) {
		t.Fatal("rich result was incorrectly marked prunable")
	}
}

func TestPruningThresholdBoundaryPreservesSmallResults(t *testing.T) {
	const threshold = 8192
	messages := make([]protocol.Message, 3)
	for i, size := range []int{threshold - 1, threshold, threshold + 1} {
		messages[i] = protocol.Message{Role: protocol.RoleTool, Content: []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", size))}}
	}
	resolverCalls := 0
	got := PruneHistoricalToolResultsWithRefs(messages, threshold, 4096, 1024, func(protocol.Message, string) string {
		resolverCalls++
		return "retained-result"
	})
	for i := range 2 {
		if got[i].Content[0].Text != messages[i].Content[0].Text {
			t.Fatal("result at or below the threshold was changed")
		}
	}
	if resolverCalls != 1 || !strings.Contains(got[2].Content[0].Text, "bytes omitted") || len(got[2].Content[0].Text) >= threshold+1 {
		t.Fatal("oversized result was not pruned with one artifact lookup")
	}
	got[0].Content[0].Text = "changed projection"
	if messages[0].Content[0].Text != strings.Repeat("x", threshold-1) {
		t.Fatal("skipping pruning weakened defensive ownership")
	}
}

func TestPruneHistoricalToolResultsKeepsUTF8AndSkipsRichBlocks(t *testing.T) {
	value := strings.Repeat("界", 100)
	plain := protocol.NewToolResultMessage("plain", "", "call", "read", []protocol.ContentBlock{protocol.NewTextBlock(value)}, false)
	rich := protocol.NewToolResultMessage("rich", "", "call-2", "read", []protocol.ContentBlock{
		protocol.NewTextBlock(value), {Type: protocol.BlockImage, MIMEType: "image/png", Data: []byte{1}},
	}, false)
	out := PruneHistoricalToolResults([]protocol.Message{plain, rich}, 180, 60, 30)
	if !utf8.ValidString(out[0].Content[0].Text) || !strings.Contains(out[0].Content[0].Text, "bytes omitted") {
		t.Fatalf("invalid UTF-8 projection: %q", out[0].Content[0].Text)
	}
	if out[1].Content[0].Text != value || len(out[1].Content) != 2 {
		t.Fatalf("rich result was changed: %+v", out[1].Content)
	}
}
