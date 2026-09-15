package web

import (
	"context"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (m *RuntimeManager) reasoningRuntime(ctx context.Context, projectID, instanceID, sessionID string) (*liveRuntime, error) {
	if !runtimeIdentifier(sessionID) {
		return nil, ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return nil, err
	}
	if !r.supports(protocol.RPCSessionReasoningCapability) {
		r.control.Unlock()
		return nil, ErrRuntimeInvalid
	}
	r.mu.Lock()
	idle := r.reasoningIdleLocked(sessionID)
	r.mu.Unlock()
	if !idle {
		r.control.Unlock()
		return nil, ErrRuntimeBusy
	}
	if err := r.idle(); err != nil {
		r.control.Unlock()
		return nil, err
	}
	return r, nil
}

// readReasoning uses only explicit local read-only RPCs, never settings_get or
// network discovery. The worker supplies the full session/branch/tip CAS.
func (r *liveRuntime) readReasoning(sessionID string) (RuntimeReasoning, protocol.RPCSessionInfo, error) {
	info, err := r.verifiedInfo(sessionID)
	if err != nil {
		return RuntimeReasoning{}, info, err
	}
	var settings protocol.RPCSessionReasoning
	if err := r.call(protocol.RPCRequest{Type: "session_reasoning_get"}, protocol.RPCSessionReasoningGetParams{SessionID: sessionID}, &settings); err != nil {
		return RuntimeReasoning{}, info, err
	}
	if !reasoningValues(info, settings) {
		r.fail()
		return RuntimeReasoning{}, info, ErrRuntimeUnavailable
	}
	view := RuntimeReasoning{ProjectID: r.project.ID, InstanceID: r.instanceID, SessionID: sessionID, BranchID: settings.BranchID, TipID: settings.TipID, Provider: info.Provider, Model: info.Model, Mode: string(info.CollaborationMode), PermissionMode: info.PermissionMode, Thinking: string(info.Thinking), ReasoningSummary: string(info.ReasoningSummary), TextVerbosity: string(info.TextVerbosity), CurrentSessionAvailable: true}
	if !reasoningCapabilities(&view, info, settings) {
		r.fail()
		return RuntimeReasoning{}, info, ErrRuntimeUnavailable
	}
	return view, info, nil
}

func (m *RuntimeManager) InspectReasoning(ctx context.Context, projectID, instanceID, sessionID string) (RuntimeReasoning, error) {
	r, err := m.reasoningRuntime(ctx, projectID, instanceID, sessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	defer r.control.Unlock()
	view, info, err := r.readReasoning(sessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	// Re-read effective authority before publishing a usable browser revision.
	check, finalInfo, err := r.readReasoning(sessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	if !reasoningFactsEqual(view, check) || !slices.Equal(info.ThinkingLevels, finalInfo.ThinkingLevels) {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeReasoning{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.reasoningIdleLocked(sessionID) {
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	// Keep streaming model/mode/thinking authority synchronized with inspection.
	r.applyInfo(finalInfo)
	r.publishLocked()
	view.Revision = r.snapshot.Revision
	return view, nil
}

func (m *RuntimeManager) SetReasoning(ctx context.Context, projectID, instanceID string, input RuntimeReasoningInput) (RuntimeReasoning, error) {
	expected := input.Expected
	if !input.Confirm || input.Scope != "session" || !reasoningField(input.Field) || input.Value == "" || !runtimeOption(input.Value) || expected.Revision == 0 || expected.ProjectID != projectID || expected.InstanceID != instanceID {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	r, err := m.reasoningRuntime(ctx, projectID, instanceID, expected.SessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	defer r.control.Unlock()
	r.mu.Lock()
	revisionMatches := expected.Revision == r.snapshot.Revision
	r.mu.Unlock()
	if !revisionMatches {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	before, _, err := r.readReasoning(expected.SessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	if !reasoningFactsEqual(expected, before) || !slices.Contains(reasoningOptions(before, input.Field), input.Value) {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	check, _, err := r.readReasoning(expected.SessionID)
	if err != nil {
		return RuntimeReasoning{}, err
	}
	if !reasoningFactsEqual(before, check) {
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeReasoning{}, err
	}
	r.mu.Lock()
	if !r.reasoningIdleLocked(expected.SessionID) || r.snapshot.Revision != expected.Revision {
		r.mu.Unlock()
		return RuntimeReasoning{}, ErrRuntimeInvalid
	}
	r.invalidateReasoningPreparationsLocked()
	r.mu.Unlock()
	// A typed single-field transaction cannot enable extensions or accidentally
	// write defaults; the worker checks full expected authority under admission.
	params := protocol.RPCSessionReasoningSetParams{Expected: reasoningRPCState(before), Field: input.Field, Value: input.Value}
	var reply protocol.RPCSessionReasoning
	if err := r.call(protocol.RPCRequest{Type: "session_reasoning_set"}, params, &reply); err != nil {
		r.fail()
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	after, info, err := r.readReasoning(expected.SessionID)
	if err != nil {
		r.fail()
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	want := before
	switch input.Field {
	case "thinking":
		want.Thinking = input.Value
	case "reasoning_summary":
		want.ReasoningSummary = input.Value
	case "text_verbosity":
		want.TextVerbosity = input.Value
	}
	if !reasoningFactsEqual(want, after) || reply.RPCSessionReasoningState != reasoningRPCState(after) {
		r.fail()
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		r.fail()
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	r.mu.Lock()
	if !r.reasoningIdleLocked(expected.SessionID) {
		r.mu.Unlock()
		r.fail()
		return RuntimeReasoning{}, ErrRuntimeUnavailable
	}
	defer r.mu.Unlock()
	r.applyInfo(info)
	r.publishLocked()
	after.Revision = r.snapshot.Revision
	return after, nil
}
