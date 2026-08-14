package provider

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestWrapIdleReadCloserTimesOutBlockedRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	wrapped := WrapIdleReadCloser(reader, 25*time.Millisecond)
	defer wrapped.Close()
	result := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1)
		_, err := wrapped.Read(buffer)
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrStreamIdle) {
			t.Fatalf("Read error=%v, want ErrStreamIdle", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle wrapper did not unblock stalled read")
	}
}

func TestWrapIdleReadCloserDisabledReturnsOriginal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	if got := WrapIdleReadCloser(reader, 0); got != reader {
		t.Fatal("disabled wrapper replaced reader")
	}
}
