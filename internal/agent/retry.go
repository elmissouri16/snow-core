package agent

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// RetryProfile bounds one consecutive provider-failure episode. Both the
// attempt and elapsed limits must permit another request.
type RetryProfile struct {
	MaxAttempts  int
	MaxElapsed   time.Duration
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Jitter       float64
}

// RetryOptions keeps ordinary interactive recovery bounded while allowing an
// automatic goal to ride through a substantially longer provider outage.
type RetryOptions struct {
	Normal RetryProfile
	Goal   RetryProfile
}

// DefaultRetryOptions returns the operator defaults used by standalone agents.
func DefaultRetryOptions() RetryOptions {
	return RetryOptions{
		Normal: RetryProfile{MaxAttempts: 12, MaxElapsed: 5 * time.Minute, InitialDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: 0.20},
		Goal:   RetryProfile{MaxAttempts: 30, MaxElapsed: 30 * time.Minute, InitialDelay: 2 * time.Second, MaxDelay: 2 * time.Minute, Jitter: 0.20},
	}
}

func (o RetryOptions) validate() error {
	if o == (RetryOptions{}) {
		return nil
	}
	if err := o.Normal.validate(); err != nil {
		return errors.New("agent: invalid normal retry policy: " + err.Error())
	}
	if err := o.Goal.validate(); err != nil {
		return errors.New("agent: invalid goal retry policy: " + err.Error())
	}
	return nil
}

func (p RetryProfile) validate() error {
	if p.MaxAttempts < 1 || p.MaxAttempts > 100 {
		return errors.New("max attempts must be 1..100")
	}
	if p.MaxElapsed <= 0 || p.MaxElapsed > 24*time.Hour {
		return errors.New("max elapsed must be positive and at most 24h")
	}
	if p.InitialDelay <= 0 || p.InitialDelay > p.MaxDelay {
		return errors.New("initial delay must be positive and no greater than max delay")
	}
	if p.MaxDelay > time.Hour {
		return errors.New("max delay must be at most 1h")
	}
	if p.Jitter < 0 || p.Jitter > 1 {
		return errors.New("jitter must be 0..1")
	}
	return nil
}

func (a *Agent) retryProfile() RetryProfile {
	a.mu.RLock()
	goal := a.turnOrigin == "goal" || a.autoRunning && a.opts.Goal != nil
	a.mu.RUnlock()
	if goal {
		return a.opts.Retry.Goal
	}
	return a.opts.Retry.Normal
}

func retryBackoff(profile RetryProfile, failure int, advice provider.RetryAdvice) time.Duration {
	delay := profile.InitialDelay
	for i := 1; i < failure && delay < profile.MaxDelay; i++ {
		if delay > profile.MaxDelay/2 {
			delay = profile.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > profile.MaxDelay {
		delay = profile.MaxDelay
	}
	if profile.Jitter > 0 {
		factor := 1 - profile.Jitter + rand.Float64()*(2*profile.Jitter)
		delay = time.Duration(float64(delay) * factor)
		if delay < 0 {
			delay = 0
		}
	}
	if advice.RetryAfter > delay {
		delay = advice.RetryAfter
	}
	return delay
}

func (a *Agent) scheduleProviderRetry(ctx context.Context, profile RetryProfile, started time.Time, attempt int, activity bool, advice provider.RetryAdvice) (time.Duration, bool) {
	if attempt >= profile.MaxAttempts || ctx.Err() != nil {
		return 0, false
	}
	elapsed := time.Since(started)
	delay := retryBackoff(profile, attempt, advice)
	if elapsed >= profile.MaxElapsed || delay > profile.MaxElapsed-elapsed {
		return 0, false
	}
	phase := "pre_activity"
	if activity {
		phase = "recovery"
	}
	a.publish(protocol.AgentEvent{Type: protocol.EvProviderRetry, ProviderRetry: &protocol.ProviderRetry{
		Provider: a.Model().Provider, Kind: string(advice.Kind), Phase: phase,
		Attempt: attempt + 1, MaxAttempts: profile.MaxAttempts, DelayMS: delay.Milliseconds(),
		ElapsedMS: elapsed.Milliseconds(), MaxElapsedMS: profile.MaxElapsed.Milliseconds(),
	}})
	return delay, true
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
