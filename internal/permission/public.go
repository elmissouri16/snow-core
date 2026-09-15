package permission

import (
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	maxPublicEffects      = 32
	maxPublicCapabilities = 32
	maxPublicPaths        = 32
	maxPublicFieldRunes   = 512
	maxPublicReasonRunes  = 2048
)

// PublicRequest returns the bounded protocol projection used by every trusted
// permission broker. The opaque scope key is deliberately not exposed.
func PublicRequest(req Request) protocol.PermissionRequest {
	effectCount := min(len(req.Effects), maxPublicEffects)
	effectsTruncated := len(req.Effects) > effectCount
	boundEffectField := func(value string, limit int) string {
		bounded := boundRunes(value, limit)
		if bounded != value {
			effectsTruncated = true
		}
		return bounded
	}
	effects := make([]protocol.PermissionEffect, effectCount)
	for i, effect := range req.Effects[:effectCount] {
		effects[i] = protocol.PermissionEffect{
			Type:       boundEffectField(effect.Type, maxPublicFieldRunes),
			Capability: boundEffectField(string(effect.Capability), maxPublicFieldRunes),
			Operation:  boundEffectField(effect.Operation, maxPublicFieldRunes),
			Resource:   boundEffectField(effect.Resource, maxPublicReasonRunes),
			Command:    boundEffectField(effect.Command, maxPublicFieldRunes),
			Reason:     boundEffectField(effect.Reason, maxPublicReasonRunes),
			Confidence: boundEffectField(effect.Confidence, maxPublicFieldRunes),
			Dynamic:    effect.Dynamic,
		}
	}
	capabilityCount := min(len(req.Capabilities), maxPublicCapabilities)
	capabilities := make([]string, capabilityCount)
	for i, capability := range req.Capabilities[:capabilityCount] {
		capabilities[i] = boundRunes(string(capability), maxPublicFieldRunes)
	}
	pathCount := min(len(req.Paths), maxPublicPaths)
	paths := slices.Clone(req.Paths[:pathCount])
	for i := range paths {
		paths[i] = boundRunes(paths[i], maxPublicReasonRunes)
	}
	var plugin *protocol.PluginOrigin
	if req.Plugin != nil {
		plugin = new(*req.Plugin)
	}
	return protocol.PermissionRequest{
		Plugin:                plugin,
		Tool:                  boundRunes(req.Tool, maxPublicFieldRunes),
		Args:                  slices.Clone(req.Args),
		Paths:                 paths,
		Risk:                  boundRunes(string(req.Risk), maxPublicFieldRunes),
		Reason:                boundRunes(req.Reason, maxPublicReasonRunes),
		Effects:               effects,
		Capabilities:          capabilities,
		Unknown:               req.Unknown,
		Rememberable:          req.Rememberable,
		EffectsTruncated:      effectsTruncated,
		CapabilitiesTruncated: len(req.Capabilities) > capabilityCount,
		PathsTruncated:        len(req.Paths) > pathCount,
		ScopeLabel:            boundRunes(req.ScopeLabel, maxPublicReasonRunes),
	}
}

func boundRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
