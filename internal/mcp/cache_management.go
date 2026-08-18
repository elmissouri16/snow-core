package mcp

import (
	"fmt"
	"sort"
	"time"

	publicmcp "github.com/snow-core/snow/pkg/mcp"
)

// CacheStatuses inspects configured declarations without creating transports.
// Returned metadata is bounded and excludes cache keys, fingerprints, roots,
// arguments, credentials, server instructions, and resource/prompt contents.
func (m *Manager) CacheStatuses(declarations []Declaration) []publicmcp.CacheStatus {
	statuses := make([]publicmcp.CacheStatus, 0, len(declarations))
	if m.cache == nil {
		for _, decl := range declarations {
			statuses = append(statuses, publicmcp.CacheStatus{ID: decl.Spec.ID, Scope: decl.Scope, State: publicmcp.CacheStateDisabled, Message: "MCP cache is disabled"})
		}
		return statuses
	}
	entries, readErr := m.cache.snapshot()
	now := m.now()
	for _, decl := range declarations {
		status := publicmcp.CacheStatus{ID: decl.Spec.ID, Scope: decl.Scope, State: publicmcp.CacheStateMissing}
		if readErr != nil {
			status.State, status.Message = publicmcp.CacheStateError, "MCP cache could not be read"
			statuses = append(statuses, status)
			continue
		}
		key, projectHash, fingerprint := cacheIdentity(decl, m.opts.Roots)
		if entry, ok := entries[key]; ok {
			status.CachedAt = entry.CachedAt
			status.ExpiresAt = entry.CachedAt.Add(defaultCacheAge)
			if now.Before(entry.CachedAt.Add(-defaultClockSkew)) || now.Sub(entry.CachedAt) > defaultCacheAge {
				status.State, status.Message = publicmcp.CacheStateExpired, "cached catalog is expired"
			} else if entry.ServerID != decl.Spec.ID || entry.Scope != decl.Scope || entry.ProjectIdentityHash != projectHash || entry.ConfigurationFingerprint != fingerprint {
				status.State, status.Message = publicmcp.CacheStateMismatch, "cached catalog does not match the active declaration"
			} else {
				status.State, status.Valid = publicmcp.CacheStateValid, true
				status.ProtocolVersion, status.ServerName, status.ServerVersion = entry.ProtocolVersion, entry.ServerName, entry.ServerVersion
				status.Capabilities = append([]string(nil), entry.Capabilities...)
				status.ToolCount = len(entry.Tools)
			}
			statuses = append(statuses, status)
			continue
		}
		for _, entry := range entries {
			if entry.ServerID == decl.Spec.ID && entry.Scope == decl.Scope && entry.ProjectIdentityHash == projectHash {
				status.State, status.Message = publicmcp.CacheStateMismatch, "cached catalog does not match the active declaration"
				break
			}
		}
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].ID == statuses[j].ID {
			return statuses[i].Scope < statuses[j].Scope
		}
		return statuses[i].ID < statuses[j].ID
	})
	return statuses
}

// ClearCache removes cached catalogs for the declaration's server, scope, and
// canonical project identity, including entries from superseded fingerprints.
func (m *Manager) ClearCache(decl Declaration) (int, error) {
	if m.cache == nil {
		return 0, fmt.Errorf("MCP cache is disabled")
	}
	_, projectHash, _ := cacheIdentity(decl, m.opts.Roots)
	return m.cache.remove(decl.Spec.ID, decl.Scope, projectHash, m.now())
}

// defaultClockSkew is the maximum tolerated future cache timestamp. It is kept
// separate from expiry so cache inspection and startup apply the same rule.
const defaultClockSkew = 5 * time.Minute
