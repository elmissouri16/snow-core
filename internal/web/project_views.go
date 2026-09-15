package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// inspectProject exposes only typed, read-only project views. Paths remain in
// authenticated POST bodies rather than browser history/referrer URLs. None of
// these operations activates an agent or invokes the model-facing tool loop.
func (s *shell) inspectProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeFormLimit(w, r, 64<<10); !ok {
		return
	}
	if s.registry == nil {
		http.Error(w, "Project inspection is unavailable", http.StatusServiceUnavailable)
		return
	}
	select {
	case s.inspectSlots <- struct{}{}:
		defer func() { <-s.inspectSlots }()
	default:
		http.Error(w, "Project inspection is busy; no request was queued", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	project, err := s.registry.Lookup(ctx, r.PathValue("project"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !project.Available {
		http.Error(w, "The registered host folder is missing or changed", http.StatusConflict)
		return
	}
	path := r.PostForm.Get("path")
	var value any
	switch r.PathValue("action") {
	case "files":
		offset := 0
		if raw := r.PostForm.Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "Invalid directory page", http.StatusBadRequest)
				return
			}
		}
		value, err = InspectFiles(ctx, project, path, offset)
	case "file":
		value, err = InspectFile(ctx, project, path)
	case "changes":
		value, err = InspectChanges(ctx, project)
	case "diff":
		value, err = InspectDiff(ctx, project, path, r.PostForm.Get("kind"))
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		message := "The project view is unavailable. Refresh or choose another path."
		switch {
		case errors.Is(err, ErrInspectionPath):
			message = "This path is excluded or is not a safe relative project path."
		case errors.Is(err, ErrInspectionBinary):
			message = "Only UTF-8 text files can be previewed."
		case errors.Is(err, ErrInspectionTooLarge):
			message = "This file exceeds the bounded preview size."
		}
		http.Error(w, message, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, value)
}
