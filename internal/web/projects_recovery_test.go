//go:build darwin || linux

package web

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectRecoveryPresentationIsAuthorizedAndInert(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "Recovery project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "worker.log")
	manager := restartRecoveryManager(t, s.registry, log)
	s.runtimes = manager
	query := url.Values{"view": {"projects"}, "project": {project.ID}}
	const sessionID = "last-saved-session"
	wantURL := "/?" + url.Values{"view": {"projects"}, "project": {project.ID}, "session": {sessionID}}.Encode()
	for _, tc := range []struct {
		state RecoveryState
		text  string
	}{
		{RecoveryBound, "A saved conversation was previously opened. Opening it again requires explicit activation."},
		{RecoveryAdmissionUnknown, "Prompt admission is unknown. Review saved history before explicitly continuing; nothing will be replayed."},
		{RecoveryAdmitted, "Prompt was admitted, but completion was not observed. Admission does not prove it was saved. Review saved history before explicitly continuing."},
		{RecoveryCompleted, "Prompt completion was observed. Review saved history before explicitly continuing."},
		{RecoveryFailed, "Prompt failure was observed. Review saved history before explicitly retrying."},
		{RecoveryCanceled, "Prompt cancellation was observed. Review saved history before explicitly continuing."},
		{RecoveryRejected, "Prompt was not accepted. No retry was queued."},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			hint := RecoveryHint{SessionID: sessionID, State: tc.state, UpdatedAt: time.UnixMilli(1700000000123).UTC()}
			if err := s.registry.SaveRecovery(t.Context(), project.ID, hint); err != nil {
				t.Fatal(err)
			}
			before := catalog.calls
			unpaired := request(t, s, http.MethodGet, "/?"+query.Encode(), nil)
			if unpaired.Code != http.StatusSeeOther || unpaired.Header().Get("Location") != "/login" || strings.Contains(unpaired.Body.String(), sessionID) || strings.Contains(unpaired.Body.String(), tc.text) || catalog.calls != before {
				t.Fatal("unauthorized read exposed recovery or reached catalog")
			}
			data := pageData{View: "projects"}
			if err := s.projectData(t.Context(), query, &data); err != nil {
				t.Fatal(err)
			}
			if data.Recovery == nil || *data.Recovery != hint || data.Recovery.Message() != tc.text || data.RecoveryURL != wantURL || data.Live != nil {
				t.Fatalf("recovery projection: %+v", data)
			}
			page := request(t, s, http.MethodGet, "/?"+query.Encode(), nil, cookie)
			if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), tc.text) || !strings.Contains(page.Body.String(), `href="`+html.EscapeString(wantURL)+`"`) || !strings.Contains(page.Body.String(), `aria-label="Saved conversation recovery"`) {
				t.Fatalf("missing fixed recovery copy/navigation: %d %s", page.Code, page.Body.String())
			}
			selected := request(t, s, http.MethodGet, wantURL, nil, cookie)
			if selected.Code != http.StatusOK || !strings.Contains(selected.Body.String(), tc.text) {
				t.Fatal("matching saved session lost recovery hint")
			}
			unrelated := query.Clone()
			unrelated.Set("session", "unrelated-session")
			other := pageData{View: "projects"}
			if err := s.projectData(t.Context(), unrelated, &other); err != nil || other.Recovery != nil || other.RecoveryURL != "" || other.SessionID != "unrelated-session" {
				t.Fatalf("unrelated conversation inherited last-session evidence: %+v %v", other, err)
			}
			page = request(t, s, http.MethodGet, "/?"+unrelated.Encode(), nil, cookie)
			if page.Code != http.StatusOK || strings.Contains(page.Body.String(), `aria-label="Saved conversation recovery"`) || strings.Contains(page.Body.String(), tc.text) {
				t.Fatal("unrelated conversation rendered last-session recovery")
			}
			stored, found, err := manager.Recovery(t.Context(), project.ID)
			if err != nil || !found || stored != hint {
				t.Fatalf("reads changed evidence: %+v %v %v", stored, found, err)
			}
		})
	}
	restartRecoveryAssertCold(t, manager, []Project{project}, log)
}

func TestProjectRecoveryRejectsMissingChangedAndRemovedIdentity(t *testing.T) {
	for _, state := range []string{"missing", "changed", "removed"} {
		t.Run(state, func(t *testing.T) {
			s, cookie, catalog := projectShell(t)
			project, err := s.registry.Add(t.Context(), "Identity", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hint := RecoveryHint{SessionID: "private-recovery-session", State: RecoveryAdmitted, UpdatedAt: time.Now().UTC()}
			if err := s.registry.SaveRecovery(t.Context(), project.ID, hint); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(t.TempDir(), "worker.log")
			manager := restartRecoveryManager(t, s.registry, log)
			s.runtimes = manager
			if state == "removed" {
				if err := s.registry.Remove(t.Context(), project.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				// Keep the old inode alive; recreation cannot accidentally reuse it.
				moved := project.Path + "-original"
				if err := os.Rename(project.Path, moved); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(moved) })
				if state == "changed" {
					if err := os.Mkdir(project.Path, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			query := url.Values{"view": {"projects"}, "project": {project.ID}}
			data := pageData{View: "projects"}
			if err := s.projectData(t.Context(), query, &data); err == nil || data.Recovery != nil || data.RecoveryURL != "" {
				t.Fatalf("invalid identity exposed recovery: %+v %v", data, err)
			}
			page := request(t, s, http.MethodGet, "/?"+query.Encode(), nil, cookie)
			if strings.Contains(page.Body.String(), hint.SessionID) || strings.Contains(page.Body.String(), hint.Message()) || strings.Contains(page.Body.String(), `aria-label="Saved conversation recovery"`) || catalog.calls != 0 {
				t.Fatal("invalid identity disclosed recovery or reached catalog")
			}
			restartRecoveryAssertCold(t, manager, []Project{project}, log)
		})
	}
}
