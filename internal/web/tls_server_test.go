package web

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type tlsStartupWriter struct {
	ready       chan string
	beforeServe func() error
}

func (w tlsStartupWriter) Write(data []byte) (int, error) {
	if w.beforeServe != nil {
		if err := w.beforeServe(); err != nil {
			return 0, err
		}
	}
	w.ready <- string(data)
	return len(data), nil
}

func TestTLSRunServesPreloadedHTTPSOnly(t *testing.T) {
	opts, pool := tlsFixture(t)
	// Even an explicit executable must remain dormant until browser activation.
	marker := filepath.Join(t.TempDir(), "worker-started")
	opts.Executable = filepath.Join(t.TempDir(), "forbidden-worker")
	if err := os.WriteFile(opts.Executable, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	opts.SessionsRoot = t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	// Remove both files before ServeTLS. Every subsequent handshake must use the
	// preloaded pair, not implicit ReadFile calls or a certificate reload hook.
	writer := tlsStartupWriter{ready: ready, beforeServe: func() error {
		if err := os.Remove(opts.TLSCertFile); err != nil {
			return err
		}
		return os.Remove(opts.TLSKeyFile)
	}}
	go func() { done <- Run(ctx, opts, writer) }()
	var output string
	select {
	case output = <-ready:
	case err := <-done:
		t.Fatalf("startup failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("TLS startup timeout")
	}
	var origin, code string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(line, "https://") {
			origin = line
		}
		if strings.HasPrefix(line, "Pairing code (") {
			_, code, _ = strings.Cut(line, "): ")
		}
	}
	if origin == "" || code == "" || strings.Contains(output, opts.TLSKeyFile) || strings.Contains(output, opts.TLSCertFile) {
		t.Fatal("unexpected startup output")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	do := func(method, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(t.Context(), method, origin+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", origin)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.TLS == nil || response.TLS.Version < tls.VersionTLS12 {
			t.Fatal("request not served using TLS policy")
		}
		if strings.HasPrefix(response.Header.Get("Location"), "http://") {
			t.Fatal("HTTPS redirected to plaintext")
		}
		return response
	}
	page := do(http.MethodGet, "/login", nil)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("login page = %d", page.StatusCode)
	}
	var pair *http.Cookie
	for _, cookie := range page.Cookies() {
		if cookie.Name == pairCookie {
			pair = cookie
		}
	}
	if pair == nil {
		t.Fatal("missing pairing CSRF cookie")
	}
	// render.go's parent-owned pairing page must use s.localCookie too.
	if !pair.Secure {
		t.Error("HTTPS pairing-CSRF cookie lacks Secure; wire render.go to s.localCookie")
	}
	login := do(http.MethodPost, "/login", url.Values{"csrf": {pair.Value}, "code": {code}}, pair)
	if login.StatusCode != http.StatusSeeOther {
		t.Fatalf("TLS pairing = %d", login.StatusCode)
	}
	var session *http.Cookie
	for _, cookie := range login.Cookies() {
		if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" {
			t.Error("HTTPS cookie attributes not strict and secure")
		}
		if cookie.Name == sessionCookie {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("missing session cookie")
	}
	// A new connection after pairing still uses the preloaded certificate.
	if response := do(http.MethodGet, "/healthz", nil, session); response.StatusCode != http.StatusOK {
		t.Fatal("HTTPS health check failed")
	}
	address := strings.TrimPrefix(origin, "https://")
	plain, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = plain.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(plain, "GET /login HTTP/1.1\r\nHost: "+address+"\r\nConnection: close\r\n\r\n")
	rejected, err := http.ReadResponse(bufio.NewReader(plain), nil)
	if err != nil {
		plain.Close()
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, rejected.Body)
	rejected.Body.Close()
	plain.Close()
	if rejected.StatusCode != http.StatusBadRequest || rejected.Header.Get("Location") != "" || len(rejected.Cookies()) != 0 {
		t.Fatal("plaintext was served or redirected")
	}
	old, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", address, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	if err == nil {
		old.Close()
		t.Fatal("obsolete TLS accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("HTTPS startup executed a worker")
	}
	if entries, err := os.ReadDir(opts.SessionsRoot); err != nil || len(entries) != 0 {
		t.Fatal("HTTPS startup created sessions")
	}
	// Hold a socket without sending ClientHello: shutdown must still be bounded.
	idle, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("TLS shutdown did not finish")
	}
}

func TestTLSCookiePolicyIncludesDeletion(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		s, err := newShell(scheme+"://127.0.0.1:7443", "test")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{sessionCookie, pairCookie} {
			for _, age := range []int{-1, 300} {
				cookie := s.localCookie(name, "", age)
				if cookie.Secure != (scheme == "https") || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" || cookie.Path != "/" || cookie.MaxAge != age {
					t.Fatal("cookie transport policy not preserved")
				}
			}
		}
	}
}

func TestTLSHandshakeDeadline(t *testing.T) {
	opts, _ := tlsFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- Run(ctx, opts, tlsStartupWriter{ready: ready}) }()
	var origin string
	select {
	case output := <-ready:
		for line := range strings.SplitSeq(output, "\n") {
			if strings.HasPrefix(line, "https://") {
				origin = line
			}
		}
	case err := <-done:
		t.Fatalf("TLS startup: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("TLS startup timeout")
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(origin, "https://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Send a partial TLS record, then stall the handshake. The server must close
	// this connection at its five-second header/handshake deadline, not idle it.
	_ = conn.SetDeadline(time.Now().Add(7 * time.Second))
	if _, err := conn.Write([]byte{0x16, 0x03, 0x03}); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("partial handshake accepted")
	} else if timeout, ok := errors.AsType[net.Error](err); ok && timeout.Timeout() {
		t.Fatal("server did not enforce its handshake deadline")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("TLS shutdown timeout")
	}
}
