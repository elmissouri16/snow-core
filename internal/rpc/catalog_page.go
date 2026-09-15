package rpc

import (
	json "encoding/json/v2"
	"errors"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Text-byte limits alone are not wire limits: JSON escapes control characters
// up to sixfold. Bound the complete response, including IDs and the final LF.
func boundCatalogResponse(response Response) (Response, error) {
	for {
		encoded, err := json.Marshal(response)
		if err != nil {
			return Response{}, err
		}
		limit := protocol.RPCCatalogMaxOutputBytes
		if _, ok := response.Data.(protocol.RPCCatalogImage); ok && response.Command == "catalog_image" {
			limit = protocol.RPCMessageImageMaxOutputBytes
		}
		if len(encoded)+1 <= limit {
			return response, nil
		}
		switch page := response.Data.(type) {
		case protocol.RPCCatalogSessionsPage:
			if len(page.Sessions) < 2 {
				return Response{}, errors.New("catalog: session exceeds output bound")
			}
			page.Sessions = page.Sessions[:len(page.Sessions)-1]
			page.NextOffset = page.Offset + len(page.Sessions)
			page.HasMore = true
			response.Data = page
		case protocol.RPCCatalogMessagesPage:
			if len(page.Messages) > 1 {
				page.Messages = page.Messages[:len(page.Messages)-1]
				page.NextOffset = page.Offset + len(page.Messages)
				page.HasMore = true
			} else if len(page.Messages) == 1 && page.Messages[0].Text != "" {
				message := &page.Messages[0]
				end := len(message.Text) / 2
				for end > 0 && !utf8.RuneStart(message.Text[end]) {
					end--
				}
				message.Text = message.Text[:end]
				message.Truncated = true
			} else if len(page.Messages) == 1 && len(page.Messages[0].Tools) > 0 {
				// Even an empty-text owner can exceed the wire bound through escaped
				// output or repeated owner/result IDs. Prefer shortening output before
				// omitting the oldest tool; never truncate identity strings.
				message := &page.Messages[0]
				largest := -1
				for i := range message.Tools {
					if message.Tools[i].Output != "" && (largest < 0 || len(message.Tools[i].Output) > len(message.Tools[largest].Output)) {
						largest = i
					}
				}
				if largest >= 0 {
					tool := &message.Tools[largest]
					end := len(tool.Output) / 2
					for end > 0 && !utf8.RuneStart(tool.Output[end]) {
						end--
					}
					tool.Output = tool.Output[:end]
					tool.Truncated = true
				} else {
					message.Tools = message.Tools[1:]
				}
				page.ToolsTruncated = true
			} else {
				return Response{}, errors.New("catalog: message exceeds output bound")
			}
			response.Data = page
		default:
			return Response{}, errors.New("catalog: response exceeds output bound")
		}
	}
}
