package tui

import (
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestPluginExactAliasPrecedesLongerPrefix(t *testing.T) {
	commands := []commandSpec{{name: "/notes"}, {name: "/note"}, {name: "/notebook"}}
	for _, input := range []string{"/note", "/NOTE", "note"} {
		matches := completeCommand(input, commands...)
		if len(matches) < 3 || matches[0] != "/note" {
			t.Fatalf("%q prefers %v over its exact command", input, matches)
		}
		if !slices.Equal(matches[:3], []string{"/note", "/notes", "/notebook"}) {
			t.Fatalf("prefix order changed: %v", matches)
		}
	}
}

func TestPluginExactAliasResetsPriorPaletteSelection(t *testing.T) {
	testHome(t)
	m := newModel(t.Context(), app.Options{})
	m.plugins = &pluginUIState{specs: []commandSpec{{name: "/notes"}, {name: "/note"}, {name: "/notebook"}}}
	m.compIndex = 2
	m.refreshPaletteFor("/note")
	if m.compMatches[m.compIndex] != "/note" {
		t.Fatalf("Enter would run %s instead of /note", m.compMatches[m.compIndex])
	}
}
