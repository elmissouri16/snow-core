package web

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestHostFolderBrowserUsesHostPathsAndDirectoriesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "<project>"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "private-file"), []byte("never returned"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := readHostFolders(t.Context(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != canonical || len(page.Folders) != 1 || page.Folders[0].Name != "<project>" {
		t.Fatalf("host folders: %+v", page)
	}
	if page.Parent != filepath.Dir(canonical) {
		t.Fatal("host parent missing")
	}
	for _, path := range []string{"relative", "/no/such/snow-host-directory"} {
		if _, err := readHostFolders(t.Context(), path, 0); err == nil {
			t.Fatalf("accepted invalid host path %q", path)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readHostFolders(ctx, home, 0); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestHostFolderBrowserPagesWithoutReadingFiles(t *testing.T) {
	root := t.TempDir()
	for i := range 270 {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("project-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	first, err := readHostFolders(t.Context(), root, 0)
	if err != nil || len(first.Folders) != 256 || !first.HasMore || first.NextOffset != 256 {
		t.Fatalf("first page: %d %+v %v", len(first.Folders), first, err)
	}
	second, err := readHostFolders(t.Context(), root, first.NextOffset)
	if err != nil || len(second.Folders) != 14 || second.HasMore {
		t.Fatalf("second page: %+v %v", second, err)
	}
	if _, err := readHostFolders(t.Context(), root, 4097); err == nil {
		t.Fatal("unbounded offset")
	}
}

func TestHostFolderBrowserRequiresPairingAndCSRF(t *testing.T) {
	s := testShell(t)
	root := t.TempDir()
	form := url.Values{"path": {root}}
	if w := request(t, s, "POST", "/projects/folders", form); w.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	cookie := pairBrowser(t, s, s.initialCode)
	if w := request(t, s, "POST", "/projects/folders", form, cookie); w.Code != http.StatusForbidden {
		t.Fatalf("CSRF: %d", w.Code)
	}
	form.Set("csrf", csrfFor(t, s, cookie))
	w := request(t, s, "POST", "/projects/folders", form, cookie)
	var page hostFolders
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Path == "" {
		t.Fatalf("folder response: %d %s", w.Code, w.Body.String())
	}
}
