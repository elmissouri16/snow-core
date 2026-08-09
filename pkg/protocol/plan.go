package protocol

import "fmt"

// CollaborationMode identifies the behavior contract for subsequent turns.
type CollaborationMode string

const (
	ModeDefault CollaborationMode = "default"
	ModePlan    CollaborationMode = "plan"
)

// ParseCollaborationMode validates a public collaboration-mode value. An
// omitted value preserves the historical Default behavior.
func ParseCollaborationMode(value string) (CollaborationMode, error) {
	mode := CollaborationMode(value)
	if mode == "" {
		mode = ModeDefault
	}
	switch mode {
	case ModeDefault, ModePlan:
		return mode, nil
	default:
		return ModeDefault, fmt.Errorf("invalid collaboration mode %q (want default|plan)", value)
	}
}

// CollaborationModeState is the public, effective mode snapshot.
type CollaborationModeState struct {
	Mode            CollaborationMode `json:"mode"`
	ReasoningEffort ThinkingLevel     `json:"reasoning_effort"`
}

// PlanItem is one proposed implementation plan emitted in Plan mode.
type PlanItem struct {
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
}

// PlanStepStatus is the update_plan checklist state.
type PlanStepStatus string

const (
	PlanStepPending    PlanStepStatus = "pending"
	PlanStepInProgress PlanStepStatus = "in_progress"
	PlanStepCompleted  PlanStepStatus = "completed"
)

// PlanStep is one update_plan checklist entry.
type PlanStep struct {
	Step   string         `json:"step"`
	Status PlanStepStatus `json:"status"`
}

// PlanUpdate is the structured update_plan payload shown by clients.
type PlanUpdate struct {
	Explanation string     `json:"explanation,omitempty"`
	Plan        []PlanStep `json:"plan"`
}

// Clone returns an independent plan update.
func (p *PlanUpdate) Clone() *PlanUpdate {
	if p == nil {
		return nil
	}
	out := *p
	out.Plan = append([]PlanStep(nil), p.Plan...)
	return &out
}
