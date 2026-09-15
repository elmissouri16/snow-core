package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Selection already owns queuePublishMu until delivery returns. New controls
// TryLock rather than waiting, so hooks can safely call them reentrantly.
func (a *Agent) queueControlSelectingLocked(id string) {
	q := &a.queueControl
	for i := range q.items {
		if q.items[i].ID == id {
			q.items[i].State = "delivering"
			q.revision++
			q.change = protocol.QueueControlChange{Kind: "selected", ItemID: id}
			a.publishQueueControlLocked()
			return
		}
	}
}

func (a *Agent) persistQueuedInput(ctx context.Context, item protocol.QueuedInput, msg protocol.Message) (string, error) {
	a.mu.RLock()
	root := a.turnID
	owned := slices.ContainsFunc(a.queueControl.items, func(i protocol.QueueControlItem) bool { return i.ID == item.ID })
	a.mu.RUnlock()
	if owned {
		if err := session.ValidateMessageEditText(msg.Content[0].Text); err != nil {
			return "", err
		}
	}
	user := session.Entry{Type: session.EntryMessage, ID: msg.ID, Message: &msg}
	spanID := ""
	a.mailboxPersistMu.Lock()
	var err error
	if batch, ok := a.opts.Session.(session.BatchStore); ok {
		spanID = newID()
		var span session.Entry
		span, err = session.NewAgentInputSpanEntry(spanID, session.AgentInputSpan{Version: 1, RootTurnID: root, QueueID: item.ID, UserEntryID: msg.ID, Kind: item.Kind})
		if err == nil {
			err = batch.AppendBatch([]session.Entry{span, user})
		}
	} else if owned {
		err = errors.New("session lacks atomic input-span persistence")
	} else {
		err = a.opts.Session.Append(user)
	}
	a.mailboxPersistMu.Unlock()
	if err == nil {
		return spanID, nil
	}
	if !owned {
		return spanID, fmt.Errorf("agent: append queued %s input: %w", item.Kind, err)
	}
	// A failed append can have committed. Only an exact durable absence probe
	// permits retention as held/unsent. Missing support is deliberately unknown.
	found, known := false, false
	if probe, ok := a.opts.Session.(session.MessageEditEntryPresenceStore); ok {
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		var probeErr error
		found, probeErr = probe.MessageEditEntryExists(probeCtx, msg.ID)
		cancel()
		known = probeErr == nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	q := &a.queueControl
	if known && !found {
		return spanID, fmt.Errorf("agent: queued input was not persisted: %w", err)
	}
	a.queuedInputs = slices.DeleteFunc(a.queuedInputs, func(i protocol.QueuedInput) bool { return i.ID == item.ID })
	if found {
		a.queueControlDeliveredLocked(item, msg, spanID)
	} else {
		for _, i := range q.items {
			if i.ID == item.ID {
				i.State = "delivery_unknown"
				q.review = append(q.review, i)
			}
		}
		q.items = slices.DeleteFunc(q.items, func(i protocol.QueueControlItem) bool { return i.ID == item.ID })
		q.revision++
		q.change = protocol.QueueControlChange{Kind: "delivery_unknown", ItemID: item.ID}
		a.publishQueueControlLocked()
	}
	return spanID, ErrQueueUnknown
}

func (a *Agent) queueControlDeliveredLocked(item protocol.QueuedInput, msg protocol.Message, spanID string) {
	q := &a.queueControl
	// Native accepted steering can deliver after provider-failure closure.
	// Its durable identity must still be reported even when managed follow-ups
	// have moved to review and q.ready is false.
	if q.turnID != a.turnID || q.sessionID != a.opts.Session.ID() {
		return
	}
	q.items = slices.DeleteFunc(q.items, func(i protocol.QueueControlItem) bool { return i.ID == item.ID })
	q.revision++
	q.change = protocol.QueueControlChange{Kind: "delivered", ItemID: item.ID, UserEntryID: msg.ID, PreviousUserEntryID: q.userID, PrecedingReplyEntryID: q.replyID, SpanID: spanID, Text: msg.Content[0].Text}
	if session.ValidateMessageEditText(q.change.Text) != nil {
		q.change.Text = ""
	}
	q.userID = msg.ID
	q.replyID = ""
	a.publishQueueControlLocked()
}
