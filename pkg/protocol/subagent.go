package protocol

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	RootAgentPath         AgentPath = "/root"
	MaxAgentPathBytes               = 512
	MaxAgentMessageBytes            = 64 * 1024
	MaxAgentNameBytes               = 64
	MaxAgentMetadataBytes           = 256
)

var agentPathSegmentRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// AgentPath is the canonical, model-facing identity of one agent in a tree.
type AgentPath string

// Validate rejects non-canonical and escaping paths.
func (p AgentPath) Validate() error {
	s := string(p)
	if s == string(RootAgentPath) {
		return nil
	}
	if s == "" || len(s) > MaxAgentPathBytes || !strings.HasPrefix(s, string(RootAgentPath)+"/") {
		return fmt.Errorf("invalid agent path %q", s)
	}
	for _, segment := range strings.Split(strings.TrimPrefix(s, string(RootAgentPath)+"/"), "/") {
		if segment == "root" || !agentPathSegmentRE.MatchString(segment) {
			return fmt.Errorf("invalid agent path segment %q", segment)
		}
	}
	return nil
}

// ResolveAgentPath resolves a relative child name or canonical path without
// allowing traversal outside /root.
func ResolveAgentPath(caller AgentPath, target string) (AgentPath, error) {
	if err := caller.Validate(); err != nil {
		return "", err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("agent path is empty")
	}
	var out AgentPath
	if strings.HasPrefix(target, "/") {
		out = AgentPath(target)
	} else {
		if strings.Contains(target, "/") {
			return "", fmt.Errorf("relative agent path must be one segment: %q", target)
		}
		out = AgentPath(strings.TrimSuffix(string(caller), "/") + "/" + target)
	}
	if err := out.Validate(); err != nil {
		return "", err
	}
	return out, nil
}

// Parent returns the canonical parent path. Root has no parent.
func (p AgentPath) Parent() (AgentPath, bool) {
	if p == RootAgentPath {
		return "", false
	}
	if err := p.Validate(); err != nil {
		return "", false
	}
	i := strings.LastIndexByte(string(p), '/')
	return AgentPath(string(p)[:i]), true
}

// Depth returns root=0, direct child=1.
func (p AgentPath) Depth() int {
	if p.Validate() != nil {
		return -1
	}
	return strings.Count(strings.TrimPrefix(string(p), "/root"), "/")
}

// AgentStatus is the event-derived lifecycle state of a subagent.
type AgentStatus string

const (
	AgentPendingInit AgentStatus = "pending_init"
	AgentQueued      AgentStatus = "queued"
	AgentRunning     AgentStatus = "running"
	AgentInterrupted AgentStatus = "interrupted"
	AgentCompleted   AgentStatus = "completed"
	AgentErrored     AgentStatus = "errored"
	AgentShutdown    AgentStatus = "shutdown"
	AgentNotLoaded   AgentStatus = "not_loaded"
	AgentNotFound    AgentStatus = "not_found"
)

func (s AgentStatus) Valid() bool {
	switch s {
	case AgentPendingInit, AgentQueued, AgentRunning, AgentInterrupted, AgentCompleted, AgentErrored, AgentShutdown, AgentNotLoaded, AgentNotFound:
		return true
	}
	return false
}

func (s AgentStatus) Terminal() bool {
	switch s {
	case AgentCompleted, AgentInterrupted, AgentErrored, AgentShutdown:
		return true
	}
	return false
}

// AgentRef identifies one node in a subagent tree.
type AgentRef struct {
	ThreadID       string    `json:"thread_id"`
	ParentThreadID string    `json:"parent_thread_id,omitempty"`
	Path           AgentPath `json:"path"`
	ParentPath     AgentPath `json:"parent_path,omitempty"`
	Role           string    `json:"role,omitempty"`
	Nickname       string    `json:"nickname,omitempty"`
	Depth          int       `json:"depth"`
}

func (r AgentRef) Validate() error {
	if strings.TrimSpace(r.ThreadID) == "" || len(r.ThreadID) > 128 {
		return errors.New("agent thread id is invalid")
	}
	if len(r.ParentThreadID) > 128 || len(r.Role) > 64 || len(r.Nickname) > MaxAgentMetadataBytes {
		return errors.New("agent reference metadata is too large")
	}
	if err := r.Path.Validate(); err != nil {
		return err
	}
	if r.Depth != r.Path.Depth() {
		return errors.New("agent depth does not match path")
	}
	if r.Path != RootAgentPath {
		parent, _ := r.Path.Parent()
		if r.ParentPath != parent || strings.TrimSpace(r.ParentThreadID) == "" {
			return errors.New("agent parent does not match path")
		}
	}
	return nil
}

func (r *AgentRef) Clone() *AgentRef {
	if r == nil {
		return nil
	}
	out := *r
	return &out
}

// SubagentState is a bounded immutable snapshot returned by manager surfaces.
type SubagentState struct {
	Agent      AgentRef      `json:"agent"`
	Status     AgentStatus   `json:"status"`
	Model      string        `json:"model,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	Thinking   ThinkingLevel `json:"thinking,omitempty"`
	CreatedAt  int64         `json:"created_at"`
	StartedAt  int64         `json:"started_at,omitempty"`
	FinishedAt int64         `json:"finished_at,omitempty"`
	Result     string        `json:"result,omitempty"`
	Error      string        `json:"error,omitempty"`
	Usage      *Usage        `json:"usage,omitempty"`
	Generation uint64        `json:"generation,omitempty"`
}

func (s SubagentState) Validate() error {
	if err := s.Agent.Validate(); err != nil {
		return err
	}
	if !s.Status.Valid() || s.Status == AgentNotFound {
		return fmt.Errorf("invalid persisted agent status %q", s.Status)
	}
	if len(s.Model) > MaxAgentMetadataBytes || len(s.Provider) > MaxAgentMetadataBytes {
		return errors.New("agent model metadata is too large")
	}
	if _, err := ParseThinkingLevel(string(s.Thinking)); err != nil {
		return err
	}
	if len(s.Result) > MaxAgentMessageBytes || len(s.Error) > MaxAgentMessageBytes {
		return errors.New("agent state text is too large")
	}
	return nil
}

func (s *SubagentState) Clone() *SubagentState {
	if s == nil {
		return nil
	}
	out := *s
	out.Usage = s.Usage.Clone()
	return &out
}

// AgentMessageKind distinguishes task, ordinary mail, and completion mail.
type AgentMessageKind string

const (
	AgentMessageNewTask AgentMessageKind = "new_task"
	AgentMessageNormal  AgentMessageKind = "message"
	AgentMessageFinal   AgentMessageKind = "final_answer"
)

// AgentMessage is an attributed, ordered mailbox envelope.
type AgentMessage struct {
	ID          string           `json:"id"`
	Author      AgentPath        `json:"author"`
	Recipient   AgentPath        `json:"recipient"`
	Kind        AgentMessageKind `json:"kind"`
	Content     string           `json:"content"`
	TriggerTurn bool             `json:"trigger_turn,omitempty"`
	CreatedAt   int64            `json:"created_at"`
}

func (m AgentMessage) Validate() error {
	if strings.TrimSpace(m.ID) == "" || len(m.ID) > 128 {
		return errors.New("agent message id is invalid")
	}
	if err := m.Author.Validate(); err != nil {
		return fmt.Errorf("author: %w", err)
	}
	if err := m.Recipient.Validate(); err != nil {
		return fmt.Errorf("recipient: %w", err)
	}
	switch m.Kind {
	case AgentMessageNewTask, AgentMessageNormal, AgentMessageFinal:
	default:
		return fmt.Errorf("invalid agent message kind %q", m.Kind)
	}
	if strings.TrimSpace(m.Content) == "" {
		return errors.New("agent message is empty")
	}
	if len(m.Content) > MaxAgentMessageBytes {
		return errors.New("agent message is too large")
	}
	return nil
}

func (m *AgentMessage) Clone() *AgentMessage {
	if m == nil {
		return nil
	}
	out := *m
	return &out
}

// SpawnSubagentRequest is shared by SDK/RPC and manager-bound model tools.
type SpawnSubagentRequest struct {
	Name            string        `json:"name"`
	Task            string        `json:"task"`
	Role            string        `json:"role,omitempty"`
	ForkTurns       string        `json:"fork_turns,omitempty"`
	Provider        string        `json:"provider,omitempty"`
	Model           string        `json:"model,omitempty"`
	ReasoningEffort ThinkingLevel `json:"reasoning_effort,omitempty"`
}

type WaitSubagentsResult struct {
	Message     string `json:"message"`
	TimedOut    bool   `json:"timed_out"`
	Clamped     bool   `json:"clamped,omitempty"`
	Running     int    `json:"running"`
	Queued      int    `json:"queued"`
	Terminal    int    `json:"terminal"`
	AllTerminal bool   `json:"all_terminal"`
}

type SubagentList struct {
	Agents          []SubagentState `json:"agents"`
	Running         int             `json:"running"`
	Queued          int             `json:"queued"`
	Terminal        int             `json:"terminal"`
	ConcurrentLimit int             `json:"concurrent_limit"`
	AgentLimit      int             `json:"agent_limit"`
	Truncated       bool            `json:"truncated,omitempty"`
}

// SealedText renders a compatibility-safe attributed envelope for providers.
func (m AgentMessage) SealedText() string {
	return fmt.Sprintf("<snow_agent_message>\nMessage Type: %s\nTask name: %s\nSender: %s\nPayload:\n%s\n</snow_agent_message>", strings.ToUpper(string(m.Kind)), m.Recipient, m.Author, m.Content)
}
