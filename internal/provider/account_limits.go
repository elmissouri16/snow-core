package provider

import (
	"context"
	"errors"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type accountLimit struct {
	identity string
	kind     string
	resetsAt time.Time
}

// AccountQuota returns only quota observed by this runtime for this exact
// credential identity. It neither probes a provider nor infers remaining credit.
func (p *Authenticated) AccountQuota(account, identity string) (string, time.Time) {
	p.accountMu.RLock()
	limit, ok := p.limits[account]
	p.accountMu.RUnlock()
	if !ok || identity == "" || limit.identity != identity || (!limit.resetsAt.IsZero() && !time.Now().Before(limit.resetsAt)) {
		return "", time.Time{}
	}
	return limit.kind, limit.resetsAt
}

func (p *Authenticated) observeAccountLimit(credential auth.Credential, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	limit, ok := errors.AsType[*LimitError](err)
	if !ok || limit.Kind == "" {
		return
	}
	p.accountMu.Lock()
	defer p.accountMu.Unlock()
	if p.limits == nil {
		p.limits = make(map[string]accountLimit)
	}
	p.limits[credential.Account] = accountLimit{identity: auth.AccountIdentity(credential), kind: limit.Kind, resetsAt: limit.ResetsAt}
}

type accountObservingStream struct {
	protocol.EventStream
	provider   *Authenticated
	credential auth.Credential
}

func (s *accountObservingStream) Next(ctx context.Context) (protocol.StreamEvent, error) {
	event, err := s.EventStream.Next(ctx)
	s.provider.observeAccountLimit(s.credential, err)
	if event.Type == protocol.EvStreamError {
		s.provider.observeAccountLimit(s.credential, event.Err)
	}
	return event, err
}
