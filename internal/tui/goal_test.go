package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"testing"
)

func TestTUIGoalCommandsAndReplacementConfirmation(t *testing.T) {
	testHome(t)
	m := newModel(context.Background(), app.Options{})
	a, e := app.New(context.Background(), app.Options{Provider: "fake", Permission: "allow", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	m.app = a
	defer a.Close()
	m.runCommand("/goal first objective")
	if m.goal == nil {
		t.Fatal("goal not created")
	}
	m.runCommand("/goal pause")
	if m.goal.Status != protocol.GoalPaused {
		t.Fatalf("goal=%+v", m.goal)
	}
	m.runCommand("/goal second objective")
	if !m.confirmGoalReplace {
		t.Fatal("replacement confirmation not shown")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.goal == nil || m.goal.Objective != "second objective" {
		t.Fatalf("goal=%+v", m.goal)
	}
	m.runCommand("/goal clear")
	if m.goal != nil {
		t.Fatalf("goal=%+v", m.goal)
	}
}
