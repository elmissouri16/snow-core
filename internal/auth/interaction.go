package auth

import "context"

// PromptKind identifies a host interaction without exposing provider-specific UI code.
type PromptKind string

const (
	PromptSecret PromptKind = "secret"
	PromptText   PromptKind = "text"
)

// Prompt is a safe login question. Secret responses must never be copied into events or logs.
type Prompt struct {
	ID          string
	Kind        PromptKind
	Title       string
	Description string
	Placeholder string
	Optional    bool
}

type Response struct {
	Value string
}

// Progress is a safe login lifecycle notification.
type Progress struct {
	Kind     string
	Message  string
	URL      string
	UserCode string
}

// Interaction is implemented by CLI/TUI hosts. Authentication drivers remain
// independent from Cobra, Bubble Tea, and terminal I/O.
type Interaction interface {
	Prompt(context.Context, Prompt) (Response, error)
	OpenURL(context.Context, string) error
	Progress(Progress)
}

// PromptAvailability is an optional capability signal for interactions that
// cannot collect text input. Drivers use it to avoid starting fallback prompt
// goroutines that can never produce a response.
type PromptAvailability interface {
	PromptAvailable() bool
}

// NopInteraction is useful for non-interactive status/runtime operations.
type NopInteraction struct{}

func (NopInteraction) Prompt(context.Context, Prompt) (Response, error) {
	return Response{}, ErrInteractionUnavailable
}
func (NopInteraction) OpenURL(context.Context, string) error { return ErrInteractionUnavailable }
func (NopInteraction) Progress(Progress)                     {}
func (NopInteraction) PromptAvailable() bool                 { return false }

var ErrInteractionUnavailable = interactionError("auth: interactive login input is unavailable")

type interactionError string

func (e interactionError) Error() string { return string(e) }
