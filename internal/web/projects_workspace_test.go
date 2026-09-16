package web

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type workspaceCatalog struct {
	page              CatalogSessions
	listed            []int
	readIDs           []string
	readOffsets       []int
	messagesErr       error
	messageErrors     map[string]error
	listErr           error
	historyHasMore    bool
	historyNextOffset int
}

func (c *workspaceCatalog) Sessions(_ context.Context, _ Project, offset int) (CatalogSessions, error) {
	c.listed = append(c.listed, offset)
	return c.page, c.listErr
}
func (c *workspaceCatalog) Messages(_ context.Context, _ Project, id string, offset int) (CatalogMessages, error) {
	c.readIDs = append(c.readIDs, id)
	c.readOffsets = append(c.readOffsets, offset)
	if err := c.messageErrors[id]; err != nil {
		return CatalogMessages{}, err
	}
	return CatalogMessages{Messages: []HistoryMessage{{ID: "message", Role: "assistant", Text: "Saved"}}, HasMore: c.historyHasMore, NextOffset: c.historyNextOffset}, c.messagesErr
}

func TestWorkspaceNavigationSelectsSavedOrEmptyWithoutActivation(t *testing.T) {
	s, _, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &workspaceCatalog{page: CatalogSessions{Sessions: []SessionSummary{{ID: "newest"}, {ID: "older"}}}}
	s.catalog = catalog
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	query := url.Values{"view": {"projects"}, "project": {project.ID}}
	for _, tc := range []struct {
		name       string
		suffix     url.Values
		wantID     string
		wantNew    bool
		wantList   int
		wantOffset int
	}{
		{name: "ordinary", wantID: "newest", wantList: 1},
		{name: "new", suffix: url.Values{"new": {"1"}}, wantNew: true},
		{name: "explicit", suffix: url.Values{"session": {"older"}, "offset": {"25"}}, wantID: "older", wantOffset: 25},
		{name: "workspace offset cannot skip newest", suffix: url.Values{"offset": {"25"}}, wantID: "newest", wantList: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := query.Clone()
			for k, v := range tc.suffix {
				q[k] = v
			}
			before := len(catalog.listed)
			reads := len(catalog.readIDs)
			data := pageData{View: "projects"}
			if err := s.projectData(t.Context(), q, &data); err != nil {
				t.Fatal(err)
			}
			if data.SessionID != tc.wantID || data.NewSession != tc.wantNew || (data.History != nil) != (tc.wantID != "") || len(catalog.listed)-before != tc.wantList {
				t.Fatalf("%+v", data)
			}
			if tc.wantList != 0 && (data.Sessions == nil || len(data.Sessions.Sessions) != 2) {
				t.Fatal("ordinary cold page lost session metadata seed")
			}
			if tc.wantID != "" && (catalog.readIDs[reads] != tc.wantID || catalog.readOffsets[reads] != tc.wantOffset) {
				t.Fatal("wrong saved target")
			}
		})
	}
	catalog.page = CatalogSessions{}
	data := pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err != nil || !data.NewSession || data.History != nil || data.SessionID != "" {
		t.Fatalf("empty: %+v %v", data, err)
	}
	catalog.messagesErr = errors.New("unavailable")
	query.Set("session", "recover")
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err == nil || data.SessionID != "recover" || data.History != nil {
		t.Fatalf("recovery: %+v %v", data, err)
	}
	if len(runtime.calls) != 0 {
		t.Fatal("navigation mutated runtime")
	}
}

func TestWorkspaceNewNavigationBoundedAndLiveMismatchSafe(t *testing.T) {
	s, _, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "current", Status: "idle"}}
	s.runtimes = runtime
	for _, suffix := range []string{"new=", "new=2", "new=1&new=1", "new=1&session=older", "new=1&offset=25"} {
		query, _ := url.ParseQuery("project=" + project.ID + "&" + suffix)
		data := pageData{View: "projects"}
		if err := s.projectData(t.Context(), query, &data); err == nil || data.History != nil || data.Live != nil {
			t.Fatalf("invalid intent %s: %+v %v", suffix, data, err)
		}
	}
	query := url.Values{"project": {project.ID}, "new": {"1"}}
	data := pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err != nil || data.Live == nil || data.NewSession || data.SessionID != "current" {
		t.Fatalf("live new: %+v %v", data, err)
	}
	query.Del("new")
	query.Set("session", "older")
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err == nil || data.Live != nil || data.RuntimeEnabled || data.SessionID != "older" {
		t.Fatalf("mismatch: %+v %v", data, err)
	}
	if len(runtime.calls) != 0 || catalog.calls != 0 {
		t.Fatal("GET mutated or queried a live workspace")
	}
}

func TestWorkspaceLastSessionHintColdPreferenceAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hint      string
		page      CatalogSessions
		errors    map[string]error
		listErr   error
		want      string
		wantReads []string
		wantErr   bool
	}{
		{name: "valid hint outside first page", hint: "last", page: CatalogSessions{Sessions: []SessionSummary{{ID: "newest", Name: "Newest"}}}, want: "last", wantReads: []string{"last"}},
		{name: "failed hint falls back", hint: "gone", page: CatalogSessions{Sessions: []SessionSummary{{ID: "newest", Name: "Newest"}}}, errors: map[string]error{"gone": errors.New("gone")}, want: "newest", wantReads: []string{"gone", "newest"}},
		{name: "failed newest hint skips failed candidate", hint: "newest", page: CatalogSessions{Sessions: []SessionSummary{{ID: "newest"}, {ID: "older"}}}, errors: map[string]error{"newest": errors.New("busy")}, want: "older", wantReads: []string{"newest", "older"}},
		{name: "read errors cannot imply empty", hint: "last", errors: map[string]error{"last": errors.New("busy")}, wantReads: []string{"last"}, wantErr: true},
		{name: "hint works if metadata list unavailable", hint: "last", listErr: errors.New("list unavailable"), want: "last", wantReads: []string{"last"}},
		{name: "both reads fail", hint: "last", listErr: errors.New("list unavailable"), errors: map[string]error{"last": errors.New("busy")}, wantReads: []string{"last"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := projectShell(t)
			project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			catalog := &workspaceCatalog{page: tc.page, messageErrors: tc.errors, listErr: tc.listErr}
			s.catalog = catalog
			data := pageData{View: "projects"}
			query := url.Values{"project": {project.ID}, "last_session": {tc.hint}}
			err = s.projectData(t.Context(), query, &data)
			if (err != nil) != tc.wantErr || data.SessionID != tc.want || data.NewSession || (data.History != nil) != (tc.want != "") || !slices.Equal(catalog.readIDs, tc.wantReads) {
				t.Fatalf("%+v reads=%v err=%v", data, catalog.readIDs, err)
			}
			if tc.listErr == nil && data.Sessions == nil {
				t.Fatal("cold metadata seed absent")
			}
		})
	}
}

func TestWorkspaceLastSessionHintStrictAndNeverSwitchAuthority(t *testing.T) {
	s, _, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "invalid/path", strings.Repeat("x", 129)} {
		data := pageData{View: "projects"}
		if err := s.projectData(t.Context(), url.Values{"project": {project.ID}, "last_session": {value}}, &data); err == nil {
			t.Fatalf("invalid hint %q accepted", value)
		}
	}
	data := pageData{View: "projects"}
	if err := s.projectData(t.Context(), url.Values{"project": {project.ID}, "last_session": {"one", "two"}}, &data); err == nil {
		t.Fatal("duplicate hint accepted")
	}
	query := url.Values{"project": {project.ID}, "last_session": {"older"}, "new": {"1"}}
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err != nil || !data.NewSession || data.History != nil || catalog.calls != 0 {
		t.Fatalf("new ignores hint: %+v %v", data, err)
	}
	runtime := &fakeRuntime{live: true, snapshot: RuntimeSnapshot{ProjectID: project.ID, InstanceID: "instance", SessionID: "current", Status: "idle"}}
	s.runtimes = runtime
	query.Del("new")
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err != nil || data.Live == nil || data.SessionID != "current" || catalog.calls != 0 {
		t.Fatalf("live ignores hint: %+v %v", data, err)
	}
	query.Set("session", "stale-explicit")
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err == nil || data.Live != nil || data.RuntimeEnabled {
		t.Fatalf("hint bypassed explicit mismatch: %+v %v", data, err)
	}
	runtime.live = false
	data = pageData{View: "projects"}
	if err := s.projectData(t.Context(), query, &data); err != nil || data.SessionID != "stale-explicit" || catalog.calls != 1 {
		t.Fatalf("explicit target ignores hint: %+v %v", data, err)
	}
	if len(runtime.calls) != 0 {
		t.Fatal("hint mutated runtime")
	}
}

func TestWorkspaceCatalogFailureIsNotRenderedAsEmptySession(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &workspaceCatalog{listErr: errors.New("PRIVATE catalog diagnostics"), messagesErr: errors.New("PRIVATE history diagnostics")}
	s.catalog = catalog
	backend := &fakeRuntime{}
	s.runtimes = backend
	base := "/?view=projects&project=" + project.ID
	for _, tc := range []struct {
		name, suffix string
		want, absent []string
	}{
		{
			name:   "ordinary catalog failure",
			want:   []string{"<h1>Workspace</h1>", "<h2>Sessions unavailable</h2>", "Saved sessions could not be read. Nothing has started.", "Starting below creates a new session; it does not resume an unread session.", `data-runtime-open`},
			absent: []string{"<h1>New session</h1>", "<h2>What would you like to work on?</h2>", "A new session in workspace.", `name="session_id"`, "Resume session"},
		},
		{
			name: "explicit failed history retains resume", suffix: "&session=interrupted",
			want:   []string{"<h2>Session history unavailable</h2>", `name="session_id" value="interrupted"`, "Resume session"},
			absent: []string{"<h2>Sessions unavailable</h2>", "<h1>New session</h1>", "<h2>What would you like to work on?</h2>", "Starting below creates a new session"},
		},
		{
			name: "explicit new skips unavailable catalog", suffix: "&new=1",
			want:   []string{"<h1>New session</h1>", "<h2>What would you like to work on?</h2>", "A new session in workspace."},
			absent: []string{"<h2>Sessions unavailable</h2>", "Starting below creates a new session", "Resume session"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := request(t, s, "GET", base+tc.suffix, nil, cookie)
			body := response.Body.String()
			if response.Code != http.StatusOK {
				t.Fatalf("read returned %d: %s", response.Code, body)
			}
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("missing state-specific presentation %q", want)
				}
			}
			for _, absent := range append(tc.absent, "PRIVATE") {
				if strings.Contains(body, absent) {
					t.Errorf("misleading or private presentation %q", absent)
				}
			}
			if len(backend.calls) != 0 {
				t.Fatal("read activated a runtime or sent work")
			}
		})
	}
	if len(catalog.listed) != 1 || !slices.Equal(catalog.readIDs, []string{"interrupted"}) {
		t.Fatalf("navigation retried failed reads or read explicit-new history: lists=%v histories=%v", catalog.listed, catalog.readIDs)
	}
	catalog.listErr = nil
	response := request(t, s, "GET", base, nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<h1>New session</h1>") || !strings.Contains(response.Body.String(), "<h2>What would you like to work on?</h2>") || strings.Contains(response.Body.String(), "<h2>Sessions unavailable</h2>") || len(backend.calls) != 0 {
		t.Fatal("genuinely empty catalog lost its passive new-session presentation")
	}
}

func TestWorkspaceSavedHistoryPaginationUsesNativeWorkspaceNavigation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog := &workspaceCatalog{historyHasMore: true, historyNextOffset: 25}
	s.catalog = catalog
	backend := &fakeRuntime{}
	s.runtimes = backend
	response := request(t, s, "GET", "/?view=projects&project="+project.ID+"&session=saved", nil, cookie)
	nextURL := "/?" + url.Values{"view": {"projects"}, "project": {project.ID}, "session": {"saved"}, "offset": {"25"}}.Encode()
	escaped := html.EscapeString(nextURL)
	link := `<a class="button" href="` + escaped + `" data-snow-navigation="">Next page →</a>`
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), link) {
		t.Fatal("saved history pagination lost its canonical native workspace navigation")
	}
	response = request(t, s, "GET", nextURL, nil, cookie)
	if response.Code != http.StatusOK || !slices.Equal(catalog.readIDs, []string{"saved", "saved"}) || !slices.Equal(catalog.readOffsets, []int{0, 25}) || len(catalog.listed) != 0 || len(backend.calls) != 0 {
		t.Fatalf("pagination changed target, queried inventory, or activated runtime: ids=%v offsets=%v", catalog.readIDs, catalog.readOffsets)
	}
}
