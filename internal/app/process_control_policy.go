package app

import (
	"context"
	"encoding/json/v2"

	"github.com/elmissouri16/snow-core/internal/permission"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// processControlInvocationPolicy is the read-only authority adapter supplied by
// Agent. It must delegate to that agent's actual configured InvocationPolicy,
// not a replacement DefaultPolicy, and must not reacquire agent admission (the
// App session transaction already holds it). Until the adapter is available,
// operator Stop fails closed, including in permission allow mode.
type processControlInvocationPolicy interface {
	EvaluateInvocationPolicy(context.Context, permission.Request) (permission.PolicyDecision, error)
}

func processControlStopRequest(p protocol.RPCProcessControlStopParams) permission.Request {
	grace := p.GraceMS
	if grace == 0 {
		grace = 2000
	}
	// Match the builtin process_stop request shape and classification. It has no
	// tool preflight: its default analysis is Rememberable=true, without inferred
	// paths/effects. Neither a raw command nor a sessionless PID is accepted here.
	args, _ := json.Marshal(struct {
		ProcessID string `json:"process_id"`
		GraceMS   int    `json:"grace_ms"`
	}{p.ProcessID, grace})
	return permission.Request{Tool: "process_stop", Args: args, Risk: permission.RiskExec, Rememberable: true}
}
func authorizeProcessControlPolicy(ctx context.Context, evaluator any, req permission.Request) error {
	policy, ok := evaluator.(processControlInvocationPolicy)
	if !ok {
		return ErrProcessControlDenied
	}
	decision, err := policy.EvaluateInvocationPolicy(ctx, req)
	if err != nil || decision.Denied || ctx.Err() != nil {
		return ErrProcessControlDenied
	}
	return nil
}
