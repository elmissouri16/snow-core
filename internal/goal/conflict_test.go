package goal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestUpdateToolNormalizesIDAndReportsReplacementConflict(t *testing.T) {
	st := persisted(t)
	controller, err := New(st, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := controller.Create("first", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	tool := &updateTool{c: controller}
	raw, _ := json.Marshal(map[string]any{"goal_id": "  " + first.GoalID + "\n", "status": protocol.GoalComplete})
	result, err := tool.Run(t.Context(), raw, nil)
	if err != nil || result.IsError {
		t.Fatalf("normalized update result=%+v err=%v", result, err)
	}

	second, err := controller.Create("second", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"goal_id": first.GoalID, "status": protocol.GoalComplete})
	result, err = tool.Run(t.Context(), raw, nil)
	if err != nil || !result.IsError {
		t.Fatalf("conflict result=%+v err=%v", result, err)
	}
	details, ok := result.Details.(ConflictDetails)
	if !ok {
		t.Fatalf("details type = %T", result.Details)
	}
	if details.Conflict.CurrentGoalID != second.GoalID || details.Conflict.ExpectedGoalID != first.GoalID || details.Binding.SessionID != st.ID() || details.Binding.BranchID != "main" {
		t.Fatalf("details = %+v", details)
	}
	if text := result.Content[0].Text; !strings.Contains(text, first.GoalID) || !strings.Contains(text, second.GoalID) {
		t.Fatalf("conflict output = %q", text)
	}
}

func TestUpdateToolRejectsNoncanonicalGoalID(t *testing.T) {
	controller, err := New(persisted(t), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := &updateTool{c: controller}
	result, err := tool.Run(t.Context(), json.RawMessage(`{"goal_id":"goal-abc\u200b","status":"complete"}`), nil)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "invalid goal_id") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, ok := result.Details.(ConflictDetails); ok {
		t.Fatal("invalid input was reported as a conflict")
	}
}

func TestControllerBindingGenerationAndTypedPreflightConflict(t *testing.T) {
	firstStore := persisted(t)
	controller, err := New(firstStore, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	firstBinding := controller.Binding()
	goal, err := controller.Create("first", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	secondStore := persisted(t)
	if err := controller.SetStore(secondStore); err != nil {
		t.Fatal(err)
	}
	second, err := controller.Create("second", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	secondBinding := controller.Binding()
	if secondBinding.Generation != firstBinding.Generation+1 || secondBinding.SessionID != secondStore.ID() {
		t.Fatalf("bindings first=%+v second=%+v", firstBinding, secondBinding)
	}
	_, err = controller.SetStatus(goal.GoalID, protocol.GoalComplete, true)
	if !errors.Is(err, session.ErrGoalConflict) {
		t.Fatalf("error = %v", err)
	}
	conflict, ok := errors.AsType[*session.GoalConflictError](err)
	if !ok || conflict.CurrentGoalID != second.GoalID || conflict.BindingGeneration != secondBinding.Generation {
		t.Fatalf("conflict = %+v, ok=%v", conflict, ok)
	}
	accounting := []struct {
		name string
		run  func() error
	}{
		{name: "account", run: func() error {
			_, _, err := controller.Account(goal.GoalID, 1, 1)
			return err
		}},
		{name: "duration", run: func() error {
			_, _, err := controller.AccountDuration(goal.GoalID, 1, time.Second, nil)
			return err
		}},
	}
	for _, tt := range accounting {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			conflict, ok := errors.AsType[*session.GoalConflictError](err)
			if !ok || conflict.CurrentGoalID != second.GoalID || conflict.BindingGeneration != secondBinding.Generation {
				t.Fatalf("accounting conflict = %+v, ok=%v, err=%v", conflict, ok, err)
			}
		})
	}
}

var _ tools.Tool = (*updateTool)(nil)
