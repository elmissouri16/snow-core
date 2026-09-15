package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestSavedSessionNavigationDoesNotSelectDifferentLiveSession(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "Navigation fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "current-instance", SessionID: "current-session", Status: "idle"}}
	s.runtimes = runtime
	path := "/?view=projects&project=" + project.ID
	stale := request(t, s, "GET", path+"&session=earlier-session", nil, cookie)
	if stale.Code != http.StatusOK || !strings.Contains(stale.Body.String(), "Another session owns this project") {
		t.Fatalf("stale navigation: %d %s", stale.Code, stale.Body.String())
	}
	for _, forbidden := range []string{`data-runtime="true"`, `data-runtime-open`} {
		if strings.Contains(stale.Body.String(), forbidden) {
			t.Fatalf("stale session link exposed controls: %s", forbidden)
		}
	}
	if catalog.calls != 0 || len(runtime.calls) != 0 {
		t.Fatal("navigation activated or queried a foreign session")
	}
	for _, suffix := range []string{"", "&session=current-session"} {
		current := request(t, s, "GET", path+suffix, nil, cookie)
		if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), `id="live-session"`) {
			t.Fatalf("explicit current navigation unavailable: %d", current.Code)
		}
	}
}
