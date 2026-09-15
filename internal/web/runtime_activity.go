package web

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	runtimeActivityCount       = 64
	runtimeActivityOutputBytes = 8 << 10
	runtimeActivityTotalBytes  = 128 << 10
)

// RuntimeActivity is a bounded public tool lifecycle projection. Status is
// running, completed, failed, canceled, or unknown. Summary never contains raw arguments
// or event Message (which can contain a credential-bearing command).
//
// Output comes only from the normalized explicit-public-text ToolResult field.
// Legacy ToolOutput, progress, plugin views, and arbitrary metadata are never
// projected. Public task output is not automatically scrubbed for secrets.
// MessageID binds the original local tool_activity timeline marker, not a
// persisted assistant message. If history trimming removes that marker, the ID
// stays unchanged: consumers must show an unpositioned fallback, never attach
// the activity to an unrelated message.
type RuntimeActivity struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Tool      string `json:"tool"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	Output    string `json:"output"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated"`
}

// Hash correlation data rather than displaying arbitrary worker IDs or retaining
// unbounded metadata. The local prompt generation also separates legacy events
// without TurnID and providers that reuse call IDs between prompts.
type runtimeActivityKey struct {
	prompt uint64
	turn   [32]byte
	call   [32]byte
}

// projectActivity runs under r.mu, after the ordinary root/active-event gate.
func (r *liveRuntime) projectActivity(event protocol.AgentEvent) {
	if r.activityCanceled || event.ToolCallID == "" || (event.Type != protocol.EvToolStart && event.Type != protocol.EvToolEnd) {
		return
	}
	key := runtimeActivityKey{prompt: r.activityPrompt, turn: sha256.Sum256([]byte(event.TurnID)), call: sha256.Sum256([]byte(event.ToolCallID))}
	index := slices.Index(r.activityKeys, key)
	if index < 0 {
		tool := strings.Clone(runtimeText(event.ToolName, 128))
		if strings.TrimSpace(tool) == "" {
			tool = "tool"
		}
		// Only a new call advances the timeline. Synthetic ends also establish
		// a boundary; duplicate starts/ends must not split later assistant text.
		r.assistant = -1
		r.assistantHasPlan = false
		last := len(r.snapshot.Messages) - 1
		if last < 0 || r.snapshot.Messages[last].Role != "tool_activity" {
			r.addMessage(RuntimeMessage{Role: "tool_activity"})
		}
		activity := RuntimeActivity{
			MessageID: r.snapshot.Messages[len(r.snapshot.Messages)-1].ID,
			ID:        fmt.Sprintf("%d-%x-%x", key.prompt, key.turn, key.call),
			Tool:      tool, Status: "running", Summary: tool,
			Truncated: len(event.ToolName) > 128,
		}
		r.snapshot.Activities = append(r.snapshot.Activities, activity)
		r.activityKeys = append(r.activityKeys, key)
		index = len(r.snapshot.Activities) - 1
	}
	activity := &r.snapshot.Activities[index]
	// A duplicate/late event cannot resurrect an explicitly canceled activity or
	// turn a finished call back into running. Only tool_end proves completion;
	// some synthetic tool results (e.g. denial) legitimately have no tool_start.
	if activity.Status == "running" && event.Type == protocol.EvToolEnd {
		if event.ToolResult != nil {
			activity.Output = event.ToolResult.Text
			activity.Truncated = activity.Truncated || event.ToolResult.Truncated
		}
		activity.IsError = event.IsError
		activity.Status = "completed"
		if event.IsError {
			activity.Status = "failed"
		}
	}
	r.trimActivities()
	r.publishLocked()
}

func (r *liveRuntime) trimActivities() {
	bytes := 0
	for i := range r.snapshot.Activities {
		activity := &r.snapshot.Activities[i]
		activity.Truncated = activity.Truncated || len(activity.Output) > runtimeActivityOutputBytes
		activity.Output = strings.Clone(runtimeText(activity.Output, runtimeActivityOutputBytes))
		bytes += len(activity.Output)
	}
	for len(r.snapshot.Activities) > runtimeActivityCount || bytes > runtimeActivityTotalBytes {
		bytes -= len(r.snapshot.Activities[0].Output)
		r.snapshot.Activities = slices.Delete(r.snapshot.Activities, 0, 1)
		r.activityKeys = slices.Delete(r.activityKeys, 0, 1)
		r.snapshot.ActivitiesTruncated = true
	}
	r.pruneActivityMarkers()
}

// Remove only local markers whose bounded activity group was entirely evicted.
// Stable IDs and streaming indices of retained messages must survive removal.
func (r *liveRuntime) pruneActivityMarkers() {
	owners := make(map[string]bool, len(r.snapshot.Activities))
	for _, activity := range r.snapshot.Activities {
		owners[activity.MessageID] = true
	}
	for i := len(r.snapshot.Messages) - 1; i >= 0; i-- {
		message := r.snapshot.Messages[i]
		if message.Role != "tool_activity" || owners[message.ID] {
			continue
		}
		r.snapshot.Messages = slices.Delete(r.snapshot.Messages, i, i+1)
		if r.assistant >= i {
			r.assistant--
		}
		if r.plan >= i {
			r.plan--
		}
	}
}

// cancelActivities finalizes only still-pending calls, never claiming that work
// completed without tool_end. Callers hold r.mu and publish their own revision.
func (r *liveRuntime) cancelActivities() {
	r.activityCanceled = true
	for i := range r.snapshot.Activities {
		if r.snapshot.Activities[i].Status == "running" {
			r.snapshot.Activities[i].Status = "canceled"
		}
	}
}
