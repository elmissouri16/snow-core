package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type startupWriter struct{ ready chan string }

func (w startupWriter) Write(data []byte) (int, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "http://") {
			w.ready <- line
		}
	}
	return len(data), nil
}

func TestRunServesAndJoinsShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- Run(ctx, Options{Listen: "127.0.0.1:0"}, startupWriter{ready}) }()
	var origin string
	select {
	case origin = <-ready:
	case err := <-done:
		t.Fatalf("startup failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timeout")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	client.CloseIdleConnections()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login page = %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown = %v", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("shutdown did not join server")
	}
}
