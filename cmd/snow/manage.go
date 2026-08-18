package main

import (
	"github.com/snow-core/snow/internal/config"
	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

type mcpConfigView struct {
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	Scope          string            `json:"scope"`
	Transport      string            `json:"transport"`
	Target         string            `json:"target"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	URL            string            `json:"url,omitempty"`
	CWD            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	TimeoutMS      int               `json:"timeout_ms,omitempty"`
	ToolDiscovery  string            `json:"tool_discovery,omitempty"`
	Lifecycle      string            `json:"lifecycle,omitempty"`
	CacheBootstrap string            `json:"cache_bootstrap,omitempty"`
	IdleTimeoutMS  int               `json:"idle_timeout_ms,omitempty"`
	Shadowed       bool              `json:"shadowed,omitempty"`
	DisabledBy     string            `json:"disabled_by,omitempty"`
	spec           publicmcp.ServerSpec
}

type mcpConfigSet struct {
	Views           []mcpConfigView
	Effective       map[string]publicmcp.ServerSpec
	Scopes          map[string]string
	ProjectIdentity string
	ProjectBlocked  bool
	Config          config.Config
}

type commandReceipt struct {
	Resource string `json:"resource"`
	Name     string `json:"name,omitempty"`
	Action   string `json:"action"`
	Scope    string `json:"scope"`
	Path     string `json:"path"`
}
