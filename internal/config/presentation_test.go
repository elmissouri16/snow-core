package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveThemesReturnsAdaptivePathFreeCatalog(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()
	for _, dir := range []string{filepath.Join(global, "themes"), filepath.Join(project, ".snow", "themes")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(global, "themes", "ocean.yaml"), []byte("version: 1\nname: ocean\nextends: frost\ncolors:\n  accent: {light: '#111111', dark: '#eeeeee'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".snow", "themes", "ocean.yaml"), []byte("version: 1\nname: ocean\nextends: ember\ncolors:\n  success: {dark: '#00ff00'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	themes, diagnostics := ResolveThemes(global, project, true)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if got := themes[:4]; !slices.EqualFunc(got, []ResolvedTheme{{Name: "default"}, {Name: "frost"}, {Name: "ember"}, {Name: "aurora"}}, func(a, b ResolvedTheme) bool { return a.Name == b.Name }) {
		t.Fatalf("built-ins = %+v", got)
	}
	custom := themes[4]
	if custom.Name != "ocean" || custom.Scope != "project" || custom.Colors.Success != (AdaptiveColor{Light: "#00ff00", Dark: "#00ff00"}) {
		t.Fatalf("custom = %+v", custom)
	}
	if custom.Colors.Accent != builtInThemeColors["ember"].Accent {
		t.Fatalf("custom base accent = %+v", custom.Colors.Accent)
	}
}

func TestResolveKeybindingsPreservesEmergencyBindingsAndRejectsCollisions(t *testing.T) {
	effective, err := ResolveKeybindings(map[string][]string{"abort": {"alt+x"}}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, emergency := range []string{"alt+x", "ctrl+c", "esc"} {
		if !slices.Contains(effective["abort"], emergency) {
			t.Fatalf("abort = %v, missing %q", effective["abort"], emergency)
		}
	}
	if _, err := ResolveKeybindings(map[string][]string{"submit": {"ctrl+t"}}, nil, false); err == nil {
		t.Fatal("context collision accepted")
	}
}
