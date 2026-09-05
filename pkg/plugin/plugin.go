// Package plugin defines the dependency-light extension contract used by
// statically linked Go plugins.
//
// The package deliberately does not depend on internal snow packages. Plugin
// implementations can therefore live in external Go modules and are adapted
// to the agent runtime by internal/plugin.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ProtocolVersion is the version of the Go plugin manifest and event contract.
const ProtocolVersion = 2

// Manifest describes a loadable extension.
type Manifest struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	ProtocolVersion int      `json:"protocol_version"`
	Source          string   `json:"source,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

// Plugin is the lifecycle contract for a statically linked Go extension.
type Plugin interface {
	Manifest() Manifest
	Register(context.Context, Registrar) error
	Close(context.Context) error
}

// Registrar is scoped to one plugin owner. Registrations are rejected before
// the agent starts when names or schemas are invalid or collide.
type Registrar interface {
	RegisterTool(ToolDefinition) error
	Subscribe(EventType, EventHandler) (unsubscribe func())
}

// ToolDefinition describes a model-callable capability. Name is the plugin's
// original, unqualified name; the host adds the plugin namespace.
type ToolDefinition struct {
	Name         string
	Description  string
	Parameters   json.RawMessage
	Discovery    *protocol.ToolDiscovery
	Risk         string
	Capabilities []string
	Executor     ToolExecutor
}

// ToolExecutor executes a registered tool.
type ToolExecutor func(context.Context, ToolContext, json.RawMessage) (ToolResult, error)

// ToolContext carries bounded host capabilities to a tool.
type ToolContext struct {
	Context    context.Context
	SessionID  string
	CWD        string
	ToolCallID string
	Progress   func(ProgressUpdate) error
}

// ProgressUpdate is a bounded observation emitted by a running tool.
type ProgressUpdate struct {
	Message string `json:"message,omitempty"`
	Done    bool   `json:"done"`
	IsError bool   `json:"is_error,omitzero"`
}

// ToolResult is the result returned to the model. Details are private to the
// host and are not included in the provider-facing conversation.
type ToolResult struct {
	Content []protocol.ContentBlock
	Details any
	IsError bool
}

// EventType aliases the versioned public agent event type. Plugins can observe
// events but cannot mutate or veto them in protocol version 2.
type EventType = protocol.AgentEventType

const (
	EventSessionUpdated    = protocol.EvSessionUpdated
	EventTextDelta         = protocol.EvTextDelta
	EventThinkingDelta     = protocol.EvThinkingDelta
	EventToolStart         = protocol.EvToolStart
	EventToolProgress      = protocol.EvToolProgress
	EventToolEnd           = protocol.EvToolEnd
	EventToolRouting       = protocol.EvToolRouting
	EventPermissionRequest = protocol.EvPermissionRequest
	EventUserInputRequest  = protocol.EvUserInputRequest
	EventUsage             = protocol.EvUsage
	EventTurnDone          = protocol.EvTurnDone
	EventError             = protocol.EvError
	EventAborted           = protocol.EvAborted
	EventModelChanged      = protocol.EvModelChanged
	EventModeChanged       = protocol.EvModeChanged
	EventPlanStarted       = protocol.EvPlanStarted
	EventPlanDelta         = protocol.EvPlanDelta
	EventPlanCompleted     = protocol.EvPlanCompleted
	EventPlanUpdate        = protocol.EvPlanUpdate
	EventCompactionStarted = protocol.EvCompactionStarted
	EventCompactionDone    = protocol.EvCompactionDone
	EventThreadGoalUpdated = protocol.EvThreadGoalUpdated
)

// Event is an observation delivered to an extension.
type Event struct {
	Version int                 `json:"version"`
	Type    EventType           `json:"type"`
	Payload protocol.AgentEvent `json:"payload"`
}

// EventHandler observes an agent event. Panics are isolated by the host.
type EventHandler func(Event)

var identifierRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateIdentifier validates stable manifest and namespace components.
func ValidateIdentifier(kind, value string) error {
	if !identifierRE.MatchString(value) {
		return fmt.Errorf("plugin: invalid %s %q (want lowercase [a-z0-9][a-z0-9_-]{0,63})", kind, value)
	}
	return nil
}

// ValidateManifest validates the public lifecycle identity.
func ValidateManifest(m Manifest) error {
	if err := ValidateIdentifier("plugin id", m.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("plugin: manifest name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("plugin: manifest version is required")
	}
	if m.ProtocolVersion != 0 && m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("plugin: unsupported protocol version %d", m.ProtocolVersion)
	}
	return nil
}

// Namespace returns the canonical namespaced tool name.
func Namespace(prefix, owner, name string) (string, error) {
	if err := ValidateIdentifier("owner", owner); err != nil {
		return "", err
	}
	if err := ValidateIdentifier("tool name", name); err != nil {
		return "", err
	}
	if prefix == "" {
		return owner + "_" + name, nil
	}
	return prefix + "_" + owner + "_" + name, nil
}
