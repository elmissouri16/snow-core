package snowsdk

import (
	"context"
	"testing"
)

func TestRetryOptionsValidateThroughSDKOpen(t *testing.T) {
	valid := &RetryOptions{
		Normal: RetryProfile{MaxAttempts: 2, MaxElapsedMS: 100, InitialDelayMS: 1, MaxDelayMS: 10},
		Goal:   RetryProfile{MaxAttempts: 3, MaxElapsedMS: 200, InitialDelayMS: 1, MaxDelayMS: 20},
	}
	session, err := Open(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Retry: valid})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	invalid := *valid
	invalid.Normal.MaxAttempts = 0
	if _, err := Open(context.Background(), Options{CWD: t.TempDir(), Provider: "fake", NoSession: true, NoPlugins: true, NoMCP: true, NoSkills: true, Retry: &invalid}); err == nil {
		t.Fatal("invalid retry override was accepted")
	}
}
