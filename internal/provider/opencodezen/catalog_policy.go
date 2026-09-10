package opencodezen

import (
	"cmp"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/provider/modelsdev"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// discoveredFreeModel admits a new ID only with explicit free pricing and a
// supported protocol. Model names and ID suffixes are not pricing evidence.
func discoveredFreeModel(id string, details modelsdev.Model) (freeModelSpec, bool) {
	if strings.TrimSpace(id) == "" || (details.ID != "" && details.ID != id) ||
		details.Status == "deprecated" || !details.Cost.Free() ||
		details.ToolCall == nil || !*details.ToolCall || details.Limit.Context <= 0 ||
		details.Limit.Output <= 0 || details.Limit.Input < 0 ||
		!slices.Contains(details.Modalities.Input, "text") || !slices.Contains(details.Modalities.Output, "text") {
		return freeModelSpec{}, false
	}
	var transport transportKind
	switch details.Provider.NPM {
	case "@ai-sdk/openai-compatible":
		transport = transportChat
	case "@ai-sdk/openai":
		transport = transportResponses
	default:
		return freeModelSpec{}, false
	}
	model := protocol.Model{
		Provider: ProviderID, ID: id, DisplayName: cmp.Or(details.Name, id),
		Description:   "Privacy: free-model data retention and training terms vary; review the current OpenCode Zen policy before submitting sensitive data.",
		ContextWindow: details.Limit.Context, MaxOutputTokens: details.Limit.Output,
		SupportsTools: true, SupportsVision: slices.Contains(details.Modalities.Input, "image"),
		Pricing: &protocol.ModelPricing{Currency: "USD"},
	}
	if details.Limit.Input > 0 && details.Limit.Input < model.ContextWindow {
		model.MaxContextWindow = model.ContextWindow
		model.ContextWindow = details.Limit.Input
	}
	if local, ok := freeModelByID(id); ok {
		model.Description = local.Model.Description
		// Retain locally verified stricter input limits even if upstream omits one.
		if local.Model.MaxContextWindow > 0 && local.Model.ContextWindow < model.ContextWindow {
			model.MaxContextWindow = model.ContextWindow
			model.ContextWindow = local.Model.ContextWindow
		}
	}
	applyReasoningMetadata(&model, details)
	return freeModelSpec{Model: model, Transport: transport}, true
}

// catalogSpec rehydrates transport and policy from verified metadata. Bundled
// models remain usable with older/sparse metadata, but explicit pricing or
// deprecation always overrides the bundled fallback.
func catalogSpec(id string, details modelsdev.Model, hasMetadata bool) (freeModelSpec, bool) {
	if hasMetadata {
		if (details.ID != "" && details.ID != id) || details.Status == "deprecated" || (details.Cost != nil && !details.Cost.Free()) {
			return freeModelSpec{}, false
		}
		if npm := details.Provider.NPM; npm != "" && npm != "@ai-sdk/openai" && npm != "@ai-sdk/openai-compatible" {
			return freeModelSpec{}, false
		}
		if spec, ok := discoveredFreeModel(id, details); ok {
			return spec, true
		}
	}
	spec, ok := freeModelByID(id)
	if ok && hasMetadata {
		applyReasoningMetadata(&spec.Model, details)
	}
	return spec, ok
}

func (p *Provider) modelSpec(id string) (freeModelSpec, bool) {
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	if p.cachedAt.IsZero() {
		return freeModelByID(id)
	}
	for _, model := range p.cachedModels {
		if model.ID == id {
			details, ok := p.cachedMetadata[id]
			spec, valid := catalogSpec(id, details, ok)
			spec.Model = model.Clone()
			return spec, valid
		}
	}
	return freeModelSpec{}, false
}
