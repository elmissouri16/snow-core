package chatgpt

import (
	"github.com/elmissouri16/snow-core/internal/provider/responsesapi"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func responseInput(message protocol.Message) ([]any, error) {
	if message.Provider == "" {
		message.Provider = ProviderID
	}
	return responsesapi.MessageInput(message, ProviderID)
}

func messageText(message protocol.Message) string {
	return responsesapi.MessageText(message)
}
