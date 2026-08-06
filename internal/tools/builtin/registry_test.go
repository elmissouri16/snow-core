package builtin

import (
	"testing"
	"time"

	"github.com/snow-core/snow/internal/tools"
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
	for _, want := range []string{"read", "write", "edit", "bash"} {
		if !names[want] {
			t.Errorf("tool %q not registered", want)
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
		MaxOutputBytes: 999,
		BashTimeout:    5 * time.Second,
		Roots:          []string{dir},
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
	if got := bash.(*Bash).Timeout; got != 5*time.Second {
		t.Errorf("bash Timeout = %s, want 5s", got)
	}
}
