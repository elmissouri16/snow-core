// Package mcp defines Snow's dependency-light public MCP server configuration.
// The protocol implementation lives under internal/mcp and uses the official
// Model Context Protocol Go SDK.
package mcp

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "streamable-http"

	LifecycleEager         = "eager"
	LifecycleLazy          = "lazy"
	LifecycleLazyKeepAlive = "lazy-keep-alive"

	CacheBootstrapAuto     = "auto"
	CacheBootstrapExplicit = "explicit"
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
	Disabled  bool              `json:"disabled,omitzero"`
	TimeoutMS int               `json:"timeout_ms,omitzero"`
	// ToolDiscovery is "deferred" (default) or "always".
	ToolDiscovery string `json:"tool_discovery,omitempty"`
	// Lifecycle is "eager" (default), "lazy", or "lazy-keep-alive".
	Lifecycle string `json:"lifecycle,omitempty"`
	// CacheBootstrap is "auto" (default) or "explicit". Explicit requires a
	// lazy lifecycle and never performs startup transport work on a cache miss.
	CacheBootstrap string `json:"cache_bootstrap,omitempty"`
	// IdleTimeoutMS overrides the ten-minute lazy idle timeout. Zero uses the
	// default; negative values are invalid.
	IdleTimeoutMS int `json:"idle_timeout_ms,omitzero"`
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
	if int64(s.TimeoutMS) > int64((time.Duration(1<<63-1))/time.Millisecond) {
		return errors.New("mcp: timeout_ms exceeds the maximum duration")
	}
	if s.ToolDiscovery != "" && s.ToolDiscovery != "deferred" && s.ToolDiscovery != "always" {
		return fmt.Errorf("mcp: invalid tool_discovery %q", s.ToolDiscovery)
	}
	if s.Lifecycle != "" && s.Lifecycle != LifecycleEager && s.Lifecycle != LifecycleLazy && s.Lifecycle != LifecycleLazyKeepAlive {
		return fmt.Errorf("mcp: invalid lifecycle %q", s.Lifecycle)
	}
	if s.CacheBootstrap != "" && s.CacheBootstrap != CacheBootstrapAuto && s.CacheBootstrap != CacheBootstrapExplicit {
		return fmt.Errorf("mcp: invalid cache_bootstrap %q", s.CacheBootstrap)
	}
	if s.CacheBootstrap == CacheBootstrapExplicit && s.Lifecycle != LifecycleLazy && s.Lifecycle != LifecycleLazyKeepAlive {
		return errors.New("mcp: cache_bootstrap explicit requires a lazy lifecycle")
	}
	if s.IdleTimeoutMS < 0 {
		return errors.New("mcp: idle_timeout_ms cannot be negative")
	}
	if int64(s.IdleTimeoutMS) > int64((time.Duration(1<<63-1))/time.Millisecond) {
		return errors.New("mcp: idle_timeout_ms exceeds the maximum duration")
	}
	if s.IdleTimeoutMS > 0 && s.Lifecycle != LifecycleLazy {
		return errors.New("mcp: idle_timeout_ms requires lifecycle lazy")
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

const (
	CacheStateValid    = "valid"
	CacheStateMissing  = "missing"
	CacheStateExpired  = "expired"
	CacheStateMismatch = "mismatch"
	CacheStateError    = "error"
	CacheStateDisabled = "disabled"
)

// CacheStatus is a secret-free snapshot of one configured server's metadata
// cache. It never exposes cache keys, fingerprints, roots, arguments, or server
// content.
type CacheStatus struct {
	ID              string    `json:"id"`
	Scope           string    `json:"scope,omitempty"`
	State           string    `json:"state"`
	Valid           bool      `json:"valid"`
	CachedAt        time.Time `json:"cached_at,omitzero"`
	ExpiresAt       time.Time `json:"expires_at,omitzero"`
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	ServerName      string    `json:"server_name,omitempty"`
	ServerVersion   string    `json:"server_version,omitempty"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	ToolCount       int       `json:"tool_count,omitzero"`
	Message         string    `json:"message,omitempty"`
}

// Status is a secret-free snapshot of a configured MCP connection.
type Status struct {
	ID              string    `json:"id"`
	Transport       string    `json:"transport"`
	Connected       bool      `json:"connected"`
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	ServerName      string    `json:"server_name,omitempty"`
	ServerVersion   string    `json:"server_version,omitempty"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	ToolCount       int       `json:"tool_count,omitzero"`
	Message         string    `json:"message,omitempty"`
	State           string    `json:"state,omitempty"`
	Cached          bool      `json:"cached,omitzero"`
	CachedAt        time.Time `json:"cached_at,omitzero"`
	LastUsedAt      time.Time `json:"last_used_at,omitzero"`
}
