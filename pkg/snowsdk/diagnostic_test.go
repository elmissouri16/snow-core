package snowsdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsReportsMissingSelectedCustomTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"tui":{"theme":"missing"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	diagnostics, err := s.Diagnostics()
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v err=%v", diagnostics, err)
	}
}

func TestDiagnosticsReturnsIndependentAuxiliaryWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "search.yaml"), []byte("version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), Options{Provider: "fake", NoSession: true, PermissionMode: "deny", CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Diagnostics()
	if err != nil || len(first) != 1 {
		t.Fatalf("diagnostics=%+v err=%v", first, err)
	}
	first[0].Message = "mutated"
	second, err := s.Diagnostics()
	if err != nil || second[0].Message == "mutated" {
		t.Fatalf("snapshot=%+v err=%v", second, err)
	}
}
