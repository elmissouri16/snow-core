package subagent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPlanRoleReadOnlyUsesResolvedTools(t *testing.T) {
	tests := []struct {
		name      string
		role      Role
		recursive bool
		want      bool
	}{
		{name: "explicit reads", role: Role{Name: "custom", Tools: []string{"read", "grep", "artifact_read"}}, want: true},
		{name: "empty inherits shell", role: Role{Name: "explorer"}},
		{name: "explorer with bash", role: Role{Name: "explorer", Tools: []string{"read", "bash"}}},
		{name: "write despite harmless name", role: Role{Name: "explorer", Tools: []string{"read", "write"}, AllowMutation: true}},
		{name: "unknown capability", role: Role{Name: "explorer", Tools: []string{"read", "custom"}}},
		{name: "recursive delegation", role: Role{Name: "explorer", Tools: []string{"read", "grep"}}, recursive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planRoleReadOnly(tt.role, tt.recursive); got != tt.want {
				t.Fatalf("planRoleReadOnly(%+v)=%v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func newPlanPolicyManager(t *testing.T, roles map[string]Role) (*Manager, *agentFixture) {
	t.Helper()
	return newPlanPolicyManagerWithDelay(t, roles, time.Millisecond)
}

func newPlanPolicyManagerWithDelay(t *testing.T, roles map[string]Role, childDelay time.Duration) (*Manager, *agentFixture) {
	t.Helper()
	st := session.NewMemoryStore(session.Options{})
	root := rootAgent(t, st)
	fixture := &agentFixture{root: root, store: st}
	m := New(context.Background(), Limits{
		MaxConcurrentThreads: 1, MaxAgentsPerSession: 4, MaxDepth: 1,
		TaskTimeout: time.Second, MinWait: time.Millisecond, DefaultWait: time.Millisecond, MaxWait: time.Second,
		DefaultRole: "unsafe", Roles: roles,
	})
	var active, maxActive atomic.Int32
	factory := ChildFactoryFunc(func(context.Context, ChildSpec) (ChildRuntime, error) {
		return &mockChild{delay: childDelay, active: &active, max: &maxActive}, nil
	})
	if err := m.Bind(root, factory, root.Publish, st); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.Close(context.Background())
		root.Close()
	})
	return m, fixture
}

type agentFixture struct {
	root interface {
		SetMode(protocol.CollaborationMode) error
	}
	store session.Store
}

func TestPlanSpawnChecksCapabilitiesNotRoleName(t *testing.T) {
	roles := map[string]Role{
		"unsafe":   {Name: "explorer", Tools: []string{"read", "bash"}},
		"safe_alt": {Name: "not-explorer", Tools: []string{"read", "grep"}},
	}
	m, fixture := newPlanPolicyManager(t, roles)
	if err := fixture.root.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	caller := m.RootCaller()
	if _, err := m.Spawn(t.Context(), caller, protocol.SpawnSubagentRequest{Name: "unsafe", Task: "inspect", Role: "unsafe"}); !errors.Is(err, errPlanRequiresReadOnlyChild) {
		t.Fatalf("unsafe spawn error=%v", err)
	}
	if _, err := m.Spawn(t.Context(), caller, protocol.SpawnSubagentRequest{Name: "safe", Task: "inspect", Role: "safe_alt"}); err != nil {
		t.Fatalf("safe renamed role rejected: %v", err)
	}
}

func TestPlanSpawnRejectsRecursiveEscalation(t *testing.T) {
	roles := map[string]Role{"safe": {Name: "safe", Tools: []string{"read", "grep"}}}
	m, fixture := newPlanPolicyManager(t, roles)
	m.mu.Lock()
	m.limits.Recursive = true
	m.limits.MaxDepth = 2
	m.mu.Unlock()
	if err := fixture.root.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	_, err := m.Spawn(t.Context(), m.RootCaller(), protocol.SpawnSubagentRequest{Name: "recursive", Task: "inspect", Role: "safe"})
	if !errors.Is(err, errPlanRequiresReadOnlyChild) {
		t.Fatalf("recursive Plan spawn error=%v", err)
	}
}

func TestPlanTransitionAndMessageRejectActiveUnsafeChild(t *testing.T) {
	roles := map[string]Role{"unsafe": {Name: "unsafe", Tools: []string{"read", "bash"}}}
	m, fixture := newPlanPolicyManagerWithDelay(t, roles, 200*time.Millisecond)
	caller := m.RootCaller()
	state, err := m.Spawn(t.Context(), caller, protocol.SpawnSubagentRequest{Name: "worker", Task: "inspect", Role: "unsafe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ValidatePlanTransition(); err == nil {
		t.Fatal("Plan transition accepted active mutation-capable child")
	}
	if err := fixture.root.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if err := m.SendMessage(t.Context(), caller, string(state.Agent.Path), "steer"); !errors.Is(err, errPlanRequiresReadOnlyChild) {
		t.Fatalf("unsafe Plan message error=%v", err)
	}
}

func TestPlanReuseRejectsChangedPersistedRoleFingerprint(t *testing.T) {
	roles := map[string]Role{"safe": {Name: "safe", Tools: []string{"read", "grep"}}}
	m, fixture := newPlanPolicyManager(t, roles)
	caller := m.RootCaller()
	state, err := m.Spawn(t.Context(), caller, protocol.SpawnSubagentRequest{Name: "worker", Task: "inspect", Role: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WaitAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CloseAgent(t.Context(), caller, string(state.Agent.Path)); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.limits.Roles["safe"] = Role{Name: "safe", Tools: []string{"read", "glob"}}
	m.mu.Unlock()
	if err := fixture.root.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResumeAgent(t.Context(), caller, string(state.Agent.Path)); err == nil || !strings.Contains(err.Error(), "authority changed") {
		t.Fatalf("changed fingerprint resume error=%v", err)
	}
}

func TestPlanFollowupAndResumeRejectUnsafeExistingChild(t *testing.T) {
	roles := map[string]Role{"unsafe": {Name: "unsafe", Tools: []string{"read", "bash"}}}
	m, fixture := newPlanPolicyManager(t, roles)
	caller := m.RootCaller()
	state, err := m.Spawn(t.Context(), caller, protocol.SpawnSubagentRequest{Name: "worker", Task: "inspect", Role: "unsafe"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WaitAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CloseAgent(t.Context(), caller, string(state.Agent.Path)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.root.SetMode(protocol.ModePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResumeAgent(t.Context(), caller, string(state.Agent.Path)); !errors.Is(err, errPlanRequiresReadOnlyChild) {
		t.Fatalf("unsafe resume error=%v", err)
	}
	if err := m.Followup(t.Context(), caller, string(state.Agent.Path), "continue"); !errors.Is(err, errPlanRequiresReadOnlyChild) {
		t.Fatalf("unsafe followup error=%v", err)
	}
	resolved, _, err := m.resolveTarget(caller, string(state.Agent.Path))
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.snapshot().Status; got != protocol.AgentClosed {
		t.Fatalf("unsafe child status=%s, want closed", got)
	}
}
