package rpc

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const sessionModelMetadataKey = "rpc_session_model_v1"

type savedSessionModel struct {
	Version  int    `json:"version"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
}

// handleSessionSetModel changes and durably remembers only the current
// conversation selection. Unlike legacy set_model, it never rewrites host or
// project defaults.
func (s *Server) handleSessionSetModel(ctx context.Context, req Request) error {
	if !discoveryIdentity(req.Provider, modelDiscoveryProviderBytes) || !discoveryIdentity(req.Model, modelDiscoveryIDBytes) {
		return errors.New("session_set_model requires an exact provider and model")
	}
	if req.Thinking != "" || len(req.Params) != 0 || req.Message != "" || len(req.Content) != 0 || req.ReasoningSummary != "" || req.TextVerbosity != "" || req.Mode != "" || req.Method != "" || req.Secret != "" {
		return errors.New("session_set_model accepts only provider and model")
	}
	s.mu.Lock()
	busy := s.promptDone != nil
	s.mu.Unlock()
	if busy || s.app.Agent.IsRunning() {
		return errors.New("rpc: cannot change session model while running")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Use authoritative app metadata, never caller-supplied capabilities. An
	// inactive provider must already have been explicitly discovered.
	models := s.app.SubagentModels()
	_, _, active := s.app.ActiveModelsSnapshot()
	models = append(models, active...)
	index := slices.IndexFunc(models, func(model protocol.Model) bool {
		return model.Provider == req.Provider && model.ID == req.Model
	})
	if index < 0 {
		return errors.New("session_set_model pair is unavailable in the cached catalog")
	}
	model := models[index].Clone()
	beforeProvider, beforeModel, _ := s.app.ActiveModelsSnapshot()
	beforeThinking := s.app.Agent.Thinking()
	level := sessionModelThinking(model, beforeThinking)
	ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	if err := s.app.SetProviderModelThinkingContext(ctx, req.Provider, model, level); err != nil {
		// A stale adapter may revalidate its catalog. Do not expose private
		// transport/configuration failure details through this selection command.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if rpcErrorCode(err) == "session_busy" {
			return errors.New("rpc: cannot change session model while running")
		}
		return errors.New("session_set_model could not apply the selected catalog model")
	}
	// Server.Serve dispatches mutation frames serially, so no later prompt or
	// goal command can enter between the admitted transaction and this write.
	if err := s.saveSessionModel(req.Provider, req.Model, level); err != nil {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), modelDiscoveryTimeout)
		rollbackErr := s.app.SetProviderModelThinkingContext(rollbackCtx, beforeProvider, beforeModel, beforeThinking)
		rollbackCancel()
		if rollbackErr != nil {
			return errors.New("session_set_model could not save or roll back the conversation selection")
		}
		return errors.New("session_set_model could not save the conversation selection; the previous selection was restored")
	}
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
}

func (s *Server) saveSessionModel(provider, model string, thinking protocol.ThinkingLevel) error {
	metadata, ok := s.app.Session.(session.MetadataStore)
	if !ok {
		return errors.New("session model metadata is unavailable")
	}
	raw, err := json.Marshal(savedSessionModel{Version: 1, Provider: provider, Model: model, Thinking: string(thinking)}, json.Deterministic(true))
	if err != nil {
		return err
	}
	return metadata.SetMetadata(sessionModelMetadataKey, string(raw))
}

type preparedSessionModel struct {
	provider string
	model    protocol.Model
	thinking protocol.ThinkingLevel
}

// prepareSessionModel validates and resolves conversation-owned state before a
// session switch can commit and close the previous store.
func (s *Server) prepareSessionModel(ctx context.Context, sessionID string) (*preparedSessionModel, error) {
	ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	raw, found, err := s.app.SessionMetadataByID(sessionID, sessionModelMetadataKey)
	if err != nil || !found {
		return nil, err
	}
	var saved savedSessionModel
	if err := json.Unmarshal([]byte(raw), &saved, json.RejectUnknownMembers(true)); err != nil || saved.Version != 1 || saved.Thinking == "" || !discoveryIdentity(saved.Provider, modelDiscoveryProviderBytes) || !discoveryIdentity(saved.Model, modelDiscoveryIDBytes) {
		return nil, errors.New("session model metadata is invalid")
	}
	thinking, err := protocol.ParseThinkingLevel(saved.Thinking)
	if err != nil {
		return nil, errors.New("session model metadata is invalid")
	}
	model, err := s.app.ResolveProviderModel(ctx, saved.Provider, saved.Model)
	if err != nil {
		return nil, errors.New("saved session model is unavailable")
	}
	return &preparedSessionModel{provider: saved.Provider, model: model, thinking: sessionModelThinking(model, thinking)}, nil
}

func (s *Server) applySessionModel(ctx context.Context, prepared *preparedSessionModel) error {
	if prepared == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	if err := s.app.SetProviderModelThinkingContext(ctx, prepared.provider, prepared.model, prepared.thinking); err != nil {
		return errors.New("saved session model could not be restored")
	}
	return nil
}

func (s *Server) restoreSessionModel(ctx context.Context) error {
	prepared, err := s.prepareSessionModel(ctx, s.app.Session.ID())
	if err != nil {
		return err
	}
	return s.applySessionModel(ctx, prepared)
}

func sessionModelThinking(model protocol.Model, current protocol.ThinkingLevel) protocol.ThinkingLevel {
	if model.SupportsThinkingLevel(current) {
		return protocol.NormalizeThinkingLevel(current)
	}
	if model.DefaultThinking != "" && model.SupportsThinkingLevel(model.DefaultThinking) {
		return model.DefaultThinking
	}
	return protocol.ThinkingOff
}
