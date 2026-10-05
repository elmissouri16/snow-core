package chatgpt

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"time"
)

// quotaMetadata follows the structured 429 signals in Codex api_bridge.rs at
// 447eac3b81183b32c1a09f5fba9617abfacbebe3. Neither message text nor a
// usage_limit_reached code alone establishes subscription exhaustion.
func quotaMetadata(status int, body []byte) (string, time.Time) {
	if status != http.StatusTooManyRequests || len(body) > maxHTTPErrorReadBytes {
		return "", time.Time{}
	}
	var payload struct {
		Error struct {
			Type     string         `json:"type"`
			Code     string         `json:"code"`
			ResetsAt jsontext.Value `json:"resets_at"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", time.Time{}
	}
	switch payload.Error.Type {
	case "usage_limit_reached":
		var seconds int64
		var reset time.Time
		// Ignore malformed optional metadata without losing the verified type.
		// Accept contemporary Unix seconds through the last RFC3339 year; this
		// rejects negative/zero epochs, millisecond timestamps, and overflow.
		if json.Unmarshal(payload.Error.ResetsAt, &seconds) == nil && seconds >= 946684800 && seconds <= 253402300799 {
			reset = time.Unix(seconds, 0).UTC()
		}
		return payload.Error.Type, reset
	case "usage_not_included", "insufficient_quota":
		return payload.Error.Type, time.Time{}
	}
	switch payload.Error.Code {
	case "insufficient_quota", "credit_balance_exhausted", "organization_spend_limit_exceeded", "project_spend_limit_exceeded", "organization_usage_limit_exceeded":
		return payload.Error.Code, time.Time{}
	}
	return "", time.Time{}
}
