package plugin

import (
	"strings"
	"testing"
)

func TestValidateSpecRejectsAmbiguousEnvironmentAndNUL(t *testing.T) {
	valid := PluginSpec{ID: "demo", Command: []string{"python3", "plugin.py"}, Env: []string{"PATH=/usr/bin", "MODE=test"}}
	if err := ValidateSpec(valid); err != nil {
		t.Fatalf("valid spec: %v", err)
	}
	for _, test := range []struct {
		name string
		spec PluginSpec
		want string
	}{
		{name: "missing equals", spec: PluginSpec{ID: "demo", Command: []string{"demo"}, Env: []string{"TOKEN"}}, want: "NAME=VALUE"},
		{name: "blank name", spec: PluginSpec{ID: "demo", Command: []string{"demo"}, Env: []string{"=value"}}, want: "NAME=VALUE"},
		{name: "duplicate", spec: PluginSpec{ID: "demo", Command: []string{"demo"}, Env: []string{"TOKEN=one", "TOKEN=two"}}, want: "duplicate"},
		{name: "command nul", spec: PluginSpec{ID: "demo", Command: []string{"demo", "bad\x00arg"}}, want: "NUL"},
		{name: "cwd nul", spec: PluginSpec{ID: "demo", Command: []string{"demo"}, CWD: "bad\x00cwd"}, want: "NUL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSpec(test.spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
