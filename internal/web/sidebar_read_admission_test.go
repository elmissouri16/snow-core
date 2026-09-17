package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type preemptibleSidebarCatalog struct {
	mu       sync.Mutex
	entered  chan string
	release  chan struct{}
	active   map[string]bool
	calls    int
	canceled map[string]bool
}

func (c *preemptibleSidebarCatalog) Sessions(ctx context.Context, p Project, _ int) (CatalogSessions, error) {
	c.mu.Lock()
	c.active[p.ID] = true
	c.calls++
	c.mu.Unlock()
	c.entered <- p.ID
	defer func() { c.mu.Lock(); delete(c.active, p.ID); c.mu.Unlock() }()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		c.canceled[p.ID] = true
		c.mu.Unlock()
		return CatalogSessions{}, ctx.Err()
	case <-c.release:
		return CatalogSessions{}, nil
	}
}
func (*preemptibleSidebarCatalog) Messages(context.Context, Project, string, int) (CatalogMessages, error) {
	panic("inventory requested transcript")
}

type inventoryActivationRuntime struct {
	fakeRuntime
	mu      sync.Mutex
	catalog *preemptibleSidebarCatalog
	owners  map[string]RuntimeSnapshot
	overlap bool
	starts  int
}

func (r *inventoryActivationRuntime) Open(_ context.Context, p Project, _, _, _ string) (RuntimeSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalog.mu.Lock()
	r.overlap = r.overlap || r.catalog.active[p.ID]
	r.catalog.mu.Unlock()
	r.starts++
	snapshot := RuntimeSnapshot{ProjectID: p.ID, InstanceID: "instance-" + p.ID, SessionID: "saved", Status: "idle"}
	r.owners[p.ID] = snapshot
	return snapshot, nil
}
func (r *inventoryActivationRuntime) OpenWithSkills(ctx context.Context, p Project, session, provider, model string, enabled bool) (RuntimeSnapshot, error) {
	if !enabled {
		return RuntimeSnapshot{}, ErrRuntimeInvalid
	}
	return r.Open(ctx, p, session, provider, model)
}
func (*inventoryActivationRuntime) Skills(context.Context, string, string) (RuntimeSkills, error) {
	return RuntimeSkills{}, ErrRuntimeInvalid
}
func (r *inventoryActivationRuntime) Snapshot(project string) (RuntimeSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.owners[project]
	return snapshot, ok
}

func TestSidebarReadsNeverRejectExplicitActivation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	var projects []Project
	for range 2 {
		project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		projects = append(projects, project)
	}
	catalog := &preemptibleSidebarCatalog{entered: make(chan string, 2), release: make(chan struct{}), active: make(map[string]bool), canceled: make(map[string]bool)}
	t.Cleanup(func() { close(catalog.release) })
	backend := &inventoryActivationRuntime{catalog: catalog, owners: make(map[string]RuntimeSnapshot)}
	s.catalog = catalog
	s.runtimes = backend
	results := make(chan *httptest.ResponseRecorder, 2)
	var readers sync.WaitGroup
	for _, p := range projects {
		readers.Go(func() { results <- request(t, s, "GET", "/projects/"+p.ID+"/sidebar-sessions", nil, cookie) })
	}
	// Rendezvous proves both production reads are in flight before Start. There
	// are no sleeps, retries, or releases of their normal-completion barriers.
	for range 2 {
		select {
		case <-catalog.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("cold disclosures did not run independently")
		}
	}
	for i, p := range projects {
		w := request(t, s, "POST", "/projects/"+p.ID+"/runtime/open", url.Values{"csrf": {csrfFor(t, s, cookie)}, "confirm": {"activate"}}, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("explicit Start collided with background inventory: %d %s", w.Code, w.Body.String())
		}
		catalog.mu.Lock()
		canceled := catalog.canceled[p.ID]
		otherActive := catalog.active[projects[1].ID]
		catalog.mu.Unlock()
		if !canceled || i == 0 && !otherActive {
			t.Fatal("activation must preempt only its own cold read")
		}
	}
	readers.Wait()
	for range 2 {
		w := <-results
		if w.Code != http.StatusConflict && w.Code != http.StatusServiceUnavailable {
			t.Fatalf("canceled inventory returned stale success: %d %s", w.Code, w.Body.String())
		}
	}
	if backend.overlap || backend.starts != 2 {
		t.Fatalf("catalog overlapped runtime ownership or activation retried: overlap=%v starts=%d", backend.overlap, backend.starts)
	}
	// Existing owners never fall back to catalog, even without the optional live
	// inventory capability. These reads also do not send prompts after Start.
	for _, p := range projects {
		w := request(t, s, "GET", "/projects/"+p.ID+"/sidebar-sessions", nil, cookie)
		if w.Code != http.StatusOK {
			t.Fatal(w.Code)
		}
	}
	if catalog.calls != 2 || len(backend.calls) != 0 {
		t.Fatal("inventory or activation retried, read live SQLite, or sent a prompt")
	}
}

type foregroundHistoryCatalog struct{ *preemptibleSidebarCatalog }

func (c foregroundHistoryCatalog) Messages(_ context.Context, p Project, _ string, _ int) (CatalogMessages, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active[p.ID] || !c.canceled[p.ID] {
		return CatalogMessages{}, errors.New("foreground history overlapped sidebar catalog")
	}
	return CatalogMessages{Messages: []HistoryMessage{{ID: "saved-message", Role: "assistant", Text: "Foreground history loaded"}}}, nil
}

func TestColdWorkspaceHistoryPreemptsItsSidebarRead(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &preemptibleSidebarCatalog{entered: make(chan string, 1), release: make(chan struct{}), active: make(map[string]bool), canceled: make(map[string]bool)}
	s.catalog = foregroundHistoryCatalog{catalog}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- request(t, s, http.MethodGet, "/projects/"+project.ID+"/sidebar-sessions", nil, cookie)
	}()
	t.Cleanup(func() { close(catalog.release); <-result })
	select {
	case <-catalog.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("sidebar catalog did not start")
	}
	w := request(t, s, http.MethodGet, "/?view=projects&project="+project.ID+"&session=saved", nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Foreground history loaded") {
		t.Fatalf("explicit history read collided with background catalog: %d %s", w.Code, w.Body.String())
	}
	// Completion reopens inventory admission; no retry or worker activation is
	// required, and the existing owner-scoping test protects unrelated projects.
	_, read, err := s.sidebarReads.begin(t.Context(), project.ID)
	if err != nil {
		t.Fatal("foreground navigation retained inventory admission", err)
	}
	read.finish()
	read.cancel()
}

func TestSidebarReadPreemptionBlocksOnlyOwnerAndFailsClosed(t *testing.T) {
	var admission sidebarReadAdmission
	readCtx, read, err := admission.begin(t.Context(), "project")
	if err != nil {
		t.Fatal(err)
	}
	defer read.cancel()
	defer read.finish()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if release, err := admission.preempt(ctx, "project"); !errors.Is(err, ErrRuntimeBusy) || release != nil {
		t.Fatalf("unfinished teardown admitted owner: %v", err)
	}
	if readCtx.Err() == nil {
		t.Fatal("preemption waited for normal completion instead of canceling")
	}
	read.finish()
	release, err := admission.preempt(t.Context(), "project")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err := admission.begin(t.Context(), "project"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatal("read entered activating workspace")
	}
	_, other, err := admission.begin(t.Context(), "other")
	if err != nil {
		t.Fatal("unrelated workspace blocked", err)
	}
	other.finish()
	other.cancel()
}
