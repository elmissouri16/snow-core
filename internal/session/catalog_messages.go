package session

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const catalogMaxMessageBytes = 4 << 20

// Messages returns chronological history from a saved branch tip. The optional
// includeTools flag adds bounded explicit public tool previews; absent/false
// preserves the original text-only contract and user/assistant offsets.
// The complete parent-linked history is used, not provider compaction context.
func (c *Catalog) Messages(ctx context.Context, id string, offset, limit int, includeTools ...bool) (protocol.RPCCatalogMessagesPage, error) {
	withTools := len(includeTools) > 0 && includeTools[0]
	page := protocol.RPCCatalogMessagesPage{Messages: []protocol.RPCCatalogMessage{}, Offset: offset, NextOffset: offset}
	limit, err := catalogPage(offset, limit)
	if err != nil {
		return page, err
	}
	if id == "" || len(id) > catalogMaxFieldBytes {
		return page, errors.New("catalog: invalid session_id")
	}
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	entries, err := c.entries(ctx)
	if err != nil {
		return page, err
	}
	var selected *catalogEntry
	for i := range entries {
		if entries[i].SessionID == id {
			if selected != nil {
				return page, errors.New("catalog: ambiguous session_id")
			}
			selected = &entries[i]
		}
	}
	if selected == nil {
		return page, ErrNotFound
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return page, errors.New("catalog: cannot open sessions root")
	}
	defer root.Close()
	db, cleanup, _, err := c.open(ctx, root, selected.path)
	if err != nil {
		return page, errors.New("catalog: session unavailable")
	}
	defer cleanup()
	current, err := c.inspect(ctx, db, selected.path, selected.UpdatedAt)
	if err != nil || current.SessionID != id {
		return page, ErrNotFound
	}
	if current.MessagesCapped {
		return page, errors.New("catalog: history exceeds traversal limit")
	}
	rows, err := db.QueryContext(ctx, catalogBranchSQL+`SELECT e.id,json_extract(e.message,'$.role'),
		COALESCE(json_extract(e.message,'$.ts'),0),b.depth,CASE WHEN length(CAST(e.message AS BLOB))<=? THEN e.message ELSE '' END
		FROM branch b JOIN entries e ON e.id=b.id
		WHERE b.entry_type='message' AND json_extract(e.message,'$.role') IN ('user','assistant')
		ORDER BY b.depth DESC LIMIT ? OFFSET ?`, current.tip, maxSessionQueryDepth, catalogMaxMessageBytes, limit+1, offset)
	if err != nil {
		return page, errors.New("catalog: cannot read history")
	}
	defer rows.Close()
	remaining := protocol.RPCCatalogMaxTextBytes
	decodeBudget := catalogMaxHistoryDecodeBytes
	var history []catalogHistoryRecord
	for rows.Next() {
		if len(page.Messages) == limit || remaining == 0 {
			page.HasMore = true
			break
		}
		var message protocol.RPCCatalogMessage
		var raw []byte
		var depth int
		if err := rows.Scan(&message.ID, &message.Role, &message.Timestamp, &depth, &raw); err != nil {
			return page, errors.New("catalog: invalid history entry")
		}
		if len(message.ID) > catalogMaxFieldBytes {
			return page, errors.New("catalog: oversized message id")
		}
		record := catalogHistoryRecord{depth: depth, message: protocol.Message{ID: message.ID, Role: protocol.Role(message.Role)}}
		if len(raw) == 0 || (withTools && len(raw) > decodeBudget) {
			message.Truncated = true
			if withTools {
				page.ToolsTruncated = true
			}
		} else {
			// Decode only display candidates; no protocol.Message, raw provider
			// state, tool arguments, image bytes or plugin metadata enter the DTO.
			var display struct {
				Content []catalogHistoryBlock `json:"content"`
			}
			if err := json.Unmarshal(raw, &display); err != nil {
				return page, errors.New("catalog: invalid message JSON")
			}
			if withTools {
				decodeBudget -= len(raw)
				// Preserve every tool-call ordinal, including calls whose IDs will
				// be rejected. Private/non-tool blocks cannot affect presentation IDs.
				for _, block := range display.Content {
					if block.Type == protocol.BlockToolCall {
						record.message.Content = append(record.message.Content, protocol.ContentBlock{Type: block.Type, ToolCallID: block.ToolCallID, Name: block.Name})
					}
				}
			}

			var text strings.Builder
			for _, block := range display.Content {
				if (block.Type != "text" && block.Type != "plan") || block.Text == "" {
					continue
				}
				if text.Len() > 0 {
					if text.Len() == remaining {
						message.Truncated = true
						break
					}
					text.WriteByte('\n')
				}
				bounded, truncated := catalogBoundText(block.Text, remaining-text.Len())
				text.WriteString(bounded)
				if truncated {
					message.Truncated = true
					break
				}
			}
			message.Text = text.String()
		}
		remaining -= len(message.Text)
		page.Messages = append(page.Messages, message)
		if withTools {
			history = append(history, record)
		}
	}
	if err := rows.Err(); err != nil {
		return page, errors.New("catalog: history read failed")
	}
	if err := rows.Close(); err != nil {
		return page, errors.New("catalog: history close failed")
	}
	if err := catalogHistoryImages(ctx, db, current.tip, page.Messages); err != nil {
		return page, err
	}
	if withTools {
		tools, truncated, err := catalogHistoryTools(ctx, db, current.tip, history, decodeBudget)
		if err != nil {
			return page, err
		}
		page.ToolsTruncated = page.ToolsTruncated || truncated
		for i := range page.Messages {
			page.Messages[i].Tools = tools[page.Messages[i].ID]
		}
	}
	page.NextOffset = offset + len(page.Messages)
	return page, nil
}

func catalogBoundText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit], true
}
