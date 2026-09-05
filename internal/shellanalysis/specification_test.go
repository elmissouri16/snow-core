package shellanalysis

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
)

func TestCommandSpecificationValidation(t *testing.T) {
	if specificationError != nil || pathSpecificationError != nil {
		t.Fatalf("invalid built-in specifications: %v %v", specificationError, pathSpecificationError)
	}
	for _, source := range []string{
		`[{"names":["x"],"handler":"paths","typo":true}]`,
		`[{"names":["x"],"handler":"missing"}]`,
		`[{"names":["x","x"],"handler":"paths"}]`,
		`[{"names":["x"],"handler":"paths","options":[{"names":["-f","-f"]}]}]`,
	} {
		if _, err := compileSpecifications([]byte(source)); err == nil {
			t.Fatal("malformed specification accepted")
		}
	}
}

func TestGrepOptionRoles(t *testing.T) {
	root, home := canonicalTempDir(t), canonicalTempDir(t)
	for _, tc := range []struct {
		command string
		paths   []string
	}{
		{`grep needle README`, []string{"README"}},
		{`grep -e needle README`, []string{"README"}},
		{`grep -ineedle README`, []string{"README"}},
		{`grep --regexp=needle README`, []string{"README"}},
		{`grep -f patterns README`, []string{"patterns", "README"}},
		{`grep -ifpatterns README`, []string{"patterns", "README"}},
		{`grep -n -A 3 -- needle -file`, []string{"-file"}},
		{`grep -e needle -- -file other`, []string{"-file", "other"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			got, err := Analyze(t.Context(), tc.command, root, []string{root}, home)
			if err != nil {
				t.Fatal(err)
			}
			want := make([]string, len(tc.paths))
			for i, path := range tc.paths {
				want[i] = filepath.Join(root, path)
			}
			slices.Sort(want)
			if got.Unknown || !slices.Equal(want, got.Paths) {
				t.Fatalf("paths=%v unknown=%v want=%v", got.Paths, got.Unknown, want)
			}
		})
	}
}

func TestUnmodeledSemanticsCannotBeRemembered(t *testing.T) {
	root, home := canonicalTempDir(t), canonicalTempDir(t)
	for _, command := range []string{
		`cat --future-option=value file`, `cat $HOME/file`, `cat *.txt`,
		`cat "$MISSING"suffix`, `./cat file`, `env -C sub cat file`,
		`env HOME=/other sh -c 'cat "$HOME/file"'`, `git remote add name url`,
		`git -C sub push origin branch`, `curl https://example.invalid`,
		`for x in a b; do cat "$x"; done`, `read value; cat "$value"`,
		`value=a; set -- b; cat "$value"`, `printf -v value text; cat "$value"`,
		`find . -name '*.txt'`, `cp -R src dst`,
	} {
		t.Run(command, func(t *testing.T) {
			got, err := Analyze(t.Context(), command, root, []string{root}, home)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Unknown || got.Rememberable {
				t.Fatal("unmodeled semantics received reusable approval")
			}
		})
	}
}

func TestApprovalScopeIncludesArgumentsDirectoryAndPolicy(t *testing.T) {
	root, home := canonicalTempDir(t), canonicalTempDir(t)
	base, err := Analyze(t.Context(), "cat file", root, []string{root}, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source, cwd string
		opts        Options
	}{
		{"cat -n file", root, Options{}},
		{"cat file", filepath.Join(root, "sub"), Options{}},
		{"cat file", root, Options{ProtectedPaths: []string{filepath.Join(root, "private")}}},
	} {
		got, err := AnalyzeWithOptions(t.Context(), tc.source, tc.cwd, []string{root}, home, tc.opts)
		if err != nil {
			t.Fatal(err)
		}
		if got.ScopeKey == base.ScopeKey {
			t.Fatal("different invocation or policy reused scope")
		}
	}
}

func TestAdditionalProtectedPathsAreAdditiveAndBounded(t *testing.T) {
	root, home := canonicalTempDir(t), canonicalTempDir(t)
	opts := Options{ProtectedPaths: []string{filepath.Join(root, "private")}}
	for _, command := range []string{`cat private/input`, `printf x > private/output`, `rm private/input`} {
		got, err := AnalyzeWithOptions(t.Context(), command, root, []string{root}, home, opts)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := (permission.DefaultPolicy{}).Evaluate(t.Context(), permission.Request{Effects: got.Effects, Capabilities: got.Capabilities})
		if err != nil || !decision.Denied {
			t.Fatal("operator protected resource was not denied")
		}
	}
	for _, paths := range [][]string{{"relative"}, {strings.Repeat("/", maxValueBytes+1)}, make([]string, 129)} {
		if _, err := AnalyzeWithOptions(t.Context(), "true", root, []string{root}, home, Options{ProtectedPaths: paths}); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestShellAnalysisCancellationAndBounds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Analyze(ctx, "cat file", t.TempDir(), nil, ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := Analyze(t.Context(), strings.Repeat("x", maxSourceBytes+1), t.TempDir(), nil, ""); err == nil {
		t.Fatal("source limit ignored")
	}
	got, err := Analyze(t.Context(), strings.Repeat("command ", 40)+"true", t.TempDir(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Capabilities, permission.CapabilityAnalysisIncomplete) {
		t.Fatal("wrapper bound not enforced")
	}
}

func FuzzAnalyzeConservativeBounds(f *testing.F) {
	for _, source := range []string{`cat README`, `cat *.txt`, `if true; then x=b; fi; cat "$x"`, `printf x > output`, `env -C sub cat input`} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > maxSourceBytes {
			t.Skip()
		}
		got, err := Analyze(t.Context(), source, "/", []string{"/"}, "")
		if err != nil {
			return
		}
		if got.Unknown && got.Rememberable {
			t.Fatal("unknown analysis is rememberable")
		}
		if len(got.Effects) > maxEffects || got.ScopeKey == "" {
			t.Fatal("invalid analysis bounds or scope")
		}
	})
}

func BenchmarkAnalyzeShell(b *testing.B) {
	root, home := b.TempDir(), b.TempDir()
	for _, source := range []string{`cat README.md`, `grep -n -e TODO -- README.md`, `git status --short`, `cat a b c d e f g h`} {
		b.Run(source, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Analyze(b.Context(), source, root, []string{root}, home); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
