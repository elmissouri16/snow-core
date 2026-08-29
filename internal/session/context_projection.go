package session

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	contextProjectionChunkMessages = 64
	compactedCheckpointPrefix      = "Working-state checkpoint for compacted history:\n"
)

// contextMessagesFromEntries projects a branch after its latest compaction
// marker. History remains append-only; only the provider-facing projection
// replaces the compacted prefix with one harness summary message.
//
// Projection is a recurring provider-request path. It avoids compaction-index
// work entirely for the common uncompacted branch and packs independently
// capped mutable slices into bounded message chunks. Callers still receive
// fully defensive messages: appending to or mutating one returned field cannot
// change the cached entries or another returned message. Chunking also prevents
// one separately retained message from pinning the complete projected context.
func contextMessagesFromEntries(entries []Entry) []protocol.Message {
	lastCompaction, boundaryPos := latestContextCompaction(entries)
	return contextMessagesFromEntriesAt(entries, lastCompaction, boundaryPos)
}

func contextMessagesFromEntriesAt(entries []Entry, lastCompaction, boundaryPos int) []protocol.Message {
	start := 0
	messageCount := 0
	if lastCompaction >= 0 {
		messageCount = 1
		start = boundaryPos + 1
	}
	for i := start; i < len(entries); i++ {
		entry := &entries[i]
		if entry.Type == EntryMessage && entry.Message != nil {
			messageCount++
		}
	}
	if messageCount == 0 {
		return nil
	}

	builder := newContextProjectionBuilder(messageCount)
	sources := make([]contextProjectionSource, 0, min(contextProjectionChunkMessages, messageCount))
	if lastCompaction >= 0 {
		sources = append(sources, contextProjectionSource{checkpoint: &entries[lastCompaction]})
	}
	for i := start; i < len(entries); i++ {
		entry := &entries[i]
		if entry.Type != EntryMessage || entry.Message == nil {
			continue
		}
		sources = append(sources, contextProjectionSource{message: entry.Message})
		if len(sources) == contextProjectionChunkMessages {
			builder.appendChunk(sources)
			sources = sources[:0]
		}
	}
	if len(sources) > 0 {
		builder.appendChunk(sources)
	}
	return builder.messages
}

// latestContextCompaction deliberately performs no map allocation when a
// branch has no effective compaction marker, which is the common hot path.
func projectCompactedBranchContext(entries []Entry) ([]protocol.Message, bool) {
	lastCompaction, boundaryPos := latestContextCompaction(entries)
	if lastCompaction < 0 {
		return nil, false
	}
	return contextMessagesFromEntriesAt(entries, lastCompaction, boundaryPos), true
}

func (s *MemoryStore) ProjectBranchContext(entries []Entry) ([]protocol.Message, bool) {
	return projectCompactedBranchContext(entries)
}

func (s *SQLiteStore) ProjectBranchContext(entries []Entry) ([]protocol.Message, bool) {
	return projectCompactedBranchContext(entries)
}

func latestContextCompaction(entries []Entry) (lastCompaction, boundaryPos int) {
	lastCompaction, boundaryPos = -1, -1
	hasCompaction := false
	for i := range entries {
		if entries[i].Type == EntryCompaction && strings.TrimSpace(entries[i].Summary) != "" {
			hasCompaction = true
			break
		}
	}
	if !hasCompaction {
		return lastCompaction, boundaryPos
	}

	positions := make(map[string]int, len(entries))
	for i := range entries {
		positions[entries[i].ID] = i
	}
	for i, entry := range entries {
		if entry.Type != EntryCompaction || strings.TrimSpace(entry.Summary) == "" {
			continue
		}
		resolved := i - 1
		if pos, ok := positions[entry.CompactedThrough]; ok && pos < i {
			resolved = pos
		}
		// Prefer the marker that hides the greatest valid prefix. A newer marker
		// wins ties, while corrupt forward/unknown references are clamped at the
		// marker so they can never resurface an older prefix.
		if resolved > boundaryPos || (resolved == boundaryPos && i > lastCompaction) {
			lastCompaction = i
			boundaryPos = resolved
		}
	}
	return lastCompaction, boundaryPos
}

type contextProjectionSource struct {
	message    *protocol.Message
	checkpoint *Entry
}

type contextProjectionShape struct {
	blocks        int
	dataBytes     int
	argumentBytes int
	usages        int
	costs         int
	displays      int
	progressRows  int
}

func (s *contextProjectionShape) addMessage(message *protocol.Message) {
	s.blocks += len(message.Content)
	for _, block := range message.Content {
		s.dataBytes += len(block.Data)
		s.argumentBytes += len(block.Arguments)
	}
	if message.Usage != nil {
		s.usages++
		if message.Usage.Cost != nil {
			s.costs++
		}
	}
	if message.ToolDisplay != nil {
		s.displays++
		s.progressRows += len(message.ToolDisplay.Progress)
	}
}

type contextProjectionBuilder struct {
	messages  []protocol.Message
	blocks    []protocol.ContentBlock
	data      []byte
	arguments json.RawMessage
	usages    []protocol.Usage
	costs     []protocol.Cost
	displays  []protocol.ToolDisplay
	progress  []string

	messageAt  int
	blockAt    int
	dataAt     int
	argumentAt int
	usageAt    int
	costAt     int
	displayAt  int
	progressAt int
}

func newContextProjectionBuilder(messageCount int) *contextProjectionBuilder {
	return &contextProjectionBuilder{messages: make([]protocol.Message, messageCount)}
}

func (b *contextProjectionBuilder) appendChunk(sources []contextProjectionSource) {
	shape := contextProjectionShape{}
	for _, source := range sources {
		if source.checkpoint != nil {
			shape.blocks++
		} else {
			shape.addMessage(source.message)
		}
	}
	b.blocks = make([]protocol.ContentBlock, shape.blocks)
	b.data = make([]byte, shape.dataBytes)
	b.arguments = make(json.RawMessage, shape.argumentBytes)
	b.usages = make([]protocol.Usage, shape.usages)
	b.costs = make([]protocol.Cost, shape.costs)
	b.displays = make([]protocol.ToolDisplay, shape.displays)
	b.progress = make([]string, shape.progressRows)
	b.blockAt, b.dataAt, b.argumentAt = 0, 0, 0
	b.usageAt, b.costAt, b.displayAt, b.progressAt = 0, 0, 0, 0

	for _, source := range sources {
		if source.checkpoint != nil {
			b.appendCheckpoint(source.checkpoint)
		} else {
			b.appendMessage(source.message)
		}
	}
}

func (b *contextProjectionBuilder) appendCheckpoint(entry *Entry) {
	message := &b.messages[b.messageAt]
	b.messageAt++
	*message = protocol.Message{
		ID:        "compaction-" + entry.ID,
		ParentID:  entry.ParentID,
		Role:      protocol.RoleCustom,
		Timestamp: time.Now().UnixMilli(),
	}
	blockAt := b.blockAt
	b.blockAt++
	b.blocks[blockAt] = protocol.NewTextBlock(compactedCheckpointPrefix + entry.Summary)
	message.Content = b.blocks[blockAt:b.blockAt:b.blockAt]
}

func (b *contextProjectionBuilder) appendMessage(source *protocol.Message) {
	destination := &b.messages[b.messageAt]
	b.messageAt++
	*destination = *source

	if len(source.Content) == 0 {
		// Message.Clone historically returns an owned non-nil empty content slice.
		destination.Content = make([]protocol.ContentBlock, 0)
	} else {
		blockStart := b.blockAt
		for _, sourceBlock := range source.Content {
			destinationBlock := &b.blocks[b.blockAt]
			b.blockAt++
			*destinationBlock = sourceBlock
			destinationBlock.Data = nil
			if len(sourceBlock.Data) > 0 {
				start := b.dataAt
				b.dataAt += len(sourceBlock.Data)
				copy(b.data[start:b.dataAt], sourceBlock.Data)
				destinationBlock.Data = b.data[start:b.dataAt:b.dataAt]
			}
			destinationBlock.Arguments = nil
			if len(sourceBlock.Arguments) > 0 {
				start := b.argumentAt
				b.argumentAt += len(sourceBlock.Arguments)
				copy(b.arguments[start:b.argumentAt], sourceBlock.Arguments)
				destinationBlock.Arguments = b.arguments[start:b.argumentAt:b.argumentAt]
			}
		}
		destination.Content = b.blocks[blockStart:b.blockAt:b.blockAt]
	}

	destination.Usage = nil
	if source.Usage != nil {
		usage := &b.usages[b.usageAt]
		b.usageAt++
		*usage = *source.Usage
		usage.Cost = nil
		if source.Usage.Cost != nil {
			cost := &b.costs[b.costAt]
			b.costAt++
			*cost = *source.Usage.Cost
			usage.Cost = cost
		}
		destination.Usage = usage
	}

	destination.ToolDisplay = nil
	if source.ToolDisplay != nil {
		display := &b.displays[b.displayAt]
		b.displayAt++
		*display = *source.ToolDisplay
		display.Progress = nil
		if len(source.ToolDisplay.Progress) > 0 {
			start := b.progressAt
			b.progressAt += len(source.ToolDisplay.Progress)
			copy(b.progress[start:b.progressAt], source.ToolDisplay.Progress)
			display.Progress = b.progress[start:b.progressAt:b.progressAt]
		}
		destination.ToolDisplay = display
	}
}
