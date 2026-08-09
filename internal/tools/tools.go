// Package tools defines the Tool contract, the host interface tools execute
// against, and the registry used by the agent loop.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/snow-core/snow/internal/permission"
	publicplugin "github.com/snow-core/snow/pkg/plugin"
	"github.com/snow-core/snow/pkg/protocol"
)

// ToolSchema is the JSON-schema-backed description of a tool.
type ToolSchema = protocol.ToolSchema

// ToolResult is what a tool returns. Content is sent back to the model;
// Details is tool-private metadata for the UI and is not sent to the model.
type ToolResult struct {
	Content []protocol.ContentBlock
	IsError bool
	Details any
}

// PrivateDetails marks model-visible output that must not be copied into
// ordinary UI/plugin/log previews.
type PrivateDetails struct{}

// DiffDetails is private tool metadata for a file change. The agent uses it to
// render a colored preview for interactive clients without adding the diff to
// the model-facing tool result.
type DiffDetails struct {
	Path string
	Diff string
}

// TextResult builds a simple text tool result.
func TextResult(text string) ToolResult {
	return ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(text)}}
}

// ErrorResult builds an error tool result.
func ErrorResult(err error) ToolResult {
	return ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock("Error: " + err.Error())},
		IsError: true,
	}
}

// ToolProgressEvent is emitted via the host during long-running tools.
type ToolProgressEvent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Message    string `json:"message,omitempty"`
	Done       bool   `json:"done"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ToolHost gives tools access to environment and safety services.
type ToolHost interface {
	CWD() string
	// Roots returns path roots the tool may touch (cwd + explicit allows).
	Roots() []string
	Permission() permission.Service
	EmitProgress(event ToolProgressEvent)
	Environ() []string
}

// UserInputHost is an optional interactive host capability. Tools must fail
// fast when the active surface does not implement it.
type UserInputHost interface {
	RequestUserInput(context.Context, protocol.UserInputRequest) (protocol.UserInputResponse, error)
}

// Tool is a model-invoked capability.
type Tool interface {
	Schema() ToolSchema
	Run(ctx context.Context, args json.RawMessage, host ToolHost) (ToolResult, error)
}

// ToolMatch is compact router output. Full JSON schemas and executable tool
// handlers remain in Registry and are loaded only after selection.
type ToolMatch struct {
	ID          string  `json:"id"`
	Namespace   string  `json:"namespace,omitempty"`
	Name        string  `json:"name,omitempty"`
	Description string  `json:"description,omitempty"`
	Score       float64 `json:"score"`
}

// Router selects deferred tool IDs without knowing how tools execute.
type Router interface {
	Search(ctx context.Context, query string, limit int) ([]ToolMatch, error)
	DeferredCount() int
	Close() error
}

// RefreshableRouter can atomically rebuild its metadata index after a dynamic
// capability source (such as MCP tools/list_changed) updates the registry.
type RefreshableRouter interface {
	Router
	Refresh([]ToolDescriptor) error
}

// DiscoveryDetails is private tool-result metadata returned by search_tools.
// The agent consumes it to expand schemas on the next provider continuation.
type DiscoveryDetails struct {
	Matches        []ToolMatch
	CandidateCount int
	LatencyMS      int64
}

// SkillActivationDetails lets the agent keep activated skill instructions in
// durable context even after ordinary conversation compaction.
type SkillActivationDetails struct {
	Name    string
	Content string
}

// PlanUpdateDetails is private tool metadata promoted by the agent to a
// structured public plan_update event.
type PlanUpdateDetails struct{ Update protocol.PlanUpdate }

// CollaborationModeHost is an optional host capability used by mode-aware tools.
type CollaborationModeHost interface {
	CollaborationMode() protocol.CollaborationMode
}

// Source identifies where a capability was registered.
type Source string

const (
	SourceBuiltin  Source = "builtin"
	SourceGoPlugin Source = "go-plugin"
	SourceExternal Source = "external-plugin"
	SourceMCP      Source = "mcp"
	SourceSDK      Source = "sdk"
)

// ToolDescriptor keeps host-only registration metadata alongside the adapter.
type ToolDescriptor struct {
	Schema       ToolSchema
	Tool         Tool
	Source       Source
	Owner        string
	PluginID     string
	OriginalName string
	Risk         permission.Risk
	Capabilities []string
	Prompt       string
}

// Registry holds the set of tools available to the agent and the richer
// host-only descriptor metadata used by extension managers.
type Registry interface {
	Register(t Tool) error
	Get(name string) (Tool, bool)
	Schemas() []ToolSchema
	List() []Tool
	RegisterDescriptor(ToolDescriptor) error
	Descriptor(name string) (ToolDescriptor, bool)
	Descriptors() []ToolDescriptor
	UnregisterOwner(owner string) int
}

// DescriptorRegistry is kept as a descriptive alias for manager APIs.
type DescriptorRegistry = Registry

// SimpleRegistry is a thread-safe in-memory registry.
type SimpleRegistry struct {
	mu          sync.RWMutex
	descriptors map[string]ToolDescriptor
	keys        []string
}

var toolNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,127}$`)
var namespaceRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// NewRegistry returns an empty thread-safe registry.
func NewRegistry() *SimpleRegistry {
	return &SimpleRegistry{descriptors: make(map[string]ToolDescriptor)}
}

// Register adds a builtin tool, failing on duplicate names.
func (r *SimpleRegistry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("tool is nil")
	}
	schema := t.Schema()
	return r.RegisterDescriptor(ToolDescriptor{
		Schema: schema,
		Tool:   t,
		Source: SourceBuiltin,
		Owner:  "builtin",
		Risk:   defaultRisk(schema.Name),
	})
}

// RegisterDescriptor adds a tool and its host metadata.
func (r *SimpleRegistry) RegisterDescriptor(desc ToolDescriptor) error {
	if desc.Source == SourceGoPlugin || desc.Source == SourceExternal || desc.Source == SourceMCP {
		prefix := "plugin"
		if desc.Source == SourceMCP {
			prefix = "mcp"
		}
		if desc.PluginID == "" {
			return fmt.Errorf("tool %q has no plugin/server id", desc.Schema.Name)
		}
		canonicalPrefix := prefix + "_" + desc.PluginID + "_"
		if desc.OriginalName == "" {
			desc.OriginalName = desc.Schema.Name
		}
		if !strings.HasPrefix(desc.Schema.Name, canonicalPrefix) {
			name, err := publicplugin.Namespace(prefix, desc.PluginID, desc.OriginalName)
			if err != nil {
				return err
			}
			desc.Schema.Name = name
		}
	}
	if desc.Tool == nil {
		return fmt.Errorf("tool %q is nil", desc.Schema.Name)
	}
	if desc.Schema.Name == "" {
		return fmt.Errorf("tool has empty name")
	}
	if !toolNameRE.MatchString(desc.Schema.Name) {
		return fmt.Errorf("tool %q has invalid name", desc.Schema.Name)
	}
	if desc.Schema.Parameters == nil || len(desc.Schema.Parameters) == 0 {
		desc.Schema.Parameters = json.RawMessage(`{"type":"object"}`)
	}
	var schema map[string]any
	if err := json.Unmarshal(desc.Schema.Parameters, &schema); err != nil || schema == nil {
		return fmt.Errorf("tool %q has invalid parameters schema", desc.Schema.Name)
	}
	if desc.Risk == "" || !validRisk(desc.Risk) {
		desc.Risk = defaultRisk(desc.Schema.Name)
	}
	if desc.OriginalName == "" {
		desc.OriginalName = desc.Schema.Name
	}
	if desc.Owner == "" {
		desc.Owner = string(desc.Source)
	}
	if err := normalizeDiscovery(&desc); err != nil {
		return err
	}
	desc.Schema = cloneSchema(desc.Schema)
	desc.Capabilities = append([]string(nil), desc.Capabilities...)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.descriptors[desc.Schema.Name]; ok {
		return fmt.Errorf("tool %q already registered", desc.Schema.Name)
	}
	r.descriptors[desc.Schema.Name] = desc
	r.keys = append(r.keys, desc.Schema.Name)
	return nil
}

// Get returns a tool by name.
func (r *SimpleRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.descriptors[name]
	if !ok {
		return nil, false
	}
	return d.Tool, true
}

// Schemas returns schemas in registration order.
func (r *SimpleRegistry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSchema, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, cloneSchema(r.descriptors[k].Schema))
	}
	return out
}

// List returns tools in registration order.
func (r *SimpleRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, r.descriptors[k].Tool)
	}
	return out
}

// Descriptor returns host metadata for a registered tool.
func (r *SimpleRegistry) Descriptor(name string) (ToolDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.descriptors[name]
	if !ok {
		return ToolDescriptor{}, false
	}
	return cloneDescriptor(d), true
}

// Descriptors returns metadata in registration order.
func (r *SimpleRegistry) Descriptors() []ToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDescriptor, 0, len(r.keys))
	for _, k := range r.keys {
		out = append(out, cloneDescriptor(r.descriptors[k]))
	}
	return out
}

// UnregisterOwner removes all tools owned by owner and returns the count.
func (r *SimpleRegistry) UnregisterOwner(owner string) int {
	if owner == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	keys := r.keys[:0]
	for _, k := range r.keys {
		if r.descriptors[k].Owner == owner {
			delete(r.descriptors, k)
			removed++
			continue
		}
		keys = append(keys, k)
	}
	r.keys = keys
	return removed
}

func validRisk(r permission.Risk) bool {
	switch r {
	case permission.RiskRead, permission.RiskWrite, permission.RiskExec, permission.RiskNet, permission.RiskDelegate:
		return true
	}
	return false
}

// IsDeferred reports whether a descriptor is selected through the router.
func IsDeferred(desc ToolDescriptor) bool {
	return desc.Schema.Discovery != nil && desc.Schema.Discovery.Mode == protocol.ToolDiscoveryDeferred
}

// CanExpose performs the non-interactive permission check used for deferred
// schemas. Unknown permission implementations preserve the existing behavior.
func CanExpose(service permission.Service, desc ToolDescriptor) bool {
	if service == nil {
		return true
	}
	policy, ok := service.(permission.ExposurePolicy)
	if !ok {
		return true
	}
	return policy.CanExpose(desc.Schema.Name, desc.Risk)
}

func normalizeDiscovery(desc *ToolDescriptor) error {
	discovery := desc.Schema.Discovery
	if discovery == nil {
		return nil
	}
	copy := *discovery
	copy.Namespace = strings.TrimSpace(copy.Namespace)
	copy.Keywords = cloneStrings(copy.Keywords)
	if copy.Mode == "" {
		copy.Mode = protocol.ToolDiscoveryAlways
	}
	switch copy.Mode {
	case protocol.ToolDiscoveryAlways:
	case protocol.ToolDiscoveryDeferred:
		if copy.Namespace == "" && desc.PluginID != "" {
			copy.Namespace = desc.PluginID
		}
		if copy.Namespace == "" {
			return fmt.Errorf("tool %q uses deferred discovery without a namespace", desc.Schema.Name)
		}
	default:
		return fmt.Errorf("tool %q has invalid discovery mode %q", desc.Schema.Name, copy.Mode)
	}
	if copy.Namespace != "" && !namespaceRE.MatchString(copy.Namespace) {
		return fmt.Errorf("tool %q has invalid discovery namespace %q", desc.Schema.Name, copy.Namespace)
	}
	if len(copy.Keywords) > 64 {
		return fmt.Errorf("tool %q has too many discovery keywords", desc.Schema.Name)
	}
	for i, keyword := range copy.Keywords {
		copy.Keywords[i] = strings.TrimSpace(keyword)
		if len(copy.Keywords[i]) > 256 {
			return fmt.Errorf("tool %q has an oversized discovery keyword", desc.Schema.Name)
		}
	}
	desc.Schema.Discovery = &copy
	return nil
}

func cloneDescriptor(desc ToolDescriptor) ToolDescriptor {
	desc.Schema = cloneSchema(desc.Schema)
	desc.Capabilities = cloneStrings(desc.Capabilities)
	return desc
}

func cloneSchema(schema ToolSchema) ToolSchema {
	schema.Parameters = append(json.RawMessage(nil), schema.Parameters...)
	if schema.Discovery != nil {
		discovery := *schema.Discovery
		discovery.Keywords = cloneStrings(discovery.Keywords)
		schema.Discovery = &discovery
	}
	return schema
}

func cloneStrings(in []string) []string {
	return append([]string(nil), in...)
}

// CloneRegistry creates an independent registry snapshot filtered by immutable
// descriptor metadata. Tool implementations are shared; the registry map and
// deferred metadata are not. Callers must only share implementations whose Run
// method is concurrency-safe.
func CloneRegistry(src Registry, allow func(ToolDescriptor) bool) (*SimpleRegistry, error) {
	if src == nil {
		return nil, fmt.Errorf("source registry is nil")
	}
	out := NewRegistry()
	for _, desc := range src.Descriptors() {
		if allow != nil && !allow(desc) {
			continue
		}
		if err := out.RegisterDescriptor(desc); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func defaultRisk(name string) permission.Risk {
	switch name {
	case "read", "grep", "glob", "search_tools", "ask_user", "request_user_input", "update_plan":
		return permission.RiskRead
	case "write", "edit":
		return permission.RiskWrite
	case "bash":
		return permission.RiskExec
	case "webfetch":
		return permission.RiskNet
	case "spawn_agent", "followup_task":
		return permission.RiskDelegate
	case "send_message", "wait_agent", "interrupt_agent", "list_agents":
		return permission.RiskRead
	default:
		return permission.RiskExec
	}
}
