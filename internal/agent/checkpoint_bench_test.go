package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func BenchmarkCheckpointContextUsage(b *testing.B) {
	for _, count := range []int{50, 1500} {
		for _, size := range []int{4096, 32768} {
			b.Run(fmt.Sprintf("tail-%d/checkpoint-%d", count, size), func(b *testing.B) {
				messages := make([]protocol.Message, count+1)
				messages[0] = protocol.Message{ID: "compaction-marker", Role: protocol.RoleCustom, Content: []protocol.ContentBlock{protocol.NewTextBlock("Conversation summary:\n" + strings.Repeat("x", size))}}
				for i := 1; i < len(messages); i++ {
					messages[i] = protocol.Message{ID: fmt.Sprint(i), ParentID: "old", Role: protocol.RoleAssistant, Usage: &protocol.Usage{Input: 99}}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if got := latestPersistedContextTokens(messages); got != 0 {
						b.Fatal(got)
					}
				}
			})
		}
	}
}
