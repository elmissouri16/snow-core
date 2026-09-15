package agent

import (
	"context"
	"sync/atomic"
)

// PromptOutcome captures terminal evidence for one caller-owned prompt without
// changing Prompt's error contract. It is independent of asynchronous event
// delivery and cannot be overwritten by a later turn on the same Agent.
// Context cancellation and returned errors must still be checked by the caller.
type PromptOutcome struct {
	providerAborted atomic.Bool
}

type promptOutcomeKey struct{}

// CapturePromptOutcome returns a context and a fresh outcome for one prompt
// invocation (including an admitted edit/regeneration). Read the outcome after
// the call returns. Do not reuse the returned context for another invocation.
// This does not cancel work, subscribe to events, or change admission.
func CapturePromptOutcome(ctx context.Context) (context.Context, *PromptOutcome) {
	if ctx == nil {
		ctx = context.Background()
	}
	outcome := new(PromptOutcome)
	return context.WithValue(ctx, promptOutcomeKey{}, outcome), outcome
}

// ProviderAborted reports an explicit terminal provider abort, not an Abort
// request, historical assistant status, or an unrelated child-agent event.
func (o *PromptOutcome) ProviderAborted() bool {
	return o != nil && o.providerAborted.Load()
}

func recordPromptProviderAbort(ctx context.Context) {
	if outcome, ok := ctx.Value(promptOutcomeKey{}).(*PromptOutcome); ok {
		outcome.providerAborted.Store(true)
	}
}
