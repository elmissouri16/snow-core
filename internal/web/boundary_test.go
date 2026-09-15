package web

import (
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultPortBrowserAuthority(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:80", "http://[::1]:80"} {
		s, err := newShell(origin, "test")
		if err != nil {
			t.Fatal(err)
		}
		canonical := strings.TrimSuffix(origin, ":80")
		if s.origin != canonical {
			t.Fatalf("origin = %q, want %q", s.origin, canonical)
		}
		r := httptest.NewRequest("GET", "/login", nil)
		r.Host = strings.TrimPrefix(canonical, "http://")
		r.Header.Set("Origin", canonical)
		w := httptest.NewRecorder()
		s.handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("canonical browser authority rejected: %d", w.Code)
		}
	}
}

func TestWebAndClientDoNotImportAgentInternals(t *testing.T) {
	for _, root := range []string{".", "../../pkg/agentclient"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				value, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if strings.Contains(value, "snow-core/internal/") {
					t.Errorf("%s imports an internal implementation: %s", path, value)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Count(string(data), "\n") > 1000 {
				t.Errorf("%s exceeds the Go file line cap", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
