package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuxiliarySearchMergeAndFallback(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(global, "search.yaml"), []byte("version: 1\nhidden: true\nexclude: ['*.gen']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".snow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".snow", "search.yaml"), []byte("version: 1\nrespect_gitignore: false\nexclude: ['tmp/**']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, diagnostics := LoadSearchPolicy(global, project, true)
	if len(diagnostics) != 0 || !got.Hidden || got.RespectGitignore || len(got.Exclude) != 2 {
		t.Fatalf("policy=%+v diagnostics=%+v", got, diagnostics)
	}
	if err := os.WriteFile(filepath.Join(project, ".snow", "search.yaml"), []byte("version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, diagnostics = LoadSearchPolicy(global, project, true)
	if len(diagnostics) != 1 || !got.RespectGitignore || !got.Hidden {
		t.Fatalf("fallback=%+v diagnostics=%+v", got, diagnostics)
	}
}

func TestAuxiliaryThemesPrecedenceReservedAndBounded(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	for _, root := range []string{filepath.Join(global, "themes"), filepath.Join(project, ".snow", "themes")} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	base := "version: 1\nname: ocean\nextends: dark\ncolors:\n  accent: {light: '#112233', dark: '39'}\n"
	if err := os.WriteFile(filepath.Join(global, "themes", "ocean.yaml"), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	projectTheme := "version: 1\nname: ocean\nextends: light\ncolors:\n  success: {light: '28', dark: '#00ff00'}\n"
	if err := os.WriteFile(filepath.Join(project, ".snow", "themes", "ocean.yaml"), []byte(projectTheme), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "themes", "bad.yaml"), []byte("version: 1\nname: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	themes, diagnostics := LoadThemes(global, project, true)
	if themes["ocean"].Extends != "light" || len(diagnostics) != 1 {
		t.Fatalf("themes=%+v diagnostics=%+v", themes, diagnostics)
	}
}

func TestAuxiliaryKeybindingsProjectOverridesAndStrictFields(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	_ = os.MkdirAll(filepath.Join(project, ".snow"), 0o755)
	_ = os.WriteFile(filepath.Join(global, "keybindings.yaml"), []byte("version: 1\nbindings:\n  submit: [ctrl+s]\n"), 0o600)
	_ = os.WriteFile(filepath.Join(project, ".snow", "keybindings.yaml"), []byte("version: 1\nbindings:\n  submit: [enter]\n"), 0o600)
	got, diagnostics := LoadKeybindings(global, project, true)
	if len(diagnostics) != 0 || got.Bindings["submit"][0] != "enter" {
		t.Fatalf("got=%+v diagnostics=%+v", got, diagnostics)
	}
	for _, invalid := range []string{
		"version: 1\nbindings:\n  not_an_action: [x]\n",
		"version: 1\nbindings:\n  submit: [esc]\n",
		"version: 1\nbindings:\n  follow_up: [ctrl+c]\n",
	} {
		_ = os.WriteFile(filepath.Join(project, ".snow", "keybindings.yaml"), []byte(invalid), 0o600)
		got, diagnostics = LoadKeybindings(global, project, true)
		if len(diagnostics) != 1 || got.Bindings["submit"][0] != "ctrl+s" {
			t.Fatalf("fallback=%+v diagnostics=%+v", got, diagnostics)
		}
	}
}

func TestProjectAuxiliaryRejectsSymlinkedSnowParentAndOversize(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "search.yaml"), []byte("version: 1\nhidden: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, ".snow")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, diagnostics := LoadSearchPolicy(t.TempDir(), project, true)
	if len(diagnostics) == 0 || got.Hidden {
		t.Fatalf("symlinked project policy loaded: %+v diagnostics=%+v", got, diagnostics)
	}
	global := t.TempDir()
	if err := os.WriteFile(filepath.Join(global, "search.yaml"), make([]byte, AuxiliaryFileLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = LoadSearchPolicy(global, "", false)
	if len(diagnostics) == 0 {
		t.Fatal("oversized auxiliary file accepted")
	}
}

func TestDefaultSearchPolicy(t *testing.T) {
	got := DefaultSearchPolicy()
	if !got.RespectGitignore || !got.RespectIgnore || got.Hidden || len(got.GeneratedDirs) == 0 {
		t.Fatalf("%+v", got)
	}
}
