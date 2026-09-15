package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestVendoredHTMXMatchesReviewedAsset(t *testing.T) {
	for name, want := range map[string]string{
		"htmx-2.0.10.min.js": "71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de",
		"htmx-LICENSE":       "d3d2456f76414f2456104660ebd65aff1c04cd7966b942bdabd63f3cdb316a38",
	} {
		data, err := assets.ReadFile("static/vendor/" + name)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != want {
			t.Fatalf("%s differs from reviewed upstream asset", name)
		}
	}
}
