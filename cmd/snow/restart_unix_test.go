//go:build unix

package main

import (
	"slices"
	"testing"
)

func TestRestartExecArgumentsPreservesDurableSession(t *testing.T) {
	tests := []struct {
		name     string
		original []string
		initial  string
		active   string
		resume   bool
		want     []string
	}{
		{name: "fresh session", original: []string{"snow", "--provider", "fake"}, active: "/sessions/current.db", want: []string{"snow", "--provider", "fake", "--session", "/sessions/current.db"}},
		{name: "explicit session", original: []string{"snow", "--session=/sessions/old.db"}, initial: "/sessions/old.db", active: "/sessions/current.db", want: []string{"snow", "--session", "/sessions/current.db"}},
		{name: "separate session flag", original: []string{"snow", "--session", "/sessions/old.db"}, initial: "/sessions/old.db", active: "/sessions/current.db", want: []string{"snow", "--session", "/sessions/current.db"}},
		{name: "explicit resume path", original: []string{"snow", "resume", "/sessions/old.db"}, initial: "/sessions/old.db", active: "/sessions/current.db", resume: true, want: []string{"snow", "resume", "--session", "/sessions/current.db"}},
		{name: "picker resume", original: []string{"snow", "resume"}, active: "/sessions/chosen.db", resume: true, want: []string{"snow", "resume", "--session", "/sessions/chosen.db"}},
		{name: "ephemeral", original: []string{"snow", "--no-session"}, want: []string{"snow", "--no-session"}},
		{name: "ephemeral switched durable", original: []string{"snow", "--no-session", "--provider", "fake"}, active: "/sessions/current.db", want: []string{"snow", "--provider", "fake", "--session", "/sessions/current.db"}},
		{name: "missing argv zero", active: "/sessions/current.db", want: []string{"snow", "--session", "/sessions/current.db"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := restartExecArguments(tt.original, tt.initial, tt.active, tt.resume); !slices.Equal(got, tt.want) {
				t.Fatalf("restart args = %q, want %q", got, tt.want)
			}
		})
	}
}
