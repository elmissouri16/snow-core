package main

import (
	"strings"
	"testing"
)

func TestCLIRemovedExternalPluginEntryPoints(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"flag", []string{"snow", "--plugin", "/unused/plugin"}, "unknown flag: --plugin"},
		{"command", []string{"snow", "plugin", "list"}, `unknown command "plugin"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCLI(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
