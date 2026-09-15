package web

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// HostSettings is an allowlist projection. It deliberately has no CWD, raw
// config, credentials, extensions, permissions, debug, or trust fields.
type HostSettings struct {
	Scope        string                        `json:"scope"`
	ProjectID    string                        `json:"project_id,omitempty"`
	Revision     string                        `json:"revision"`
	AppliesTo    string                        `json:"applies_to"`
	Availability string                        `json:"availability"`
	Global       *protocol.HostGlobalDefaults  `json:"global,omitempty"`
	Project      *protocol.HostProjectDefaults `json:"project,omitempty"`
}

func hostProvider(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || i > 0 && (r == '-' || r == '_' || r == '.')) {
			return false
		}
	}
	return true
}
func hostText(value string, limit int) bool {
	return len(value) > 0 && len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}
func hostValue(field, value string) bool {
	switch field {
	case "thinking":
		switch value {
		case "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
			return true
		}
	case "reasoning_summary":
		switch value {
		case "auto", "concise", "detailed", "off":
			return true
		}
	case "text_verbosity":
		switch value {
		case "low", "medium", "high":
			return true
		}
	}
	return false
}
func hostSource(source, scope string) bool {
	return source == "builtin" || source == "global" || scope == "project" && source == "project"
}
func validHostString(value protocol.HostStringDefault, field, scope string) bool {
	return hostValue(field, value.Effective) && hostSource(value.Source, scope) && (value.Explicit == nil || hostValue(field, *value.Explicit))
}
func validHostModel(value protocol.HostProviderModel) bool {
	return hostProvider(value.Provider) && hostText(value.Model, 256)
}
func validHostModelDefault(value protocol.HostProviderModelDefault, scope string) bool {
	// A fresh config deliberately leaves the model empty (provider default).
	// Existing host configs can also contain a partial explicit pair; only new
	// writes require both values. Empty explicit components mean inherited.
	validProjection := func(pair protocol.HostProviderModel, explicit bool) bool {
		return (hostProvider(pair.Provider) || explicit && pair.Provider == "") && (pair.Model == "" || hostText(pair.Model, 256))
	}
	return validProjection(value.Effective, false) && hostSource(value.Source, scope) && (value.Explicit == nil || validProjection(*value.Explicit, true))
}
func projectHostSettings(raw protocol.HostDefaultsResponse, scope, project string) (HostSettings, error) {
	invalid := func() (HostSettings, error) { return HostSettings{}, ErrHostControlUnavailable }
	if raw.Scope != scope || !hostText(raw.Revision, 128) || raw.AppliesTo != "future_runtime" {
		return invalid()
	}
	result := HostSettings{Scope: scope, ProjectID: project, Revision: raw.Revision, AppliesTo: "future_runtime", Availability: "not_network_verified"}
	switch scope {
	case "global":
		g := raw.Global
		if g == nil || raw.Project != nil || !validHostModelDefault(g.ProviderModel, scope) || !validHostString(g.Thinking, "thinking", scope) || !validHostString(g.ReasoningSummary, "reasoning_summary", scope) || !validHostString(g.TextVerbosity, "text_verbosity", scope) {
			return invalid()
		}
		result.Global = &protocol.HostGlobalDefaults{ProviderModel: g.ProviderModel, Thinking: g.Thinking, ReasoningSummary: g.ReasoningSummary, TextVerbosity: g.TextVerbosity}
	case "project":
		p := raw.Project
		if p == nil || raw.Global != nil || !validProjectID(project) || !validHostModelDefault(p.ProviderModel, scope) || !validHostString(p.Thinking, "thinking", scope) {
			return invalid()
		}
		result.Project = &protocol.HostProjectDefaults{ProviderModel: p.ProviderModel, Thinking: p.Thinking}
	default:
		return invalid()
	}
	return result, nil
}
func validHostStringOp(op *protocol.HostStringOperation, field string) bool {
	if op == nil {
		return true
	}
	return op.Op == "reset" && op.Value == nil || op.Op == "set" && op.Value != nil && hostValue(field, *op.Value)
}
func validHostModelOp(op *protocol.HostProviderModelOperation) bool {
	if op == nil {
		return true
	}
	return op.Op == "reset" && op.Value == nil || op.Op == "set" && op.Value != nil && validHostModel(*op.Value)
}
func validHostPatch(input protocol.HostDefaultsUpdateRequest) bool {
	if !hostText(input.Revision, 128) {
		return false
	}
	switch input.Scope {
	case "global":
		p := input.Global
		return p != nil && input.Project == nil && (p.ProviderModel != nil || p.Thinking != nil || p.ReasoningSummary != nil || p.TextVerbosity != nil) && validHostModelOp(p.ProviderModel) && validHostStringOp(p.Thinking, "thinking") && validHostStringOp(p.ReasoningSummary, "reasoning_summary") && validHostStringOp(p.TextVerbosity, "text_verbosity")
	case "project":
		p := input.Project
		return p != nil && input.Global == nil && (p.ProviderModel != nil || p.Thinking != nil) && validHostModelOp(p.ProviderModel) && validHostStringOp(p.Thinking, "thinking")
	}
	return false
}

func projectProviderStatus(raw protocol.HostProviderStatusResponse) (protocol.HostProviderStatusResponse, error) {
	result := protocol.HostProviderStatusResponse{Providers: []protocol.HostProviderStatus{}, CheckedLocally: true}
	seen := map[string]bool{}
	if !raw.CheckedLocally || len(raw.Providers) > 128 {
		return result, ErrHostControlUnavailable
	}
	for _, p := range raw.Providers {
		if !hostProvider(p.ProviderID) || seen[p.ProviderID] || !p.CheckedLocally {
			return result, ErrHostControlUnavailable
		}
		seen[p.ProviderID] = true
		// Fixed reason allowlist; never echo free-text worker diagnostics.
		reason := p.Reason
		switch p.State {
		case "configured":
			if reason != "credential_present" && reason != "anonymous_access" {
				return result, ErrHostControlUnavailable
			}
		case "expired":
			if reason != "credential_expired" {
				return result, ErrHostControlUnavailable
			}
		case "unavailable":
			switch reason {
			case "credential_missing", "credential_invalid", "auth_store_unavailable":
			default:
				return result, ErrHostControlUnavailable
			}
		default:
			return result, ErrHostControlUnavailable
		}
		result.Providers = append(result.Providers, protocol.HostProviderStatus{ProviderID: p.ProviderID, State: p.State, Reason: reason, CheckedLocally: true})
	}
	return result, nil
}
