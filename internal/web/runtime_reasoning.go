package web

import (
	"context"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeReasoning is an explicitly refreshed view, not a second settings store.
// Values are effective in Mode. Defaults persistence is never inferred from them.
type RuntimeReasoning struct {
	ProjectID               string   `json:"project_id"`
	InstanceID              string   `json:"instance_id"`
	SessionID               string   `json:"session_id"`
	BranchID                string   `json:"branch_id"`
	TipID                   string   `json:"tip_id"`
	Revision                uint64   `json:"revision"`
	Provider                string   `json:"provider"`
	Model                   string   `json:"model"`
	Mode                    string   `json:"mode"`
	PermissionMode          string   `json:"permission_mode"`
	Thinking                string   `json:"thinking"`
	ReasoningSummary        string   `json:"reasoning_summary"`
	TextVerbosity           string   `json:"text_verbosity"`
	ThinkingLevels          []string `json:"thinking_levels"`
	ReasoningSummaries      []string `json:"reasoning_summaries"`
	TextVerbosities         []string `json:"text_verbosities"`
	CurrentSessionAvailable bool     `json:"current_session_available"`
	DefaultsAvailable       bool     `json:"defaults_available"`
}

// RuntimeReasoningInput permits exactly one response preference per confirmation.
// No arbitrary RPC request or settings map crosses this interface.
type RuntimeReasoningInput struct {
	Expected RuntimeReasoning
	Scope    string
	Field    string
	Value    string
	Confirm  bool
}

type RuntimeReasoningBackend interface {
	InspectReasoning(context.Context, string, string, string) (RuntimeReasoning, error)
	SetReasoning(context.Context, string, string, RuntimeReasoningInput) (RuntimeReasoning, error)
}

func reasoningField(field string) bool {
	return field == "thinking" || field == "reasoning_summary" || field == "text_verbosity"
}

func (r *liveRuntime) reasoningIdleLocked(sessionID string) bool {
	if !r.permissionPolicyIdleLocked() || r.snapshot.SessionID != sessionID || r.snapshot.CancelRequested || r.goalBlocksHistoryLocked() || (r.snapshot.Mode != "default" && r.snapshot.Mode != "plan") {
		return false
	}
	return !(r.snapshot.Queue != nil && len(r.snapshot.Queue.Items) != 0 || r.queue.control != nil && (len(r.queue.control.Items) != 0 || len(r.queue.control.ReviewItems) != 0))
}

// Call with the control gate and mu held, before a settings mutation. Parent
// controls should share this invalidation when changing model/mode/permission.
// Held/review queue content must be rejected before this helper is called.
func (r *liveRuntime) invalidateReasoningPreparationsLocked() {
	r.messageEdit = runtimeMessageEditState{}
	r.versions = runtimeVersionState{}
	r.queue = runtimeQueueState{}
}

func reasoningValues(info protocol.RPCSessionInfo, settings protocol.RPCSessionReasoning) bool {
	if !runtimeOption(info.Provider) || !runtimeOption(info.Model) || runtimeText(info.Provider, 256) != info.Provider || runtimeText(info.Model, 256) != info.Model {
		return false
	}
	if info.SessionID != settings.SessionID || !runtimeIdentifier(settings.BranchID) || (settings.TipID != "" && !runtimeIdentifier(settings.TipID)) || info.CollaborationMode != settings.Mode || info.Provider != settings.Provider || info.Model != settings.Model || info.Thinking != settings.Thinking || info.ReasoningSummary != settings.ReasoningSummary || info.TextVerbosity != settings.TextVerbosity || info.PermissionMode != settings.PermissionMode {
		return false
	}
	if info.Goal != nil && !info.Goal.Status.Terminal() {
		return false
	}
	return slices.Contains(protocol.KnownThinkingLevels(), info.Thinking) && slices.Contains(protocol.KnownReasoningSummaries(), info.ReasoningSummary) && slices.Contains(protocol.KnownTextVerbosities(), info.TextVerbosity) && (info.CollaborationMode == protocol.ModeDefault || info.CollaborationMode == protocol.ModePlan) && info.PendingInputs.Total == 0 && info.PendingInputs.Steering == 0 && info.PendingInputs.FollowUp == 0
}

func reasoningFactsEqual(a, b RuntimeReasoning) bool {
	return a.ProjectID == b.ProjectID && a.InstanceID == b.InstanceID && a.SessionID == b.SessionID && a.BranchID == b.BranchID && a.TipID == b.TipID && a.Provider == b.Provider && a.Model == b.Model && a.Mode == b.Mode && a.PermissionMode == b.PermissionMode && a.Thinking == b.Thinking && a.ReasoningSummary == b.ReasoningSummary && a.TextVerbosity == b.TextVerbosity
}

func reasoningOptions(view RuntimeReasoning, field string) []string {
	switch field {
	case "thinking":
		return view.ThinkingLevels
	case "reasoning_summary":
		return view.ReasoningSummaries
	case "text_verbosity":
		return view.TextVerbosities
	default:
		return nil
	}
}

// Choices are already capability-derived by the admitted worker; validate the
// public enums and bounds rather than rediscovering or guessing model support.
func reasoningCapabilities(view *RuntimeReasoning, info protocol.RPCSessionInfo, value protocol.RPCSessionReasoning) bool {
	if len(value.ThinkingLevels) > 16 || len(value.ReasoningSummaries) > 16 || len(value.TextVerbosities) > 16 {
		return false
	}
	for _, level := range value.ThinkingLevels {
		if !slices.Contains(protocol.KnownThinkingLevels(), level) || !slices.Contains(info.ThinkingLevels, level) || slices.Contains(view.ThinkingLevels, string(level)) {
			return false
		}
		view.ThinkingLevels = append(view.ThinkingLevels, string(level))
	}
	if len(view.ThinkingLevels) < 2 {
		view.ThinkingLevels = nil
	}
	for _, item := range value.ReasoningSummaries {
		if !slices.Contains(protocol.KnownReasoningSummaries(), item) || slices.Contains(view.ReasoningSummaries, string(item)) {
			return false
		}
		view.ReasoningSummaries = append(view.ReasoningSummaries, string(item))
	}
	for _, item := range value.TextVerbosities {
		if !slices.Contains(protocol.KnownTextVerbosities(), item) || slices.Contains(view.TextVerbosities, string(item)) {
			return false
		}
		view.TextVerbosities = append(view.TextVerbosities, string(item))
	}
	return true
}

func reasoningRPCState(value RuntimeReasoning) protocol.RPCSessionReasoningState {
	return protocol.RPCSessionReasoningState{SessionID: value.SessionID, BranchID: value.BranchID, TipID: value.TipID, Provider: value.Provider, Model: value.Model, Mode: protocol.CollaborationMode(value.Mode), PermissionMode: value.PermissionMode, Thinking: protocol.ThinkingLevel(value.Thinking), ReasoningSummary: protocol.ReasoningSummary(value.ReasoningSummary), TextVerbosity: protocol.TextVerbosity(value.TextVerbosity)}
}

var _ RuntimeReasoningBackend = (*RuntimeManager)(nil)
