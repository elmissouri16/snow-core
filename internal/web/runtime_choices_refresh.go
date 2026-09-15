package web

import (
	"context"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (r *liveRuntime) supports(capability string) bool {
	return slices.Contains(r.worker.Client.Ready().Capabilities, capability)
}

func (m *RuntimeManager) Choices(ctx context.Context, projectID, instanceID string) (RuntimeChoices, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeChoices{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeChoices{}, err
	}
	if err := r.refreshModels(); err != nil {
		return RuntimeChoices{}, err
	}
	if err := r.refreshSessions(); err != nil {
		return RuntimeChoices{}, err
	}
	if err := r.refreshTelemetry(); err != nil {
		return RuntimeChoices{}, err
	}
	result := r.choices
	result.ProjectID = projectID
	result.InstanceID = instanceID
	result.Models = slices.Clone(result.Models)
	result.Sessions = slices.Clone(result.Sessions)
	r.mu.Lock()
	if r.snapshot.Telemetry != nil {
		result.Telemetry = cloneRuntimeTelemetry(r.snapshot.Telemetry)
	}
	r.mu.Unlock()
	return result, nil
}

func (r *liveRuntime) refreshModels() error {
	var models []protocol.Model
	partial, truncated := false, false
	if r.supports("model_discovery") {
		var discovery protocol.RPCModelDiscovery
		if err := r.call(protocol.RPCRequest{Type: "models_discover"}, nil, &discovery); err != nil {
			return err
		}
		models = discovery.Models
		partial = discovery.Partial
		truncated = discovery.Truncated
	} else if r.supports("models_list") {
		var list protocol.RPCModelList
		if err := r.call(protocol.RPCRequest{Type: "models_list"}, nil, &list); err != nil {
			if !errors.Is(err, ErrRuntimeInvalid) {
				return err
			}
			partial = true
		} else {
			r.mu.Lock()
			activeProvider := r.snapshot.Provider
			r.mu.Unlock()
			if list.Provider != activeProvider {
				return ErrRuntimeInvalid
			}
			for _, model := range list.Models {
				if model.Provider == "" {
					model.Provider = list.Provider
				}
				if model.Provider == list.Provider {
					models = append(models, model)
				}
			}
		}
		partial = true // Legacy discovery is active-provider-only, not a global inventory.
	} else {
		partial = true
	}
	choices := make([]RuntimeModelChoice, 0, min(len(models), 1000))
	seen := make(map[[2]string]bool)
	for _, model := range models[:min(len(models), 1000)] {
		key := [2]string{model.Provider, model.ID}
		if seen[key] {
			continue
		}
		if model.Provider == "" || model.ID == "" || !runtimeOption(model.Provider) || !runtimeOption(model.ID) || runtimeText(model.Provider, 256) != model.Provider || runtimeText(model.ID, 256) != model.ID {
			truncated = true
			continue
		}
		seen[key] = true
		if runtimeText(model.DisplayName, 256) != model.DisplayName {
			truncated = true
		}
		choices = append(choices, RuntimeModelChoice{Provider: model.Provider, ID: model.ID, Name: runtimeText(model.DisplayName, 256), ContextWindow: max(0, model.ContextWindow)})
	}
	r.choices.Models = choices
	r.choices.ModelsPartial = partial
	r.choices.ModelsTruncated = truncated || len(models) > 1000
	return nil
}

func (r *liveRuntime) refreshSessions() error {
	var list protocol.RPCSessionList
	r.choices.SessionsAvailable = false
	if err := r.call(protocol.RPCRequest{Type: "sessions_list"}, nil, &list); err != nil {
		if errors.Is(err, ErrRuntimeInvalid) {
			r.choices.Sessions = []RuntimeSessionChoice{}
			r.choices.SessionsTruncated = false
			return nil
		}
		return err
	}
	r.applySessionList(list)
	return nil
}

func (r *liveRuntime) applySessionList(list protocol.RPCSessionList) {
	r.choices.SessionsAvailable = true
	choices := make([]RuntimeSessionChoice, 0, min(len(list.Sessions), 100))
	truncated := false
	for _, session := range list.Sessions[:min(len(list.Sessions), 1000)] {
		if !runtimeIdentifier(session.SessionID) {
			continue
		}
		if len(choices) == 100 {
			break
		}
		name := runtimeText(session.Name, 256)
		truncated = truncated || name != session.Name
		choices = append(choices, RuntimeSessionChoice{SessionID: session.SessionID, Name: name, UpdatedAt: session.UpdatedAt, Active: session.Active})
	}
	r.choices.Sessions = choices
	r.choices.SessionsTruncated = truncated || len(list.Sessions) > len(choices)
}

func (m *RuntimeManager) SetModel(ctx context.Context, projectID, instanceID, provider, model string) error {
	if provider == "" || model == "" || !runtimeOption(provider) || !runtimeOption(model) {
		return ErrRuntimeInvalid
	}
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return err
	}
	// Legacy set_model writes host configuration; never use it for a
	// conversation-local browser control.
	if !r.supports("session_model_selection") {
		return ErrRuntimeInvalid
	}
	found := false
	for _, choice := range r.choices.Models {
		if choice.Provider == provider && choice.ID == model {
			found = true
			break
		}
	}
	if !found {
		return ErrRuntimeInvalid
	}
	if err := r.call(protocol.RPCRequest{Type: "session_set_model", Provider: provider, Model: model}, nil, nil); err != nil {
		return err
	}
	r.mu.Lock()
	sessionID := r.snapshot.SessionID
	r.mu.Unlock()
	info, err := r.verifiedInfo(sessionID)
	if err != nil {
		return err
	}
	if info.Provider != provider || info.Model != model {
		r.fail()
		return ErrRuntimeUnavailable
	}
	r.mu.Lock()
	r.applyInfo(info)
	r.snapshot.Telemetry = &RuntimeTelemetry{}
	r.publishLocked()
	r.mu.Unlock()
	if err := r.refreshModels(); err != nil {
		return err
	}
	return r.refreshTelemetry()
}
