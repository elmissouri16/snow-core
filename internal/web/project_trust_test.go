package web

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestProjectTrustActivationAndRevocation(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "Remembered workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	page := "/?view=projects&project=" + project.ID + "&new=1"
	open := "/projects/" + project.ID + "/runtime/open"
	revoke := "/projects/" + project.ID + "/trust/revoke"
	trusted := url.Values{"csrf": {csrf}, "confirm": {"trusted"}}
	if w := request(t, s, "POST", open, trusted, cookie); w.Code != http.StatusConflict || len(backend.calls) != 0 {
		t.Fatal("forged remembered trust admitted")
	}
	body := request(t, s, "GET", page, nil, cookie).Body.String()
	if !strings.Contains(body, `name="confirm" type="checkbox"`) || !strings.Contains(body, `name="remember_trust" value="project"`) || len(backend.calls) != 0 {
		t.Fatal("first visit must request explicit remembered consent without activation")
	}
	for _, disclosure := range []string{"configuration and instructions", "connects enabled MCP servers", "model-directed subagents", "host account’s privileges, not in a sandbox", "Browsing does not start an agent or MCP server", "Trust does not grant tools Allow permissions", "New sessions start in Ask"} {
		if !strings.Contains(body, disclosure) {
			t.Fatalf("first-start disclosure missing: %q", disclosure)
		}
	}
	saved := request(t, s, "GET", "/?view=projects&project="+project.ID+"&session=saved-id", nil, cookie).Body.String()
	if !strings.Contains(saved, "Resume session") || !strings.Contains(saved, "Resuming restores saved session permissions. A saved Allow policy skips tool approval prompts.") || len(backend.calls) != 0 {
		t.Fatal("saved-session browsing must retain restored-policy warning without activation")
	}
	form := url.Values{"csrf": {csrf}, "confirm": {"activate"}, "remember_trust": {"project"}}
	if w := request(t, s, "POST", open, form, cookie); w.Code != http.StatusOK || len(backend.calls) != 1 {
		t.Fatalf("explicit trust/start status %d", w.Code)
	}
	p, err := s.registry.Lookup(t.Context(), project.ID)
	if err != nil || !p.Trusted || !p.TrustRemembered {
		t.Fatal("explicit consent not persisted")
	}
	// A fresh inactive runtime owner simulates the surface after restart. Durable
	// registry reopen/identity behavior is separately covered by registry tests.
	backend = &skillsHTTPRuntime{}
	s.runtimes = backend
	body = request(t, s, "GET", page, nil, cookie).Body.String()
	if strings.Contains(body, `name="confirm" type="checkbox"`) || !strings.Contains(body, `name="confirm" value="trusted"`) || !strings.Contains(body, "Start session") || len(backend.calls) != 0 {
		t.Fatal("remembered startup must be compact, revocable, and passive")
	}
	var shell shellFrontendProps
	if err := json.Unmarshal([]byte(reactPageProps(t, body, "shell")), &shell, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if shell.CSRF != csrf || len(shell.Projects) != 1 || shell.Projects[0].ID != project.ID || !shell.Projects[0].TrustRemembered {
		t.Fatal("React Settings lacks the exact project/CSRF/trust revocation presentation")
	}
	if w := request(t, s, "POST", open, url.Values{"csrf": {csrf}}, cookie); w.Code != http.StatusBadRequest || len(backend.calls) != 0 {
		t.Fatal("remembered trust bypassed explicit activation")
	}
	if w := request(t, s, "POST", open, trusted, cookie); w.Code != http.StatusOK || len(backend.calls) != 1 {
		t.Fatal("explicit remembered start failed")
	}
	// Forgetting trust neither stops the current worker nor rewrites permissions.
	if w := request(t, s, "POST", revoke, url.Values{"csrf": {csrf}, "confirm": {"revoke"}}, cookie); w.Code != http.StatusSeeOther || !backend.live || len(backend.calls) != 1 {
		t.Fatal("revocation affected the live worker or failed")
	}
	if w := request(t, s, "POST", open, trusted, cookie); w.Code != http.StatusConflict || len(backend.calls) != 1 {
		t.Fatal("stale compact form reused revoked consent")
	}
	backend.live = false
	body = request(t, s, "GET", page, nil, cookie).Body.String()
	if !strings.Contains(body, `name="confirm" type="checkbox"`) {
		t.Fatal("revocation did not restore confirmation")
	}
}

func TestProjectTrustHTTPFieldsAndAuthority(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{}
	s.runtimes = backend
	csrf := csrfFor(t, s, cookie)
	open := "/projects/" + project.ID + "/runtime/open"
	for _, fields := range []url.Values{
		{"csrf": {csrf}, "remember_trust": {"project"}},
		{"csrf": {csrf}, "confirm": {"activate", "trusted"}, "remember_trust": {"project"}},
		{"csrf": {csrf}, "confirm": {"activate"}, "remember_trust": {"project", "project"}},
		{"csrf": {csrf}, "confirm": {"activate"}, "remember_trust": {"all"}},
		{"csrf": {csrf}, "confirm": {"trusted"}, "remember_trust": {"project"}},
		{"csrf": {csrf}, "confirm": {"activate"}, "remember_trust": {"project"}, "permission_mode": {"allow"}},
		{"csrf": {"wrong"}, "confirm": {"activate"}, "remember_trust": {"project"}},
	} {
		if w := request(t, s, "POST", open, fields, cookie); w.Code < 400 {
			t.Fatal("invalid trust/activation fields accepted")
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("rejected consent started a runtime")
	}
	p, _ := s.registry.Lookup(t.Context(), project.ID)
	if p.TrustRemembered {
		t.Fatal("invalid consent stored trust")
	}
	// Legacy one-time consent remains explicit and does not migrate into trust.
	if w := request(t, s, "POST", open, url.Values{"csrf": {csrf}, "confirm": {"activate"}}, cookie); w.Code != http.StatusOK {
		t.Fatal("one-time activation failed")
	}
	p, _ = s.registry.Lookup(t.Context(), project.ID)
	if p.TrustRemembered {
		t.Fatal("one-time activation implicitly remembered trust")
	}
	if err := s.registry.RememberProjectTrust(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	revoke := "/projects/" + p.ID + "/trust/revoke"
	for _, fields := range []url.Values{{"csrf": {csrf}}, {"csrf": {"wrong"}, "confirm": {"revoke"}}, {"csrf": {csrf}, "confirm": {"revoke", "revoke"}}, {"csrf": {csrf}, "confirm": {"revoke"}, "project": {"other"}}} {
		if w := request(t, s, "POST", revoke, fields, cookie); w.Code < 400 {
			t.Fatal("invalid revocation accepted")
		}
	}
	request(t, s, "GET", revoke, nil, cookie)
	request(t, s, "POST", revoke, url.Values{"csrf": {csrf}, "confirm": {"revoke"}})
	p, _ = s.registry.Lookup(t.Context(), p.ID)
	if !p.Trusted || len(backend.calls) != 1 {
		t.Fatal("unauthorized revoke or navigation changed trust/runtime")
	}
}

func TestProjectTrustHTTPChangedFolderAndStorageFailure(t *testing.T) {
	for _, scenario := range []string{"replaced", "storage"} {
		t.Run(scenario, func(t *testing.T) {
			s, cookie, _ := projectShell(t)
			folder := filepath.Join(t.TempDir(), "workspace")
			if err := os.Mkdir(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			p, err := s.registry.Add(t.Context(), "workspace", folder)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.registry.RememberProjectTrust(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			backend := &skillsHTTPRuntime{}
			s.runtimes = backend
			csrf := csrfFor(t, s, cookie)
			if scenario == "replaced" {
				if err := os.Rename(folder, folder+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(folder, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if _, err := s.registry.db.ExecContext(t.Context(), "DROP TABLE project_trust"); err != nil {
				t.Fatal(err)
			}
			w := request(t, s, "POST", "/projects/"+p.ID+"/runtime/open", url.Values{"csrf": {csrf}, "confirm": {"trusted"}}, cookie)
			if w.Code < 400 || len(backend.calls) != 0 {
				t.Fatal("changed folder or unreadable trust storage admitted activation")
			}
		})
	}
}

// The cold composer is a tab-local draft, not part of native activation form
// submission. JavaScript may enable editing, but explicit Start never sends it.
func TestProjectActivationColdDraftIsUnnamedDisabledAndPassive(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	project, err := s.registry.Add(t.Context(), "workspace", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &skillsHTTPRuntime{}
	s.runtimes = backend
	response := request(t, s, "GET", "/?view=projects&project="+project.ID+"&new=1", nil, cookie)
	if response.Code != http.StatusOK || catalog.calls != 0 || len(backend.calls) != 0 {
		t.Fatal("empty-session navigation read history or activated a runtime")
	}
	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	attributes := func(node *html.Node) map[string]string {
		result := make(map[string]string, len(node.Attr))
		for _, attr := range node.Attr {
			result[attr.Key] = attr.Val
		}
		return result
	}
	var draft *html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "textarea" && attributes(node)["id"] == "workspace-prompt" {
			if draft != nil {
				t.Fatal("duplicate cold composer")
			}
			draft = node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if draft == nil {
		t.Fatal("cold composer is absent")
	}
	attrs := attributes(draft)
	_, disabled := attrs["disabled"]
	_, named := attrs["name"]
	if !disabled || named || attrs["maxlength"] != "65536" || attrs["data-draft-project"] != project.ID || attrs["data-draft-session"] != "" || draft.FirstChild != nil {
		t.Fatalf("cold draft is not empty, bounded, disabled and excluded from native submission: %v", attrs)
	}
	form := draft.Parent
	for form != nil && (form.Type != html.ElementNode || form.Data != "form") {
		form = form.Parent
	}
	if form == nil {
		t.Fatal("cold draft activation form is absent")
	}
	attrs = attributes(form)
	if attrs["method"] != "post" || attrs["action"] != "/projects/"+project.ID+"/runtime/open" {
		t.Fatalf("cold composer must use explicit activation, not prompt submission: %v", attrs)
	}
	if _, ok := attrs["data-runtime-open"]; !ok {
		t.Fatalf("cold composer is missing the native activation owner: %v", attrs)
	}
	if !strings.Contains(response.Body.String(), "Start, then review and send.") {
		t.Fatal("separate Start and Send explanation is absent")
	}
}
