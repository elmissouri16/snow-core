package builtin

import (
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
