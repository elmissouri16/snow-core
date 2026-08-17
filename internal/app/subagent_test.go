package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/config"
	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestSubagentLifecycleIndependentTranscriptAndFollowup(t *testing.T) {
	enabled := true
	a, err := New(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	state, err := a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "inspect", Task: "inspect files", Role: "explorer", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Agent.Path != "/root/inspect" {
		t.Fatalf("path=%s", state.Agent.Path)
	}
	state = awaitSubagent(t, a, "/root/inspect", protocol.AgentCompleted)
	if state.Agent.ParentPath != protocol.RootAgentPath {
		t.Fatalf("parent=%s", state.Agent.ParentPath)
	}
	rootMsgs, _ := a.Agent.Messages()
	agentMail := 0
	for _, m := range rootMsgs {
		if m.Role == protocol.RoleAgent {
			agentMail++
		}
	}
	if agentMail != 1 {
		t.Fatalf("root mail=%d messages=%+v", agentMail, rootMsgs)
	}
	childMsgs, err := a.SubagentMessages(context.Background(), "/root/inspect")
	if err != nil {
		t.Fatal(err)
	}
	if len(childMsgs) == 0 || childMsgs[0].Role != protocol.RoleUser {
		t.Fatalf("child=%+v", childMsgs)
	}
	if err := a.FollowupSubagent(context.Background(), "inspect", "check tests too"); err != nil {
		t.Fatal(err)
	}
	awaitSubagent(t, a, "inspect", protocol.AgentCompleted)
	rootMsgs, _ = a.Agent.Messages()
	agentMail = 0
	for _, m := range rootMsgs {
		if m.Role == protocol.RoleAgent {
			agentMail++
		}
	}
	if agentMail != 2 {
		t.Fatalf("followup mail=%d", agentMail)
	}
	list, err := a.ListSubagents(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Agents) != 2 || list.Agents[0].Agent.Path != protocol.RootAgentPath {
		t.Fatalf("list=%+v", list)
	}
}

func TestSubagentSessionSwitchAfterChildrenComplete(t *testing.T) {
	enabled := true
	a, err := New(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	state, err := a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "finished", Task: "inspect", Role: "explorer", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	awaitSubagent(t, a, string(state.Agent.Path), protocol.AgentCompleted)

	next := session.NewMemoryStore(session.Options{CWD: a.CWD()})
	if err := a.SetSession(next); err != nil {
		t.Fatalf("switch after terminal child: %v", err)
	}
	list, err := a.ListSubagents(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Agents) != 1 || list.Agents[0].Agent.Path != protocol.RootAgentPath {
		t.Fatalf("new session retained old tree: %+v", list.Agents)
	}
	if _, err := a.Subagent(context.Background(), string(state.Agent.Path)); err == nil {
		t.Fatal("old child remained attached after session switch")
	}

	// The manager remains usable for the new root session.
	state, err = a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "new_child", Task: "inspect again", Role: "explorer", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	awaitSubagent(t, a, string(state.Agent.Path), protocol.AgentCompleted)
}

func TestSubagentDuplicateRollbackAndDelegationRisk(t *testing.T) {
	enabled := true
	a, err := New(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow", Subagents: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "same", Task: "one", ForkTurns: "none"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "same", Task: "two", ForkTurns: "none"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, name := range []string{"spawn_agent", "followup_task"} {
		desc, ok := a.Registry.Descriptor(name)
		if !ok || desc.Risk != permission.RiskDelegate {
			t.Fatalf("%s risk=%q", name, desc.Risk)
		}
	}
	for _, name := range []string{"send_message", "wait_agent", "interrupt_agent", "list_agents"} {
		desc, ok := a.Registry.Descriptor(name)
		if !ok || desc.Risk != permission.RiskRead {
			t.Fatalf("%s risk=%q", name, desc.Risk)
		}
	}
}

func TestDefaultDurableSubagentColdResumeDoesNotRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DefaultProvider = "fake"
	cfg.Subagents.Enabled = true
	if !cfg.Subagents.Durable {
		t.Fatal("durable child history must default on")
	}
	configPath := filepath.Join(dir, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "root.db")
	a, err := New(context.Background(), Options{CWD: dir, ConfigPath: configPath, SessionPath: rootPath, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	state, err := a.SpawnSubagent(context.Background(), protocol.SpawnSubagentRequest{Name: "durable", Task: "inspect", ForkTurns: "none"})
	if err != nil {
		t.Fatal(err)
	}
	awaitSubagent(t, a, "durable", protocol.AgentCompleted)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	_ = state // model-facing identity is intentionally not a filesystem locator
	childDir := rootPath + ".agents"
	entries, err := os.ReadDir(childDir)
	if err != nil {
		t.Fatal(err)
	}
	entries = slices.DeleteFunc(entries, func(entry os.DirEntry) bool { return !strings.HasSuffix(entry.Name(), ".db") })
	if len(entries) != 1 {
		t.Fatalf("child database entries=%d", len(entries))
	}
	if mode, err := os.Stat(childDir); err != nil || mode.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode=%v err=%v", mode, err)
	}
	childInfo, err := entries[0].Info()
	if err != nil || childInfo.Mode().Perm() != 0o600 {
		t.Fatalf("child mode=%v err=%v", childInfo, err)
	}
	re, err := New(context.Background(), Options{CWD: dir, ConfigPath: configPath, SessionPath: rootPath, NoPlugins: true, NoMCP: true, NoSkills: true, Permission: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	events := 0
	re.Agent.Subscribe(func(ev protocol.AgentEvent) {
		if ev.Type == protocol.EvSubagentStatus {
			events++
		}
	})
	if err := re.ReadySubagents(); err != nil {
		t.Fatal(err)
	}
	if err := re.Agent.DrainEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := re.Subagent(context.Background(), "durable")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != protocol.AgentNotLoaded {
		t.Fatalf("restored=%s", restored.Status)
	}
	if events == 0 {
		t.Fatal("ready did not publish restored topology")
	}
}

func awaitSubagent(t *testing.T, a *App, target string, want protocol.AgentStatus) protocol.SubagentState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, err := a.Subagent(context.Background(), target)
		if err == nil && s.Status == want {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	s, _ := a.Subagent(context.Background(), target)
	t.Fatalf("status=%s want=%s", s.Status, want)
	return s
}
