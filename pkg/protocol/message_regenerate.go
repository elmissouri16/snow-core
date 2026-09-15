package protocol

import "strings"

// IsRegeneratableReply checks only the local assistant message shape. It is not
// authority to regenerate: core preparation must still prove an exact final
// reply, unique plain user ownership, active ancestry, and safe tool boundaries.
// Public projections may remove private fields, but must not turn a false result
// into true by dropping failure or unsupported-content evidence.
func (message Message) IsRegeneratableReply() bool {
	if message.Role != RoleAssistant || (message.StopReason != StopStop && message.StopReason != StopLength) || message.IsError || message.Error != "" || message.ToolCallID != "" || message.ToolName != "" {
		return false
	}
	text := false
	for _, block := range message.Content {
		switch block.Type {
		case BlockText:
			text = text || strings.TrimSpace(block.Text) != ""
		case BlockThinking, BlockProviderData:
			// Hidden continuity may accompany text, but is never a reply by itself.
		default:
			return false
		}
	}
	return text
}
