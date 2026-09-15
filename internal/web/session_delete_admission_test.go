package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

type preemptDeletionCatalog struct {
	*preemptibleSidebarCatalog
	deleting chan struct{}
	finish   chan struct{}
	overlap  bool
	deletes  int
}

func (c *preemptDeletionCatalog) DeleteSession(ctx context.Context, p Project, id string) error {
	c.mu.Lock()
	c.overlap = c.active[p.ID]
	c.deletes++
	c.mu.Unlock()
	close(c.deleting)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.finish:
		return nil
	}
}
func TestSessionDeletePreemptsSidebarAndExcludesActivation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	p, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reads := &preemptibleSidebarCatalog{entered: make(chan string, 1), release: make(chan struct{}), active: make(map[string]bool), canceled: make(map[string]bool)}
	c := &preemptDeletionCatalog{preemptibleSidebarCatalog: reads, deleting: make(chan struct{}), finish: make(chan struct{})}
	s.catalog = c
	r := &fakeRuntime{}
	s.runtimes = r
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	wg.Go(func() { results <- request(t, s, "GET", "/projects/"+p.ID+"/sidebar-sessions", nil, cookie) })
	select {
	case <-reads.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not enter")
	}
	csrf := csrfFor(t, s, cookie)
	body := url.Values{"csrf": {csrf}, "confirm": {"delete"}, "instance_id": {""}}
	wg.Go(func() { results <- request(t, s, "POST", "/projects/"+p.ID+"/sessions/saved/delete", body, cookie) })
	select {
	case <-c.deleting:
	case <-time.After(3 * time.Second):
		t.Fatal("delete did not preempt read")
	}
	if w := request(t, s, "POST", "/projects/"+p.ID+"/runtime/open", url.Values{"csrf": {csrf}, "confirm": {"activate"}}, cookie); w.Code != http.StatusConflict {
		t.Fatalf("activation overlapped deletion: %d", w.Code)
	}
	if w := request(t, s, "GET", "/projects/"+p.ID+"/sidebar-sessions", nil, cookie); w.Code != http.StatusConflict {
		t.Fatalf("sidebar overlapped deletion: %d", w.Code)
	}
	close(c.finish)
	wg.Wait()
	successes := 0
	for range 2 {
		if w := <-results; w.Code == 200 {
			successes++
		}
	}
	if successes != 1 || c.overlap || c.deletes != 1 || len(r.calls) != 0 || !reads.canceled[p.ID] {
		t.Fatalf("unsafe admission: successes %d overlap %v deletes %d calls %v", successes, c.overlap, c.deletes, r.calls)
	}
}
