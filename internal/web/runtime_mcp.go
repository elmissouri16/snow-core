package web

import (
	"context"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RuntimeMCPBackend reads the activated worker's negotiated MCP connections.
// It is a read-only status projection, like the TUI's /mcp: no server is
// started, reconnected, or configured from the browser.
type RuntimeMCPBackend interface {
	MCPServers(ctx context.Context, projectID, instanceID string) (RuntimeMCP, error)
}

type RuntimeMCP struct {
	ProjectID  string             `json:"project_id"`
	InstanceID string             `json:"instance_id"`
	SessionID  string             `json:"session_id"`
	Servers    []RuntimeMCPServer `json:"servers"`
	Limited    bool               `json:"limited"`
}

type RuntimeMCPServer struct {
	ID              string   `json:"id"`
	Transport       string   `json:"transport"`
	Connected       bool     `json:"connected"`
	State           string   `json:"state"`
	ServerName      string   `json:"server_name"`
	ServerVersion   string   `json:"server_version"`
	ProtocolVersion string   `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
	ToolCount       int      `json:"tool_count"`
	Message         string   `json:"message"`
	Cached          bool     `json:"cached"`
}

const runtimeMCPServersLimit = 64

func (m *RuntimeManager) MCPServers(ctx context.Context, projectID, instanceID string) (RuntimeMCP, error) {
	r, err := m.controlRuntime(ctx, projectID, instanceID)
	if err != nil {
		return RuntimeMCP{}, err
	}
	defer r.control.Unlock()
	if err := r.idle(); err != nil {
		return RuntimeMCP{}, err
	}
	if !r.supports("mcp_servers") {
		return RuntimeMCP{}, ErrRuntimeInvalid
	}
	var list protocol.RPCMCPServersList
	if err := r.call(protocol.RPCRequest{Type: "mcp_servers"}, nil, &list); err != nil {
		return RuntimeMCP{}, err
	}
	if err := m.revalidate(r, projectID, instanceID); err != nil {
		return RuntimeMCP{}, err
	}
	result := projectRuntimeMCP(list)
	r.mu.Lock()
	result.ProjectID, result.InstanceID, result.SessionID = projectID, instanceID, r.snapshot.SessionID
	r.mu.Unlock()
	return result, nil
}

func projectRuntimeMCP(list protocol.RPCMCPServersList) RuntimeMCP {
	result := RuntimeMCP{Servers: []RuntimeMCPServer{}, Limited: len(list.Servers) > runtimeMCPServersLimit}
	for _, server := range list.Servers[:min(len(list.Servers), runtimeMCPServersLimit)] {
		entry := RuntimeMCPServer{
			ID: runtimeText(server.ID, 256), Transport: runtimeText(server.Transport, 64), Connected: server.Connected,
			State: runtimeText(server.State, 64), ServerName: runtimeText(server.ServerName, 256), ServerVersion: runtimeText(server.ServerVersion, 64),
			ProtocolVersion: runtimeText(server.ProtocolVersion, 64), Capabilities: []string{}, ToolCount: max(server.ToolCount, 0),
			Message: runtimeText(server.Message, 1024), Cached: server.Cached,
		}
		for _, capability := range server.Capabilities[:min(len(server.Capabilities), 32)] {
			entry.Capabilities = append(entry.Capabilities, runtimeText(capability, 64))
		}
		result.Servers = append(result.Servers, entry)
	}
	return result
}

var _ RuntimeMCPBackend = (*RuntimeManager)(nil)
