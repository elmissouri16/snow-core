// Package supervisor owns human-controlled Snow RPC workers in linked Git
// worktrees. It is intentionally internal and exposes no model tools.
package supervisor

import (
	"context"
	"os/exec"
	"time"

	"github.com/snow-core/snow/pkg/protocol"
)

// WorkerID is the supervisor-local identity above each child process's own
// independent /root agent namespace.
type WorkerID string

type ProcessStatus string

const (
	ProcessStarting ProcessStatus = "starting"
	ProcessReady    ProcessStatus = "ready"
	ProcessStopping ProcessStatus = "stopping"
	ProcessStopped  ProcessStatus = "stopped"
	ProcessCrashed  ProcessStatus = "crashed"
)

type TurnStatus string

const (
	TurnIdle           TurnStatus = "idle"
	TurnWorking        TurnStatus = "working"
	TurnPermission     TurnStatus = "permission"
	TurnInputNeeded    TurnStatus = "input_needed"
	TurnAborting       TurnStatus = "aborting"
	TurnOutcomeUnknown TurnStatus = "outcome_unknown"
)

// WorkerState is a defensive snapshot of one supervisor-owned process.
type WorkerState struct {
	ID                WorkerID
	WorkspaceID       string
	SessionID         string
	SessionPath       string
	WorktreePath      string
	Branch            string
	ProcessGeneration uint64
	ProcessStatus     ProcessStatus
	TurnStatus        TurnStatus
	Provider          string
	Model             string
	Thinking          protocol.ThinkingLevel
	StartedAt         time.Time
	TurnStartedAt     time.Time
	Usage             *protocol.Usage
	LastError         string
	OutcomeUnknown    bool
	Messages          []protocol.Message
	Permission        *protocol.PermissionRequest
	UserInput         *protocol.UserInputRequest
}

func (s WorkerState) clone() WorkerState {
	out := s
	out.Usage = s.Usage.Clone()
	out.Messages = make([]protocol.Message, len(s.Messages))
	for i, message := range s.Messages {
		out.Messages[i] = message.Clone()
	}
	if s.Permission != nil {
		permission := *s.Permission
		permission.Args = append([]byte(nil), s.Permission.Args...)
		permission.Paths = append([]string(nil), s.Permission.Paths...)
		out.Permission = &permission
	}
	if s.UserInput != nil {
		input := *s.UserInput
		input.Questions = make([]protocol.UserInputQuestion, len(s.UserInput.Questions))
		for i, question := range s.UserInput.Questions {
			input.Questions[i] = question
			input.Questions[i].Options = append([]protocol.UserInputOption(nil), question.Options...)
		}
		out.UserInput = &input
	}
	return out
}

// Event attributes either a worker state transition or a normalized child
// AgentEvent to one worker generation.
type Event struct {
	WorkerID   WorkerID
	Generation uint64
	State      *WorkerState
	Agent      *protocol.AgentEvent
}

// StartRequest identifies one exact worktree/session and its runtime selection.
type StartRequest struct {
	ID           WorkerID
	WorkspaceID  string
	SessionID    string
	SessionPath  string
	WorktreePath string
	Branch       string
	Provider     string
	Model        string
	Thinking     protocol.ThinkingLevel
	ConfigPath   string
	AuthPath     string

	RequireSandbox bool
	DisableSandbox bool
}

// Options configures process ownership. CommandFactory is an internal test seam;
// production always uses the current Snow executable with direct arguments.
type Options struct {
	Executable      string
	MaxConcurrent   int
	ShutdownTimeout time.Duration
	StartupTimeout  time.Duration
	EventBuffer     int
	CommandFactory  func(context.Context, StartRequest) *exec.Cmd
}
