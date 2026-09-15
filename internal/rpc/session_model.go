package rpc

import (
	"context"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// handleSessionSetModel changes only the live session selection. Unlike the
// legacy set_model command, it never applies a persisted settings update.
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
	level := sessionModelThinking(model, s.app.Agent.Thinking())
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
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true})
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
