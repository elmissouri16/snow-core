package web

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type managerActivityBackend struct {
	fakeRuntime
	snapshots map[string]RuntimeSnapshot
	read      func(string)
	reads     int
}

func (b *managerActivityBackend) Snapshot(id string) (RuntimeSnapshot, bool) {
	b.reads++
	if b.read != nil {
		b.read(id)
	}
	snapshot, ok := b.snapshots[id]
	return snapshot, ok
}

func managerActivityRequest(t *testing.T, s *shell, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, s.origin+"/activity", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.managerActivity(w, req)
	return w
}
func managerActivityDecode(t *testing.T, w *httptest.ResponseRecorder) ManagerActivitySummary {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("activity = %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() > managerActivityMaxBytes {
		t.Fatal("unbounded summary")
	}
	var result ManagerActivitySummary
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func managerActivityAdd(t *testing.T, s *shell, name string) Project {
	t.Helper()
	project, err := s.registry.Add(t.Context(), name, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func TestManagerActivityAuthenticationAndReadOnly(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	backend := &managerActivityBackend{}
	s.runtimes = backend
	managerActivityAdd(t, s, "private project")
	w := managerActivityRequest(t, s, nil)
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "private project") || backend.reads != 0 {
		t.Fatalf("unauthenticated summary: %d", w.Code)
	}
	result := managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if len(result.Projects) != 1 || result.Projects[0].RuntimeState != "inactive" {
		t.Fatalf("inactive = %+v", result)
	}
	req := httptest.NewRequest(http.MethodPost, s.origin+"/activity", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.managerActivity(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("activity admitted mutation")
	}
	if len(backend.calls) != 0 || catalog.calls != 0 {
		t.Fatal("summary activated or controlled a worker/catalog")
	}
	request(t, s, "POST", "/logout", url.Values{"csrf": {csrfFor(t, s, cookie)}}, cookie)
	if w := managerActivityRequest(t, s, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked cookie read activity")
	}
}

func TestManagerActivitySimultaneousAttentionAndPrivacy(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	a := managerActivityAdd(t, s, "First project")
	b := managerActivityAdd(t, s, "Second project")
	backend := &managerActivityBackend{snapshots: map[string]RuntimeSnapshot{
		a.ID: {ProjectID: a.ID, SessionID: "approval-session", Status: "permission", InstanceID: "private-instance", CancelToken: "private-cancel", SessionName: "private-session-name", Error: "private-diagnostic", Permission: &RuntimePermission{ID: "private-permission", Reason: "private-reason"}, Messages: []RuntimeMessage{{Text: "private-transcript"}}, Queue: &RuntimeQueue{Token: "private-queue-token", Items: []RuntimeQueueItem{{ID: "private-queue-id", Text: "private-queue-text", State: "pending"}, {State: "held"}}}},
		b.ID: {ProjectID: b.ID, SessionID: "question-session", Status: "input", Input: &protocol.UserInputRequest{ID: "private-input", Questions: []protocol.UserInputQuestion{{Question: "private-question"}}}},
	}}
	s.runtimes = backend
	w := managerActivityRequest(t, s, cookie)
	result := managerActivityDecode(t, w)
	if result.Counts.Running != 2 || result.Counts.Permissions != 1 || result.Counts.Questions != 1 || result.Counts.Queued != 1 || result.Counts.Review != 1 {
		t.Fatalf("attention = %+v", result.Counts)
	}
	for _, item := range result.Projects {
		target, err := url.Parse(item.SessionURL)
		if err != nil || target.Path != "/" || target.Query().Get("project") != item.ProjectID || target.Query().Get("session") != item.SessionID || target.Query().Get("view") != "projects" {
			t.Fatalf("unsafe session target: %+v", item)
		}
	}
	for _, secret := range []string{"private-", a.Path, b.Path, "instance_id", "cancel_token", "messages", "permission_id", "question\"", "queue_token"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("summary exposed %q", secret)
		}
	}
	if len(backend.calls) != 0 || catalog.calls != 0 {
		t.Fatal("read caused worker/catalog execution")
	}
	// A definitive completion clears attention, even if obsolete pointers remain.
	snapshot := backend.snapshots[a.ID]
	snapshot.Status = "idle"
	backend.snapshots[a.ID] = snapshot
	result = managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if result.Counts.Permissions != 0 || result.Counts.Running != 1 || result.Counts.Review != 2 {
		t.Fatalf("stale completion retained attention: %+v", result.Counts)
	}
}

func TestManagerActivityFailedWorkerIsolationAndRecovery(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	a := managerActivityAdd(t, s, "Failed worker")
	b := managerActivityAdd(t, s, "Still running")
	c := managerActivityAdd(t, s, "Restart recovery")
	hint := RecoveryHint{SessionID: "recover-exact", State: RecoveryAdmissionUnknown, UpdatedAt: time.Now()}
	if err := s.registry.SaveRecovery(t.Context(), c.ID, hint); err != nil {
		t.Fatal(err)
	}
	backend := &managerActivityBackend{snapshots: map[string]RuntimeSnapshot{
		a.ID: {ProjectID: a.ID, SessionID: "failed-session", Status: "failed", Error: "private failure detail", Permission: &RuntimePermission{ID: "old-authority"}, Queue: &RuntimeQueue{Items: []RuntimeQueueItem{{State: "pending"}, {State: "uncertain"}}}},
		b.ID: {ProjectID: b.ID, SessionID: "running-session", Status: "running", Recovery: RecoveryHint{SessionID: "running-session", State: RecoveryAdmitted, UpdatedAt: time.Now()}},
	}}
	s.runtimes = backend
	result := managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if result.Counts.Failed != 1 || result.Counts.Running != 1 || result.Counts.Recovery != 1 || result.Counts.Review != 2 || result.Counts.Queued != 0 || result.Counts.Permissions != 0 {
		t.Fatalf("isolation = %+v", result.Counts)
	}
	for _, item := range result.Projects {
		if item.ProjectID == c.ID && (item.SessionID != hint.SessionID || item.RuntimeState != "inactive" || !item.Recovery) {
			t.Fatalf("recovery = %+v", item)
		}
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("recovery read activated backend")
	}
}

func TestManagerActivityRemovalDuringReadAndNoCachedResurrection(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project := managerActivityAdd(t, s, "Removed during read")
	backend := &managerActivityBackend{snapshots: map[string]RuntimeSnapshot{project.ID: {ProjectID: project.ID, SessionID: "old-session", Status: "permission"}}}
	backend.read = func(id string) {
		backend.read = nil
		if err := s.registry.Remove(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	s.runtimes = backend
	result := managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if len(result.Projects) != 0 || result.Counts.Registered != 0 || result.Counts.Permissions != 0 {
		t.Fatalf("removed project resurfaced: %+v", result)
	}
	result = managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if len(result.Projects) != 0 || backend.reads != 1 {
		t.Fatal("removed registration queried stale runtime")
	}
}

func TestManagerActivityBoundsAndInvalidSnapshot(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	backend := &managerActivityBackend{snapshots: make(map[string]RuntimeSnapshot)}
	s.runtimes = backend
	for range MaxProjects {
		project := managerActivityAdd(t, s, strings.Repeat("界", 42))
		snapshot := RuntimeSnapshot{ProjectID: project.ID, SessionID: strings.Repeat("s", 128), Status: "running", Queue: &RuntimeQueue{}}
		for range 80 {
			snapshot.Queue.Items = append(snapshot.Queue.Items, RuntimeQueueItem{State: "pending", Text: strings.Repeat("private", 1000)})
		}
		backend.snapshots[project.ID] = snapshot
	}
	result := managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if len(result.Projects) != MaxProjects || result.Counts.Queued != MaxProjects*protocol.RPCQueueMaxItems || backend.reads != MaxProjects {
		t.Fatalf("bounds: %+v", result.Counts)
	}
	if _, err := s.registry.Add(t.Context(), "too many", t.TempDir()); err == nil {
		t.Fatal("registration limit bypassed")
	}
	for id, snapshot := range backend.snapshots {
		snapshot.SessionID = "//external.invalid/private"
		snapshot.Status = "private-status"
		backend.snapshots[id] = snapshot
	}
	result = managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	for _, item := range result.Projects {
		if item.SessionID != "" || item.SessionURL != "" || item.RuntimeState != "unknown" || !item.Unavailable {
			t.Fatalf("unsafe runtime summary: %+v", item)
		}
	}
	if len(backend.calls) != 0 || catalog.calls != 0 {
		t.Fatal("summary started work")
	}
}

func TestManagerActivityProductionRoute(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	managerActivityAdd(t, s, "Production route")
	w := request(t, s, "GET", "/activity", nil, cookie)
	result := managerActivityDecode(t, w)
	if result.Counts.Registered != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("production summary wiring")
	}
	if w := request(t, s, "GET", "/activity", nil); w.Code != http.StatusUnauthorized {
		t.Fatal("production route permits unpaired read")
	}
	if catalog.calls != 0 {
		t.Fatal("production route activated catalog")
	}
}

func TestManagerActivityRealManagerRemainsWorkerFree(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project := managerActivityAdd(t, s, "Read without activation")
	if err := s.registry.SaveRecovery(t.Context(), project.ID, RecoveryHint{SessionID: "saved-only", State: RecoveryAdmitted, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	manager := NewRuntimeManager(t.Context(), "/must-not-execute/snow", t.TempDir(), s.registry)
	t.Cleanup(func() { _ = manager.Close() })
	s.runtimes = manager
	result := managerActivityDecode(t, managerActivityRequest(t, s, cookie))
	if result.Counts.Running != 0 || result.Counts.Recovery != 1 {
		t.Fatalf("cold manager summary = %+v", result.Counts)
	}
	manager.mu.Lock()
	workers := len(manager.workers)
	manager.mu.Unlock()
	if workers != 0 || catalog.calls != 0 {
		t.Fatal("Activity activated worker or catalog")
	}
}

func TestManagerActivityCanceledReadFailsClosed(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	managerActivityAdd(t, s, "No partial response")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest(http.MethodGet, s.origin+"/activity", nil).WithContext(ctx)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.managerActivity(w, req)
	if (w.Code != http.StatusServiceUnavailable && w.Code != http.StatusUnauthorized) || strings.Contains(w.Body.String(), "No partial response") || catalog.calls != 0 {
		t.Fatalf("canceled summary = %d %s", w.Code, w.Body.String())
	}
}

func TestManagerActivityProductionPageAndAssets(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	backend := &managerActivityBackend{}
	s.runtimes = backend
	w := request(t, s, "GET", "/?view=activity", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("Activity page = %d", w.Code)
	}
	for _, marker := range []string{`data-react-page="activity"`, `data-react-props="`, `&#34;registryEnabled&#34;:true`, "data-manager-activity", "Activity &amp; attention", `<script type="module" src="/static/generated/app.js"></script>`, "/static/manager-activity.css"} {
		if !strings.Contains(w.Body.String(), marker) {
			t.Errorf("Activity page lacks %q", marker)
		}
	}
	for name, contentType := range map[string]string{"generated/app.js": "text/javascript", "manager-activity.css": "text/css"} {
		t.Run(name, func(t *testing.T) {
			w := request(t, s, "GET", "/static/"+name, nil, cookie)
			if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), contentType) || w.Body.Len() == 0 {
				t.Fatalf("Activity asset = %d %s", w.Code, w.Header().Get("Content-Type"))
			}
		})
	}
	if strings.Contains(w.Body.String(), `src="/static/manager-activity.js"`) {
		t.Fatal("Activity page loaded the retired DOM owner")
	}
	if retired := request(t, s, "GET", "/static/manager-activity.js", nil, cookie); retired.Code != http.StatusNotFound {
		t.Fatal("retired Activity client is still routed")
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("Activity page activated a backend")
	}
}
