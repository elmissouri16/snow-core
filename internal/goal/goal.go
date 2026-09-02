package goal

import (
	_ "embed"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const materializeThreshold = 8 * 1024

//go:embed continuation.md
var continuationMarkdown string

//go:embed objective-updated.md
var objectiveUpdatedMarkdown string

var (
	continuationTemplate     = template.Must(template.New("goal-continuation").Option("missingkey=error").Parse(continuationMarkdown))
	objectiveUpdatedTemplate = template.Must(template.New("goal-objective-updated").Option("missingkey=error").Parse(objectiveUpdatedMarkdown))
)

type Controller struct {
	mu                sync.Mutex
	store             session.Store
	bindingGeneration uint64
	home              string
	emit              func(protocol.AgentEvent)
	objectiveUpdated  map[string]bool
	auditGoalID       string
	auditTurns        int
	remainders        map[string]time.Duration
}

const managedPrefix = "Read the Snow goal objective file at "
const managedSuffix = " before continuing."

const managedObjectiveName = "goal-objective.md"

// Binding identifies the controller's current non-sensitive store projection.
type Binding struct {
	Generation uint64 `json:"generation"`
	SessionID  string `json:"session_id"`
	BranchID   string `json:"branch_id,omitempty"`
}

// ConflictDetails is private structured metadata attached to a failed goal
// update so automatic continuation can stop repeated conflicts safely.
type ConflictDetails struct {
	Conflict session.GoalConflictError
	Binding  Binding
}

type managedRoots struct {
	home, goals, session, owner *os.Root
}

type continuationPromptData struct {
	Turn          int
	Remaining     string
	Objective     string
	BudgetReached bool
}

type objectiveUpdatedPromptData struct {
	Objective string
}

type getTool struct{ c *Controller }
type createTool struct{ c *Controller }
type updateTool struct{ c *Controller }
