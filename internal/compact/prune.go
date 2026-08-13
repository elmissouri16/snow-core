package compact

import (
	"strings"
	"unicode/utf8"

	"github.com/snow-core/snow/pkg/protocol"
)

const (
	// HistoricalToolResultThreshold is the default size at which an older plain
	// text tool result is shortened before semantic compaction.
	HistoricalToolResultThreshold = 8 * 1024
	// HistoricalToolResultHead and HistoricalToolResultTail retain useful setup
	// and terminal diagnostics while removing a usually repetitive middle.
	HistoricalToolResultHead   = 4 * 1024
	HistoricalToolResultTail   = 1024
	historicalToolResultMarker = "\n\n[... historical tool result middle pruned ...]\n\n"
)

// PruneHistoricalToolResults returns a defensive projection in which oversized
// plain-text tool results retain a bounded head and tail. Exact durable session
// messages are never modified. Mixed/rich results are left intact because their
// block ordering may carry provider-specific meaning.
func PruneHistoricalToolResults(messages []protocol.Message, threshold, head, tail int) []protocol.Message {
	out := make([]protocol.Message, len(messages))
	for i, message := range messages {
		out[i] = message.Clone()
		if message.Role != protocol.RoleTool || threshold <= 0 || head < 0 || tail < 0 {
			continue
		}
		var text strings.Builder
		plain := len(message.Content) > 0
		for _, block := range message.Content {
			if block.Type != protocol.BlockText {
				plain = false
				break
			}
			text.WriteString(block.Text)
		}
		if !plain || text.Len() <= threshold {
			continue
		}
		value := text.String()
		prefix := validUTF8Prefix(value, min(head, len(value)))
		suffixBudget := min(tail, len(value)-len(prefix))
		suffix := validUTF8Suffix(value, suffixBudget)
		pruned := prefix + historicalToolResultMarker + suffix
		if len(pruned) >= len(value) || len(pruned) > threshold {
			continue
		}
		out[i].Content = []protocol.ContentBlock{protocol.NewTextBlock(pruned)}
	}
	return out
}

func validUTF8Prefix(value string, limit int) string {
	if limit >= len(value) {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit]
}

func validUTF8Suffix(value string, limit int) string {
	if limit >= len(value) {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.ValidString(value[start:]) {
		start++
	}
	return value[start:]
}
