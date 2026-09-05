package permission

import (
	"context"
	"slices"
	"strings"
)

// Capability is a stable policy primitive inferred for one tool invocation.
type Capability string

const (
	CapabilityFilesystemReadWorkspace   Capability = "filesystem.read.workspace"
	CapabilityFilesystemWriteWorkspace  Capability = "filesystem.write.workspace"
	CapabilityFilesystemDeleteWorkspace Capability = "filesystem.delete.workspace"
	CapabilityFilesystemReadExternal    Capability = "filesystem.read.external"
	CapabilityFilesystemWriteExternal   Capability = "filesystem.write.external"
	CapabilityFilesystemDeleteExternal  Capability = "filesystem.delete.external"
	CapabilityProtectedResourceAccess   Capability = "resource.protected.access"
	CapabilityCredentialsRead           Capability = "credentials.read"
	CapabilitySSHAuthorizationWrite     Capability = "ssh.authorization.write"
	CapabilityRawDeviceAccess           Capability = "device.raw.access"
	CapabilityDockerSocketAccess        Capability = "container.socket.access"
	CapabilityPersistenceWrite          Capability = "persistence.write"
	CapabilityPrivilegeEscalation       Capability = "privilege.escalation"
	CapabilityProcessExec               Capability = "process.exec"
	CapabilityDynamicExec               Capability = "process.exec.dynamic"
	CapabilityNetworkRead               Capability = "network.read"
	CapabilityNetworkWrite              Capability = "network.write"
	CapabilityGitRead                   Capability = "git.read"
	CapabilityGitWrite                  Capability = "git.write"
	CapabilityGitRemoteRead             Capability = "git.remote_read"
	CapabilityGitRemoteWrite            Capability = "git.remote_write"
	CapabilityUnknown                   Capability = "effect.unknown"
	CapabilityAnalysisIncomplete        Capability = "analysis.incomplete"
)

// Effect is a bounded, tool-independent description of an inferred operation.
type Effect struct {
	Type       string     `json:"type"`
	Capability Capability `json:"capability"`
	Operation  string     `json:"operation,omitempty"`
	Resource   string     `json:"resource,omitempty"`
	Command    string     `json:"command,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Confidence string     `json:"confidence,omitempty"`
	Dynamic    bool       `json:"dynamic,omitzero"`
}

// Analysis is the preflight result used by policy and permission handling.
type Analysis struct {
	Effects      []Effect
	Capabilities []Capability
	Paths        []string
	Summary      string
	Unknown      bool
	Rememberable bool
	ScopeKey     string
	ScopeLabel   string
}

// PolicyDecision is the non-interactive hard-policy result.
type PolicyDecision struct {
	Denied bool
	Reason string
}

// InvocationPolicy applies non-overridable rules before user permission.
type InvocationPolicy interface {
	Evaluate(context.Context, Request) (PolicyDecision, error)
}

// DefaultPolicy denies high-confidence protected capabilities. Static analysis
// is advisory for all other effects, which continue through the normal
// ask/allow/deny permission service.
type DefaultPolicy struct{}

// Evaluate implements InvocationPolicy.
func (DefaultPolicy) Evaluate(_ context.Context, req Request) (PolicyDecision, error) {
	if slices.Contains(req.Capabilities, CapabilityAnalysisIncomplete) {
		return PolicyDecision{Denied: true, Reason: "shell analysis was incomplete"}, nil
	}
	for _, effect := range req.Effects {
		if effect.Confidence != "high" {
			continue
		}
		switch effect.Capability {
		case CapabilityProtectedResourceAccess, CapabilityCredentialsRead,
			CapabilitySSHAuthorizationWrite,
			CapabilityRawDeviceAccess,
			CapabilityDockerSocketAccess,
			CapabilityPersistenceWrite,
			CapabilityPrivilegeEscalation:
			reason := strings.TrimSpace(effect.Reason)
			if reason == "" {
				reason = "protected operation"
			}
			if effect.Resource != "" {
				reason += ": " + effect.Resource
			}
			return PolicyDecision{Denied: true, Reason: reason}, nil
		}
	}
	return PolicyDecision{}, nil
}

// CloneAnalysis makes a defensive copy for request assembly and tests.
func CloneAnalysis(in Analysis) Analysis {
	out := in
	out.Effects = slices.Clone(in.Effects)
	out.Capabilities = slices.Clone(in.Capabilities)
	out.Paths = slices.Clone(in.Paths)
	return out
}
