package agent

import (
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrSessionReasoningStale = errors.New("agent: session reasoning authority changed; inspect again")
var ErrSessionReasoningBusy = errors.New("agent: session reasoning requires idle authority without queued work or nonterminal goals")
var ErrSessionReasoningUnsupported = errors.New("agent: session reasoning preference is not supported")
var ErrSessionReasoningUnknown = errors.New("agent: session reasoning outcome requires authoritative refresh")

// SessionReasoningAdmitted requires the caller's admission lock. Unlike the
// historical IdleSessionAdmitted helper it never stops or resumes a goal.
func (a *Agent) SessionReasoningAdmitted(sessionID string) (protocol.RPCSessionReasoning, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sessionReasoningLocked(sessionID)
}

func (a *Agent) sessionReasoningLocked(sessionID string) (protocol.RPCSessionReasoning, error) {
	var out protocol.RPCSessionReasoning
	if a.closed || a.running || a.autoRunning || a.goalRun != nil || len(a.queuedInputs) != 0 || len(a.queueControl.items) != 0 || len(a.queueControl.review) != 0 {
		return out, ErrSessionReasoningBusy
	}
	if a.opts.Session == nil || sessionID == "" || a.opts.Session.ID() != sessionID {
		return out, ErrSessionReasoningStale
	}
	if a.opts.Goal != nil {
		goal, err := a.opts.Goal.Get()
		if err != nil {
			return out, err
		}
		if goal != nil && !goal.Status.Terminal() {
			return out, ErrSessionReasoningBusy
		}
	}
	branch, ok := a.opts.Session.(session.ActiveBranchStore)
	if !ok || branch.ActiveBranchID() == "" {
		return out, ErrSessionReasoningUnsupported
	}
	if a.model.ID == "" || a.model.Provider == "" || (a.mode != protocol.ModeDefault && a.mode != protocol.ModePlan) {
		return out, ErrSessionReasoningStale
	}
	permission := string(a.opts.Permission.Mode())
	if permission != "ask" && permission != "deny" && permission != "allow" {
		return out, ErrSessionReasoningStale
	}
	out.RPCSessionReasoningState = protocol.RPCSessionReasoningState{SessionID: sessionID, BranchID: branch.ActiveBranchID(), TipID: a.opts.Session.BranchTip(), Provider: a.model.Provider, Model: a.model.ID, Mode: a.mode, PermissionMode: permission, Thinking: a.effectiveThinkingLocked(a.mode), ReasoningSummary: protocol.NormalizeReasoningSummary(a.opts.ReasoningSummary), TextVerbosity: protocol.NormalizeTextVerbosity(a.opts.TextVerbosity)}
	out.ThinkingLevels = a.model.SupportedThinkingLevels()
	out.ReasoningSummaries = []protocol.ReasoningSummary{}
	out.TextVerbosities = []protocol.TextVerbosity{}
	// Unknown legacy summary support is not affirmative support for this control.
	if a.model.SupportsReasoningSummary != nil && *a.model.SupportsReasoningSummary {
		out.ReasoningSummaries = protocol.KnownReasoningSummaries()
	}
	if a.model.SupportsVerbosity {
		out.TextVerbosities = protocol.KnownTextVerbosities()
	}
	return out, nil
}

// SetSessionReasoningAdmitted performs one in-memory preference mutation under
// admission and mu. No persistence, callbacks, event publication, or automatic
// goal transitions occur here. Default and Plan retain independent thinking.
func (a *Agent) SetSessionReasoningAdmitted(p protocol.RPCSessionReasoningSetParams) (protocol.RPCSessionReasoning, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	before, err := a.sessionReasoningLocked(p.Expected.SessionID)
	if err != nil {
		return protocol.RPCSessionReasoning{}, err
	}
	if before.RPCSessionReasoningState != p.Expected {
		return protocol.RPCSessionReasoning{}, ErrSessionReasoningStale
	}
	switch p.Field {
	case "thinking":
		level := protocol.ThinkingLevel(p.Value)
		if !slices.Contains(before.ThinkingLevels, level) {
			return protocol.RPCSessionReasoning{}, ErrSessionReasoningUnsupported
		}
		if a.mode == protocol.ModePlan {
			a.opts.PlanThinking = new(level)
		} else {
			a.opts.Thinking = level
		}
	case "reasoning_summary":
		value := protocol.ReasoningSummary(p.Value)
		if !slices.Contains(before.ReasoningSummaries, value) {
			return protocol.RPCSessionReasoning{}, ErrSessionReasoningUnsupported
		}
		a.opts.ReasoningSummary = value
	case "text_verbosity":
		value := protocol.TextVerbosity(p.Value)
		if !slices.Contains(before.TextVerbosities, value) {
			return protocol.RPCSessionReasoning{}, ErrSessionReasoningUnsupported
		}
		a.opts.TextVerbosity = value
	default:
		return protocol.RPCSessionReasoning{}, ErrSessionReasoningUnsupported
	}
	a.latestContextTokens = 0
	a.latestRequestEstimate = 0
	a.latestContextReport = nil
	after, err := a.sessionReasoningLocked(p.Expected.SessionID)
	if err != nil || after.PermissionMode != before.PermissionMode || after.BranchID != before.BranchID || after.TipID != before.TipID {
		return protocol.RPCSessionReasoning{}, ErrSessionReasoningUnknown
	}
	return after, nil
}
