package rpc

import (
	"github.com/snow-core/snow/pkg/protocol"
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
			Capabilities: append([]string(nil), st.Capabilities...), ToolCount: st.ToolCount, Message: st.Message,
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

// handleSandboxStatus emits the secret-free Bash execution boundary snapshot.
func (s *Server) handleSandboxStatus(req Request) error {
	status := s.app.SandboxStatus()
	s.write(Response{ID: req.ID, Type: "response", Command: req.Type, Success: true, Data: protocol.RPCSandboxStatusResponse{Status: protocol.RPCSandboxStatus{
		Configured: status.Configured, Active: status.Active, Backend: status.Backend, Machine: status.Machine,
		Profile: status.Profile, GuestCWD: status.GuestCWD, ReadOnly: status.ReadOnly, Network: status.Network,
		CPUs: status.CPUs, MemoryMiB: status.MemoryMiB, StorageGiB: status.StorageGiB, OverlayGiB: status.OverlayGiB,
	}}})
	return nil
}
