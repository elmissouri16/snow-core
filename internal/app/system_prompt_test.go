package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSystemPromptFileEnforcesLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "system.md")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSystemPromptFile("system.md", root, "", 4); err == nil || !strings.Contains(err.Error(), "exceeds context_cap_bytes") {
		t.Fatalf("size limit error = %v", err)
	}
}

func TestLoadProjectSystemPromptRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "system.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := loadSystemPromptFile("system.md", root, root, 1024); err == nil || !strings.Contains(err.Error(), "contains a symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}
