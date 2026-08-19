package tui

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestOpenOAuthBrowserReapsChild(t *testing.T) {
	originalCommand, originalReap := oauthBrowserCommand, oauthBrowserReap
	defer func() { oauthBrowserCommand, oauthBrowserReap = originalCommand, originalReap }()
	done := make(chan error, 1)
	oauthBrowserCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestOAuthBrowserHelperProcess")
		cmd.Env = append(os.Environ(), "SNOW_OAUTH_HELPER=1")
		return cmd
	}
	oauthBrowserReap = func(cmd *exec.Cmd) { go func() { done <- cmd.Wait() }() }
	if err := openOAuthBrowser(context.Background(), "https://example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("browser child reap: %v", err)
	}
}

func TestOAuthBrowserHelperProcess(t *testing.T) {
	if os.Getenv("SNOW_OAUTH_HELPER") != "1" {
		return
	}
	os.Exit(0)
}
