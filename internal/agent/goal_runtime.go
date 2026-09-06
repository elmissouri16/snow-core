package agent

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"strings"
	"time"

	goalpkg "github.com/elmissouri16/snow-core/internal/goal"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// goalProgressAudit retains only a digest across turns, never objective or
// response text. Tool work prevents repeated narration from causing a pause.
type goalProgressAudit struct {
	text     hash.Hash
	work     bool
	goalID   string
	previous [sha256.Size]byte
	repeats  int
}

func (a *Agent) goalBudgetReached() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running && a.turnMode != protocol.ModePlan && a.budgetWrap
}

func (a *Agent) beginGoalReport() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.budgetWrap {
		return false
	}
	a.budgetReportDone = true
	return true
}

// Keep accepted input recoverable when a budget boundary ends a tool chain.
func (a *Agent) closeGoalInputQueue(success, cancelled bool) {
	a.closeInputQueue(cancelled || (success && !a.goalBudgetReached()))
}

// Flush the previous owner's elapsed time before the built-in creation tool
// can replace a completed goal. Failed creation leaves the same owner bound.
func (a *Agent) prepareGoalCreation(tool tools.Tool) error {
	if !goalpkg.IsCreateTool(tool, a.opts.Goal) {
		return nil
	}
	a.mu.RLock()
	g, started, mode := a.goalAtTurn.Clone(), a.turnStarted, a.turnMode
	a.mu.RUnlock()
	if g == nil || mode == protocol.ModePlan {
		return nil
	}
	now := time.Now()
	if _, _, err := a.opts.Goal.AccountDuration(g.GoalID, 0, now.Sub(started), nil); err != nil {
		return fmt.Errorf("goal: account before creation: %w", err)
	}
	a.mu.Lock()
	a.turnStarted = now
	a.mu.Unlock()
	return nil
}

func (a *Agent) bindCreatedGoal(tool tools.Tool, result tools.ToolResult) error {
	if result.IsError || !goalpkg.IsCreateTool(tool, a.opts.Goal) {
		return nil
	}
	g, err := a.opts.Goal.Get()
	if err != nil {
		return fmt.Errorf("goal: bind created goal: %w", err)
	}
	if g == nil {
		return fmt.Errorf("goal: created goal is unavailable")
	}
	a.mu.Lock()
	a.goalAtTurn = g
	a.turnStarted = time.Now()
	a.goalTurnID, a.goalTurn = g.GoalID, 1
	a.goalProgress = goalProgressAudit{text: sha256.New()}
	a.mu.Unlock()
	a.opts.Goal.RecordGoalTurn(g.GoalID)
	return nil
}

func (a *Agent) recordGoalResponse(content []protocol.ContentBlock) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.turnOrigin != "goal" {
		return
	}
	if a.goalProgress.text == nil {
		a.goalProgress.text = sha256.New()
	}
	for _, block := range content {
		if block.Type == protocol.BlockText {
			// Normalize whitespace after complete response assembly so stream chunking
			// does not affect the repetition signal.
			for field := range strings.FieldsSeq(block.Text) {
				_, _ = a.goalProgress.text.Write([]byte(field))
				_, _ = a.goalProgress.text.Write([]byte{0})
			}
		}
	}
}

// Called with a.mu held. Three identical tool-free turns are enough to pause;
// this is a repetition detector, not a claim to judge semantic progress.
func (a *Agent) repeatedGoalResponseLocked(goalID string) bool {
	audit := &a.goalProgress
	if audit.work {
		audit.repeats, audit.goalID = 0, ""
		return false
	}
	var digest [sha256.Size]byte
	if audit.text != nil {
		copy(digest[:], audit.text.Sum(nil))
	}
	if audit.goalID != goalID || audit.previous != digest {
		audit.goalID, audit.previous, audit.repeats = goalID, digest, 1
	} else {
		audit.repeats++
	}
	return audit.repeats >= 3
}
