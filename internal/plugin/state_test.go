package plugin

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginStateIsolationPersistenceAndQuotaRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s := NewStateStore(path)
	if _, err := s.Call(t.Context(), "one", "project:a", "storage.set", []byte(`{"key":"value","value":{"count":1}}`)); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"two", "project:a"}, {"one", "project:b"}, {"one", "session:a"}} {
		raw, err := s.Call(t.Context(), pair[0], pair[1], "storage.get", []byte(`{"key":"value"}`))
		if err != nil || string(raw) != "null" {
			t.Fatalf("scope leak %s %v", raw, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = NewStateStore(path)
	defer s.Close()
	raw, err := s.Call(t.Context(), "one", "project:a", "storage.get", []byte(`{"key":"value"}`))
	if err != nil || string(raw) != `{"count":1}` {
		t.Fatalf("persistence %s %v", raw, err)
	}
	for i := range 16 {
		_, err := s.Call(t.Context(), "quota", "global", "storage.set", fmt.Appendf(nil, `{"key":"k%d","value":"%s"}`, i, strings.Repeat("a", 65500)))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Call(t.Context(), "quota", "global", "storage.set", fmt.Appendf(nil, `{"key":"overflow","value":"%s"}`, strings.Repeat("b", 65500))); err == nil {
		t.Fatal("quota not enforced")
	}
	raw, err = s.Call(t.Context(), "quota", "global", "storage.get", []byte(`{"key":"overflow"}`))
	if err != nil || string(raw) != "null" {
		t.Fatalf("quota write was not rolled back %s %v", raw, err)
	}
}
