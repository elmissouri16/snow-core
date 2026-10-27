package web

import (
	"context"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeSubagentsBackend reads the activated worker's subagent fleet, as the
// TUI's /agent inspector does. It is read-only: the browser never spawns,
// messages, interrupts, or closes a child agent.
type RuntimeSubagentsBackend interface {
	Subagents(ctx context.Context, projectID, instanceID string) (RuntimeSubagents, error)
}

type RuntimeSubagents struct {
	ProjectID       string            `json:"project_id"`
	InstanceID      string            `json:"instance_id"`
	SessionID       string            `json:"session_id"`
	Agents          []RuntimeSubagent `json:"agents"`
	Running         int               `json:"running"`
	Queued          int               `json:"queued"`
	Terminal        int               `json:"terminal"`
	Open            int               `json:"open"`
	ConcurrentLimit int               `json:"concurrent_limit"`
	AgentLimit      int               `json:"agent_limit"`
	Truncated       bool              `json:"truncated"`
}

type RuntimeSubagent struct {
	Path       string `json:"path"`
	ParentPath string `json:"parent_path"`
	Role       string `json:"role"`
	Nickname   string `json:"nickname"`
	Depth      int    `json:"depth"`
	Status     string `json:"status"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Thinking   string `json:"thinking"`
	CreatedAt  int64  `json:"created_at"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	Result     string `json:"result"`
	Error      string `json:"error"`
}

const (
	runtimeSubagentsLimit     = 128
	runtimeSubagentResultByte = 2048
)

func (m *RuntimeManager) Subagents(ctx context.Context, projectID, instanceID string) (RuntimeSubagents, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeSubagents{}, err
	}
	defer r.control.Unlock()
	if !r.supports("subagents") {
		return RuntimeSubagents{}, ErrRuntimeInvalid
	}
	// Unlike other inspectors this read is allowed while a turn runs: that is
	// exactly when child agents exist. It only lists; it admits nothing.
	var list protocol.SubagentList
	if err := r.call(protocol.RPCRequest{Type: "subagent_list"}, nil, &list); err != nil {
		return RuntimeSubagents{}, err
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeSubagents{}, err
	}
	result := projectRuntimeSubagents(list)
	r.mu.Lock()
	result.ProjectID, result.InstanceID, result.SessionID = projectID, instanceID, r.snapshot.SessionID
	r.mu.Unlock()
	return result, nil
}

func projectRuntimeSubagents(list protocol.SubagentList) RuntimeSubagents {
	result := RuntimeSubagents{
		Agents: []RuntimeSubagent{}, Running: max(list.Running, 0), Queued: max(list.Queued, 0), Terminal: max(list.Terminal, 0), Open: max(list.Open, 0),
		ConcurrentLimit: max(list.ConcurrentLimit, 0), AgentLimit: max(list.AgentLimit, 0), Truncated: list.Truncated || len(list.Agents) > runtimeSubagentsLimit,
	}
	for _, agent := range list.Agents[:min(len(list.Agents), runtimeSubagentsLimit)] {
		result.Agents = append(result.Agents, RuntimeSubagent{
			Path: runtimeText(string(agent.Agent.Path), 512), ParentPath: runtimeText(string(agent.Agent.ParentPath), 512),
			Role: runtimeText(agent.Agent.Role, 128), Nickname: runtimeText(agent.Agent.Nickname, 128), Depth: max(agent.Agent.Depth, 0),
			Status: runtimeText(string(agent.Status), 32), Provider: runtimeText(agent.Provider, 128), Model: runtimeText(agent.Model, 256),
			Thinking: runtimeText(string(agent.Thinking), 32), CreatedAt: agent.CreatedAt, StartedAt: agent.StartedAt, FinishedAt: agent.FinishedAt,
			Result: runtimeText(agent.Result, runtimeSubagentResultByte), Error: runtimeText(agent.Error, runtimeSubagentResultByte),
		})
	}
	return result
}

var _ RuntimeSubagentsBackend = (*RuntimeManager)(nil)
