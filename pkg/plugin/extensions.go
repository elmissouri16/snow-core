package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrUnavailable = errors.New("plugin capability unavailable")

// Invocation is created by the host adapter, not supplied by JavaScript.
type Invocation struct {
	PluginID    string
	Name        string
	Kind        string
	Fingerprint string
	Uses        []string
	ToolCallID  string
	Generation  uint64
}

type Environment struct {
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	Kind       string `json:"kind"`
	UI         bool   `json:"ui"`
	Generation uint64 `json:"generation"`
}

// ExtensionHost is an optional bridge. Payloads must be copied, public data.
// Implementations enforce the session's permissions and lifecycle authority.
type ExtensionHost interface {
	Environment() Environment
	Call(context.Context, Invocation, string, json.RawMessage) (json.RawMessage, error)
}

type HookRequest struct {
	Phase         string                             `json:"phase"`
	Text          string                             `json:"text,omitempty"`
	Tool          string                             `json:"tool,omitempty"`
	Arguments     json.RawMessage                    `json:"arguments,omitempty"`
	Content       []protocol.ContentBlock            `json:"content,omitempty"`
	IsError       bool                               `json:"isError,omitzero"`
	Context       []protocol.InternalContextFragment `json:"context,omitempty"`
	Agent         *protocol.AgentRef                 `json:"agent,omitempty"`
	Workflow      map[string]json.RawMessage         `json:"workflow,omitempty"`
	SessionChange *SessionChange                     `json:"sessionChange,omitempty"`
	Compaction    *CompactionPlan                    `json:"compaction,omitempty"`
}

// SessionChange describes a validated, not yet committed active transition.
type SessionChange struct {
	Operation    string `json:"operation"`
	OldSessionID string `json:"oldSessionId"`
	NewSessionID string `json:"newSessionId"`
	OldBranchID  string `json:"oldBranchId"`
	NewBranchID  string `json:"newBranchId,omitempty"`
	FromEntryID  string `json:"fromEntryId,omitempty"`
}

// CompactionPlan contains safe counts, never provider-private conversation data.
type CompactionPlan struct {
	Trigger            string `json:"trigger"`
	BoundaryID         string `json:"boundaryId"`
	SummarizedMessages int    `json:"summarizedMessages"`
	RetainedMessages   int    `json:"retainedMessages"`
}

// WorkflowHooks is optional; old Go extensions need not implement it.
type WorkflowHooks interface {
	HookWorkflowKeys(phase string) []string
}

type HookResult struct {
	Text      *string                            `json:"text,omitempty"`
	Arguments json.RawMessage                    `json:"arguments,omitempty"`
	Content   []protocol.ContentBlock            `json:"content,omitempty"`
	Context   []protocol.InternalContextFragment `json:"context,omitempty"`
	Block     string                             `json:"block,omitempty"`
}

// Extension is additive to Plugin; existing Go plugins need not implement it.
type Extension interface {
	ExtensionInfo() protocol.PluginInfo
	BindHost(ExtensionHost)
	Ready(context.Context) error
	RunCommand(context.Context, string, string) (ToolResult, error)
	HasHook(string, bool) bool
	RunHook(context.Context, HookRequest) (HookResult, error)
	RenderTool(context.Context, string, json.RawMessage) (*protocol.PluginNode, error)
}

// CapabilityForOperation is shared by adapters and hosts so an invocation
// cannot gain authority by retaining another handler's context.
func CapabilityForOperation(op string) string {
	switch op {
	case "agent.state", "agent.pending", "models.list", "session.messages", "session.branches", "goals.get", "subagents.list", "subagents.get", "subagents.messages", "subagents.models":
		return "read"
	case "agent.prompt", "agent.steer", "agent.followUp", "agent.abort", "models.set":
		return "agent"
	case "session.rename", "session.fork", "session.selectBranch", "session.renameBranch", "session.deleteBranch", "session.compact":
		return "session"
	case "goals.create", "goals.edit", "goals.pause", "goals.resume", "goals.clear":
		return "goals"
	case "subagents.spawn", "subagents.message", "subagents.followUp", "subagents.wait", "subagents.interrupt", "subagents.close", "subagents.resume":
		return "subagents"
	case "storage.get", "storage.set", "storage.delete":
		return "storage"
	case "ui.update", "ui.open", "ui.close", "ui.notify", "ui.input", "ui.select", "ui.confirm", "ui.form", "ui.editorGet", "ui.editorSet", "ui.editorInsert", "ui.theme":
		return "ui"
	case "workflow.get", "workflow.set", "workflow.delete", "workflow.update":
		return "workflow"
	case "tools.list", "tools.restrict", "tools.clearRestriction":
		return "tool_policy"
	case "tools.call":
		return "tools"
	case "sleep":
		return "read"
	}
	return ""
}

func AllowsOperation(uses []string, op string) bool {
	capability := CapabilityForOperation(op)
	return capability == "read" || (capability != "" && slices.Contains(uses, capability))
}
