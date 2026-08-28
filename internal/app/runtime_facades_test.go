package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestConfigDiagnosticsFingerprintIgnoresUnrelatedThemeEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	themes := filepath.Join(home, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < config.ThemeFileLimit+8; i++ {
		if err := os.WriteFile(filepath.Join(themes, fmt.Sprintf("%03d.txt", i)), []byte("ignored"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{Cfg: config.Default(), ProjectInputRoot: t.TempDir()}
	a.Cfg.TUI.Theme = "ocean"
	if got := a.ConfigDiagnostics(); len(got) == 0 {
		t.Fatal("missing custom theme did not produce a diagnostic")
	}
	theme := "version: 1\nname: ocean\nextends: dark\ncolors:\n  accent: {light: '#112233', dark: '39'}\n"
	if err := os.WriteFile(filepath.Join(themes, "zzz-ocean.yaml"), []byte(theme), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range a.ConfigDiagnostics() {
		if diagnostic.Path == "tui.theme" && strings.Contains(diagnostic.Message, "missing or invalid") {
			t.Fatalf("diagnostics cache ignored valid theme after unrelated entries: %+v", diagnostic)
		}
	}
}

func TestConfigDiagnosticsAcceptsEveryBuiltInTheme(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	themes := append(config.BuiltInTUIThemes(), "dark", "light", "high-contrast", "nord", "dracula", "gruvbox")
	for _, theme := range themes {
		t.Run(theme, func(t *testing.T) {
			a := &App{Cfg: config.Default(), ProjectInputRoot: t.TempDir()}
			a.Cfg.TUI.Theme = theme
			for _, diagnostic := range a.ConfigDiagnostics() {
				if diagnostic.Path == "tui.theme" && strings.Contains(diagnostic.Message, "missing or invalid") {
					t.Fatalf("built-in theme %q rejected: %+v", theme, diagnostic)
				}
			}
		})
	}
}
