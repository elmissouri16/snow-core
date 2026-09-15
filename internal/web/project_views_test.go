package web

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectInspectionHTTPAuthorizationAndReadOnlyViews(t *testing.T) {
	s, cookie, catalog := projectShell(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("<script>file content</script>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := s.registry.Add(t.Context(), "View", root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{}
	s.runtimes = runtime
	base := "/projects/" + project.ID + "/inspect/"
	csrf := csrfFor(t, s, cookie)
	for _, action := range []string{"files", "file", "changes", "diff"} {
		for _, tc := range []struct {
			cookie bool
			csrf   string
		}{{false, csrf}, {true, "wrong"}} {
			values := url.Values{"csrf": {tc.csrf}, "path": {"README.md"}, "kind": {"unstaged"}}
			w := request(t, s, "POST", base+action, values)
			if tc.cookie {
				w = request(t, s, "POST", base+action, values, cookie)
			}
			if tc.cookie && w.Code != http.StatusForbidden || !tc.cookie && (w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login") {
				t.Fatalf("unauthorized %s: %d", action, w.Code)
			}
		}
	}
	w := request(t, s, "POST", base+"files", url.Values{"csrf": {csrf}}, cookie)
	var listing InspectionFiles
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &listing) != nil || len(listing.Entries) != 1 {
		t.Fatalf("files: %d %s", w.Code, w.Body.String())
	}
	w = request(t, s, "POST", base+"file", url.Values{"csrf": {csrf}, "path": {"README.md"}}, cookie)
	var preview InspectionFile
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Text != "<script>file content</script>\n" {
		t.Fatalf("file: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), root) {
		t.Fatal("response leaked absolute host path")
	}
	for _, path := range []string{"../README.md", ".git/config", ".env.local", root} {
		w = request(t, s, "POST", base+"file", url.Values{"csrf": {csrf}, "path": {path}}, cookie)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("accepted unsafe path %q: %d", path, w.Code)
		}
	}
	if len(runtime.calls) != 0 || catalog.calls != 0 {
		t.Fatal("inspection activated runtime/catalog")
	}
	if w := request(t, s, "GET", base+"file?path=README.md", nil, cookie); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("path in GET accepted: %d", w.Code)
	}
	if w := request(t, s, "POST", base+"write", url.Values{"csrf": {csrf}}, cookie); w.Code != http.StatusNotFound {
		t.Fatal("unknown inspection action accepted")
	}
}
func TestProjectInspectionAdmissionAndUnavailableRoot(t *testing.T) {
	s, cookie, _ := projectShell(t)
	path := t.TempDir()
	p, err := s.registry.Add(t.Context(), "test", path)
	if err != nil {
		t.Fatal(err)
	}
	urlPath := "/projects/" + p.ID + "/inspect/files"
	values := url.Values{"csrf": {csrfFor(t, s, cookie)}}
	for range cap(s.inspectSlots) {
		s.inspectSlots <- struct{}{}
	}
	if w := request(t, s, "POST", urlPath, values, cookie); w.Code != http.StatusConflict {
		t.Fatalf("request queued: %d", w.Code)
	}
	for range cap(s.inspectSlots) {
		<-s.inspectSlots
	}
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(path+"-moved", path) })
	if w := request(t, s, "POST", urlPath, values, cookie); w.Code != http.StatusConflict {
		t.Fatalf("replaced root accepted: %d", w.Code)
	}
}
