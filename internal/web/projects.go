package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Catalog is the browser surface's read-only backend contract. It owns neither
// runtimes nor session stores. Implementations may use another transport.
type Catalog interface {
	Sessions(context.Context, Project, int) (CatalogSessions, error)
	Messages(context.Context, Project, string, int) (CatalogMessages, error)
}

type SessionSummary struct {
	ID, Name string
	Updated  string
}

type CatalogSessions struct {
	Sessions   []SessionSummary
	NextOffset int
	HasMore    bool
}

type HistoryMessage struct {
	Images    []MessageImage            `json:"images,omitempty"`
	ID        string                    `json:"id"`
	Role      string                    `json:"role"`
	Text      string                    `json:"text"`
	Truncated bool                      `json:"truncated"`
	Tools     []protocol.RPCHistoryTool `json:"tools,omitempty"`
}

type CatalogMessages struct {
	Messages       []HistoryMessage
	NextOffset     int
	HasMore        bool
	ToolsTruncated bool
}

func (s *shell) addProject(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authorizeForm(w, r)
	if !ok {
		return
	}
	if s.registry == nil {
		http.Error(w, "Project registry is unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	project, err := s.registry.Add(ctx, r.PostForm.Get("name"), r.PostForm.Get("path"))
	if err != nil {
		// Keep POST-only routes and user-supplied paths out of browser history.
		http.Redirect(w, r, "/?view=projects&notice=register_failed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?view=projects&project="+url.QueryEscape(project.ID)+"&new=1", http.StatusSeeOther)
}

func (s *shell) removeProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	if !s.projectControl.TryLock() {
		http.Error(w, "Project activation or removal is busy", http.StatusConflict)
		return
	}
	defer s.projectControl.Unlock()
	if s.registry == nil {
		http.Error(w, "Project registry is unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.runtimes != nil {
		if _, live := s.runtimes.Snapshot(r.PathValue("project")); live {
			http.Error(w, "Close the live session before removing this project", http.StatusConflict)
			return
		}
	}
	if r.PostForm.Get("confirm") != "remove" {
		http.Error(w, "Confirm removal from the manager; project files and sessions are retained", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.registry.Remove(ctx, r.PathValue("project")); err != nil {
		http.Error(w, "Could not remove this registration", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/?view=projects", http.StatusSeeOther)
}

func pageOffset(query url.Values) (int, error) {
	for _, key := range []string{"project", "session", "offset", "new", "last_session"} {
		if len(query[key]) > 1 {
			return 0, errors.New("duplicate catalog parameter")
		}
	}
	if len(query.Get("project")) > 128 || len(query.Get("session")) > 128 {
		return 0, errors.New("invalid catalog identity")
	}
	if values, present := query["new"]; present && (len(values) != 1 || values[0] != "1" || query.Get("session") != "" || query.Get("offset") != "") {
		return 0, errors.New("invalid new session navigation")
	}
	if values, present := query["last_session"]; present && (len(values) != 1 || !runtimeIdentifier(values[0])) {
		return 0, errors.New("invalid last session navigation")
	}
	value := query.Get("offset")
	if value == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 || offset > 10_000 {
		return 0, errors.New("invalid catalog page offset")
	}
	return offset, nil
}

func (s *shell) projectData(ctx context.Context, query url.Values, data *pageData) error {
	if s.registry == nil {
		return nil
	}
	data.RegistryEnabled = true
	data.RuntimeEnabled = s.runtimes != nil
	_, data.TurnCancelEnabled = s.runtimes.(RuntimeTurnCancelBackend)
	_, data.WorkflowEnabled = s.runtimes.(RuntimeWorkflowBackend)
	_, data.MessageEditEnabled = s.runtimes.(RuntimeMessageEditBackend)
	_, data.MessageRegenerateEnabled = s.runtimes.(RuntimeMessageRegenerateBackend)
	_, data.QueueNextEnabled = s.runtimes.(RuntimeQueueNextBackend)
	_, data.VersionsEnabled = s.runtimes.(RuntimeVersionsBackend)
	_, data.HistoryControlEnabled = s.runtimes.(RuntimeHistoryControlBackend)
	data.HistoryControlEnabled = data.HistoryControlEnabled && data.VersionsEnabled
	_, data.ReasoningEnabled = s.runtimes.(RuntimeReasoningBackend)
	_, data.CompactionEnabled = s.runtimes.(RuntimeCompactionBackend)
	data.ProjectOperationsEnabled = s.operations != nil
	_, data.ProcessControlEnabled = s.runtimes.(RuntimeProcessBackend)
	_, data.GoalsEnabled = s.runtimes.(RuntimeGoalBackend)
	_, data.PermissionPolicyEnabled = s.runtimes.(RuntimePermissionPolicyBackend)
	data.PermissionPolicyEnabled = data.PermissionPolicyEnabled && data.WorkflowEnabled
	projects, err := s.registry.List(ctx)
	if err != nil {
		return errors.New("Project registry could not be read")
	}
	data.Projects, err = s.registry.DecorateProjects(ctx, projects)
	if err != nil {
		return errors.New("Project organization could not be read")
	}
	if data.View == "projects" && query.Get("notice") == "register_failed" {
		data.Error = "Could not register this folder. Use an existing absolute directory path, a name of at most 128 bytes, and check for duplicate registrations or the 100-project limit."
	}
	if data.View != "projects" || query.Get("project") == "" {
		return nil
	}
	offset, err := pageOffset(query)
	if err != nil {
		return err
	}
	project, err := s.registry.Lookup(ctx, query.Get("project"))
	if err != nil {
		return errors.New("This project is no longer registered")
	}
	data.Project = &project
	data.NewSession = query.Get("new") == "1"
	if s.runtimes != nil {
		if snapshot, ok := s.runtimes.Snapshot(project.ID); ok {
			if requested := query.Get("session"); requested != "" && requested != snapshot.SessionID {
				data.SessionID = requested
				data.RuntimeEnabled = false
				return errors.New("Another session owns this project. Open the workspace without a saved-session link to review the current session. No session was switched")
			}
			data.NewSession = false // A GET never creates or switches a live session.
			snapshot = displaySnapshot(snapshot)
			data.Live = &snapshot
			data.SessionID = snapshot.SessionID
			return nil // A live session is read through its owner, never catalog SQLite.
		}
	}
	if !project.Available {
		return errors.New("The registered folder is missing or its identity changed. Restore the original folder, or remove and register it again explicitly")
	}
	if hint, found, err := s.registry.LoadRecovery(ctx, project.ID); err != nil {
		return errors.New("Recovery metadata could not be read. No runtime was activated")
	} else if found && (query.Get("session") == "" || query.Get("session") == hint.SessionID) {
		data.Recovery = &hint
		data.RecoveryURL = "/?" + url.Values{"view": {"projects"}, "project": {project.ID}, "session": {hint.SessionID}}.Encode()
	}
	if data.NewSession {
		return nil // Navigation intent only, including folders with saved history.
	}
	if s.catalog == nil {
		return errors.New("Read-only catalog is not available")
	}
	// Foreground history must not race its own background inventory for the
	// bounded catalog pool. Wait only for canceled I/O teardown, as activation
	// does; other projects remain independent and no read is retried.
	releaseReads, err := s.sidebarReads.preempt(ctx, project.ID)
	if err != nil {
		return errors.New("Session history is busy. Retry this read; no agent was started")
	}
	defer releaseReads()
	id := query.Get("session")
	if id == "" {
		// Catalog's first page is newest-first; never scan all workspace history.
		page, listErr := s.catalog.Sessions(ctx, project, 0)
		if listErr == nil {
			page = workspaceSessionSeed(page)
			data.Sessions = &page
		}
		// The tab's last viewed ID is only an advisory cold read. It cannot
		// override a live owner, explicit saved link, or new-session intent.
		last := query.Get("last_session")
		if last != "" {
			history, err := s.catalog.Messages(ctx, project, last, 0)
			if err == nil {
				setProjectHistory(data, project.ID, last, history)
				return nil
			}
		}
		if listErr != nil {
			return errors.New("Sessions are unavailable or the catalog is busy. Retry this read; no agent was started")
		}
		for _, session := range page.Sessions {
			if session.ID != last {
				id = session.ID
				break
			}
		}
		if id == "" {
			if last != "" {
				return errors.New("Session history is unavailable or the catalog is busy. Retry this read; no agent was started")
			}
			data.NewSession = true
			return nil
		}
		offset = 0 // A workspace link is not a transcript pagination request.
	}
	if id != "" {
		if !runtimeIdentifier(id) {
			return errors.New("Invalid saved session identity")
		}
		// Retain the explicit target even if read-only catalog access is
		// unavailable (for example, a crashed session requiring WAL recovery).
		// This is navigation only; activation independently verifies the ID.
		data.SessionID = id
		page, err := s.catalog.Messages(ctx, project, id, offset)
		if err != nil {
			return errors.New("Session history is unavailable or the catalog is busy. Retry this read; no agent was started")
		}
		setProjectHistory(data, project.ID, id, page)
		return nil
	}
	return nil
}

// workspaceSessionSeed is the same bounded, public metadata used to seed the
// selected cold branch. Explicit saved links do not require a second list read.
func workspaceSessionSeed(page CatalogSessions) CatalogSessions {
	result := CatalogSessions{HasMore: page.HasMore && page.NextOffset > 0 && page.NextOffset <= 10_000, NextOffset: page.NextOffset}
	seen := make(map[string]bool)
	for _, session := range page.Sessions[:min(len(page.Sessions), sidebarSessionPageSize)] {
		if !runtimeIdentifier(session.ID) || seen[session.ID] {
			continue
		}
		seen[session.ID] = true
		result.Sessions = append(result.Sessions, SessionSummary{ID: session.ID, Name: runtimeText(session.Name, 256), Updated: runtimeText(session.Updated, 64)})
	}
	if !result.HasMore {
		result.NextOffset = 0
	}
	return result
}

func setProjectHistory(data *pageData, projectID, id string, page CatalogMessages) {
	page = displayCatalogImages(projectID, id, page)
	data.SessionID, data.History = id, &page
	if page.HasMore {
		data.NextURL = "/?" + url.Values{"view": {"projects"}, "project": {projectID}, "session": {id}, "offset": {strconv.Itoa(page.NextOffset)}}.Encode()
	}
}
