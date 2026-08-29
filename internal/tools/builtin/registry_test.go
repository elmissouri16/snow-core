package builtin

import (
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestRegisterBuiltins_RegistersAll(t *testing.T) {
	reg := tools.NewRegistry()
	if err := RegisterBuiltins(reg, Options{Roots: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range reg.Schemas() {
		names[s.Name] = true
	}
	for _, want := range []string{"read", "write", "edit", "bash", "grep", "glob", "ask_user", "request_user_input", "update_plan", "webfetch"} {
		if !names[want] {
			t.Errorf("tool %q not registered", want)
		}
	}
	desc, ok := reg.Descriptor("webfetch")
	if !ok || desc.Risk != permission.RiskNet || desc.Schema.Discovery == nil || desc.Schema.Discovery.Mode != protocol.ToolDiscoveryDeferred {
		t.Fatalf("webfetch descriptor = %+v", desc)
	}
	ask, ok := reg.Descriptor("ask_user")
	if !ok || ask.Risk != permission.RiskRead || ask.Schema.Discovery != nil {
		t.Fatalf("ask_user descriptor = %+v", ask)
	}
}

func TestRegisterBuiltins_DescriptionsDocumentOperationalBoundaries(t *testing.T) {
	reg := tools.NewRegistry()
	if err := RegisterBuiltins(reg, Options{Roots: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	wantPhrases := map[string][]string{
		"read":               {"one exact regular file path", "use glob", "grep to locate content", "nonexistent path returns an error", "is not created", "Rejects invalid UTF-8", "initial NUL-byte probe", "1-based line number"},
		"write":              {"atomically replace", "complete content", "Preserves existing file permissions"},
		"edit":               {"literal text", "exactly once", "replace_all", "8 MiB"},
		"bash":               {"POSIX sh", "OS privileges", "not confined to file-tool roots", "process_start"},
		"grep":               {"Go RE2", "ignore rules by default", "never follows symlinks"},
		"glob":               {"regular-file paths", "** matches zero or more directories", "never followed"},
		"ask_user":           {"interactive user", "free-form answers", "custom answer", "Other in the TUI", "no interactive input surface"},
		"request_user_input": {"interactive user", "free-form answers", "custom answer", "Other in the TUI", "no interactive input surface"},
		"update_plan":        {"current task TODO/checklist", "At most one step", "does not enter Plan mode"},
		"webfetch":           {"public HTTP(S) URL", "private or non-HTTP(S) destinations are blocked", "does not execute JavaScript"},
	}
	for name, phrases := range wantPhrases {
		descriptor, ok := reg.Descriptor(name)
		if !ok {
			t.Fatalf("descriptor %q missing", name)
		}
		for _, phrase := range phrases {
			if !strings.Contains(descriptor.Schema.Description, phrase) {
				t.Errorf("%s description missing %q: %q", name, phrase, descriptor.Schema.Description)
			}
		}
	}
}

func TestRegisterBuiltins_DuplicateFails(t *testing.T) {
	reg := tools.NewRegistry()
	if err := RegisterBuiltins(reg, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterBuiltins(reg, Options{}); err == nil {
		t.Error("duplicate registration should fail")
	}
}

func TestRegisterBuiltins_OptionsApplied(t *testing.T) {
	reg := tools.NewRegistry()
	dir := t.TempDir()
	if err := RegisterBuiltins(reg, Options{
		MaxOutputBytes:   999,
		SearchMaxMatches: 7,
		GlobMaxResults:   8,
		BashTimeout:      5 * time.Second,
		Roots:            []string{dir},
	}); err != nil {
		t.Fatal(err)
	}
	read, _ := reg.Get("read")
	if got := read.(*Read).MaxOutputBytes; got != 999 {
		t.Errorf("read MaxOutputBytes = %d, want 999", got)
	}
	bash, _ := reg.Get("bash")
	if got := bash.(*Bash).MaxOutputBytes; got != 999 {
		t.Errorf("bash MaxOutputBytes = %d, want 999", got)
	}
	grep, _ := reg.Get("grep")
	if got := grep.(*Grep).MaxOutputBytes; got != 999 {
		t.Errorf("grep MaxOutputBytes = %d, want 999", got)
	}
	glob, _ := reg.Get("glob")
	if got := glob.(*Glob).MaxOutputBytes; got != 999 {
		t.Errorf("glob MaxOutputBytes = %d, want 999", got)
	}
	if got := grep.(*Grep).MaxMatches; got != 7 {
		t.Errorf("grep MaxMatches = %d, want 7", got)
	}
	if got := glob.(*Glob).MaxResults; got != 8 {
		t.Errorf("glob MaxResults = %d, want 8", got)
	}
	webfetch, _ := reg.Get("webfetch")
	if got := webfetch.(*WebFetch).MaxOutputBytes; got != 999 {
		t.Errorf("webfetch MaxOutputBytes = %d, want 999", got)
	}
	if got := bash.(*Bash).Timeout; got != 5*time.Second {
		t.Errorf("bash Timeout = %s, want 5s", got)
	}
}
