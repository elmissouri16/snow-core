package rpc

import (
	"slices"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// handleMCPServers emits secret-free negotiated MCP server status.
func (s *Server) handleMCPServers(req Request) error {
	statuses := s.app.MCPStatuses
	if s.app.MCPManager != nil {
		statuses = s.app.MCPManager.Statuses()
	}
	servers := make([]protocol.RPCMCPServer, 0, len(statuses))
	for _, st := range statuses {
		servers = append(servers, protocol.RPCMCPServer{
			ID: st.ID, Transport: st.Transport, Connected: st.Connected,
			ProtocolVersion: st.ProtocolVersion, ServerName: st.ServerName, ServerVersion: st.ServerVersion,
			Capabilities: slices.Clone(st.Capabilities), ToolCount: st.ToolCount, Message: st.Message,
			State: st.State, Cached: st.Cached, CachedAt: st.CachedAt, LastUsedAt: st.LastUsedAt,
		})
	}
	s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCMCPServersList{Servers: servers}})
	return nil
}

// handleSkills emits the full skill catalog plus discovery diagnostics.
func (s *Server) handleSkills(req Request) error {
	skills, err := s.app.SkillInventoryPublic()
	if err != nil {
		return err
	}
	s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: skills})
	return nil
}
