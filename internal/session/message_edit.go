package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// MessageEditSource is an exact active-path edit boundary. It is internal state,
// not an authorization to accept caller-supplied parent IDs.
type MessageEditSource struct {
	SessionID, BranchID, TipID, EntryID, TurnID, BoundaryID, Text string
}

// ResolveMessageEdit proves ownership from persisted identities. Legacy or
// ambiguous user messages are deliberately not editable via this narrow API.
func ResolveMessageEdit(ctx context.Context, store Store, params protocol.RPCMessageEditPrepareParams) (MessageEditSource, error) {
	var source MessageEditSource
	if params.SessionID == "" || params.SessionID != store.ID() || (params.EntryID == "") == (params.TurnID == "") {
		return source, errors.New("message edit requires the active session and exactly one entry_id or turn_id")
	}
	entriesStore, ok := store.(messageEditHistoryStore)
	if !ok {
		return source, errors.New("session does not support bounded exact edit history")
	}
	_, ok = store.(ActiveBranchStore)
	if !ok {
		return source, errors.New("session does not support edit branches")
	}
	entries, err := entriesStore.messageEditEntries(ctx)
	if err != nil {
		return source, err
	}
	return resolveMessageEditEntries(ctx, store, params, entries)
}

func resolveMessageEditEntries(ctx context.Context, store Store, params protocol.RPCMessageEditPrepareParams, entries []Entry) (MessageEditSource, error) {
	var source MessageEditSource
	active, ok := store.(ActiveBranchStore)
	if !ok {
		return source, errors.New("session does not support edit branches")
	}
	if len(entries) < 3 || len(entries) > messageEditMaxEntries {
		return source, errors.New("message edit history exceeds bounds or has no editable user")
	}
	root := entries[0]
	if root.ID == "" || root.ParentID != "" || root.Type != EntryMeta || root.Key != "root" || root.Value != store.ID() {
		return source, errors.New("message edit history has no verified session root")
	}
	userIndex, markerIndex := -1, -1
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return source, err
		}
		if i > 0 && entry.ParentID != entries[i-1].ID {
			return source, errors.New("message edit history is not a connected path")
		}
		if params.EntryID != "" && entry.ID == params.EntryID {
			userIndex = i
		}
		if params.TurnID != "" && entry.ID == params.TurnID {
			markerIndex = i
		}
	}
	spans, err := verifiedInputSpans(ctx, entries)
	if err != nil {
		return source, err
	}
	selected := verifiedInputSpan{root: -1, marker: -1}
	for _, span := range spans {
		if (params.EntryID != "" && userIndex == span.marker+1) || (params.TurnID != "" && markerIndex == span.root && span.marker == span.root) {
			selected = span
			break
		}
	}
	markerIndex = selected.marker
	if selected.root < 1 || !IsAgentTurnMarker(entries[selected.root]) || entries[selected.root].Value != "user" {
		return source, errors.New("message edit requires an exact user-origin turn or input span")
	}
	count := 0
	for i := markerIndex + 1; i < selected.end; i++ {
		if entries[i].Message != nil && entries[i].Message.Role == protocol.RoleUser {
			count++
			userIndex = i
		}
	}
	if count != 1 || userIndex != markerIndex+1 {
		return source, errors.New("message edit span has ambiguous user ownership")
	}
	entry := entries[userIndex]
	if entry.Type != EntryMessage || entry.Message == nil || !editablePlainUser(*entry.Message) || entry.Message.ID != entry.ID {
		return source, errors.New("message edit supports only untransformed plain user text")
	}
	text := entry.Message.Content[0].Text
	if err := ValidateMessageEditText(text); err != nil {
		return source, err
	}
	if err := ValidateForkBoundary(entries[:markerIndex]); err != nil {
		return source, err
	}
	if store.BranchTip() != entries[len(entries)-1].ID {
		return source, errors.New("message edit tip changed during preparation")
	}
	return MessageEditSource{SessionID: store.ID(), BranchID: active.ActiveBranchID(), TipID: store.BranchTip(), EntryID: entry.ID, TurnID: entries[selected.root].ID, BoundaryID: entries[markerIndex-1].ID, Text: text}, nil
}

func ValidateMessageEditText(text string) error {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" || len(text) > protocol.RPCMessageEditMaxTextBytes {
		return fmt.Errorf("message edit requires nonempty NUL-free UTF-8 text of at most %d bytes", protocol.RPCMessageEditMaxTextBytes)
	}
	return nil
}

func editablePlainUser(message protocol.Message) bool {
	if message.Role != protocol.RoleUser || len(message.Content) != 1 || len(message.PluginTransforms) != 0 || len(message.PluginDetails) != 0 || message.ToolCallID != "" || message.ToolName != "" || message.Provider != "" || message.Model != "" || message.StopReason != "" || message.Error != "" || message.Usage != nil || message.IsError || message.ToolDisplay != nil || message.ToolOutcomeUnknown || message.PublicToolResult != nil {
		return false
	}
	block := message.Content[0]
	return block.Type == protocol.BlockText && !block.PlanComplete && block.MIMEType == "" && len(block.Data) == 0 && block.ToolCallID == "" && block.Name == "" && len(block.Arguments) == 0
}
