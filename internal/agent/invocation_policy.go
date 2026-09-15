package agent

import (
	"context"

	"github.com/elmissouri16/snow-core/internal/permission"
)

// EvaluateInvocationPolicy evaluates this agent's configured hard policy without
// executing a tool or asking permission. Callers may already hold admission.
func (a *Agent) EvaluateInvocationPolicy(ctx context.Context, req permission.Request) (permission.PolicyDecision, error) {
	if err := ctx.Err(); err != nil {
		return permission.PolicyDecision{}, err
	}
	if a.opts.InvocationPolicy == nil {
		return permission.PolicyDecision{Denied: true}, nil
	}
	req.Agent = a.opts.Identity.Clone()
	return a.opts.InvocationPolicy.Evaluate(ctx, req)
}
