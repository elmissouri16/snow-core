package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// UpdatePlan is the Default-mode TODO/checklist tool. It does not enter Plan mode.
type UpdatePlan struct{}

func NewUpdatePlan() *UpdatePlan { return &UpdatePlan{} }

func (*UpdatePlan) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        "update_plan",
		Description: "In Default mode, replace the current task TODO/checklist with one or more ordered steps and statuses. At most one step may be in_progress. This tool is unavailable in Plan mode and does not enter Plan mode.",
		Parameters: json.RawMessage(`{
  "type":"object","additionalProperties":false,"required":["plan"],
  "properties":{
    "explanation":{"type":"string","description":"Optional short reason for this checklist update."},
    "plan":{"type":"array","minItems":1,"description":"Complete replacement checklist in display order.","items":{"type":"object","additionalProperties":false,
      "required":["step","status"],"properties":{
        "step":{"type":"string","description":"Concrete task step."},
        "status":{"type":"string","enum":["pending","in_progress","completed"],"description":"Current step state."}
      }}}
  }
}`),
	}
}

func (*UpdatePlan) Run(_ context.Context, raw json.RawMessage, host tools.ToolHost) (tools.ToolResult, error) {
	if modeHost, ok := host.(tools.CollaborationModeHost); ok && modeHost.CollaborationMode() == protocol.ModePlan {
		return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock("update_plan is a TODO/checklist tool and is not allowed in Plan mode")}, IsError: true}, nil
	}
	var update protocol.PlanUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		return tools.ErrorResult(fmt.Errorf("update_plan: invalid arguments: %w", err)), nil
	}
	if len(update.Plan) == 0 {
		return tools.ErrorResult(fmt.Errorf("update_plan: plan must contain at least one item")), nil
	}
	inProgress := 0
	for i := range update.Plan {
		update.Plan[i].Step = strings.TrimSpace(update.Plan[i].Step)
		if update.Plan[i].Step == "" {
			return tools.ErrorResult(fmt.Errorf("update_plan: item %d has an empty step", i+1)), nil
		}
		switch update.Plan[i].Status {
		case protocol.PlanStepPending, protocol.PlanStepCompleted:
		case protocol.PlanStepInProgress:
			inProgress++
		default:
			return tools.ErrorResult(fmt.Errorf("update_plan: item %d has invalid status %q", i+1, update.Plan[i].Status)), nil
		}
	}
	if inProgress > 1 {
		return tools.ErrorResult(fmt.Errorf("update_plan: at most one item may be in_progress")), nil
	}
	return tools.ToolResult{
		Content: []protocol.ContentBlock{protocol.NewTextBlock("Plan updated")},
		Details: tools.PlanUpdateDetails{Update: update},
	}, nil
}
