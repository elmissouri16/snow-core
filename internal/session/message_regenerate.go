package session

import (
	"context"
	"errors"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// MessageRegenerateSource binds an eligible terminal assistant reply to its
// unique, exact owning user-origin turn and original plain user input.
type MessageRegenerateSource struct {
	MessageEditSource
	ReplyEntryID string
}

func ResolveMessageRegenerate(ctx context.Context, store Store, params protocol.RPCMessageRegeneratePrepareParams) (MessageRegenerateSource, error) {
	var result MessageRegenerateSource
	if params.SessionID == "" || params.SessionID != store.ID() || (params.EntryID == "") == (params.TurnID == "") {
		return result, errors.New("regeneration requires the active session and exactly one assistant entry_id or root turn_id")
	}
	bounded, ok := store.(messageEditHistoryStore)
	if !ok {
		return result, errors.New("session does not support bounded exact regeneration history")
	}
	entries, err := bounded.messageEditEntries(ctx)
	if err != nil {
		return result, err
	}
	spans, err := verifiedInputSpans(ctx, entries)
	if err != nil {
		return result, err
	}
	marker, selected, end := -1, -1, len(entries)
	for i, entry := range entries {
		if entry.ID == params.EntryID {
			selected = i
		}
	}
	root := -1
	for _, span := range spans {
		if (params.EntryID != "" && selected > span.marker && selected < span.end) || (params.TurnID != "" && entries[span.root].ID == params.TurnID && span.marker == span.root) {
			marker, end, root = span.marker, span.end, span.root
		}
	}
	if root < 0 || !IsAgentTurnMarker(entries[root]) || entries[root].Value != "user" {
		return result, errors.New("regeneration requires an exact user-origin root turn")
	}
	if params.TurnID != "" {
		for _, span := range spans {
			if span.root == root && span.marker != root {
				return result, errors.New("regeneration root has multiple input spans; select exact reply entry_id")
			}
		}
	}
	lastMessage, lastAssistant := -1, -1
	for i := marker + 1; i < end; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if entries[i].Type == EntryMeta && entries[i].Key == MetaAgentTurn {
			end = i
			break
		}
		if entries[i].Type == EntryMessage && entries[i].Message != nil {
			lastMessage = i
			if entries[i].Message.Role == protocol.RoleAssistant {
				lastAssistant = i
			}
		}
	}
	if lastAssistant < 0 || lastAssistant != lastMessage || (params.EntryID != "" && selected != lastAssistant) {
		return result, errors.New("regeneration selects only the final assistant reply of its owning turn")
	}
	reply := entries[lastAssistant]
	if reply.Message.ID != reply.ID || !reply.Message.IsRegeneratableReply() {
		return result, errors.New("regeneration requires a text-bearing terminal assistant reply, not a tool preface, plan or private-only output")
	}
	source, err := resolveMessageEditEntries(ctx, store, protocol.RPCMessageEditPrepareParams{SessionID: params.SessionID, EntryID: entries[marker+1].ID}, entries)
	if err != nil {
		return result, err
	}
	if err := ValidateForkBoundary(entries[:end]); err != nil {
		return result, err
	}
	return MessageRegenerateSource{MessageEditSource: source, ReplyEntryID: reply.ID}, nil
}
