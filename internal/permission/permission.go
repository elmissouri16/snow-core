// Package permission implements the ask/allow/deny decision service that
// gates mutating and exec tools.
package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Mode is the permission policy.
type Mode string

const (
	ModeAsk   Mode = "ask"
	ModeAllow Mode = "allow"
	ModeDeny  Mode = "deny"
)

// Risk classifies a tool request.
type Risk string

const (
	RiskRead  Risk = "read"
	RiskWrite Risk = "write"
	RiskExec  Risk = "exec"
	RiskNet   Risk = "network"
)

// Request describes a tool invocation for authorization.
type Request struct {
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
	Paths  []string        `json:"paths,omitempty"`
	Risk   Risk            `json:"risk"`
	Reason string          `json:"reason,omitempty"`
}

// Decision is the authorization outcome.
type Decision string

const (
	DecisionAllow         Decision = "allow"
	DecisionDeny          Decision = "deny"
	DecisionAllowSession  Decision = "allow_session"
	DecisionAllowAlways   Decision = "allow_always"
)

// Asker resolves interactive ask-mode requests. The TUI and SDK provide
// implementations; headless callers use a deny-by-default asker.
type Asker interface {
	Ask(ctx context.Context, req Request) (Decision, error)
}

// DenyAll is an Asker that always denies (safe headless default).
type DenyAll struct{}

// Ask implements Asker.
func (DenyAll) Ask(context.Context, Request) (Decision, error) { return DecisionDeny, nil }

// AllowAll is an Asker that always allows (dangerous; CI only).
type AllowAll struct{}

// Ask implements Asker.
func (AllowAll) Ask(context.Context, Request) (Decision, error) { return DecisionAllow, nil }

// Service is the permission gate used by the agent loop and tools.
type Service interface {
	Mode() Mode
	SetMode(Mode)
	// Authorize returns the decision for req, blocking on the Asker when mode
	// is ask and no cached rule applies. An error means the request is denied.
	Authorize(ctx context.Context, req Request) (Decision, error)
	// Remember records a session-scoped rule (allow_session / deny).
	Remember(req Request, d Decision)
	SetAsker(Asker)
}

// SimpleService is the default in-memory implementation.
type SimpleService struct {
	mu    sync.Mutex
	mode  Mode
	asker Asker
	rules map[string]Decision // tool:decision for session rules
}

// NewService creates a service with the given mode. asker defaults to
// DenyAll when mode is ask and none is provided.
func NewService(mode Mode, asker Asker) *SimpleService {
	if asker == nil {
		asker = DenyAll{}
	}
	return &SimpleService{mode: mode, asker: asker, rules: make(map[string]Decision)}
}

// Mode returns the current mode.
func (s *SimpleService) Mode() Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// SetMode changes the mode.
func (s *SimpleService) SetMode(m Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

// SetAsker replaces the interactive asker.
func (s *SimpleService) SetAsker(a Asker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asker = a
}

// Remember stores a session-scoped rule keyed by tool+risk.
func (s *SimpleService) Remember(req Request, d Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[ruleKey(req)] = d
}

func ruleKey(req Request) string {
	return fmt.Sprintf("%s|%s", req.Tool, req.Risk)
}

// Authorize evaluates the request.
func (s *SimpleService) Authorize(ctx context.Context, req Request) (Decision, error) {
	s.mu.Lock()
	mode := s.mode
	rules := make(map[string]Decision, len(s.rules))
	for k, v := range s.rules {
		rules[k] = v
	}
	s.mu.Unlock()

	switch mode {
	case ModeDeny:
		// Read-only requests are always allowed (reads are not mutating).
		if req.Risk == RiskRead {
			return DecisionAllow, nil
		}
		return DecisionDeny, nil
	case ModeAllow:
		return DecisionAllow, nil
	}

	// ModeAsk
	if req.Risk == RiskRead {
		return DecisionAllow, nil
	}
	if d, ok := rules[ruleKey(req)]; ok {
		return d, nil
	}
	s.mu.Lock()
	asker := s.asker
	s.mu.Unlock()
	if asker == nil {
		return DecisionDeny, nil
	}
	d, err := asker.Ask(ctx, req)
	if err != nil {
		return DecisionDeny, err
	}
	if d == DecisionAllowAlways {
		s.Remember(req, DecisionAllow)
	}
	return d, nil
}
