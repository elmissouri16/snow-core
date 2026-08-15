//go:build darwin || linux

package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLifecycleOutputIsBounded(t *testing.T) {
	out, err := boundedCombinedOutput(context.Background(), "sh", "-c", "yes x | head -c 200000; exit 7")
	if err == nil {
		t.Fatal("expected non-zero lifecycle command")
	}
	if len(out) > lifecycleOutputLimit+64 || !strings.Contains(string(out), "lifecycle output truncated") {
		t.Fatalf("bounded output length=%d suffix=%q", len(out), string(out[max(0, len(out)-80):]))
	}
}

func TestLifecycleCommandHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := boundedCombinedOutput(ctx, "sh", "-c", "trap '' INT; sleep 30 & wait")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}
