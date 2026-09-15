package session

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// MetaAgentInputSpan is deliberately neither a root turn nor a provider step.
// Its reserved prefix is versioned in the value; unknown variants fail closed.
const MetaAgentInputSpan = "agent_input_span"

type AgentInputSpan struct {
	Version     int                      `json:"version"`
	RootTurnID  string                   `json:"root_turn_id"`
	QueueID     string                   `json:"queue_id"`
	UserEntryID string                   `json:"user_entry_id"`
	Kind        protocol.QueuedInputKind `json:"kind"`
}

func (s AgentInputSpan) valid() bool {
	if s.Version != 1 || (s.Kind != protocol.QueuedInputFollowUp && s.Kind != protocol.QueuedInputSteer) {
		return false
	}
	for _, id := range []string{s.RootTurnID, s.QueueID, s.UserEntryID} {
		if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.ContainsRune(id, 0) {
			return false
		}
	}
	return true
}
func NewAgentInputSpanEntry(id string, span AgentInputSpan) (Entry, error) {
	if id == "" || !span.valid() {
		return Entry{}, errors.New("invalid input span")
	}
	data, err := json.Marshal(span)
	return Entry{Type: EntryMeta, ID: id, Key: MetaAgentInputSpan, Value: string(data)}, err
}

type verifiedInputSpan struct{ root, marker, end int }

// verifiedInputSpans validates every reserved marker on the bounded path before
// ownership selection. Missing user partners, wrong roots, duplicate queue IDs,
// unknown versions/fields and dangling partial markers are never guessed past.
func verifiedInputSpans(ctx context.Context, entries []Entry) ([]verifiedInputSpan, error) {
	spans := []verifiedInputSpan{}
	root := -1
	seen := map[string]bool{}
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type == EntryMeta && entry.Key == MetaAgentTurn {
			if len(spans) > 0 {
				spans[len(spans)-1].end = i
			}
			root = i
			spans = append(spans, verifiedInputSpan{root: i, marker: i, end: len(entries)})
		}
		if !strings.HasPrefix(entry.Key, MetaAgentInputSpan) {
			continue
		}
		var span AgentInputSpan
		if entry.Type != EntryMeta || entry.Key != MetaAgentInputSpan || len(entry.Value) > 2048 || json.Unmarshal([]byte(entry.Value), &span, json.RejectUnknownMembers(true)) != nil || !span.valid() || root < 0 || !IsAgentTurnMarker(entries[root]) || span.RootTurnID != entries[root].ID || seen[span.QueueID] || i+1 >= len(entries) {
			return nil, errors.New("invalid or unsupported persisted input span")
		}
		user := entries[i+1]
		if user.Type != EntryMessage || user.Message == nil || user.Message.Role != protocol.RoleUser || user.ID != span.UserEntryID || user.Message.ID != span.UserEntryID || user.ParentID != entry.ID {
			return nil, errors.New("input span lacks its exact adjacent user")
		}
		seen[span.QueueID] = true
		spans[len(spans)-1].end = i
		spans = append(spans, verifiedInputSpan{root: root, marker: i, end: len(entries)})
	}
	return spans, nil
}
