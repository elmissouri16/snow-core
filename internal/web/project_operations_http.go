//go:build darwin || linux

package web

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type projectOperationHTTP struct {
	shell      *shell
	operations *ProjectOperations
}

func (s *shell) registerProjectOperationRoutes(mux *http.ServeMux, operations *ProjectOperations) {
	h := &projectOperationHTTP{s, operations}
	mux.HandleFunc("POST /projects/folders/select", h.selectParent)
	mux.HandleFunc("POST /projects/create", h.admit)
	mux.HandleFunc("POST /projects/clone", h.admit)
	mux.HandleFunc("GET /operations", h.list)
	mux.HandleFunc("GET /operations/{id}", h.get)
	mux.HandleFunc("POST /operations/{id}/cancel", h.control)
	mux.HandleFunc("POST /operations/{id}/reconcile", h.control)
	mux.HandleFunc("POST /operations/{id}/register", h.control)
	mux.HandleFunc("POST /operations/{id}/dismiss", h.control)
}
func operationHTTPError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, ErrOperationUnavailable.Error()
	switch {
	case errors.Is(err, ErrOperationInvalid):
		status, message = http.StatusBadRequest, ErrOperationInvalid.Error()
	case errors.Is(err, ErrOperationNotFound):
		status, message = http.StatusNotFound, ErrOperationNotFound.Error()
	case errors.Is(err, ErrOperationBusy):
		status, message = http.StatusConflict, ErrOperationBusy.Error()
	case errors.Is(err, ErrOperationLimit):
		status, message = http.StatusConflict, ErrOperationLimit.Error()
	case errors.Is(err, ErrOperationConflict), errors.Is(err, ErrProjectDuplicate), errors.Is(err, ErrProjectInvalid), errors.Is(err, ErrProjectLimit):
		status, message = http.StatusConflict, ErrOperationConflict.Error()
	}
	http.Error(w, message, status)
}
func operationJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64<<10 {
		operationHTTPError(w, ErrOperationUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (h *projectOperationHTTP) ready(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := h.shell.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return false
	}
	if h.operations == nil {
		operationHTTPError(w, ErrOperationUnavailable)
		return false
	}
	return true
}
func (h *projectOperationHTTP) form(w http.ResponseWriter, r *http.Request, allowed ...string) (browserSession, bool) {
	// Keep the check local too: tests and future route composition must not rely
	// solely on outer middleware to enforce exact configured browser origin.
	if r.Header.Get("Origin") != h.shell.requestBoundary(r).origin || len(r.Header.Values("Origin")) != 1 {
		http.Error(w, "Invalid browser origin", http.StatusForbidden)
		return browserSession{}, false
	}
	browser, ok := h.shell.authorizeFormLimit(w, r, 8<<10)
	if !ok {
		return browserSession{}, false
	}
	if h.operations == nil {
		operationHTTPError(w, ErrOperationUnavailable)
		return browserSession{}, false
	}
	fields := map[string]bool{"csrf": true}
	for _, key := range allowed {
		fields[key] = true
	}
	if r.URL.RawQuery != "" || !operationHTTPFields(r.PostForm, fields) || len(r.PostForm["csrf"]) != 1 {
		operationHTTPError(w, ErrOperationInvalid)
		return browserSession{}, false
	}
	return browser, true
}
func operationHTTPFields(values url.Values, allowed map[string]bool) bool {
	for key, list := range values {
		if !allowed[key] || len(list) != 1 || len(list[0]) > 4096 {
			return false
		}
	}
	return true
}
func (h *projectOperationHTTP) selectParent(w http.ResponseWriter, r *http.Request) {
	browser, ok := h.form(w, r, "path")
	if !ok {
		return
	}
	if len(r.PostForm["path"]) != 1 {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	selection, err := h.operations.SelectParent(r.Context(), browser.ID, r.PostForm.Get("path"))
	if err != nil {
		operationHTTPError(w, err)
		return
	}
	operationJSON(w, http.StatusOK, selection)
}
func (h *projectOperationHTTP) admit(w http.ResponseWriter, r *http.Request) {
	fields := []string{"operation_id", "name"}
	kind := "create"
	if r.URL.Path == "/projects/clone" {
		kind = "clone"
		fields = append(fields, "remote")
	}
	browser, ok := h.form(w, r, fields...)
	if !ok {
		return
	}
	if len(r.PostForm["operation_id"]) != 1 || len(r.PostForm["name"]) != 1 || (kind == "clone" && len(r.PostForm["remote"]) != 1) {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	op, err := h.operations.Admit(r.Context(), browser.ID, ProjectOperationRequest{OperationID: r.PostForm.Get("operation_id"), Kind: kind, Name: r.PostForm.Get("name"), Remote: r.PostForm.Get("remote")})
	if err != nil {
		operationHTTPError(w, err)
		return
	}
	operationJSON(w, http.StatusAccepted, op)
}
func (h *projectOperationHTTP) list(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w, r) {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !operationHTTPFields(values, map[string]bool{"offset": true}) {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	offset := 0
	if value, ok := values["offset"]; ok {
		offset, err = strconv.Atoi(value[0])
		if err != nil {
			operationHTTPError(w, ErrOperationInvalid)
			return
		}
	}
	page, err := h.operations.List(r.Context(), offset)
	if err != nil {
		operationHTTPError(w, err)
		return
	}
	operationJSON(w, http.StatusOK, page)
}
func (h *projectOperationHTTP) get(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	op, err := h.operations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		operationHTTPError(w, err)
		return
	}
	operationJSON(w, http.StatusOK, op)
}
func (h *projectOperationHTTP) control(w http.ResponseWriter, r *http.Request) {
	_, ok := h.form(w, r, "revision", "review")
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
	if err != nil || revision < 1 {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	id := r.PathValue("id")
	review := r.PostForm.Get("review")
	register := r.URL.Path == "/operations/"+id+"/register"
	if (!register && len(r.PostForm["review"]) != 0) || (register && review != "" && review != "true") {
		operationHTTPError(w, ErrOperationInvalid)
		return
	}
	var op ProjectOperation
	switch r.URL.Path {
	case "/operations/" + id + "/cancel":
		op, err = h.operations.Cancel(r.Context(), id, revision)
	case "/operations/" + id + "/reconcile":
		op, err = h.operations.Reconcile(r.Context(), id, revision)
	case "/operations/" + id + "/register":
		op, err = h.operations.Register(r.Context(), id, revision, review == "true")
	case "/operations/" + id + "/dismiss":
		err = h.operations.Dismiss(r.Context(), id, revision)
	default:
		err = ErrOperationInvalid
	}
	if err != nil {
		operationHTTPError(w, err)
		return
	}
	if op.ID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	operationJSON(w, http.StatusOK, op)
}
