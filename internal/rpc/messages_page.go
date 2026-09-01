package rpc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	defaultMessagesPageLimit    = 32
	maxMessagesPageLimit        = 128
	defaultMessagesPageBytes    = 2 * 1024 * 1024
	minMessagesPageBytes        = 64 * 1024
	maxMessagesPageBytes        = protocol.RPCMaxInputBytes - 64*1024
	maxMessagesPageImages       = 32
	maxMessagesPageCursorLength = 4096
	messagesPageCursorVersion   = 1
)

type messagesPageCursor struct {
	Version      int    `json:"version"`
	Next         int    `json:"next"`
	Total        int    `json:"total"`
	FirstAnchor  string `json:"first"`
	LastAnchor   string `json:"last"`
	BeforeAnchor string `json:"before"`
}

func (s *Server) handleMessagesPage(req Request) error {
	var params protocol.RPCMessagesPageParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("messages_page params: %w", err)
		}
	}
	if params.Limit == 0 {
		params.Limit = defaultMessagesPageLimit
	}
	if params.Limit < 1 || params.Limit > maxMessagesPageLimit {
		return fmt.Errorf("messages_page limit must be between 1 and %d", maxMessagesPageLimit)
	}
	if params.MaxBytes == 0 {
		params.MaxBytes = defaultMessagesPageBytes
	}
	if params.MaxBytes < minMessagesPageBytes || params.MaxBytes > maxMessagesPageBytes {
		return fmt.Errorf("messages_page max_bytes must be between %d and %d", minMessagesPageBytes, maxMessagesPageBytes)
	}

	messages, err := s.app.Agent.Messages()
	if err != nil {
		return err
	}
	messages = publicMessages(messages)
	page, err := buildMessagesPage(req.ID, messages, params)
	if err != nil {
		return err
	}
	s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: page})
	return nil
}

func buildMessagesPage(requestID string, messages []protocol.Message, params protocol.RPCMessagesPageParams) (protocol.RPCMessagesPage, error) {
	cursor, err := decodeMessagesPageCursor(params.Cursor, messages)
	if err != nil {
		return protocol.RPCMessagesPage{}, err
	}
	start := cursor.Next
	total := cursor.Total
	if params.Cursor == "" {
		total = stableMessagesSnapshotTotal(messages)
		for i := range total {
			if messages[i].ID == "" {
				return protocol.RPCMessagesPage{}, errors.New("messages_page history contains a message without an id")
			}
		}
		cursor = snapshotMessagesCursor(messages, 0, total)
	}
	if start == total {
		return protocol.RPCMessagesPage{Messages: []protocol.Message{}, Start: start, Total: total}, nil
	}

	end := start
	imageCount := 0
	var selected protocol.RPCMessagesPage
	for end < total && end-start < params.Limit {
		nextImages := imageCount + historyImageCount(messages[end])
		if nextImages > maxMessagesPageImages {
			if end == start {
				return protocol.RPCMessagesPage{}, fmt.Errorf("messages_page entry at offset %d exceeds the %d image page limit", start, maxMessagesPageImages)
			}
			break
		}
		end++
		imageCount = nextImages
		candidate, err := makeMessagesPage(messages, cursor, start, end, total)
		if err != nil {
			return protocol.RPCMessagesPage{}, err
		}
		size, err := messagesPageFrameSize(requestID, candidate)
		if err != nil {
			return protocol.RPCMessagesPage{}, err
		}
		if size > maxMessagesPageBytes {
			if end == start+1 {
				return protocol.RPCMessagesPage{}, fmt.Errorf("messages_page entry at offset %d exceeds the %d byte frame limit", start, maxMessagesPageBytes)
			}
			break
		}
		if size > params.MaxBytes && end > start+1 {
			break
		}
		selected = candidate
	}
	if len(selected.Messages) == 0 {
		candidate, err := makeMessagesPage(messages, cursor, start, start+1, total)
		if err != nil {
			return protocol.RPCMessagesPage{}, err
		}
		size, err := messagesPageFrameSize(requestID, candidate)
		if err != nil {
			return protocol.RPCMessagesPage{}, err
		}
		if size > maxMessagesPageBytes {
			return protocol.RPCMessagesPage{}, fmt.Errorf("messages_page entry at offset %d exceeds the %d byte frame limit", start, maxMessagesPageBytes)
		}
		selected = candidate
	}
	return selected, nil
}

func makeMessagesPage(messages []protocol.Message, snapshot messagesPageCursor, start, end, total int) (protocol.RPCMessagesPage, error) {
	page := protocol.RPCMessagesPage{
		Messages: messages[start:end],
		Start:    start,
		Total:    total,
		HasMore:  end < total,
	}
	if page.HasMore {
		next := snapshot
		next.Next = end
		next.BeforeAnchor = messagesPageAnchor(messages[end-1])
		cursor, err := encodeMessagesPageCursor(next)
		if err != nil {
			return protocol.RPCMessagesPage{}, err
		}
		page.NextCursor = cursor
	}
	return page, nil
}

func snapshotMessagesCursor(messages []protocol.Message, next, total int) messagesPageCursor {
	cursor := messagesPageCursor{Version: messagesPageCursorVersion, Next: next, Total: total}
	if total > 0 {
		cursor.FirstAnchor = messagesPageAnchor(messages[0])
		cursor.LastAnchor = messagesPageAnchor(messages[total-1])
	}
	return cursor
}

func encodeMessagesPageCursor(cursor messagesPageCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode messages_page cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeMessagesPageCursor(encoded string, messages []protocol.Message) (messagesPageCursor, error) {
	if encoded == "" {
		return messagesPageCursor{}, nil
	}
	if len(encoded) > maxMessagesPageCursorLength {
		return messagesPageCursor{}, errors.New("messages_page cursor is too long")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return messagesPageCursor{}, errors.New("messages_page cursor is invalid")
	}
	var cursor messagesPageCursor
	if err := json.Unmarshal(payload, &cursor, json.RejectUnknownMembers(true)); err != nil {
		return messagesPageCursor{}, errors.New("messages_page cursor is invalid")
	}
	if cursor.Version != messagesPageCursorVersion || cursor.Total < 1 || cursor.Next < 1 || cursor.Next >= cursor.Total {
		return messagesPageCursor{}, errors.New("messages_page cursor is invalid")
	}
	if cursor.Total > len(messages) {
		return messagesPageCursor{}, errors.New("messages_page snapshot is no longer available")
	}
	if messages[0].ID == "" || messages[cursor.Total-1].ID == "" || messages[cursor.Next-1].ID == "" {
		return messagesPageCursor{}, errors.New("messages_page history contains a message without an id")
	}
	if messagesPageAnchor(messages[0]) != cursor.FirstAnchor || messagesPageAnchor(messages[cursor.Total-1]) != cursor.LastAnchor || messagesPageAnchor(messages[cursor.Next-1]) != cursor.BeforeAnchor {
		return messagesPageCursor{}, errors.New("messages_page cursor does not match the active session branch")
	}
	return cursor, nil
}

func stableMessagesSnapshotTotal(messages []protocol.Message) int {
	openCalls := make(map[string]struct{})
	lastSafe := 0
	for index, message := range messages {
		for _, block := range message.Content {
			if block.Type == protocol.BlockToolCall && block.ToolCallID != "" {
				openCalls[block.ToolCallID] = struct{}{}
			}
		}
		if message.Role == protocol.RoleTool && message.ToolCallID != "" {
			delete(openCalls, message.ToolCallID)
		}
		if len(openCalls) == 0 {
			lastSafe = index + 1
		}
	}
	return lastSafe
}

func messagesPageAnchor(message protocol.Message) string {
	sum := sha256.Sum256([]byte(message.ID))
	return fmt.Sprintf("%x", sum)
}

func historyImageCount(message protocol.Message) int {
	count := 0
	for _, block := range message.Content {
		if block.Type == protocol.BlockImage {
			count++
		}
	}
	return count
}

func messagesPageFrameSize(requestID string, page protocol.RPCMessagesPage) (int, error) {
	// Server.write uses encoding/json v1, whose HTML/JS escaping can expand
	// untrusted text substantially. Match those formatting rules so this is a
	// conservative bound for the bytes the shared writer will emit.
	frame, err := json.Marshal(
		Response{ID: requestID, Type: "response", Command: "messages_page", Success: true, Data: page},
		jsontext.AllowInvalidUTF8(true),
		jsontext.EscapeForHTML(true),
		jsontext.EscapeForJS(true),
	)
	if err != nil {
		return 0, fmt.Errorf("encode messages_page response: %w", err)
	}
	return len(frame) + 1, nil
}
