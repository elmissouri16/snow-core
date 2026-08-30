// Package plugin defines the dependency-light extension contract used by
// statically linked Go plugins and by foreign-runtime adapters.
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
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ProtocolVersion is the version of the external plugin protocol.
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

// ExternalToolDefinition is the protocol-v2 wire descriptor returned by an
// external runtime from initialize or tools/list. Risk is optional and defaults
// to exec so existing runtimes remain fail-closed. Capabilities are additional
// descriptor metadata scoped to this tool; risk controls permission classification.
type ExternalToolDefinition struct {
	Name         string                  `json:"name"`
	Description  string                  `json:"description"`
	Parameters   json.RawMessage         `json:"parameters"`
	Discovery    *protocol.ToolDiscovery `json:"discovery,omitempty"`
	Risk         string                  `json:"risk,omitempty"`
	Capabilities []string                `json:"capabilities,omitempty"`
}

// MergeCapabilities returns the sorted union of capability metadata groups.
func MergeCapabilities(groups ...[]string) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, capability := range group {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				seen[capability] = struct{}{}
			}
		}
	}
	merged := slices.Sorted(maps.Keys(seen))
	return merged
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

// PluginSpec declares an external executable. Command is argv, not a shell
// string. Env is an optional minimal environment supplied to the child.
type PluginSpec struct {
	ID               string          `json:"id"`
	Command          []string        `json:"command"`
	Enabled          bool            `json:"enabled"`
	CWD              string          `json:"cwd,omitempty"`
	Env              []string        `json:"env,omitempty"`
	TimeoutMS        int             `json:"timeout_ms,omitzero"`
	MaxFrameBytes    int             `json:"max_frame_bytes,omitzero"`
	MaxOutputBytes   int             `json:"max_output_bytes,omitzero"`
	MaxProgressBytes int             `json:"max_progress_bytes,omitzero"`
	MaxInputBytes    int             `json:"max_input_bytes,omitzero"`
	MaxConcurrent    int             `json:"max_concurrent,omitzero"`
	Capabilities     []string        `json:"capabilities,omitempty"`
	Config           json.RawMessage `json:"config,omitempty"`
}

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

// ValidateSpec validates an external process declaration.
func ValidateSpec(s PluginSpec) error {
	if err := ValidateIdentifier("plugin id", s.ID); err != nil {
		return err
	}
	if len(s.Command) == 0 || strings.TrimSpace(s.Command[0]) == "" {
		return errors.New("plugin: command is required")
	}
	for _, arg := range s.Command {
		if strings.ContainsRune(arg, 0) {
			return errors.New("plugin: command arguments cannot contain NUL")
		}
	}
	if strings.ContainsRune(s.CWD, 0) {
		return errors.New("plugin: cwd cannot contain NUL")
	}
	seenEnv := make(map[string]bool, len(s.Env))
	for _, entry := range s.Env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, " \t\r\n\x00") {
			return fmt.Errorf("plugin: invalid environment entry %q (want NAME=VALUE)", entry)
		}
		if seenEnv[name] {
			return fmt.Errorf("plugin: duplicate environment name %q", name)
		}
		seenEnv[name] = true
	}
	if s.TimeoutMS < 0 || s.MaxFrameBytes < 0 || s.MaxOutputBytes < 0 || s.MaxProgressBytes < 0 || s.MaxInputBytes < 0 || s.MaxConcurrent < 0 {
		return errors.New("plugin: limits cannot be negative")
	}
	if len(s.Config) > 0 && !json.Valid(s.Config) {
		return errors.New("plugin: config is not valid JSON")
	}
	return nil
}

// Namespace returns the canonical external tool name.
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
