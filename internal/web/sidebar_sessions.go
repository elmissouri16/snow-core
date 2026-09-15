package web

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const sidebarSessionPageSize = 25

// SidebarSession exposes only public saved-conversation metadata. UpdatedAt is
// Unix milliseconds, matching the runtime session inventory.
type SidebarSession struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updated_at"`
}

type SidebarSessions struct {
	ProjectID       string           `json:"project_id"`
	DeleteSupported bool             `json:"delete_supported"`
	ActiveSessionID string           `json:"active_session_id"`
	InstanceID      string           `json:"instance_id"`
	Sessions        []SidebarSession `json:"sessions"`
	Available       bool             `json:"available"`
	Truncated       bool             `json:"truncated"`
	HasMore         bool             `json:"has_more"`
	NextOffset      int              `json:"next_offset"`
}

// sidebarSessions is an authenticated, bounded read. It never activates a worker
// or asks workflow Choices to discover models or refresh provider telemetry.
func (s *shell) sidebarSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if len(r.URL.RawQuery) > 256 || !validProjectID(r.PathValue("project")) {
		http.Error(w, "Invalid session inventory request", http.StatusBadRequest)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "Invalid session inventory request", http.StatusBadRequest)
		return
	}
	for key := range query {
		if key != "offset" {
			http.Error(w, "Invalid session inventory request", http.StatusBadRequest)
			return
		}
	}
	offset, err := pageOffset(query)
	if err != nil {
		http.Error(w, "Invalid session inventory offset", http.StatusBadRequest)
		return
	}
	if s.registry == nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, read, err := s.sidebarReads.begin(ctx, project.ID)
	if err != nil {
		http.Error(w, "Session inventory is busy; retry this read", http.StatusConflict)
		return
	}
	defer read.cancel()
	defer read.finish()
	result := SidebarSessions{ProjectID: project.ID, Sessions: []SidebarSession{}}
	var snapshot RuntimeSnapshot
	live := false
	if s.runtimes != nil {
		snapshot, live = s.runtimes.Snapshot(project.ID)
	}
	if live {
		if snapshot.ProjectID != project.ID || !runtimeIdentifier(snapshot.InstanceID) {
			http.Error(w, "Session owner changed; reload this workspace", http.StatusConflict)
			return
		}
		result.InstanceID = snapshot.InstanceID
		if runtimeIdentifier(snapshot.SessionID) {
			result.ActiveSessionID = snapshot.SessionID
		}
	}
	if !project.Available {
		s.runtimeJSON(w, result)
		return
	}
	if live {
		backend, ok := s.runtimes.(RuntimeSessionInventoryBackend)
		if !ok {
			s.runtimeJSON(w, result) // Unsupported never falls back to catalog SQLite.
			return
		}
		inventory, err := backend.SessionInventory(ctx, project.ID, snapshot.InstanceID)
		read.finish() // The owner change may proceed once canceled I/O has stopped.
		if err != nil {
			http.Error(w, "Live session inventory is unavailable; retry this read", http.StatusConflict)
			return
		}
		current, ok := s.runtimes.Snapshot(project.ID)
		if !ok || current.ProjectID != project.ID || current.InstanceID != snapshot.InstanceID || inventory.ProjectID != project.ID || inventory.InstanceID != snapshot.InstanceID {
			http.Error(w, "Session owner changed; reload this workspace", http.StatusConflict)
			return
		}
		result.Available = inventory.Available
		result.Truncated = inventory.Truncated || len(inventory.Sessions) > 100
		sessions := inventory.Sessions[:min(len(inventory.Sessions), 100)]
		end := min(offset+sidebarSessionPageSize, len(sessions))
		for _, session := range sessions[min(offset, len(sessions)):end] {
			result.appendSession(session.SessionID, session.Name, session.UpdatedAt)
		}
		result.HasMore = end < len(sessions)
		if result.HasMore {
			result.NextOffset = end
		}
	} else if s.catalog != nil {
		page, err := s.catalog.Sessions(ctx, project, offset)
		read.finish() // Catalog teardown completes before a runtime may open.
		if err != nil {
			http.Error(w, "Saved session inventory is unavailable; retry this read", http.StatusServiceUnavailable)
			return
		}
		result.Available = true
		result.Truncated = len(page.Sessions) > sidebarSessionPageSize
		for _, session := range page.Sessions[:min(len(page.Sessions), sidebarSessionPageSize)] {
			updated, err := time.Parse(time.RFC3339, session.Updated)
			timestamp := int64(0)
			if err == nil {
				timestamp = updated.UnixMilli()
			}
			result.appendSession(session.ID, session.Name, timestamp)
		}
		result.HasMore = page.HasMore && page.NextOffset > offset && page.NextOffset <= 10_000
		if result.HasMore {
			result.NextOffset = page.NextOffset
		}
		result.Truncated = result.Truncated || page.HasMore && !result.HasMore
	}
	if ctx.Err() != nil {
		http.Error(w, "Session inventory changed; reload this workspace", http.StatusConflict)
		return
	}
	if !live && s.runtimes != nil {
		if _, owned := s.runtimes.Snapshot(project.ID); owned {
			http.Error(w, "Session owner changed; reload this workspace", http.StatusConflict)
			return
		}
	}
	// Registry and folder identity remain authoritative after the bounded read.
	current, err := s.registry.Lookup(ctx, project.ID)
	if err != nil || !current.Available {
		http.Error(w, "Workspace identity changed; reload this workspace", http.StatusConflict)
		return
	}
	if live {
		if backend, ok := s.runtimes.(RuntimeSessionDeleteBackend); ok {
			result.DeleteSupported = backend.SessionDeleteSupported(project.ID, snapshot.InstanceID)
		}
	} else {
		_, result.DeleteSupported = s.catalog.(CatalogSessionDeleteBackend)
	}
	s.runtimeJSON(w, result)
}

func (p *SidebarSessions) appendSession(id, name string, updated int64) {
	if !runtimeIdentifier(id) {
		p.Truncated = true
		return
	}
	for _, session := range p.Sessions {
		if session.SessionID == id {
			p.Truncated = true
			return
		}
	}
	title := runtimeText(name, 256)
	p.Truncated = p.Truncated || title != name
	p.Sessions = append(p.Sessions, SidebarSession{SessionID: id, Name: title, UpdatedAt: max(0, updated)})
}
