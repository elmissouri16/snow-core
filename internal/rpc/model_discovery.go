package rpc

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	modelDiscoveryTimeout       = 5 * time.Second
	modelDiscoveryLimit         = 512
	modelDiscoveryProviderBytes = 128
	modelDiscoveryIDBytes       = 512
	modelDiscoveryTextBytes     = 4096
	// Leave ample space below clients' 4 MiB frame limit for the response
	// envelope. Account for actual escaped model JSON, not raw string lengths.
	modelDiscoveryMaxResultBytes = 2 * 1024 * 1024
)

// Discovery is deliberately invoked only by the explicit runtime RPC command.
// Provider failures are represented by Partial, never by private error strings.
func (s *Server) handleModelDiscovery(ctx context.Context, req Request) error {
	ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	models, err := s.app.LoadProviderCatalogs(ctx)
	result := boundedModelDiscovery(models)
	result.Partial = err != nil || ctx.Err() != nil
	return s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: result})
}

func boundedModelDiscovery(models []protocol.Model) protocol.RPCModelDiscovery {
	result := protocol.RPCModelDiscovery{Models: make([]protocol.Model, 0, min(len(models), modelDiscoveryLimit))}
	// This conservatively reserves the result object, flags, array syntax and
	// ordinary response envelope before budgeting individual bounded records.
	encodedBytes := 1024
	for _, source := range models {
		if len(result.Models) == modelDiscoveryLimit {
			result.Truncated = true
			break
		}
		// Never truncate provider/model identities into a different selectable pair.
		if !discoveryIdentity(source.Provider, modelDiscoveryProviderBytes) || !discoveryIdentity(source.ID, modelDiscoveryIDBytes) {
			result.Truncated = true
			continue
		}
		model := source.Clone()
		model.DisplayName = discoveryText(model.DisplayName, modelDiscoveryIDBytes, &result.Truncated)
		model.Description = discoveryText(model.Description, modelDiscoveryTextBytes, &result.Truncated)
		if model.Upgrade != nil {
			if !discoveryIdentity(model.Upgrade.Model, modelDiscoveryIDBytes) {
				model.Upgrade = nil
				result.Truncated = true
			} else {
				model.Upgrade.Message = discoveryText(model.Upgrade.Message, modelDiscoveryTextBytes, &result.Truncated)
			}
		}
		if model.ContextWindow < 0 || model.MaxContextWindow < 0 || model.MaxOutputTokens < 0 {
			model.ContextWindow = max(0, model.ContextWindow)
			model.MaxContextWindow = max(0, model.MaxContextWindow)
			model.MaxOutputTokens = max(0, model.MaxOutputTokens)
			result.Truncated = true
		}
		if model.DefaultThinking != "" && !slices.Contains(protocol.KnownThinkingLevels(), model.DefaultThinking) {
			model.DefaultThinking = ""
			result.Truncated = true
		}
		model.ThinkingLevels = nil
		for _, level := range source.ThinkingLevels {
			if !slices.Contains(protocol.KnownThinkingLevels(), level) || slices.Contains(model.ThinkingLevels, level) {
				result.Truncated = true
				continue
			}
			model.ThinkingLevels = append(model.ThinkingLevels, level)
		}
		if model.Pricing != nil {
			p := model.Pricing
			if !discoveryPrice(p.InputPerMillion) || !discoveryPrice(p.OutputPerMillion) || !discoveryPrice(p.CacheReadPerMillion) || !discoveryPrice(p.CacheWritePerMillion) {
				model.Pricing = nil
				result.Truncated = true
			} else {
				p.Currency = discoveryText(p.Currency, 16, &result.Truncated)
			}
		}
		// Normalize first: encoding one record is bounded by the field limits
		// above. Match the existing RPC JSON writer's HTML/JS escaping even
		// though this new code uses JSON v2. Never encode the unbounded catalog.
		encoded, err := json.Marshal(model, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
		if err != nil {
			result.Truncated = true
			continue
		}
		if encodedBytes+len(encoded)+1 > modelDiscoveryMaxResultBytes {
			result.Truncated = true
			break
		}
		encodedBytes += len(encoded) + 1 // Comma between retained models.
		result.Models = append(result.Models, model)
	}
	return result
}

func discoveryIdentity(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func discoveryText(value string, limit int, truncated *bool) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "")
		*truncated = true
	}
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
		*truncated = true
	}
	return strings.Clone(value)
}

func discoveryPrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
