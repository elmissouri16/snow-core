package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	streamGlobalLimit   = 16
	streamBrowserLimit  = 4
	streamSnapshotBytes = 4 << 20
)

type streamPolicy struct {
	coalesce, heartbeat, auth, lifetime, write time.Duration
}

var defaultStreamPolicy = streamPolicy{
	coalesce: 75 * time.Millisecond, heartbeat: 10 * time.Second,
	auth: 5 * time.Second, lifetime: 10 * time.Minute, write: 5 * time.Second,
}

// One limiter belongs to the HTTP handler, not to a project or runtime. Hashed
// cookie keys are never emitted, persisted, or retained after the last stream.
type streamLimiter struct {
	mu       sync.Mutex
	total    int
	browsers map[[32]byte]int
}

func (l *streamLimiter) acquire(key [32]byte) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= streamGlobalLimit || l.browsers[key] >= streamBrowserLimit {
		return nil, false
	}
	if l.browsers == nil {
		l.browsers = make(map[[32]byte]int)
	}
	l.total++
	l.browsers[key]++
	return sync.OnceFunc(func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.total--
		l.browsers[key]--
		if l.browsers[key] == 0 {
			delete(l.browsers, key)
		}
	}), true
}

func (s *shell) runtimeEventsHandler() http.Handler {
	return s.runtimeEventsHandlerPolicy(defaultStreamPolicy)
}

func (s *shell) runtimeEventsHandlerPolicy(policy streamPolicy) http.Handler {
	limiter := new(streamLimiter)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.streamBrowser(r); !ok {
			http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
			return
		}
		backend, ok := s.runtimes.(RuntimeSubscriber)
		if !ok || s.registry == nil {
			http.Error(w, "Live events unavailable", http.StatusNotImplemented)
			return
		}
		projectID, instanceID := r.PathValue("project"), r.URL.Query().Get("instance_id")
		if !runtimeIdentifier(instanceID) || len(r.URL.Query()["instance_id"]) != 1 {
			http.Error(w, "Expected runtime instance required", http.StatusBadRequest)
			return
		}
		cookie, _ := r.Cookie(sessionCookie) // Already authenticated above.
		release, ok := limiter.acquire(sha256.Sum256([]byte(cookie.Value)))
		if !ok {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "Too many live event streams", http.StatusTooManyRequests)
			return
		}
		defer release()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_, err := s.registry.Lookup(ctx, projectID)
		cancel()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		snapshot, sub, err := backend.Subscribe(projectID, instanceID)
		if err != nil && !errors.Is(err, ErrRuntimeClosed) && !errors.Is(err, ErrRuntimeInvalid) {
			http.Error(w, "Live events busy", http.StatusTooManyRequests)
			return
		}
		if sub != nil {
			defer sub.Close()
		}
		controller := http.NewResponseController(w)
		// Override the ordinary 15s whole-response deadline only for this SSE
		// response. Unsupported writers fail closed; no goroutine can remain
		// stuck behind a slow browser. Every frame gets a fresh deadline.
		if err := controller.SetWriteDeadline(time.Now().Add(policy.write)); err != nil {
			http.Error(w, "Live events unsupported", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		binding := streamBinding{ProjectID: projectID, InstanceID: instanceID}
		send := func(event string, value any) bool {
			return writeStreamEvent(w, controller, policy.write, event, value) == nil
		}
		if err != nil || sub == nil || !sameStreamBinding(snapshot, binding) {
			send("closed", binding)
			return
		}
		if !send("snapshot", displaySnapshot(snapshot)) {
			return
		}
		revision := snapshot.Revision
		coalesce := time.NewTicker(policy.coalesce)
		heartbeat := time.NewTicker(policy.heartbeat)
		auth := time.NewTicker(policy.auth)
		lifetime := time.NewTimer(policy.lifetime)
		defer coalesce.Stop()
		defer heartbeat.Stop()
		defer auth.Stop()
		defer lifetime.Stop()
		dirty := false
		for {
			select {
			case <-r.Context().Done():
				return
			case <-lifetime.C:
				return // Reconnect GET resyncs; it never replays a command.
			case <-auth.C:
				if _, ok := s.streamBrowser(r); !ok {
					send("auth_required", binding)
					return
				}
			case _, open := <-sub.Changes():
				if !open {
					send("closed", binding)
					return
				}
				dirty = true
			case <-coalesce.C:
				if !dirty {
					continue
				}
				dirty = false
				snapshot, ok := sub.Snapshot()
				if !ok || !sameStreamBinding(snapshot, binding) {
					send("closed", binding)
					return
				}
				if snapshot.Revision > revision {
					if !send("snapshot", displaySnapshot(snapshot)) {
						return
					}
					revision = snapshot.Revision
				}
			case <-heartbeat.C:
				if writeStreamFrame(w, controller, policy.write, []byte(": heartbeat\n\n")) != nil {
					return
				}
			}
		}
	})
}

// Durable authority is periodically rechecked through the existing bounded,
// identity-pinned access store. Reads do not rewrite the credential file.
func (s *shell) streamBrowser(r *http.Request) (browserSession, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	return s.browser(r.WithContext(ctx))
}

type streamBinding struct {
	ProjectID  string `json:"project_id"`
	InstanceID string `json:"instance_id"`
}

func sameStreamBinding(snapshot RuntimeSnapshot, binding streamBinding) bool {
	return snapshot.ProjectID == binding.ProjectID && snapshot.InstanceID == binding.InstanceID && snapshot.Status != "closing"
}

type streamBuffer struct{ bytes.Buffer }

func (b *streamBuffer) Write(p []byte) (int, error) {
	if len(p) > streamSnapshotBytes-b.Len() {
		return 0, errors.New("web: snapshot display limit")
	}
	return b.Buffer.Write(p)
}

func writeStreamEvent(w http.ResponseWriter, controller *http.ResponseController, timeout time.Duration, event string, value any) error {
	var data streamBuffer
	if err := json.MarshalWrite(&data, value); err != nil {
		return err
	}
	// JSON marshaling escapes embedded newlines; each frame is one data line.
	frame := fmt.Appendf(nil, "event: %s\ndata: ", event)
	frame = append(frame, data.Bytes()...)
	frame = append(frame, '\n', '\n')
	return writeStreamFrame(w, controller, timeout, frame)
}

func writeStreamFrame(w http.ResponseWriter, controller *http.ResponseController, timeout time.Duration, frame []byte) error {
	if err := controller.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := w.Write(frame); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil {
		return err
	}
	return controller.SetWriteDeadline(time.Time{})
}
