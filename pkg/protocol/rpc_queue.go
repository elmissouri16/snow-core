package protocol

import "slices"

const (
	RPCQueueMaxItems          = 8
	RPCQueueMaxTextBytes      = 64 << 10
	RPCQueueMaxTotalBytes     = 256 << 10
	RPCQueueRejectedErrorCode = "queue_rejected"
	RPCQueueStaleErrorCode    = "queue_stale"
	RPCQueueUnknownErrorCode  = "queue_unknown"
)

type RPCQueueListParams struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
}
type RPCQueueEnqueueParams struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	Revision  uint64 `json:"revision"`
	Text      string `json:"text"`
}
type RPCQueueUpdateParams struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	Revision  uint64 `json:"revision"`
	ItemID    string `json:"item_id"`
	Text      string `json:"text"`
}
type RPCQueueRemoveParams struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	Revision  uint64 `json:"revision"`
	ItemID    string `json:"item_id"`
}

// QueueControlItem is a bounded, explicitly submitted follow-up. Held work is
// never scheduled automatically; unknown work is not proof of non-execution.
type QueueControlItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"` // pending, delivering, held, delivery_unknown
}

// QueueControlChange attributes one revision without inferring delivery from
// disappearance. Entry identities are captured at successful persistence, never
// from an unrelated branch tip. Text is only the public persisted user input.
type QueueControlChange struct {
	Kind                  string `json:"kind"`
	ItemID                string `json:"item_id,omitempty"`
	UserEntryID           string `json:"user_entry_id,omitempty"`
	PreviousUserEntryID   string `json:"previous_user_entry_id,omitempty"`
	PrecedingReplyEntryID string `json:"preceding_reply_entry_id,omitempty"`
	ReplyEntryID          string `json:"reply_entry_id,omitempty"`
	SpanID                string `json:"span_id,omitempty"`
	Text                  string `json:"text,omitempty"`
}

// QueueControl is a complete same-session, exact-root-bound queue snapshot.
// Its global revision advances even across roots. Items plus review_items are
// capped together at eight items and 256 KiB; roots cannot replace held work.
type QueueControl struct {
	SessionID   string             `json:"session_id"`
	TurnID      string             `json:"turn_id"`
	Revision    uint64             `json:"revision"`
	Accepting   bool               `json:"accepting"`
	Items       []QueueControlItem `json:"items"`
	ReviewItems []QueueControlItem `json:"review_items"`
	Change      QueueControlChange `json:"change"`
}

func (q *QueueControl) Clone() *QueueControl {
	if q == nil {
		return nil
	}
	out := *q
	out.Items = slices.Clone(q.Items)
	out.ReviewItems = slices.Clone(q.ReviewItems)
	return &out
}
