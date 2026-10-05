package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// seedChatGPTAccountHandoff carries working context, not provider continuation.
// The complete original turns remain in the source session. The new session
// contains only a bounded text checkpoint: no partial owning turn, tool calls,
// image bytes, thinking, private blocks, or old account binding is replayed.
// The caller holds stateMu and agent admission and has validated an empty target.
func (a *App) seedChatGPTAccountHandoff(ctx context.Context, target session.Store) error {
	if a.Session.Path() == "" {
		return errors.New("app: automatic account handoff requires a saved source conversation; unsaved history was not discarded")
	}
	var messages []protocol.Message
	var err error
	if projected, ok := a.Session.(session.ContextStore); ok {
		messages, err = projected.ContextMessages()
	} else {
		messages, err = a.Session.Messages()
	}
	if err != nil {
		return fmt.Errorf("app: read conversation for account handoff: %w", err)
	}
	entries := make([]session.Entry, len(messages))
	for i := range messages {
		entries[i].Message = &messages[i]
	}
	if err := session.ValidateForkBoundary(entries); err != nil {
		return fmt.Errorf("app: account handoff requires a completed conversation boundary: %w", err)
	}
	visible := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if message.Role == protocol.RoleInternal {
			continue
		}
		copy := protocol.Message{Role: message.Role, ToolName: message.ToolName, ToolCallID: message.ToolCallID, IsError: message.IsError}
		for _, block := range message.Content {
			// Construct an allowlisted input rather than cloning arbitrary fields
			// from a block; even a private block's Text/Arguments must stay private.
			switch block.Type {
			case protocol.BlockText, protocol.BlockPlan:
				copy.Content = append(copy.Content, protocol.ContentBlock{Type: protocol.BlockText, Text: block.Text})
			case protocol.BlockToolCall:
				copy.Content = append(copy.Content, protocol.ContentBlock{Type: protocol.BlockToolCall, Name: block.Name, ToolCallID: block.ToolCallID, Arguments: slices.Clone(block.Arguments)})
			}
		}
		if len(copy.Content) > 0 {
			visible = append(visible, copy)
		}
	}
	summary, err := compact.DefaultSummarizer(ctx, visible)
	if err != nil {
		return fmt.Errorf("app: create account handoff: %w", err)
	}
	summary += fmt.Sprintf("\n## Account handoff\n- Continue from this bounded text checkpoint. Account-private continuation was restarted, not transferred.\n- Exact earlier history remains in source session %q, available through /sessions.\n", a.Session.ID())
	if err := ctx.Err(); err != nil {
		return err
	}
	checkpoint := protocol.NewUserMessage("", "", summary)
	checkpoint.Role = protocol.RoleCustom
	if err := target.Append(session.Entry{Type: session.EntryMessage, Message: &checkpoint}); err != nil {
		return fmt.Errorf("app: save account handoff: %w", err)
	}
	return nil
}
