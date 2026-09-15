package web

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type streamTestReader struct {
	response *http.Response
	scanner  *bufio.Scanner
}

func (s streamTestReader) next(t *testing.T) (string, []byte) {
	t.Helper()
	event, data := "", ""
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" && event != "" {
			return event, []byte(data)
		}
		if value, ok := strings.CutPrefix(line, "event: "); ok {
			event = value
		}
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			data = value
		}
	}
	t.Fatalf("stream ended before event: %v", s.scanner.Err())
	return "", nil
}

func streamHTTPFixture(t *testing.T, policy streamPolicy) (*shell, *http.Cookie, *RuntimeManager, *liveRuntime, *httptest.Server, string) {
	t.Helper()
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "stream", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, r := streamRuntimeFixture(t, project.ID)
	s.runtimes = m
	mux := http.NewServeMux()
	mux.Handle("GET /projects/{project}/runtime/events", s.runtimeEventsHandlerPolicy(policy))
	server := httptest.NewUnstartedServer(mux)
	// Short ordinary deadlines prove only SSE gets per-write overrides.
	server.Config.WriteTimeout = 10 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	return s, cookie, m, r, server, "/projects/" + project.ID + "/runtime/events?instance_id=instance-one"
}

func shortStreamPolicy() streamPolicy {
	return streamPolicy{coalesce: 20 * time.Millisecond, heartbeat: 30 * time.Millisecond,
		auth: 20 * time.Millisecond, lifetime: 2 * time.Second, write: 200 * time.Millisecond}
}

func openTestStream(t *testing.T, server *httptest.Server, path string, cookie *http.Cookie) streamTestReader {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), streamSnapshotBytes+1024)
	return streamTestReader{response: response, scanner: scanner}
}

func TestRuntimeStreamSnapshotsCoalesceAndKeepSameGeneration(t *testing.T) {
	_, cookie, _, runtime, server, path := streamHTTPFixture(t, shortStreamPolicy())
	stream := openTestStream(t, server, path, cookie)
	if stream.response.StatusCode != 200 || !strings.HasPrefix(stream.response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream response: %+v", stream.response)
	}
	event, data := stream.next(t)
	var initial RuntimeSnapshot
	if event != "snapshot" || json.Unmarshal(data, &initial) != nil || initial.Revision != 1 {
		t.Fatalf("initial: %s %s", event, data)
	}
	// An initial response is already flushed, independently of later updates.
	// Updates happen well beyond the server's ordinary whole-response deadline.
	time.Sleep(40 * time.Millisecond)
	runtime.mu.Lock()
	for range 1000 {
		runtime.snapshot.SessionName = "latest"
		runtime.publishLocked()
	}
	runtime.addMessage(RuntimeMessage{Role: "assistant", Text: "**public**\n<script>no</script>"})
	runtime.publishLocked()
	runtime.mu.Unlock()
	event, data = stream.next(t)
	var latest RuntimeSnapshot
	if event != "snapshot" || json.Unmarshal(data, &latest) != nil || latest.Revision != 1002 || latest.SessionName != "latest" {
		t.Fatalf("latest coalesced state: %s %s", event, data)
	}
	if !strings.Contains(latest.Messages[0].HTML, "<strong>public</strong>") || strings.Contains(latest.Messages[0].HTML, "<script") {
		t.Fatal("SSE did not use the sanitized public display projection")
	}
	runtime.mu.Lock()
	runtime.instanceID, runtime.snapshot.InstanceID = "replacement", "replacement"
	runtime.snapshot.SessionID = "private-replacement-session"
	runtime.publishLocked()
	runtime.mu.Unlock()
	event, data = stream.next(t)
	if event != "closed" || strings.Contains(string(data), "replacement") || !strings.Contains(string(data), "instance-one") {
		t.Fatalf("stream followed replacement authority: %s %s", event, data)
	}
	if stream.scanner.Scan() {
		t.Fatal("terminal generation event did not close the connection")
	}
}

func TestRuntimeStreamRevokeExpiryAndDurableFailure(t *testing.T) {
	for _, reason := range []string{"revoke", "expire", "storage"} {
		t.Run(reason, func(t *testing.T) {
			s, cookie, _, runtime, server, path := streamHTTPFixture(t, shortStreamPolicy())
			stream := openTestStream(t, server, path, cookie)
			stream.next(t)
			if reason == "revoke" {
				response := request(t, s, "POST", "/logout", url.Values{"csrf": {csrfFor(t, s, cookie)}}, cookie)
				if response.Code != http.StatusSeeOther {
					t.Fatalf("logout: %d", response.Code)
				}
			}
			s.access.mu.Lock()
			switch reason {
			case "expire":
				key := sha256.Sum256([]byte(cookie.Value))
				browser := s.access.sessions[key]
				browser.Created = time.Now().Add(-browserLifetime - time.Second)
				s.access.sessions[key] = browser
			case "storage":
				s.access.failed = true
			}
			s.access.mu.Unlock()
			event, _ := stream.next(t)
			if event != "auth_required" {
				t.Fatalf("revoked session received %s", event)
			}
			if stream.scanner.Scan() {
				t.Fatal("authorization failure did not terminate stream")
			}
			if runtime.ctx.Err() != nil {
				t.Fatal("browser authority loss canceled worker")
			}
		})
	}
}

func TestRuntimeStreamClosedMissingStaleAndDisconnect(t *testing.T) {
	_, cookie, manager, runtime, server, path := streamHTTPFixture(t, shortStreamPolicy())
	stale := openTestStream(t, server, strings.ReplaceAll(path, "instance-one", "stale"), cookie)
	if event, _ := stale.next(t); event != "closed" {
		t.Fatal("stale instance did not receive authoritative closed event")
	}
	stream := openTestStream(t, server, path, cookie)
	stream.next(t)
	_ = stream.response.Body.Close()
	deadline := time.Now().Add(time.Second)
	for {
		runtime.mu.Lock()
		count := len(runtime.subscribers)
		runtime.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnect leaked subscriber")
		}
		time.Sleep(time.Millisecond)
	}
	if runtime.ctx.Err() != nil {
		t.Fatal("disconnect aborted worker")
	}
	active := openTestStream(t, server, path, cookie)
	active.next(t)
	if err := manager.CloseProject(t.Context(), runtime.snapshot.ProjectID, "instance-one"); err != nil {
		t.Fatal(err)
	}
	if event, _ := active.next(t); event != "closed" {
		t.Fatal("closing runtime did not notify subscriber")
	}
	missing := openTestStream(t, server, path, cookie)
	if event, _ := missing.next(t); event != "closed" {
		t.Fatal("missing runtime was implicitly activated")
	}
}

func TestRuntimeStreamHTTPAdmissionAndAuthBoundary(t *testing.T) {
	s, cookie, _, _, server, path := streamHTTPFixture(t, shortStreamPolicy())
	for _, test := range []struct {
		path   string
		cookie *http.Cookie
		status int
	}{{path, nil, 401}, {strings.Split(path, "?")[0], cookie, 400}, {path + "&instance_id=duplicate", cookie, 400}} {
		stream := openTestStream(t, server, test.path, test.cookie)
		if stream.response.StatusCode != test.status {
			t.Fatalf("admission = %d, want %d", stream.response.StatusCode, test.status)
		}
	}
	for range streamBrowserLimit {
		stream := openTestStream(t, server, path, cookie)
		stream.next(t)
	}
	if response := openTestStream(t, server, path, cookie).response; response.StatusCode != 429 {
		t.Fatal("per-browser stream cap not enforced")
	}
	// Run through the production boundary, not just the test stream mux.
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Host = "evil.invalid" },
		func(r *http.Request) { r.Header.Set("Origin", "https://evil.invalid") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		request := httptest.NewRequest("GET", testOrigin+path, nil)
		request.URL.Scheme, request.URL.Host = "", ""
		request.AddCookie(cookie)
		mutate(request)
		response := httptest.NewRecorder()
		s.handler().ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("stream bypassed browser origin boundary: %d", response.Code)
		}
	}
	s.runtimes = &fakeRuntime{}
	if response := request(t, s, "GET", path, nil, cookie); response.Code != 501 {
		t.Fatalf("legacy backend cannot distinguish unsupported streaming: %d", response.Code)
	}
}

func TestRuntimeStreamGlobalLimitAndRelease(t *testing.T) {
	limiter := new(streamLimiter)
	var releases []func()
	for i := range streamGlobalLimit {
		key := [32]byte{byte(i / streamBrowserLimit)}
		release, ok := limiter.acquire(key)
		if !ok {
			t.Fatalf("slot %d rejected", i)
		}
		releases = append(releases, release)
	}
	if _, ok := limiter.acquire([32]byte{99}); ok {
		t.Fatal("global stream cap exceeded")
	}
	for _, release := range releases {
		release()
		release()
	}
	if limiter.total != 0 || len(limiter.browsers) != 0 {
		t.Fatal("stream limits leak browser identifiers or slots")
	}
}

type streamDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	writeErr  error
	flushErr  error
}

func (w *streamDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}
func (w *streamDeadlineWriter) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(data)
}
func (w *streamDeadlineWriter) FlushError() error { return w.flushErr }

func TestRuntimeStreamBoundedFramesDeadlinesAndLifetime(t *testing.T) {
	writer := &streamDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	controller := http.NewResponseController(writer)
	if err := writeStreamEvent(writer, controller, time.Second, "snapshot", map[string]string{"text": "one\ntwo"}); err != nil {
		t.Fatal(err)
	}
	if len(writer.deadlines) != 2 || writer.deadlines[0].IsZero() || !writer.deadlines[1].IsZero() || !strings.Contains(writer.Body.String(), `one\ntwo`) {
		t.Fatal("frame boundaries or per-write deadlines incorrect")
	}
	before := writer.Body.Len()
	if err := writeStreamEvent(writer, controller, time.Second, "snapshot", strings.Repeat("x", streamSnapshotBytes+1)); err == nil || writer.Body.Len() != before {
		t.Fatal("oversized frame partially written or not bounded")
	}
	writer.writeErr = io.ErrClosedPipe
	if err := writeStreamFrame(writer, controller, time.Second, []byte("test")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("write error ignored")
	}
	writer.writeErr = nil
	writer.flushErr = io.ErrClosedPipe
	if err := writeStreamFrame(writer, controller, time.Second, []byte("test")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("flush error ignored")
	}
	policy := shortStreamPolicy()
	policy.lifetime = 80 * time.Millisecond
	_, cookie, _, runtime, server, path := streamHTTPFixture(t, policy)
	stream := openTestStream(t, server, path, cookie)
	stream.next(t)
	for stream.scanner.Scan() {
	}
	if stream.scanner.Err() != nil || runtime.ctx.Err() != nil {
		t.Fatal("bounded stream lifetime failed or aborted worker")
	}
}

// Periodic checks use durable authority, not merely an in-memory cookie map,
// while ordinary streaming reads must never rotate or rewrite credentials.
func TestRuntimeStreamDurableAuthorityRecheckedWithoutWrites(t *testing.T) {
	s, cookie, _, runtime, server, path := streamHTTPFixture(t, shortStreamPolicy())
	if err := s.restoreAccess(t.Context(), s.registry); err != nil {
		t.Fatal(err)
	}
	before, err := s.registry.root.Stat(accessFile)
	if err != nil {
		t.Fatal(err)
	}
	stream := openTestStream(t, server, path, cookie)
	stream.next(t)
	time.Sleep(65 * time.Millisecond)
	after, err := s.registry.root.Stat(accessFile)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.ModTime() != after.ModTime() || before.Size() != after.Size() {
		t.Fatal("stream auth reads rewrote durable credentials")
	}
	file, err := s.registry.root.OpenFile(accessFile, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(" ")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("modify fixture: %v %v", writeErr, closeErr)
	}
	if event, _ := stream.next(t); event != "auth_required" {
		t.Fatal("stream ignored changed durable credential authority")
	}
	if runtime.ctx.Err() != nil {
		t.Fatal("access invalidation canceled the runtime")
	}
}
