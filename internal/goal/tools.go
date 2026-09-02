package goal

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func (t *createTool) Run(_ context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var a struct {
		Objective   string `json:"objective"`
		TokenBudget *int64 `json:"token_budget"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return tools.ErrorResult(e), nil
	}
	g, e := t.c.Create(a.Objective, a.TokenBudget, false)
	if e != nil {
		return tools.ErrorResult(e), nil
	}
	b, _ := json.Marshal(g)
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(b))}, Details: tools.PrivateDetails{}}, nil
}

var goalIDPattern = regexp.MustCompile(`^goal-(?:[0-9a-f]{32}|[0-9]+)$`)

func (*updateTool) Schema() protocol.ToolSchema {
	return protocol.ToolSchema{Name: "update_goal", Description: "Set the active persisted goal to complete or blocked. Runtime requires a reason and at least three consecutive goal turns before blocked; blocked goals remain resumable. Call complete only after auditing the evidence, and use blocked only for a genuine recurring external blocker.", Parameters: json.RawMessage(`{"type":"object","required":["goal_id","status"],"properties":{"goal_id":{"type":"string","minLength":6,"maxLength":37,"pattern":"^goal-(?:[0-9a-f]{32}|[0-9]+)$","description":"ID of the current persisted goal."},"status":{"type":"string","enum":["complete","blocked"],"description":"Status to assign: complete is terminal; blocked is resumable."},"reason":{"type":"string","maxLength":8192,"description":"Required for blocked; describe the recurring external blocker."}},"additionalProperties":false}`)}
}

func (t *updateTool) Run(_ context.Context, raw json.RawMessage, _ tools.ToolHost) (tools.ToolResult, error) {
	var a struct {
		GoalID string                    `json:"goal_id"`
		Status protocol.ThreadGoalStatus `json:"status"`
		Reason string                    `json:"reason"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return tools.ErrorResult(e), nil
	}
	a.GoalID = strings.TrimSpace(a.GoalID)
	if !goalIDPattern.MatchString(a.GoalID) {
		return tools.ErrorResult(errors.New("goal: invalid goal_id")), nil
	}
	g, e := t.c.SetStatusWithReason(a.GoalID, a.Status, true, a.Reason)
	if e != nil {
		result := tools.ErrorResult(e)
		if conflict, ok := errors.AsType[*session.GoalConflictError](e); ok {
			result = tools.ErrorResult(fmt.Errorf("%w; call get_goal to refresh before retrying", e))
			result.Details = ConflictDetails{Conflict: *conflict, Binding: t.c.Binding()}
		}
		return result, nil
	}
	b, _ := json.Marshal(g)
	return tools.ToolResult{Content: []protocol.ContentBlock{protocol.NewTextBlock(string(b))}, Details: tools.PrivateDetails{}}, nil
}
