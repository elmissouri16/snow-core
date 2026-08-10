package protocol

// QueuedInputKind identifies when an accepted root input becomes eligible for
// delivery to the active agent run.
type QueuedInputKind string

const (
	QueuedInputSteer    QueuedInputKind = "steer"
	QueuedInputFollowUp QueuedInputKind = "follow_up"
)

// QueuedInput is one in-memory root input waiting for a safe delivery boundary.
// Order is monotonically increasing within one Agent and preserves submission
// order when a surface needs to restore cleared inputs.
type QueuedInput struct {
	ID    string          `json:"id"`
	Kind  QueuedInputKind `json:"kind"`
	Text  string          `json:"text"`
	Order uint64          `json:"order"`
}

// InputQueue is a complete, submission-ordered snapshot of pending root input.
type InputQueue struct {
	Items []QueuedInput `json:"items"`
}

// Clone returns an independent queue snapshot.
func (q *InputQueue) Clone() *InputQueue {
	if q == nil {
		return nil
	}
	out := &InputQueue{Items: make([]QueuedInput, len(q.Items))}
	copy(out.Items, q.Items)
	return out
}

// Counts returns pending steer and follow-up counts.
func (q InputQueue) Counts() (steering, followUps int) {
	for _, item := range q.Items {
		switch item.Kind {
		case QueuedInputSteer:
			steering++
		case QueuedInputFollowUp:
			followUps++
		}
	}
	return steering, followUps
}
