package subagent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func BenchmarkForkContext1500(b *testing.B) {
	messages := make([]protocol.Message, 1500)
	text := strings.Repeat("inherited context ", 16)
	parent := ""
	for i := range messages {
		id := fmt.Sprintf("message-%d", i)
		if i%2 == 0 {
			messages[i] = protocol.NewUserMessage(id, parent, text)
		} else {
			messages[i] = protocol.NewAssistantMessage(id, parent, "benchmark", "model", []protocol.ContentBlock{protocol.NewTextBlock(text)}, protocol.StopStop, nil)
		}
		parent = id
	}
	b.ReportAllocs()
	for b.Loop() {
		store, err := ForkContext(messages, "all", "/tmp", "benchmark-child")
		if err != nil {
			b.Fatal(err)
		}
		if err := store.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
