package rpc

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
)

// Index zero is valid but must be explicit. JSON null, absent selectors and
// unknown fields must never silently select a user's first content block.
func validateImageParams(raw []byte, live bool) error {
	invalid := errors.New("image request requires explicit valid selectors")
	if len(raw) == 0 || len(raw) > 32*1024 {
		return invalid
	}
	var fields map[string]jsontext.Value
	if json.Unmarshal(raw, &fields) != nil {
		return invalid
	}
	for _, key := range []string{"session_id", "index"} {
		if _, ok := fields[key]; !ok {
			return invalid
		}
	}
	if !live {
		if _, ok := fields["message_id"]; !ok {
			return invalid
		}
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid
		}
		switch key {
		case "session_id", "message_id":
		case "turn_id":
			if !live {
				return invalid
			}
		case "index":
			var index int
			if json.Unmarshal(value, &index) != nil || index < 0 || index > 10000 {
				return invalid
			}
			continue
		default:
			return invalid
		}
		var id string
		if json.Unmarshal(value, &id) != nil || id == "" || len(id) > 4096 {
			return invalid
		}
	}
	_, message := fields["message_id"]
	_, turn := fields["turn_id"]
	if live && message == turn {
		return invalid
	}
	return nil
}
