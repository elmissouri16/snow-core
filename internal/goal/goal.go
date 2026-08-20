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
	mu               sync.Mutex
	store            session.Store
	home             string
	emit             func(protocol.AgentEvent)
	objectiveUpdated map[string]bool
	auditGoalID      string
	auditTurns       int
	remainders       map[string]time.Duration
}

const managedPrefix = "Read the Snow goal objective file at "
const managedSuffix = " before continuing."

const managedObjectiveName = "goal-objective.md"

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
