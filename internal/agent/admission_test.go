package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdmissionCancellationWhileLockHeld(t *testing.T) {
	a := &Agent{}
	unlock := a.LockAdmission()
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, err := a.LockAdmissionContext(ctx)
		if release != nil {
			release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("admission error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled admission remained blocked")
	}
}
func TestCanceledAdmissionDoesNotAcquireFreeLock(t *testing.T) {
	a := &Agent{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	release, err := a.LockAdmissionContext(ctx)
	if release != nil {
		release()
		t.Fatal("canceled caller acquired admission")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unlock := a.LockAdmission()
	unlock()
}
