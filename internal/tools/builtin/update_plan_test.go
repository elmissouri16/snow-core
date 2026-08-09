package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/permission"
	"github.com/snow-core/snow/internal/tools"
	"github.com/snow-core/snow/pkg/protocol"
)

type modeHost struct{ mode protocol.CollaborationMode }

func (h modeHost) CWD() string                                   { return "" }
func (h modeHost) Roots() []string                               { return nil }
func (h modeHost) Permission() permission.Service                { return nil }
func (h modeHost) EmitProgress(tools.ToolProgressEvent)          {}
func (h modeHost) Environ() []string                             { return nil }
func (h modeHost) CollaborationMode() protocol.CollaborationMode { return h.mode }

func TestUpdatePlanValidationAndDetails(t *testing.T) {
	tool := NewUpdatePlan()
	result, err := tool.Run(context.Background(), json.RawMessage(`{"explanation":"work","plan":[{"step":"one","status":"in_progress"}]}`), modeHost{mode: protocol.ModeDefault})
	if err != nil || result.IsError || result.Content[0].Text != "Plan updated" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, ok := result.Details.(tools.PlanUpdateDetails); !ok {
		t.Fatalf("details = %T", result.Details)
	}
	result, _ = tool.Run(context.Background(), json.RawMessage(`{"plan":[{"step":"one","status":"in_progress"},{"step":"two","status":"in_progress"}]}`), modeHost{})
	if !result.IsError || !strings.Contains(result.Content[0].Text, "at most one") {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdatePlanRejectedInPlanMode(t *testing.T) {
	result, _ := NewUpdatePlan().Run(context.Background(), json.RawMessage(`{"plan":[{"step":"one","status":"pending"}]}`), modeHost{mode: protocol.ModePlan})
	if !result.IsError || !strings.Contains(result.Content[0].Text, "update_plan is a TODO/checklist tool and is not allowed in Plan mode") {
		t.Fatalf("result = %+v", result)
	}
}
