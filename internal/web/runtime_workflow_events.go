package web

import "github.com/elmissouri16/snow-core/pkg/protocol"

// acceptEvent rejects retired sessions and completed older turns before any
// projection. Untagged events remain compatible only with truly legacy workers.
// During transitions only monotonic epoch metadata is observed, never content.
func (r *liveRuntime) acceptEvent(e protocol.AgentEvent) bool {
	if r.messageEdit.committedTurn != "" && e.TurnID != r.messageEdit.committedTurn {
		return false
	}
	if e.RootEpoch != 0 {
		if e.RootEpoch <= r.retiredEpoch || e.RootEpoch < r.rootEpoch {
			return false
		}
		r.rootEpoch = e.RootEpoch
	} else if r.rootEpoch != 0 {
		return false
	}
	if r.transitioning || !r.busy {
		return false
	}
	if e.TurnSequence != 0 {
		if e.TurnSequence < r.turnSequence {
			return false
		}
		if e.TurnSequence == r.turnSequence && r.turnID == "" {
			return false
		}
		if e.TurnSequence > r.turnSequence {
			r.turnSequence = e.TurnSequence
			r.turnID = e.TurnID
		}
	}
	if e.TurnID != "" {
		if r.turnID != "" && r.turnID != e.TurnID {
			return false
		}
		r.turnID = e.TurnID
	}
	return true
}

func (r *liveRuntime) projectPlan(e protocol.AgentEvent) {
	// A text/plan/text response remains plan-bearing even though presentation
	// splits it into multiple rows. Only a real subsequent tool-step boundary or
	// a new admitted turn may clear this eligibility restriction.
	r.assistantHasPlan = true
	if e.Plan == nil {
		return
	}
	if e.Type == protocol.EvPlanStarted {
		r.plan = -1
		r.assistant = -1
	}
	if r.plan < 0 || (e.Plan.ID != "" && r.snapshot.Messages[r.plan].SourceID != runtimeText(e.Plan.ID, 128)) {
		r.addMessage(RuntimeMessage{SourceID: runtimeText(e.Plan.ID, 128), Role: "plan"})
		r.plan = len(r.snapshot.Messages) - 1
	}
	message := &r.snapshot.Messages[r.plan]
	if e.Type == protocol.EvPlanCompleted {
		message.Truncated = len(e.Plan.Text) > runtimeMessageBytes
		message.Text = runtimeText(e.Plan.Text, runtimeMessageBytes)
		r.plan = -1
	} else if e.Type == protocol.EvPlanDelta {
		room := runtimeMessageBytes - len(message.Text)
		message.Truncated = message.Truncated || len(e.Text) > room
		message.Text += runtimeText(e.Text, room)
	}
	r.trimMessages()
	r.publishLocked()
}
