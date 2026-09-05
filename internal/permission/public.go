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
	effects := make([]protocol.PermissionEffect, effectCount)
	for i, effect := range req.Effects[:effectCount] {
		effects[i] = protocol.PermissionEffect{
			Type:       boundRunes(effect.Type, maxPublicFieldRunes),
			Capability: boundRunes(string(effect.Capability), maxPublicFieldRunes),
			Operation:  boundRunes(effect.Operation, maxPublicFieldRunes),
			Resource:   boundRunes(effect.Resource, maxPublicReasonRunes),
			Command:    boundRunes(effect.Command, maxPublicFieldRunes),
			Reason:     boundRunes(effect.Reason, maxPublicReasonRunes),
			Confidence: boundRunes(effect.Confidence, maxPublicFieldRunes),
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
	return protocol.PermissionRequest{
		Tool:                  boundRunes(req.Tool, maxPublicFieldRunes),
		Args:                  slices.Clone(req.Args),
		Paths:                 paths,
		Risk:                  boundRunes(string(req.Risk), maxPublicFieldRunes),
		Reason:                boundRunes(req.Reason, maxPublicReasonRunes),
		Effects:               effects,
		Capabilities:          capabilities,
		Unknown:               req.Unknown,
		Rememberable:          req.Rememberable,
		EffectsTruncated:      len(req.Effects) > effectCount,
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
