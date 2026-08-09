// Package mcp defines Snow's dependency-light public MCP server configuration.
// The protocol implementation lives under internal/mcp and uses the official
// Model Context Protocol Go SDK.
package mcp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "streamable-http"
)

var identifierRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ServerSpec declares one MCP server. Stdio servers use Command plus Args;
// Streamable HTTP servers use URL. Headers and Env values may reference an
// environment variable as ${NAME}; Snow resolves it at connection time.
type ServerSpec struct {
	ID        string            `json:"id,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Disabled  bool              `json:"disabled,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
	// ToolDiscovery is "deferred" (default) or "always".
	ToolDiscovery string `json:"tool_discovery,omitempty"`
}

// Validate checks the transport declaration without starting the server.
func (s ServerSpec) Validate() error {
	if !identifierRE.MatchString(s.ID) {
		return fmt.Errorf("mcp: invalid server id %q (want lowercase [a-z0-9][a-z0-9_-]{0,63})", s.ID)
	}
	transport := s.Transport
	if transport == "" {
		if s.URL != "" {
			transport = TransportStreamableHTTP
		} else {
			transport = TransportStdio
		}
	}
	switch transport {
	case TransportStdio:
		if strings.TrimSpace(s.Command) == "" {
			return errors.New("mcp: stdio server command is required")
		}
		if s.URL != "" {
			return errors.New("mcp: stdio server cannot also declare url")
		}
	case TransportStreamableHTTP, "http":
		if strings.TrimSpace(s.URL) == "" {
			return errors.New("mcp: Streamable HTTP server url is required")
		}
		if s.Command != "" || len(s.Args) > 0 {
			return errors.New("mcp: Streamable HTTP server cannot also declare command")
		}
	default:
		return fmt.Errorf("mcp: unsupported transport %q", s.Transport)
	}
	if s.TimeoutMS < 0 {
		return errors.New("mcp: timeout_ms cannot be negative")
	}
	if s.ToolDiscovery != "" && s.ToolDiscovery != "deferred" && s.ToolDiscovery != "always" {
		return fmt.Errorf("mcp: invalid tool_discovery %q", s.ToolDiscovery)
	}
	return nil
}

// EffectiveTransport returns the normalized transport name.
func (s ServerSpec) EffectiveTransport() string {
	if s.Transport == "http" {
		return TransportStreamableHTTP
	}
	if s.Transport != "" {
		return s.Transport
	}
	if s.URL != "" {
		return TransportStreamableHTTP
	}
	return TransportStdio
}

// Status is a secret-free snapshot of a configured MCP connection.
type Status struct {
	ID              string   `json:"id"`
	Transport       string   `json:"transport"`
	Connected       bool     `json:"connected"`
	ProtocolVersion string   `json:"protocol_version,omitempty"`
	ServerName      string   `json:"server_name,omitempty"`
	ServerVersion   string   `json:"server_version,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	ToolCount       int      `json:"tool_count,omitempty"`
	Message         string   `json:"message,omitempty"`
}
