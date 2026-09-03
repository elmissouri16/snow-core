package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	defaultSubagentMessagesLimit    = 32
	maxSubagentMessagesLimit        = 128
	defaultSubagentMessagesBytes    = 512 * 1024
	minSubagentMessagesBytes        = 16 * 1024
	maxSubagentMessagesBytes        = 8 * 1024 * 1024
	maxSubagentMessagesImages       = 16
	maxSubagentMessagesCursorLength = 4096
	subagentMessagesCursorVersion   = 1
)

type subagentMessagesCursor struct {
	Version      int    `json:"version"`
	Next         int    `json:"next"`
	Total        int    `json:"total"`
	ThreadID     string `json:"thread_id"`
	Path         string `json:"path"`
	Generation   uint64 `json:"generation"`
	FirstAnchor  string `json:"first"`
	LastAnchor   string `json:"last"`
	BeforeAnchor string `json:"before"`
}

func (s *Server) handleSubagentMessages(ctx context.Context, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var params protocol.RPCSubagentMessagesParams
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("subagent_messages params: %w", err)
	}
	params.Target = strings.TrimSpace(params.Target)
	if params.Target == "" || len(params.Target) > protocol.MaxAgentPathBytes {
		return errors.New("subagent_messages target is invalid")
	}
	if params.Limit == 0 {
		params.Limit = defaultSubagentMessagesLimit
	}
	if params.Limit < 1 || params.Limit > maxSubagentMessagesLimit {
		return fmt.Errorf("subagent_messages limit must be between 1 and %d", maxSubagentMessagesLimit)
	}
	if params.MaxBytes == 0 {
		params.MaxBytes = defaultSubagentMessagesBytes
	}
	if params.MaxBytes < minSubagentMessagesBytes || params.MaxBytes > maxSubagentMessagesBytes {
		return fmt.Errorf("subagent_messages max_bytes must be between %d and %d", minSubagentMessagesBytes, maxSubagentMessagesBytes)
	}

	state, err := s.app.Subagent(ctx, params.Target)
	if err != nil {
		return err
	}
	messages, err := s.app.SubagentMessages(ctx, params.Target)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	messages = publicMessages(messages)
	page, err := buildSubagentMessagesPage(req.ID, state, messages, params)
	if err != nil {
		return err
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: page})
}

func buildSubagentMessagesPage(requestID string, state protocol.SubagentState, messages []protocol.Message, params protocol.RPCSubagentMessagesParams) (protocol.RPCSubagentMessagesPage, error) {
	cursor, err := decodeSubagentMessagesCursor(params.Cursor, state.Agent, messages)
	if err != nil {
		return protocol.RPCSubagentMessagesPage{}, err
	}
	start := cursor.Next
	total := cursor.Total
	if params.Cursor == "" {
		total = stableMessagesSnapshotTotal(messages)
		for i := range total {
			if messages[i].ID == "" {
				return protocol.RPCSubagentMessagesPage{}, errors.New("subagent_messages history contains a message without an id")
			}
		}
		cursor = snapshotSubagentMessagesCursor(state, messages, 0, total)
	}
	if start == total {
		return protocol.RPCSubagentMessagesPage{
			Agent: state.Agent, Generation: cursor.Generation, Messages: []protocol.Message{}, Start: start, Total: total,
		}, nil
	}

	end := start
	imageCount := 0
	var selected protocol.RPCSubagentMessagesPage
	for end < total && end-start < params.Limit {
		nextImages := imageCount + historyImageCount(messages[end])
		if nextImages > maxSubagentMessagesImages {
			if end == start {
				return protocol.RPCSubagentMessagesPage{}, fmt.Errorf("subagent_messages entry at offset %d exceeds the %d image page limit", start, maxSubagentMessagesImages)
			}
			break
		}
		end++
		imageCount = nextImages
		candidate, err := makeSubagentMessagesPage(state.Agent, messages, cursor, start, end, total)
		if err != nil {
			return protocol.RPCSubagentMessagesPage{}, err
		}
		size, err := subagentMessagesFrameSize(requestID, candidate)
		if err != nil {
			return protocol.RPCSubagentMessagesPage{}, err
		}
		if size > maxSubagentMessagesBytes {
			if end == start+1 {
				return protocol.RPCSubagentMessagesPage{}, fmt.Errorf("subagent_messages entry at offset %d exceeds the %d byte frame limit", start, maxSubagentMessagesBytes)
			}
			break
		}
		if size > params.MaxBytes && end > start+1 {
			break
		}
		selected = candidate
	}
	if len(selected.Messages) == 0 {
		candidate, err := makeSubagentMessagesPage(state.Agent, messages, cursor, start, start+1, total)
		if err != nil {
			return protocol.RPCSubagentMessagesPage{}, err
		}
		size, err := subagentMessagesFrameSize(requestID, candidate)
		if err != nil {
			return protocol.RPCSubagentMessagesPage{}, err
		}
		if size > maxSubagentMessagesBytes {
			return protocol.RPCSubagentMessagesPage{}, fmt.Errorf("subagent_messages entry at offset %d exceeds the %d byte frame limit", start, maxSubagentMessagesBytes)
		}
		selected = candidate
	}
	return selected, nil
}

func makeSubagentMessagesPage(agent protocol.AgentRef, messages []protocol.Message, snapshot subagentMessagesCursor, start, end, total int) (protocol.RPCSubagentMessagesPage, error) {
	page := protocol.RPCSubagentMessagesPage{
		Agent: agent, Generation: snapshot.Generation, Messages: messages[start:end], Start: start, Total: total, HasMore: end < total,
	}
	if page.HasMore {
		next := snapshot
		next.Next = end
		next.BeforeAnchor = subagentMessagesAnchor(messages[end-1])
		cursor, err := encodeSubagentMessagesCursor(next)
		if err != nil {
			return protocol.RPCSubagentMessagesPage{}, err
		}
		page.NextCursor = cursor
	}
	return page, nil
}

func snapshotSubagentMessagesCursor(state protocol.SubagentState, messages []protocol.Message, next, total int) subagentMessagesCursor {
	cursor := subagentMessagesCursor{
		Version: subagentMessagesCursorVersion, Next: next, Total: total,
		ThreadID: state.Agent.ThreadID, Path: string(state.Agent.Path), Generation: state.Generation,
	}
	if total > 0 {
		cursor.FirstAnchor = subagentMessagesAnchor(messages[0])
		cursor.LastAnchor = subagentMessagesAnchor(messages[total-1])
	}
	return cursor
}

func encodeSubagentMessagesCursor(cursor subagentMessagesCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode subagent_messages cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeSubagentMessagesCursor(encoded string, agent protocol.AgentRef, messages []protocol.Message) (subagentMessagesCursor, error) {
	if encoded == "" {
		return subagentMessagesCursor{}, nil
	}
	if len(encoded) > maxSubagentMessagesCursorLength {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor is too long")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor is invalid")
	}
	var cursor subagentMessagesCursor
	if err := json.Unmarshal(payload, &cursor, json.RejectUnknownMembers(true)); err != nil {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor is invalid")
	}
	if cursor.Version != subagentMessagesCursorVersion || cursor.Total < 1 || cursor.Next < 1 || cursor.Next >= cursor.Total {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor is invalid")
	}
	if cursor.ThreadID == "" || cursor.ThreadID != agent.ThreadID || cursor.Path != string(agent.Path) {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor does not match the selected agent")
	}
	if cursor.Total > len(messages) {
		return subagentMessagesCursor{}, errors.New("subagent_messages snapshot is no longer available")
	}
	if messages[0].ID == "" || messages[cursor.Total-1].ID == "" || messages[cursor.Next-1].ID == "" {
		return subagentMessagesCursor{}, errors.New("subagent_messages history contains a message without an id")
	}
	if subagentMessagesAnchor(messages[0]) != cursor.FirstAnchor ||
		subagentMessagesAnchor(messages[cursor.Total-1]) != cursor.LastAnchor ||
		subagentMessagesAnchor(messages[cursor.Next-1]) != cursor.BeforeAnchor {
		return subagentMessagesCursor{}, errors.New("subagent_messages cursor does not match the selected agent history")
	}
	return cursor, nil
}

func subagentMessagesFrameSize(requestID string, page protocol.RPCSubagentMessagesPage) (int, error) {
	frame, err := jsonv1.Marshal(
		Response{ID: requestID, Type: "response", Command: "subagent_messages", Success: true, Data: page},
	)
	if err != nil {
		return 0, fmt.Errorf("encode subagent_messages response: %w", err)
	}
	return len(frame) + 1, nil
}

func subagentMessagesAnchor(message protocol.Message) string {
	sum := sha256.Sum256([]byte(message.ID))
	return fmt.Sprintf("%x", sum)
}
