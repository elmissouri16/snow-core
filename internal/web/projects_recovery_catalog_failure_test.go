package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type unavailableRecoveryCatalog struct{ fakeCatalog }

func (*unavailableRecoveryCatalog) Messages(context.Context, Project, string, int) (CatalogMessages, error) {
	return CatalogMessages{}, errors.New("catalog unavailable")
}

func TestRecoveryCatalogFailurePreservesExplicitResumeTarget(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "recovery", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewRuntimeManager(t.Context(), "/unavailable-verification-worker", "", s.registry)
	t.Cleanup(func() { _ = manager.Close() })
	s.runtimes = manager
	s.catalog = &unavailableRecoveryCatalog{}
	response := request(t, s, http.MethodGet, "/?view=projects&project="+project.ID+"&session=interrupted-session", nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("history read: %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{"Session history is unavailable", `name="session_id" value="interrupted-session"`, "Resume session", `name="confirm"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("unavailable catalog lost explicit target %q", want)
		}
	}
	if _, ok := manager.Snapshot(project.ID); ok {
		t.Fatal("unavailable catalog autoactivated a recovery worker")
	}
}
