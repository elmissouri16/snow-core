package rpc

import (
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func validatePromptContent(message string, content []protocol.ContentBlock) error {
	if len(content) == 0 {
		return errors.New("prompt content must not be empty")
	}
	hasImage := false
	for _, block := range content {
		switch block.Type {
		case protocol.BlockText:
			if block.Text == "" {
				return errors.New("prompt text block cannot be empty")
			}
		case protocol.BlockImage:
			if block.MIMEType == "" {
				return errors.New("prompt image block requires mime_type")
			}
			if len(block.Data) == 0 {
				return errors.New("prompt image block cannot be empty")
			}
			hasImage = true
		default:
			return fmt.Errorf("prompt content block type %q is not allowed", block.Type)
		}
	}
	if message == "" && !hasImage {
		return errors.New("prompt requires a message or an image attachment")
	}
	return nil
}

func hasImageContent(content []protocol.ContentBlock) bool {
	for _, block := range content {
		if block.Type == protocol.BlockImage {
			return true
		}
	}
	return false
}
