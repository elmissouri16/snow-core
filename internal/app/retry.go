package app

import (
	"time"

	"github.com/elmissouri16/snow-core/internal/agent"
	"github.com/elmissouri16/snow-core/internal/config"
)

func agentRetryOptions(cfg config.RetryConfig) agent.RetryOptions {
	profile := func(value config.RetryProfileConfig) agent.RetryProfile {
		return agent.RetryProfile{
			MaxAttempts:  value.MaxAttempts,
			MaxElapsed:   time.Duration(value.MaxElapsedMS) * time.Millisecond,
			InitialDelay: time.Duration(value.InitialDelayMS) * time.Millisecond,
			MaxDelay:     time.Duration(value.MaxDelayMS) * time.Millisecond,
			Jitter:       float64(value.JitterPercent) / 100,
		}
	}
	return agent.RetryOptions{Normal: profile(cfg.Normal), Goal: profile(cfg.Goal)}
}
