package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestManagedForkDoesNotReactivateHistoricalSkills(t *testing.T) {
	p := &scriptedProvider{}
	registry := tools.NewRegistry()
	calls := 0
	tool := &testTool{schema: protocol.ToolSchema{Name: "activate_skill"}, runFunc: func(context.Context, json.RawMessage, tools.ToolHost) tools.ToolResult {
		calls++
		return tools.ToolResult{Details: tools.SkillActivationDetails{Name: "historical", Content: "never activate"}}
	}}
	if err := registry.RegisterDescriptor(tools.ToolDescriptor{Schema: tool.Schema(), Tool: tool, Owner: "skills", Source: tools.SourceBuiltin, Effect: tools.EffectReadOnly}); err != nil {
		t.Fatal(err)
	}
	a, st := setup(t, p, registry, permission.ModeDeny)
	defer a.Close()
	if err := st.Append(session.Entry{Type: session.EntryMeta, ID: "skill-marker", Key: skillActivationMeta, Value: "historical"}); err != nil {
		t.Fatal(err)
	}
	a.activeSkills["outgoing"] = "outgoing content"
	epoch := a.RootEpoch()
	unlock := a.LockAdmission()
	branch, err := a.ManagedForkBranchAdmitted(t.Context(), protocol.RPCManagedBranchForkParams{SessionID: st.ID(), SourceBranchID: "main", SourceTipID: st.BranchTip(), TargetBranchID: "main", TargetTipID: st.BranchTip(), Name: "passive"}, protocol.ModeDefault)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(p.requests) != 0 || len(a.activeSkills) != 0 || a.RootEpoch() <= epoch || st.ActiveBranchID() != branch.ID {
		t.Fatalf("passive fork calls=%d providers=%d skills=%v", calls, len(p.requests), a.activeSkills)
	}
}

func TestManagedForkReadOnlyAdmissionDoesNotPreemptWork(t *testing.T) {
	for _, state := range []string{"closed", "running", "automatic", "explicit", "legacy queued", "controlled queued", "review"} {
		t.Run(state, func(t *testing.T) {
			a, st := setup(t, &scriptedProvider{}, nil, permission.ModeDeny)
			defer a.Close()
			a.mu.Lock()
			switch state {
			case "closed":
				a.closed = true
			case "running":
				a.running = true
			case "automatic":
				a.autoRunning = true
			case "explicit":
				a.goalRun = &GoalRunHandle{}
			case "legacy queued":
				a.queuedInputs = []protocol.QueuedInput{{Text: "held"}}
			case "controlled queued":
				a.queueControl.items = []protocol.QueueControlItem{{ID: "held"}}
			case "review":
				a.queueControl.review = []protocol.QueueControlItem{{ID: "held"}}
			}
			a.mu.Unlock()
			epoch, tip := a.RootEpoch(), st.BranchTip()
			unlock := a.LockAdmission()
			_, err := a.ManagedForkBranchAdmitted(t.Context(), protocol.RPCManagedBranchForkParams{Name: "not admitted"}, protocol.ModeDefault)
			unlock()
			if err == nil {
				t.Fatal("busy state accepted")
			}
			if a.RootEpoch() != epoch || st.BranchTip() != tip || st.ActiveBranchID() != "main" {
				t.Fatal("read-only rejection mutated history")
			}
			a.mu.Lock()
			if state == "automatic" && !a.autoRunning || state == "explicit" && a.goalRun == nil || state == "legacy queued" && len(a.queuedInputs) != 1 || state == "controlled queued" && len(a.queueControl.items) != 1 || state == "review" && len(a.queueControl.review) != 1 {
				t.Error("rejection preempted work")
			}
			// These synthetic owners have no worker; restore state before Close.
			a.closed, a.running, a.autoRunning = false, false, false
			a.goalRun, a.queuedInputs = nil, nil
			a.queueControl = queueControlState{}
			a.mu.Unlock()
		})
	}
}

type historyLostAckStore struct{ *session.MemoryStore }

func (s historyLostAckStore) ForkHistoryBranch(ctx context.Context, p protocol.RPCManagedBranchForkParams, mode protocol.CollaborationMode) (protocol.SessionBranch, error) {
	branch, err := s.MemoryStore.ForkHistoryBranch(ctx, p, mode)
	if err != nil {
		return branch, err
	}
	return branch, errors.Join(session.ErrHistoryControlUnknown, errors.New("injected lost commit acknowledgement"))
}

func TestManagedForkUnknownCommittedOutcomeReconcilesPassively(t *testing.T) {
	p := &scriptedProvider{}
	a, store := setup(t, p, nil, permission.ModeDeny)
	defer a.Close()
	a.opts.Session = historyLostAckStore{store}
	epoch := a.RootEpoch()
	a.activeSkills["old"] = "must not reactivate"
	binding := protocol.RPCManagedBranchForkParams{SessionID: store.ID(), SourceBranchID: "main", SourceTipID: store.BranchTip(), TargetBranchID: "main", TargetTipID: store.BranchTip(), Name: "uncertain"}
	unlock := a.LockAdmission()
	branch, err := a.ManagedForkBranchAdmitted(t.Context(), binding, protocol.ModeDefault)
	unlock()
	if !errors.Is(err, ErrHistoryControlUnknown) {
		t.Fatalf("lost acknowledgement: %v", err)
	}
	if branch.ID == "" || store.ActiveBranchID() != branch.ID || a.RootEpoch() <= epoch || len(a.activeSkills) != 0 || len(p.requests) != 0 {
		t.Fatalf("committed unknown was not passively reconciled: %+v", branch)
	}
}
