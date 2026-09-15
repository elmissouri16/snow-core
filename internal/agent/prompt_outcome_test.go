package agent

import (
	"testing"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/internal/provider/fake"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestPromptOutcomePreservesErrorContract(t *testing.T) {
	for _, stop := range []protocol.StopReason{protocol.StopStop, protocol.StopLength, protocol.StopAborted, protocol.StopError} {
		t.Run(string(stop), func(t *testing.T) {
			p := fake.New([]fake.Step{{Kind: fake.StepDone, Stop: stop}})
			a, err := New(Options{
				Provider: p, Registry: tools.NewRegistry(), Session: session.NewMemoryStore(session.Options{}),
				Permission: permission.NewService(permission.ModeDeny, nil),
				Model:      protocol.Model{Provider: p.ID(), ID: "fake-1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			ctx, outcome := CapturePromptOutcome(t.Context())
			err = a.Prompt(ctx, "first")
			if (err != nil) != (stop == protocol.StopError) {
				t.Fatalf("Prompt error contract changed: stop=%s err=%v", stop, err)
			}
			if outcome.ProviderAborted() != (stop == protocol.StopAborted) || ctx.Err() != nil {
				t.Fatalf("outcome=%t context=%v", outcome.ProviderAborted(), ctx.Err())
			}
			// Reading old aborted history, a rejected prompt, or publishing an
			// unrelated event must not supply terminal evidence to a new capture.
			nextCtx, next := CapturePromptOutcome(t.Context())
			a.Publish(protocol.AgentEvent{Type: protocol.EvAborted})
			if err := a.Prompt(nextCtx, ""); err == nil || next.ProviderAborted() {
				t.Fatalf("rejected prompt inherited abort: err=%v aborted=%t", err, next.ProviderAborted())
			}
			if err := a.SetProviderAndModel(fake.New(nil), a.Model()); err != nil {
				t.Fatal(err)
			}
			nextCtx, next = CapturePromptOutcome(t.Context())
			if err := a.Prompt(nextCtx, "next"); err != nil || next.ProviderAborted() {
				t.Fatalf("next prompt inherited abort: err=%v aborted=%t", err, next.ProviderAborted())
			}
			if outcome.ProviderAborted() != (stop == protocol.StopAborted) {
				t.Fatal("next invocation overwrote previous outcome")
			}
		})
	}
}
