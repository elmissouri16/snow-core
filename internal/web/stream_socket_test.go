package web

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// net.Pipe has no socket send buffer: an unread peer deterministically blocks
// real net/http writes. No fake ResponseWriter or injected write error is used.
type streamPipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func (l *streamPipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *streamPipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}
func (l *streamPipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

type streamObservedPipe struct {
	net.Conn
	entered chan struct{}
	result  chan error
	once    sync.Once
}

func (c *streamObservedPipe) Write(data []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	n, err := c.Conn.Write(data)
	if err != nil {
		select {
		case c.result <- err:
		default:
		}
	}
	return n, err
}

// Gate only initial snapshot delivery, after the real manager has registered
// each observer. This allows deterministic cap admission before short write
// deadlines start, without holding the runtime mutex or replacing transport IO.
type streamSocketBarrier struct {
	RuntimeBackend
	subscriber RuntimeSubscriber
	ready      chan struct{}
	release    <-chan struct{}
}

func (b *streamSocketBarrier) Subscribe(project, instance string) (RuntimeSnapshot, RuntimeSubscription, error) {
	snapshot, subscription, err := b.subscriber.Subscribe(project, instance)
	if err == nil {
		b.ready <- struct{}{}
		<-b.release
	}
	return snapshot, subscription, err
}

func TestRuntimeStreamBlockedSocketDeadlineReleasesAdmission(t *testing.T) {
	s, cookie, _ := projectShell(t)
	project, err := s.registry.Add(t.Context(), "blocked-stream", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, runtime := streamRuntimeFixture(t, project.ID)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	barrier := &streamSocketBarrier{RuntimeBackend: manager, subscriber: manager,
		ready: make(chan struct{}, streamBrowserLimit+1), release: release}
	s.runtimes = barrier
	policy := streamPolicy{coalesce: 20 * time.Millisecond, heartbeat: time.Second,
		auth: time.Second, lifetime: 10 * time.Second, write: 100 * time.Millisecond}
	mux := http.NewServeMux()
	mux.Handle("GET /projects/{project}/runtime/events", s.runtimeEventsHandlerPolicy(policy))
	returned := make(chan struct{}, streamBrowserLimit+2)
	listener := &streamPipeListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		// Longer than the test's assertion budget. Only the SSE per-write
		// deadline can release a blocked writer before that budget expires.
		WriteTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { returned <- struct{}{} }()
			mux.ServeHTTP(w, r)
		}),
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	t.Cleanup(func() {
		unblock()
		_ = server.Close()
		select {
		case err := <-serveResult:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("HTTP server shutdown: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("HTTP server did not shut down")
		}
	})
	path := "/projects/" + project.ID + "/runtime/events?instance_id=instance-one"
	connect := func() (net.Conn, *streamObservedPipe) {
		t.Helper()
		serverSide, client := net.Pipe()
		observed := &streamObservedPipe{Conn: serverSide, entered: make(chan struct{}), result: make(chan error, 1)}
		t.Cleanup(func() { _ = client.Close() })
		if err := client.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		select {
		case listener.connections <- observed:
		case <-time.After(time.Second):
			t.Fatal("HTTP server did not accept pipe")
		}
		if _, err := fmt.Fprintf(client, "GET %s HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\nConnection: close\r\n\r\n", path, s.host, cookie.Name, cookie.Value); err != nil {
			t.Fatal(err)
		}
		return client, observed
	}
	wait := func(signal <-chan struct{}, operation string) {
		t.Helper()
		select {
		case <-signal:
		case <-time.After(time.Second):
			t.Fatalf("%s exceeded its bounded wait", operation)
		}
	}

	var blocked []*streamObservedPipe
	for range streamBrowserLimit {
		_, observed := connect() // Deliberately never read these clients.
		blocked = append(blocked, observed)
		wait(barrier.ready, "subscription admission")
	}
	denied, _ := connect()
	response, err := http.ReadResponse(bufio.NewReader(denied), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("full browser cap returned %d", response.StatusCode)
	}
	_ = response.Body.Close()
	_ = denied.Close()
	wait(returned, "rejected request cleanup")

	unblock()
	for _, connection := range blocked {
		wait(connection.entered, "real transport write entry")
	}
	// Exercise exactly the revision publication path used by the RPC drain
	// while the HTTP writers have no readers. Their queues must stay at one.
	published := make(chan struct{})
	go func() {
		defer close(published)
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		for range 1000 {
			runtime.publishLocked()
		}
	}()
	wait(published, "RPC projection publication with unread HTTP peers")
	for _, connection := range blocked {
		select {
		case err := <-connection.result:
			t.Fatalf("publication did not finish while the socket was blocked: %v", err)
		default:
		}
	}
	runtime.mu.Lock()
	if len(runtime.subscribers) != streamBrowserLimit {
		runtime.mu.Unlock()
		t.Fatal("publication did not overlap all blocked HTTP observers")
	}
	for subscription := range runtime.subscribers {
		if len(subscription.changes) != 1 {
			runtime.mu.Unlock()
			t.Fatal("blocked network writer accumulated an unbounded notification queue")
		}
	}
	runtime.mu.Unlock()
	for _, connection := range blocked {
		select {
		case err := <-connection.result:
			if networkError, ok := errors.AsType[net.Error](err); !ok || !networkError.Timeout() {
				t.Fatalf("unread pipe ended without its write deadline: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("blocked socket write was not bounded by the SSE write deadline")
		}
		wait(returned, "timed-out stream cleanup")
	}
	runtime.mu.Lock()
	remaining := len(runtime.subscribers)
	revision := runtime.snapshot.Revision
	runtime.mu.Unlock()
	if remaining != 0 || revision != 1001 || runtime.ctx.Err() != nil {
		t.Fatalf("slow readers leaked observers or affected worker: subscribers=%d revision=%d worker=%v", remaining, revision, runtime.ctx.Err())
	}

	// Same handler and browser, formerly at the cap: a healthy fresh stream
	// must now be admitted, proving HTTP slots as well as observers released.
	healthy, _ := connect()
	response, err = http.ReadResponse(bufio.NewReader(healthy), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("deadline cleanup leaked browser admission: %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	reader := streamTestReader{response: response, scanner: scanner}
	if event, _ := reader.next(t); event != "snapshot" {
		t.Fatalf("fresh observer received %s instead of snapshot", event)
	}
	_ = healthy.Close()
	_ = response.Body.Close()
	wait(returned, "healthy browser disconnect cleanup")
	if runtime.ctx.Err() != nil {
		t.Fatal("connection cancellation aborted the runtime")
	}
}
