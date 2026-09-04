package compact

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func BenchmarkPruneMixedHistory(b *testing.B) {
	for _, count := range []int{50, 500} {
		messages := make([]protocol.Message, count+1)
		for i := range messages {
			size := 4096
			if i == count {
				size = 64 << 10
			}
			messages[i] = protocol.Message{ID: fmt.Sprint(i), Role: protocol.RoleTool, Content: []protocol.ContentBlock{protocol.NewTextBlock(strings.Repeat("x", size))}}
		}
		b.Run(fmt.Sprintf("defensive-%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				pruneBenchmarkMessages = PruneHistoricalToolResults(messages, 8192, 4096, 1024)
			}
		})
		b.Run(fmt.Sprintf("owned-%d", count), func(b *testing.B) {
			working := append([]protocol.Message(nil), messages...)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				working[count] = messages[count]
				pruneBenchmarkMessages = PruneHistoricalToolResultsOwnedWithRefs(working, 8192, 4096, 1024, nil)
			}
		})
	}
}

var pruneBenchmarkMessages []protocol.Message
