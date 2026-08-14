package provider

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStreamIdle is returned when a streaming response produces no bytes within
// its configured idle interval.
var ErrStreamIdle = errors.New("provider: stream idle timeout")

// DefaultStreamIdleTimeout bounds a silent live response without imposing a
// total request duration. Any received bytes reset the watchdog.
const DefaultStreamIdleTimeout = 10 * time.Minute

// WrapIdleReadCloser closes a stalled stream body so a blocked Read unblocks.
// A non-positive timeout leaves the original body unchanged.
func WrapIdleReadCloser(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	wrapped := &idleReadCloser{
		body: body, timeout: timeout,
		reset: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go wrapped.watch()
	return wrapped
}

type idleReadCloser struct {
	body      io.ReadCloser
	timeout   time.Duration
	reset     chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	timedOut  atomic.Bool
}

func (r *idleReadCloser) watch() {
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-r.reset:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(r.timeout)
		case <-timer.C:
			r.timedOut.Store(true)
			_ = r.body.Close()
			return
		}
	}
}

func (r *idleReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		select {
		case r.reset <- struct{}{}:
		default:
		}
		return n, err
	}
	if err != nil && r.timedOut.Load() {
		return n, ErrStreamIdle
	}
	return n, err
}

func (r *idleReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.done) })
	return r.body.Close()
}
