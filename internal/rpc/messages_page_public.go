package rpc

import "github.com/elmissouri16/snow-core/pkg/protocol"

// publicHistoryMessages is an explicit allowlist, distinct from the legacy RPC
// projection. Do not clone whole messages or blocks: even public block kinds can
// carry private payloads. Nil tool provenance never permits a content fallback.
func publicHistoryMessages(messages []protocol.Message) []protocol.Message {
	out := make([]protocol.Message, len(messages))
	for i, message := range messages {
		projected := protocol.Message{
			ID: message.ID, ParentID: message.ParentID, Role: message.Role,
			Content: []protocol.ContentBlock{},
		}
		if message.Role == protocol.RoleAssistant {
			// Lifecycle enums and a failure bit are public metadata, not diagnostics.
			// Never copy a provider's arbitrary stop reason or raw Error string.
			switch message.StopReason {
			case protocol.StopStop, protocol.StopLength, protocol.StopToolUse, protocol.StopError, protocol.StopAborted, protocol.StopPending:
				projected.StopReason = message.StopReason
			}
			projected.IsError = message.IsError || message.Error != ""
		}
		if message.Role == protocol.RoleTool {
			projected.ToolCallID = message.ToolCallID
			projected.ToolName = message.ToolName
			projected.IsError = message.IsError
			projected.ToolOutcomeUnknown = message.ToolOutcomeUnknown
			if message.PublicToolResult != nil && !message.ToolOutcomeUnknown {
				projected.PublicToolResult = new(*message.PublicToolResult)
			}
		} else {
			for _, block := range message.Content {
				switch block.Type {
				case protocol.BlockText:
					projected.Content = append(projected.Content, protocol.NewTextBlock(block.Text))
				case protocol.BlockPlan:
					projected.Content = append(projected.Content, protocol.ContentBlock{
						Type: protocol.BlockPlan, Text: block.Text, PlanComplete: block.PlanComplete,
					})
				case protocol.BlockToolCall:
					projected.Content = append(projected.Content, protocol.ContentBlock{
						Type: protocol.BlockToolCall, Name: block.Name, ToolCallID: block.ToolCallID,
					})
				}
			}
		}
		// An omitted image/future block or assistant tool metadata must not turn
		// an ineligible stored reply into an eligible text-only public message.
		// Withhold the terminal claim rather than invent an error or expose fields.
		if projected.IsRegeneratableReply() && !message.IsRegeneratableReply() {
			projected.StopReason = ""
		}
		out[i] = projected
	}
	return out
}
