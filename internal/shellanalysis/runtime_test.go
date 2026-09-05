package shellanalysis

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
)

func TestShellRuntimeEffectsAreCoveredOrUnknown(t *testing.T) {
	for _, tc := range []struct{ name, command, actual string }{
		{"conditional state", `target=first; if true; then target=second; fi; cat "$target"`, "second"},
		{"pipeline cwd", `cd sub | cat; cat first`, "first"},
		{"glob expansion", `cat *.txt`, "one.txt"},
		{"unset state", `target=first; unset target; cat "${target}second"`, "second"},
		{"empty assignment", `target=first; target=; cat "${target}second"`, "second"},
		{"assignment ordering", `a=first; a=second b="$a"; cat "$b"`, "second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			for _, name := range []string{"first", "second", "one.txt"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := Analyze(t.Context(), tc.command, root, []string{root}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", tc.command)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != tc.actual {
				t.Fatalf("fixture failed: output=%q err=%v", output, err)
			}
			actual := filepath.Join(root, tc.actual)
			if !got.Unknown && got.Rememberable && !slices.Contains(got.Paths, actual) {
				t.Errorf("verified actual read is absent from complete, rememberable analysis; analyzed basenames=%v", pathBasenames(got.Paths))
			}
		})
	}
}

func pathBasenames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func TestReadWriteRedirectionIncludesCreation(t *testing.T) {
	root := canonicalTempDir(t)
	got, err := Analyze(t.Context(), `true <> created`, root, []string{root}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", `true <> created`)
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Capabilities, permission.CapabilityFilesystemWriteWorkspace) {
		t.Errorf("file creation verified; analysis omits write capability: %v", got.Capabilities)
	}
}

func TestGitListingAndMutationDoNotShareApproval(t *testing.T) {
	root := canonicalTempDir(t)
	home := canonicalTempDir(t)
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	analyses := make([]permission.Analysis, 0, 2)
	for _, source := range []string{`git remote`, `git remote add review https://example.invalid/repo`} {
		got, err := Analyze(t.Context(), source, root, []string{root}, home)
		if err != nil {
			t.Fatal(err)
		}
		analyses = append(analyses, got)
	}
	cmd = exec.CommandContext(t.Context(), "git", "-C", root, "remote", "add", "review", "https://example.invalid/repo")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %s %v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil || !strings.Contains(string(data), "example.invalid/repo") {
		t.Fatal("fixture did not change local git config")
	}
	if analyses[0].ScopeKey == analyses[1].ScopeKey && analyses[1].Rememberable {
		t.Errorf("read-only listing and verified configuration write have identical rememberable scope; write capabilities=%v", analyses[1].Capabilities)
	}
	if !slices.Contains(analyses[1].Capabilities, permission.CapabilityGitWrite) {
		t.Fatal("Git configuration mutation lacks a write effect")
	}
}

func TestPatternsAndQuotedTildeAreNotExpandedPaths(t *testing.T) {
	root := canonicalTempDir(t)
	home := canonicalTempDir(t)
	for _, tc := range []struct{ name, command, incorrect string }{
		{"grep pattern", `grep needle README`, filepath.Join(root, "needle")},
		{"quoted tilde", `cat '~/literal'`, filepath.Join(home, "literal")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Analyze(t.Context(), tc.command, root, []string{root}, home)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(got.Paths, tc.incorrect) {
				t.Errorf("non-path pattern or quoted literal is misreported as concrete filesystem resource")
			}
		})
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
