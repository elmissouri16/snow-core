package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// CatalogImageBackend is an optional runtime-free read contract. Browser input
// cannot choose a worker command, filesystem path, remote URL or active session.
type CatalogImageBackend interface {
	Image(context.Context, Project, protocol.RPCCatalogImageParams) (protocol.RPCCatalogImage, error)
}

func displayCatalogImages(project, session string, page CatalogMessages) CatalogMessages {
	page.Messages = slices.Clone(page.Messages)
	for i := range page.Messages {
		message := &page.Messages[i]
		message.Images = slices.Clone(message.Images)
		for j := range message.Images {
			image := &message.Images[j]
			image.URL = ""
			if message.Role != "user" || !runtimeIdentifier(project) || !runtimeIdentifier(session) || !runtimeIdentifier(message.ID) || image.Index < 0 || image.Index > 10000 || !rasterMIME(image.MIMEType) {
				continue
			}
			image.URL = "/projects/" + project + "/sessions/" + session + "/images/" + message.ID + "/" + strconv.Itoa(image.Index)
		}
	}
	return page
}

func (s *shell) imageRequest(w http.ResponseWriter, r *http.Request, live bool) (context.Context, context.CancelFunc, Project, int, bool) {
	fail := func(status int) (context.Context, context.CancelFunc, Project, int, bool) {
		http.Error(w, "Image preview unavailable", status)
		return nil, nil, Project{}, 0, false
	}
	if r.Method != http.MethodGet {
		return fail(http.StatusMethodNotAllowed)
	}
	if _, ok := s.browser(r); !ok {
		return fail(http.StatusUnauthorized)
	}
	if s.registry == nil {
		return fail(http.StatusNotFound)
	}
	if len(r.URL.RawQuery) > 1024 || !runtimeIdentifier(r.PathValue("project")) || !runtimeIdentifier(r.PathValue("message")) {
		return fail(http.StatusBadRequest)
	}
	rawIndex := r.PathValue("index")
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 || index > 10000 || strconv.Itoa(index) != rawIndex {
		return fail(http.StatusBadRequest)
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return fail(http.StatusBadRequest)
	}
	for key, values := range query {
		if !live || (key != "instance_id" && key != "session_id" && key != "turn_id") || len(values) != 1 || !runtimeIdentifier(values[0]) {
			return fail(http.StatusBadRequest)
		}
	}
	if live && (!runtimeIdentifier(query.Get("instance_id")) || !runtimeIdentifier(query.Get("session_id"))) || !live && !runtimeIdentifier(r.PathValue("session")) {
		return fail(http.StatusBadRequest)
	}
	// Four total nonqueued image reads, independent of inspector/stream limits.
	select {
	case s.imageSlots <- struct{}{}:
	default:
		return fail(http.StatusTooManyRequests)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	release := func() { cancel(); <-s.imageSlots }
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil || !project.Available {
		release()
		return fail(http.StatusNotFound)
	}
	return ctx, release, project, index, true
}

func (s *shell) currentImageRow(project, instance, session, message, turn string, index int) (RuntimeMessage, MessageImage, bool) {
	if s.runtimes == nil {
		return RuntimeMessage{}, MessageImage{}, false
	}
	snapshot, ok := s.runtimes.Snapshot(project)
	if !ok || snapshot.ProjectID != project || snapshot.InstanceID != instance || snapshot.SessionID != session || snapshot.Status == "opening" || snapshot.Status == "closing" || snapshot.Status == "failed" || snapshot.Status == "switching" {
		return RuntimeMessage{}, MessageImage{}, false
	}
	row, image, ok := imageRow(snapshot, message, index)
	return row, image, ok && row.SourceTurnID == turn
}

func (s *shell) runtimeImageHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, release, project, index, ok := s.imageRequest(w, r, true)
	if !ok {
		return
	}
	defer release()
	backend, ok := s.runtimes.(RuntimeImageBackend)
	if !ok {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	instance, session, turn, message := query.Get("instance_id"), query.Get("session_id"), query.Get("turn_id"), r.PathValue("message")
	row, image, ok := s.currentImageRow(project.ID, instance, session, message, turn, index)
	if !ok {
		http.Error(w, "Image identity changed", http.StatusConflict)
		return
	}
	result, err := backend.MessageImage(ctx, project.ID, instance, session, message, index)
	current, currentImage, currentOK := s.currentImageRow(project.ID, instance, session, message, turn, index)
	checked, lookupErr := s.registry.Lookup(ctx, project.ID)
	if err != nil || lookupErr != nil || !checked.Available || !currentOK || current.SourceID != row.SourceID || current.SourceTurnID != row.SourceTurnID || currentImage.MIMEType != image.MIMEType || result.MIMEType != image.MIMEType || !validImageResult(result, session, row.SourceID, index) {
		http.Error(w, "Image preview unavailable or stale", http.StatusConflict)
		return
	}
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	writeMessageImage(w, result)
}

func (s *shell) savedImageHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, release, project, index, ok := s.imageRequest(w, r, false)
	if !ok {
		return
	}
	defer release()
	// Never hold the mutation gate while reading. Live ownership and registry
	// identity are checked on both sides of the bounded RPC. Even a failed or
	// different-session owner forbids catalog access; there is no fallback.
	if s.projectHasRuntime(project.ID) {
		http.Error(w, "Project has a live owner", http.StatusConflict)
		return
	}
	backend, ok := s.catalog.(CatalogImageBackend)
	if !ok {
		http.NotFound(w, r)
		return
	}
	session, message := r.PathValue("session"), r.PathValue("message")
	result, err := backend.Image(ctx, project, protocol.RPCCatalogImageParams{SessionID: session, MessageID: message, Index: index})
	checked, lookupErr := s.registry.Lookup(ctx, project.ID)
	if err != nil || lookupErr != nil || !checked.Available || s.projectHasRuntime(project.ID) || !validImageResult(result, session, message, index) {
		http.Error(w, "Image preview unavailable or stale", http.StatusConflict)
		return
	}
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	writeMessageImage(w, result)
}

func (s *shell) projectHasRuntime(project string) bool {
	if s.runtimes == nil {
		return false
	}
	_, ok := s.runtimes.Snapshot(project)
	return ok
}

func writeMessageImage(w http.ResponseWriter, result protocol.RPCCatalogImage) {
	w.Header().Set("Content-Type", result.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	_, _ = w.Write(result.Data)
}
