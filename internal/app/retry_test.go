package app

import (
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
)

func TestAgentRetryOptionsMapsNormalAndGoalProfiles(t *testing.T) {
	cfg := config.DefaultRetry()
	got := agentRetryOptions(cfg)
	if got.Normal.MaxAttempts != cfg.Normal.MaxAttempts || got.Normal.MaxElapsed != 5*time.Minute || got.Normal.InitialDelay != time.Second || got.Normal.MaxDelay != 30*time.Second || got.Normal.Jitter != 0.20 {
		t.Fatalf("normal=%+v", got.Normal)
	}
	if got.Goal.MaxAttempts != cfg.Goal.MaxAttempts || got.Goal.MaxElapsed != 30*time.Minute || got.Goal.InitialDelay != 2*time.Second || got.Goal.MaxDelay != 2*time.Minute || got.Goal.Jitter != 0.20 {
		t.Fatalf("goal=%+v", got.Goal)
	}
}
