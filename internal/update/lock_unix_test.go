//go:build darwin || linux

package update

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestUpdateLockWaitHonorsCancellation(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	first, err := openUpdateLock(t.Context(), root, ".snow.update.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openUpdateLock(ctx, root, ".snow.update.lock"); !errors.Is(err, context.Canceled) {
		t.Fatalf("contended lock error = %v, want context cancellation", err)
	}
}
