package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// registerOrganizationRoutes is called by the shell's method-aware mux. These
// POSTs are manager-only metadata; none has access to an activation capability.
func (s *shell) registerOrganizationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /projects/{project}/organization/{action}", s.organizeProject)
	// Concrete metadata actions avoid an ambiguous wildcard intersection with
	// the immutable-ID session deletion route.
	for _, action := range []string{"pin", "unpin", "archive", "restore"} {
		mux.HandleFunc("POST /projects/{project}/sessions/organization/"+action, func(w http.ResponseWriter, r *http.Request) {
			r.SetPathValue("action", action)
			s.organizeSession(w, r)
		})
	}
}

// organizationForm gives every field a single body-only source. In particular
// duplicate csrf values must not inherit authorizeForm's first-value behavior.
func (s *shell) organizationForm(w http.ResponseWriter, r *http.Request, fields ...string) bool {
	if _, ok := s.authorizeFormLimit(w, r, 4096); !ok {
		return false
	}
	allowed := map[string]bool{"csrf": true}
	for _, key := range fields {
		allowed[key] = true
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Organization actions require body-only fields", http.StatusBadRequest)
		return false
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 || len(values[0]) > 512 {
			http.Error(w, "Invalid or duplicate organization field", http.StatusBadRequest)
			return false
		}
	}
	for key := range allowed {
		if len(r.PostForm[key]) != 1 {
			http.Error(w, "Missing organization field", http.StatusBadRequest)
			return false
		}
	}
	if !validProjectID(r.PathValue("project")) {
		http.NotFound(w, r)
		return false
	}
	if s.registry == nil {
		http.Error(w, "Project registry is unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *shell) organizeProject(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	var fields []string
	switch action {
	case "rename":
		fields = []string{"name"}
	case "pin", "unpin":
	case "archive", "restore":
		fields = []string{"confirm"}
	default:
		http.NotFound(w, r)
		return
	}
	if !s.organizationForm(w, r, fields...) {
		return
	}
	if (action == "archive" || action == "restore") && r.PostForm.Get("confirm") != action {
		http.Error(w, "Explicit archive or restore confirmation required", http.StatusBadRequest)
		return
	}
	// Serialize with activation/removal even for labels, keeping one simple
	// admission boundary. Only operations which hide work reject a live target.
	if !s.projectControl.TryLock() {
		http.Error(w, "Project control is busy", http.StatusConflict)
		return
	}
	defer s.projectControl.Unlock()
	id := r.PathValue("project")
	if action == "archive" && s.organizationLive(id) {
		http.Error(w, "Close the live session before archiving this project", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var err error
	switch action {
	case "rename":
		err = s.registry.RenameProject(ctx, id, r.PostForm.Get("name"))
	case "pin", "unpin":
		err = s.registry.PinProject(ctx, id, action == "pin")
	case "archive":
		err = s.registry.Remove(ctx, id)
	case "restore":
		_, err = s.registry.RestoreProject(ctx, id)
	}
	if err != nil {
		http.Error(w, "Could not update project organization. Restore requires the original folder identity, no active duplicate, and space within the 100-project limit.", http.StatusConflict)
		return
	}
	location := "/?view=organization"
	if action != "archive" {
		location += "&project=" + url.QueryEscape(id)
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (s *shell) organizationLive(id string) bool {
	if s.runtimes == nil {
		return false
	}
	_, live := s.runtimes.Snapshot(id)
	return live
}

func (s *shell) organizeSession(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case "pin", "unpin", "archive", "restore":
	default:
		http.NotFound(w, r)
		return
	}
	fields := []string{"session_id", "offset"}
	if action == "archive" || action == "restore" {
		fields = append(fields, "confirm")
	}
	if !s.organizationForm(w, r, fields...) {
		return
	}
	if (action == "archive" || action == "restore") && r.PostForm.Get("confirm") != action {
		http.Error(w, "Explicit archive or restore confirmation required", http.StatusBadRequest)
		return
	}
	id, sessionID := r.PathValue("project"), r.PostForm.Get("session_id")
	offset, err := organizationOffset(r.PostForm.Get("offset"))
	if err != nil || sessionID == "" || !runtimeIdentifier(sessionID) {
		http.Error(w, "Invalid saved session identity or page", http.StatusBadRequest)
		return
	}
	if !s.projectControl.TryLock() {
		http.Error(w, "Project control is busy", http.StatusConflict)
		return
	}
	defer s.projectControl.Unlock()
	// The inactive catalog cannot reliably prove membership while a worker owns
	// this project. Fail closed for every session operation until it is closed.
	if s.organizationLive(id) {
		http.Error(w, "Close the live session before organizing saved sessions", http.StatusConflict)
		return
	}
	if s.catalog == nil {
		http.Error(w, "Read-only catalog is unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	project, err := s.registry.Lookup(ctx, id)
	if err != nil || !project.Available {
		http.Error(w, "The registered folder is missing or changed", http.StatusConflict)
		return
	}
	page, err := s.catalog.Sessions(ctx, project, offset)
	if err != nil {
		http.Error(w, "Reload this saved-session page; the catalog is unavailable", http.StatusConflict)
		return
	}
	// Revalidate after the catalog read; stale or substituted roots cannot stamp
	// metadata onto a newly registered directory or another durable ID.
	current, err := s.registry.Lookup(ctx, id)
	if err != nil || !current.Available || current.Path != project.Path || current.device != project.device || current.inode != project.inode {
		http.Error(w, "Project identity changed", http.StatusConflict)
		return
	}
	if err := s.registry.saveSessionOrganization(ctx, id, sessionID, action, page); err != nil {
		http.Error(w, "Session is not on this loaded catalog page, or manager metadata is full. Reload before retrying.", http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/?"+url.Values{"view": {"organization"}, "project": {id}, "offset": {strconv.Itoa(offset)}}.Encode(), http.StatusSeeOther)
}

func organizationOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > MaxOrganizationOffset || strconv.Itoa(n) != raw {
		return 0, errors.New("Invalid organization page offset")
	}
	return n, nil
}

// OrganizationView is separate from the original catalog projection: manager
// filters never discard its identity, HasMore flag, or navigation offsets.
type OrganizationView struct {
	CSRF                     string
	Projects                 []Project
	Archived                 ArchivedProjects
	Project                  *Project
	Sessions                 []OrganizedSession
	Offset                   int
	NextURL, ArchivedNextURL string
	Live                     bool
}

func (s *shell) organizationData(ctx context.Context, query url.Values, csrf string) (*OrganizationView, error) {
	data := &OrganizationView{CSRF: csrf}
	for _, key := range []string{"project", "offset", "archived_offset"} {
		if len(query[key]) > 1 {
			return data, errors.New("Duplicate organization parameter")
		}
	}
	offset, err := organizationOffset(query.Get("offset"))
	if err != nil {
		return data, err
	}
	data.Offset = offset
	archivedOffset, err := organizationOffset(query.Get("archived_offset"))
	if err != nil {
		return data, err
	}
	if s.registry == nil {
		return data, errors.New("Project registry is unavailable")
	}
	projects, err := s.registry.List(ctx)
	if err != nil {
		return data, errors.New("Project registry could not be read")
	}
	data.Projects, err = s.registry.DecorateProjects(ctx, projects)
	if err != nil {
		return data, errors.New("Project organization could not be read")
	}
	slices.SortStableFunc(data.Projects, func(a, b Project) int {
		if a.Pinned == b.Pinned {
			return 0
		}
		if a.Pinned {
			return -1
		}
		return 1
	})
	data.Archived, err = s.registry.ListArchivedProjects(ctx, archivedOffset)
	if err != nil {
		return data, errors.New("Archived projects could not be read")
	}
	if data.Archived.HasMore {
		data.ArchivedNextURL = "/?" + url.Values{"view": {"organization"}, "archived_offset": {strconv.Itoa(data.Archived.NextOffset)}}.Encode()
	}
	if query.Get("project") == "" {
		return data, nil
	}
	project, err := s.registry.Lookup(ctx, query.Get("project"))
	if err != nil {
		return data, errors.New("Select an active project; archived projects require explicit restore")
	}
	decorated, err := s.registry.DecorateProjects(ctx, []Project{project})
	if err != nil {
		return data, errors.New("Project organization could not be read")
	}
	data.Project = &decorated[0]
	if s.organizationLive(project.ID) {
		data.Live = true
		return data, nil
	}
	if !project.Available {
		return data, errors.New("The registered folder is missing or changed; no session catalog was opened")
	}
	if s.catalog == nil {
		return data, errors.New("Read-only session catalog is unavailable")
	}
	page, err := s.catalog.Sessions(ctx, project, offset)
	if err != nil {
		return data, errors.New("Sessions are unavailable; no agent was activated")
	}
	data.Sessions, err = s.registry.DecorateSessions(ctx, project.ID, page)
	if err != nil {
		return data, errors.New("Session organization could not be read")
	}
	slices.SortStableFunc(data.Sessions, func(a, b OrganizedSession) int {
		if a.Pinned == b.Pinned {
			return 0
		}
		if a.Pinned {
			return -1
		}
		return 1
	})
	if page.HasMore && page.NextOffset > offset && page.NextOffset <= MaxOrganizationOffset {
		data.NextURL = "/?" + url.Values{"view": {"organization"}, "project": {project.ID}, "offset": {strconv.Itoa(page.NextOffset)}}.Encode()
	}
	return data, nil
}
