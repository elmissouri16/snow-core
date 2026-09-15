package web

import (
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Assistant identity is assigned when its text row is created from an accepted
// root event, not retroactively associated with a nearby user or matching text.
func (r *liveRuntime) assistantSourceTurn(e protocol.AgentEvent) string {
	if e.TurnID == "" || !runtimeOption(e.TurnID) || e.TurnID != r.turnID || e.RootEpoch == 0 || e.TurnSequence == 0 {
		return ""
	}
	return strings.Clone(e.TurnID)
}

// Only the active final text row can become a live regeneration target, and
// only after authoritative successful completion. Tool/plan boundaries clear
// assistant, so a preface before a final tool or plan is never promoted.
func (r *liveRuntime) completeRegenerateReplyLocked(status protocol.RPCPromptStatus) {
	if status != protocol.RPCPromptCompletedStatus || r.snapshot.CancelRequested || r.activityCanceled || r.assistantHasPlan || r.assistant < 0 || r.assistant >= len(r.snapshot.Messages) {
		return
	}
	message := &r.snapshot.Messages[r.assistant]
	if r.queue.control != nil && r.queue.control.TurnID == r.turnID && r.queue.sourceUserID != "" && (r.queue.replyID == "" || message.SourceID != r.queue.replyID) {
		return
	}
	if message.Role == "assistant" && message.SourceTurnID != "" && message.SourceTurnID == r.turnID && strings.TrimSpace(message.Text) != "" && !message.Truncated && len(message.Tools) == 0 {
		if r.promptID == "" {
			// Completion can beat the normal prompt ACK. Keep its exact row private
			// until Client.Call correlates the admission ID; an unrelated early
			// completion must never advertise a regeneration target.
			r.pendingRegenerateReplyID = message.ID
		} else {
			message.CanRegenerate = true
		}
	}
}

// History projections retain block-specific local IDs. Eligibility belongs to
// just the last public text segment of this exact terminal assistant entry;
// core prepare still verifies its unique user-origin turn and active branch.
func (r *liveRuntime) markHistoryRegenerateReply(message protocol.Message, source, key string) {
	if source == "" || !message.IsRegeneratableReply() {
		return
	}
	for i := len(r.snapshot.Messages) - 1; i >= 0; i-- {
		row := &r.snapshot.Messages[i]
		if row.SourceID == source && strings.HasPrefix(row.ID, key+"-") && row.Role == "assistant" && strings.TrimSpace(row.Text) != "" {
			row.CanRegenerate = !row.Truncated && len(row.Tools) == 0
			return
		}
	}
}

func (r *liveRuntime) acknowledgeRegenerateReplyLocked() {
	id := r.pendingRegenerateReplyID
	r.pendingRegenerateReplyID = ""
	if id == "" {
		return
	}
	for i := range r.snapshot.Messages {
		message := &r.snapshot.Messages[i]
		if message.ID == id && message.Role == "assistant" && message.SourceTurnID != "" && message.SourceTurnID == r.turnID && !message.Truncated {
			message.CanRegenerate = true
			r.publishLocked()
			return
		}
	}
}
