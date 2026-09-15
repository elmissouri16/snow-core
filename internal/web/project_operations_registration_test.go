//go:build darwin || linux

package web

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// BUG-159: successful filesystem work is not permission to register a project.
// Exercise a private destination through completion, reads, observation, and
// manager/registry restart; none may silently insert manager project metadata.
func TestProjectOperationsRequireExplicitRegistration(t *testing.T) {
	for _, kind := range []string{"create", "clone"} {
		t.Run(kind, func(t *testing.T) {
			m, b, parent := newOperationTestManager(t)
			req := selectOperation(t, m, parent, "private-canary", kind)
			op, err := m.Admit(t.Context(), "browser-one", req)
			if err != nil {
				t.Fatal(err)
			}
			awaitOperation(t, m, op.ID, "running")
			b.terminal <- ProjectOperationTerminal{State: "succeeded", Outcome: "observed"}
			awaitOperation(t, m, op.ID, "awaiting_registration")
			// Closing joins the worker and final persistence so the registry assertion
			// cannot race a delayed automatic completion/registration callback.
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			retained, err := m.Get(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if retained.State != "awaiting_registration" || retained.Outcome != "observed" || retained.ProjectID != "" {
				t.Fatalf("success must await explicit registration: %+v", retained)
			}
			assertOperationRegistryEmpty(t, m.registry)
			if _, err := m.List(t.Context(), 0); err != nil {
				t.Fatal(err)
			}
			observed, err := m.Reconcile(t.Context(), op.ID, retained.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if observed.State != "awaiting_registration" || observed.ProjectID != "" {
				t.Fatalf("observation registered project: %+v", observed)
			}
			assertOperationRegistryEmpty(t, m.registry)
			entries, err := os.ReadDir(filepath.Join(parent, req.Name))
			if err != nil || len(entries) != 0 {
				t.Fatalf("unexpected destination/runtime files: %v %v", entries, err)
			}
			managerDir := m.registry.path
			if err := m.registry.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := registryTestOpen(t, managerDir)
			afterBackend := &operationFakeBackend{registry: reopened, terminal: make(chan ProjectOperationTerminal, 1)}
			after, err := NewProjectOperations(t.Context(), reopened, afterBackend)
			if err != nil {
				t.Fatal(err)
			}
			defer after.Close()
			restarted, err := after.Get(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if restarted.ProjectID != "" || restarted.State != "interrupted" {
				t.Fatalf("restart must not register or replay: %+v", restarted)
			}
			if _, err := after.List(t.Context(), 0); err != nil {
				t.Fatal(err)
			}
			if _, err := after.Reconcile(t.Context(), op.ID, restarted.Revision); err != nil {
				t.Fatal(err)
			}
			assertOperationRegistryEmpty(t, reopened)
			if b.opened.Load() != 1 || b.closed.Load() != 1 || afterBackend.opened.Load() != 0 {
				t.Fatal("reads/restart dispatched work or failed to join")
			}
		})
	}
}
func assertOperationRegistryEmpty(t *testing.T, r *Registry) {
	t.Helper()
	var count int
	if err := r.db.QueryRowContext(t.Context(), `SELECT count(*) FROM projects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("project registration requires a separate explicit request; count=%d", count)
	}
	projects, err := r.List(t.Context())
	if err != nil || len(projects) != 0 {
		t.Fatalf("public registry changed: %+v %v", projects, err)
	}
}

func TestProjectOperationHTTPRegistrationIsSeparateReviewedCAS(t *testing.T) {
	m, b, parent := newOperationTestManager(t)
	s, err := newShell(testOrigin, "test")
	if err != nil {
		t.Fatal(err)
	}
	cookie := pairBrowser(t, s, s.initialCode)
	csrf := csrfFor(t, s, cookie)
	mux := http.NewServeMux()
	s.registerProjectOperationRoutes(mux, m)
	selected := operationRequest(t, s, mux, "POST", "/projects/folders/select", url.Values{"csrf": {csrf}, "path": {parent}}, cookie, s.origin)
	var grant ParentSelection
	if selected.Code != http.StatusOK || json.Unmarshal(selected.Body.Bytes(), &grant) != nil {
		t.Fatalf("select %d", selected.Code)
	}
	admitted := operationRequest(t, s, mux, "POST", "/projects/create", url.Values{"csrf": {csrf}, "operation_id": {grant.OperationID}, "name": {"reviewed"}}, cookie, s.origin)
	if admitted.Code != http.StatusAccepted {
		t.Fatalf("admit %d", admitted.Code)
	}
	awaitOperation(t, m, grant.OperationID, "running")
	b.terminal <- ProjectOperationTerminal{State: "succeeded", Outcome: "observed"}
	retained := awaitOperation(t, m, grant.OperationID, "awaiting_registration")
	assertOperationRegistryEmpty(t, m.registry)
	for _, path := range []string{"/operations", "/operations/" + retained.ID} {
		response := operationRequest(t, s, mux, "GET", path, nil, cookie, s.origin)
		if response.Code != http.StatusOK {
			t.Fatalf("metadata read %d", response.Code)
		}
		assertOperationRegistryEmpty(t, m.registry)
	}
	observed := operationRequest(t, s, mux, "POST", "/operations/"+retained.ID+"/reconcile", url.Values{"csrf": {csrf}, "revision": {strconv.FormatInt(retained.Revision, 10)}}, cookie, s.origin)
	if observed.Code != http.StatusOK || json.Unmarshal(observed.Body.Bytes(), &retained) != nil {
		t.Fatalf("observe %d", observed.Code)
	}
	assertOperationRegistryEmpty(t, m.registry)
	registered := operationRequest(t, s, mux, "POST", "/operations/"+retained.ID+"/register", url.Values{"csrf": {csrf}, "revision": {strconv.FormatInt(retained.Revision, 10)}}, cookie, s.origin)
	var result ProjectOperation
	if registered.Code != http.StatusOK || json.Unmarshal(registered.Body.Bytes(), &result) != nil || result.State != "succeeded" || result.ProjectID == "" {
		t.Fatalf("explicit register %d: %+v", registered.Code, result)
	}
	projects, err := m.registry.List(t.Context())
	if err != nil || len(projects) != 1 || projects[0].ID != result.ProjectID {
		t.Fatalf("registration %+v %v", projects, err)
	}
	if registered.Header().Get("Location") != "" || b.opened.Load() != 1 || b.started.Load() != 1 || b.closed.Load() != 1 {
		t.Fatal("registration navigated, reexecuted, or failed to clean up")
	}
	entries, err := os.ReadDir(result.Child.Path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("registration activated or changed project files: %v %v", entries, err)
	}
}
