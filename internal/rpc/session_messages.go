package rpc

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/snow-core/snow/pkg/protocol"
)

const (
	maxRPCTranscriptBytes    = 8 << 20
	maxRPCMessageBlocks      = 24
	maxRPCObservedTextBytes  = 16 << 10
	maxRPCObservedErrorBytes = 16 << 10
	maxRPCObservedArguments  = 16 << 10
	rpcTranscriptOmittedText = "… [truncated for RPC transcript hydration]"
)

var truncatedArguments = json.RawMessage(`{"_snow_truncated":true}`)

// projectSessionMessages produces a public observer projection, not provider
// continuation state. Binary attachments and provider-private blocks never
// cross RPC; text/arguments, message count, and aggregate encoded bytes are
// independently bounded below the protocol frame maximum.
func projectSessionMessages(messages []protocol.Message, limit int) []protocol.Message {
	if limit <= 0 || limit > protocol.RPCSessionMessagesMax {
		limit = protocol.RPCSessionMessagesMax
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	projected := make([]protocol.Message, 0, len(messages))
	encodedBytes := 0
	for i := len(messages) - 1; i >= 0; i-- {
		message := projectSessionMessage(messages[i])
		encoded, err := json.Marshal(message)
		if err != nil {
			continue
		}
		if encodedBytes+len(encoded) > maxRPCTranscriptBytes {
			break
		}
		encodedBytes += len(encoded)
		projected = append(projected, message)
	}
	// The scan above keeps the newest messages. Reverse back to chronological
	// order for consumers and preserve defensive ownership.
	for left, right := 0, len(projected)-1; left < right; left, right = left+1, right-1 {
		projected[left], projected[right] = projected[right], projected[left]
	}
	return projected
}

func projectSessionMessage(message protocol.Message) protocol.Message {
	out := message.Clone()
	out.Error = truncateRPCText(out.Error, maxRPCObservedErrorBytes)
	out.Content = out.Content[:0]
	for _, block := range message.Content {
		if block.Type == protocol.BlockProviderData {
			continue
		}
		if len(out.Content) >= maxRPCMessageBlocks {
			out.Content[maxRPCMessageBlocks-1] = protocol.ContentBlock{Type: protocol.BlockText, Text: rpcTranscriptOmittedText}
			break
		}
		projected := block
		projected.Data = nil
		projected.Text = truncateRPCText(projected.Text, maxRPCObservedTextBytes)
		if len(projected.Arguments) > maxRPCObservedArguments || (len(projected.Arguments) > 0 && !json.Valid(projected.Arguments)) {
			projected.Arguments = append(json.RawMessage(nil), truncatedArguments...)
		} else {
			projected.Arguments = append(json.RawMessage(nil), projected.Arguments...)
		}
		out.Content = append(out.Content, projected)
	}
	return out
}

func truncateRPCText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	keep := max(0, limit-len(rpcTranscriptOmittedText))
	for keep > 0 && !utf8.RuneStart(value[keep]) {
		keep--
	}
	return value[:keep] + rpcTranscriptOmittedText
}
