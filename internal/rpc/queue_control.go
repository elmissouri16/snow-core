package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func isQueueControlCommand(command string) bool {
	return command == "queue_list" || command == "queue_enqueue" || command == "queue_update" || command == "queue_remove"
}
func validateQueueControlParams(req Request) error {
	if len(req.Params) == 0 || len(req.Params) > 512<<10 {
		return agent.ErrQueueRejected
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(req.Params, &fields); err != nil {
		return agent.ErrQueueRejected
	}
	required := []string{"session_id", "turn_id"}
	if req.Type != "queue_list" {
		required = append(required, "revision")
	}
	if req.Type == "queue_update" || req.Type == "queue_remove" {
		required = append(required, "item_id")
	}
	if req.Type == "queue_update" || req.Type == "queue_enqueue" {
		required = append(required, "text")
	}
	if len(fields) != len(required) {
		return agent.ErrQueueRejected
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return agent.ErrQueueRejected
		}
		if key == "revision" {
			var revision uint64
			if json.Unmarshal(v, &revision) != nil {
				return agent.ErrQueueRejected
			}
			continue
		}
		var text string
		if json.Unmarshal(v, &text) != nil || text == "" || strings.ContainsRune(text, 0) || (key != "text" && len(text) > 256) {
			return agent.ErrQueueRejected
		}
	}
	return nil
}
func (s *Server) handleQueueControl(req Request) error {
	if err := validateQueueControlParams(req); err != nil {
		return err
	}
	var result protocol.QueueControl
	var err error
	switch req.Type {
	case "queue_list":
		var p protocol.RPCQueueListParams
		if err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err == nil {
			result, err = s.app.QueueList(p)
		}
	case "queue_enqueue":
		var p protocol.RPCQueueEnqueueParams
		if err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err == nil {
			result, err = s.app.QueueEnqueue(p)
		}
	case "queue_update":
		var p protocol.RPCQueueUpdateParams
		if err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err == nil {
			result, err = s.app.QueueUpdate(p)
		}
	case "queue_remove":
		var p protocol.RPCQueueRemoveParams
		if err = json.Unmarshal(req.Params, &p, json.RejectUnknownMembers(true)); err == nil {
			result, err = s.app.QueueRemove(p)
		}
	}
	if err != nil {
		return err
	}
	if err = s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result}); err != nil {
		return errors.Join(agent.ErrQueueUnknown, fmt.Errorf("queue acknowledgement unavailable: %w", err))
	}
	return nil
}
