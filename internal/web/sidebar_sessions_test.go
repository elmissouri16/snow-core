package web

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

type sidebarCatalog struct {
	page    CatalogSessions
	err     error
	offsets []int
	after   func()
}

func (c *sidebarCatalog) Sessions(ctx context.Context, _ Project, offset int) (CatalogSessions, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded catalog read")
	}
	c.offsets = append(c.offsets, offset)
	if c.after != nil {
		c.after()
	}
	return c.page, c.err
}
func (*sidebarCatalog) Messages(context.Context, Project, string, int) (CatalogMessages, error) {
	panic("sidebar read requested history")
}

type sidebarRuntime struct {
	fakeRuntime
	inventory RuntimeSessionInventory
	reads     int
	after     func()
}

func (f *sidebarRuntime) SessionInventory(ctx context.Context, project, instance string) (RuntimeSessionInventory, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded inventory read")
	}
	if project != f.snapshot.ProjectID || instance != f.snapshot.InstanceID {
		panic("unbound read")
	}
	f.reads++
	if f.after != nil {
		f.after()
	}
	return f.inventory, nil
}

func TestSidebarSessionsColdAuthBoundsAndPublicMetadata(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &sidebarCatalog{page: CatalogSessions{Sessions: []SessionSummary{{ID: "saved", Name: "Title", Updated: "2025-01-01T00:00:00Z"}}, HasMore: true, NextOffset: 25}}
	s.catalog = catalog
	path := "/projects/" + project.ID + "/sidebar-sessions"
	if w := request(t, s, "GET", path, nil); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	for _, query := range []string{"?offset=-1", "?offset=10001", "?offset=x", "?offset=1&offset=2", "?offset=%xx", "?session=saved", "?offset=" + strings.Repeat("1", 257)} {
		if w := request(t, s, "GET", path+query, nil, cookie); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	if len(catalog.offsets) != 0 {
		t.Fatal("invalid request reached catalog")
	}
	w := request(t, s, "GET", path, nil, cookie)
	var result SidebarSessions
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if result.ProjectID != project.ID || result.InstanceID != "" || !result.Available || !result.HasMore || result.NextOffset != 25 || result.Truncated || len(result.Sessions) != 1 || result.Sessions[0].UpdatedAt != 1735689600000 {
		t.Fatalf("%+v", result)
	}
	var fields map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil || len(fields) != 9 {
		t.Fatalf("public fields: %v %v", fields, err)
	}
	if strings.Contains(w.Body.String(), project.Path) {
		t.Fatal("path exposed")
	}
	request(t, s, "GET", path+"?offset=25", nil, cookie)
	if catalog.offsets[1] != 25 {
		t.Fatal(catalog.offsets)
	}
	catalog.err = errors.New("PRIVATE diagnostics")
	w = request(t, s, "GET", path, nil, cookie)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal(w.Body.String())
	}
}

func TestSidebarSessionsLiveOwnerNoCatalogFallback(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := "/projects/" + project.ID + "/sidebar-sessions"
	backend := &sidebarRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "current", Status: "idle"}}, inventory: RuntimeSessionInventory{ProjectID: project.ID, InstanceID: "instance", Available: true, Sessions: []RuntimeSessionChoice{{SessionID: "saved", Name: "Saved"}}}}
	s.runtimes = backend
	w := request(t, s, "GET", path, nil, cookie)
	var result SidebarSessions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.InstanceID != "instance" || !result.Available || len(result.Sessions) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	backend.inventory.InstanceID = "stale"
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 409 {
		t.Fatal("stale inventory admitted", w.Code)
	}
	backend.inventory.InstanceID = "instance"
	backend.after = func() { backend.snapshot.InstanceID = "replacement" }
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 409 {
		t.Fatal("replacement admitted", w.Code)
	}
	s.runtimes = &backend.fakeRuntime
	w = request(t, s, "GET", path, nil, cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Available {
		t.Fatalf("unsupported: %s", w.Body.String())
	}
	if catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("GET used catalog or mutations")
	}
}

func TestSidebarSessionsFolderIdentityAndTruncation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := "/projects/" + project.ID + "/sidebar-sessions"
	catalog := &sidebarCatalog{page: CatalogSessions{Sessions: []SessionSummary{{ID: "bad/id", Name: "PRIVATE"}, {ID: "good", Name: strings.Repeat("a", 300)}, {ID: "good", Name: "duplicate"}}, HasMore: true, NextOffset: 10001}}
	s.catalog = catalog
	w := request(t, s, "GET", path, nil, cookie)
	var result SidebarSessions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Truncated || result.HasMore || result.NextOffset != 0 || len(result.Sessions) != 1 || len(result.Sessions[0].Name) > 256 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err := os.Rename(project.Path, project.Path+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(project.Path+"-moved", project.Path) })
	before := len(catalog.offsets)
	w = request(t, s, "GET", path, nil, cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Available || len(catalog.offsets) != before {
		t.Fatalf("identity: %s", w.Body.String())
	}
	if err := os.Rename(project.Path+"-moved", project.Path); err != nil {
		t.Fatal(err)
	}
	catalog.after = func() {
		if err := os.Rename(project.Path, project.Path+"-moved"); err != nil {
			t.Fatal(err)
		}
	}
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 409 {
		t.Fatalf("identity changed during read: %d", w.Code)
	}
}

func TestSidebarSessionsLivePaginationAndRemoval(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &sidebarRuntime{fakeRuntime: fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "current", Status: "idle"}}, inventory: RuntimeSessionInventory{ProjectID: project.ID, InstanceID: "instance", Available: true, Truncated: true}}
	for i := range 30 {
		backend.inventory.Sessions = append(backend.inventory.Sessions, RuntimeSessionChoice{SessionID: fmt.Sprintf("saved-%d", i), Name: "Saved"})
	}
	s.runtimes = backend
	path := "/projects/" + project.ID + "/sidebar-sessions"
	for _, tc := range []struct {
		query string
		count int
		more  bool
		next  int
	}{{"", 25, true, 25}, {"?offset=25", 5, false, 0}, {"?offset=10000", 0, false, 0}} {
		w := request(t, s, "GET", path+tc.query, nil, cookie)
		var result SidebarSessions
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Sessions) != tc.count || result.HasMore != tc.more || result.NextOffset != tc.next || !result.Truncated {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body.String())
		}
	}
	reads := backend.reads
	releaseReads, err := s.sidebarReads.preempt(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, s, "GET", path, nil, cookie)
	releaseReads()
	if w.Code != 409 || backend.reads != reads {
		t.Fatal("inventory bypassed activation gate")
	}
	if err := s.registry.Remove(t.Context(), project.ID); err != nil {
		t.Fatal(err)
	}
	if w := request(t, s, "GET", path, nil, cookie); w.Code != 404 || backend.reads != reads || catalog.calls != 0 {
		t.Fatalf("removed project inventory: %d", w.Code)
	}
}
