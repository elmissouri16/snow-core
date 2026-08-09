package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	surfhttp "github.com/enetx/http"
	"github.com/snow-core/snow/pkg/protocol"
)

func testWebFetch() *WebFetch {
	w := NewWebFetch()
	dialer := &net.Dialer{}
	w.policy = &webFetchNetworkPolicy{
		lookupNetIP: net.DefaultResolver.LookupNetIP,
		dialContext: dialer.DialContext,
		allowIP:     func(netip.Addr) bool { return true },
	}
	return w
}

func runWebFetch(t *testing.T, w *WebFetch, rawURL string) (string, bool) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"url": rawURL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.Run(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Type != protocol.BlockText {
		t.Fatalf("result content = %+v", result.Content)
	}
	return result.Content[0].Text, result.IsError
}

func TestWebFetchSchemaIsDeferredNetworkTool(t *testing.T) {
	schema := NewWebFetch().Schema()
	if schema.Name != "webfetch" || schema.Discovery == nil || schema.Discovery.Mode != protocol.ToolDiscoveryDeferred || schema.Discovery.Namespace != "web" {
		t.Fatalf("schema = %+v", schema)
	}
	if !strings.Contains(string(schema.Parameters), `"maximum": 30000`) {
		t.Fatalf("parameters = %s", schema.Parameters)
	}
}

func TestWebFetchClientUsesSecureChrome150AndProtectedTransport(t *testing.T) {
	policy := newPublicWebFetchPolicy()
	client, standard, err := newWebFetchClient(time.Second, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.GetTLSConfig().InsecureSkipVerify {
		t.Fatal("Surf certificate verification is disabled")
	}
	transport, ok := client.GetTransport().(*surfhttp.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.GetTransport())
	}
	if transport.Proxy != nil || transport.DialContext == nil {
		t.Fatalf("protected transport not installed: proxy_set=%v dial_set=%v", transport.Proxy != nil, transport.DialContext != nil)
	}
	if standard.CheckRedirect == nil || standard.Timeout != time.Second {
		t.Fatalf("standard client = %+v", standard)
	}
}

func TestWebFetchHTMLToMarkdownChromeHeadersAndFinalURL(t *testing.T) {
	var sawChrome atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/page", http.StatusFound)
	})
	mux.HandleFunc("/docs/page", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Chrome/150.0.0.0") && strings.Contains(r.Header.Get("Sec-Ch-Ua"), `v="150"`) {
			sawChrome.Store(true)
		}
		w.Header().Set("Content-Type", "text/html; charset=windows-1252")
		_, _ = w.Write([]byte("<html><head><title>Ignored</title></head><body><h1>Caf\xe9</h1><a href=\"../next\">Next</a><script>ignore me</script></body></html>"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	output, isError := runWebFetch(t, testWebFetch(), server.URL+"/start")
	if isError {
		t.Fatalf("webfetch failed: %s", output)
	}
	for _, want := range []string{
		"Status: 200 OK",
		"# Café",
		"[Next](" + server.URL + "/next)",
		"BEGIN UNTRUSTED WEB CONTENT",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "ignore me") || !sawChrome.Load() {
		t.Fatalf("script leaked or Chrome 150 headers missing: chrome=%v output=%s", sawChrome.Load(), output)
	}
}

func TestWebFetchTextJSONHTTPErrorAndBinary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not here"))
	})
	mux.HandleFunc("/binary", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2, 3})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	w := testWebFetch()

	output, isError := runWebFetch(t, w, server.URL+"/json")
	if isError || !strings.Contains(output, `{"ok":true}`) {
		t.Fatalf("json result: error=%v output=%s", isError, output)
	}
	output, isError = runWebFetch(t, w, server.URL+"/missing")
	if !isError || !strings.Contains(output, "Status: 404 Not Found") || !strings.Contains(output, "not here") {
		t.Fatalf("404 result: error=%v output=%s", isError, output)
	}
	output, isError = runWebFetch(t, w, server.URL+"/binary")
	if !isError || !strings.Contains(output, "unsupported binary content type") {
		t.Fatalf("binary result: error=%v output=%s", isError, output)
	}
}

func TestWebFetchOutputIsBoundedAndUTF8Safe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Repeat("é", 1000)))
	}))
	defer server.Close()
	w := testWebFetch()
	w.MaxOutputBytes = 512

	output, isError := runWebFetch(t, w, server.URL)
	if isError || len(output) > w.MaxOutputBytes || !utf8.ValidString(output) {
		t.Fatalf("bounded output: error=%v bytes=%d valid=%v output=%q", isError, len(output), utf8.ValidString(output), output)
	}
	if !strings.Contains(output, "Truncated: true") || !strings.Contains(output, "webfetch output truncated") {
		t.Fatalf("missing truncation markers: %s", output)
	}
}

func TestWebFetchTimeoutAndArgumentValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	w := testWebFetch()
	w.Timeout = 25 * time.Millisecond

	output, isError := runWebFetch(t, w, server.URL)
	if !isError || !strings.Contains(output, "timed out or was cancelled") {
		t.Fatalf("timeout result: error=%v output=%s", isError, output)
	}
	for _, args := range []string{
		`{"url":""}`,
		`{"url":"https://example.com","timeout_ms":30001}`,
		`{`,
	} {
		result, err := w.Run(context.Background(), json.RawMessage(args), nil)
		if err != nil || !result.IsError {
			t.Fatalf("args %q: result=%+v err=%v", args, result, err)
		}
	}
}

func TestWebFetchRejectsNonPublicURLs(t *testing.T) {
	policy := newPublicWebFetchPolicy()
	blocked := []string{
		"file:///etc/passwd",
		"http://localhost/",
		"http://sub.localhost/",
		"http://user:pass@example.com/",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/",
		"http://[fc00::1]/",
		"http://192.0.2.1/",
	}
	for _, rawURL := range blocked {
		if _, err := policy.validateURL(rawURL); err == nil {
			t.Errorf("validateURL(%q) unexpectedly succeeded", rawURL)
		}
	}
	if got, err := policy.validateURL("HTTPS://1.1.1.1/path#fragment"); err != nil || got.Fragment != "" || got.Scheme != "https" {
		t.Fatalf("public URL = %v, err=%v", got, err)
	}
}

func TestWebFetchSafeDialRejectsMixedDNSBeforeDial(t *testing.T) {
	var dialed atomic.Bool
	policy := &webFetchNetworkPolicy{
		lookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")}, nil
		},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed.Store(true)
			return nil, errors.New("unexpected dial")
		},
		allowIP: isPublicWebFetchIP,
	}
	_, err := policy.safeDialContext(context.Background(), "tcp", "example.com:443")
	if err == nil || !strings.Contains(err.Error(), "non-public") || dialed.Load() {
		t.Fatalf("safeDialContext err=%v dialed=%v", err, dialed.Load())
	}
}

func TestWebFetchRedirectPolicyBlocksPrivateAndCapsChain(t *testing.T) {
	policy := newPublicWebFetchPolicy()
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	if err := policy.checkRedirect(req, []*http.Request{{}}); err == nil || !strings.Contains(err.Error(), "blocked redirect") {
		t.Fatalf("private redirect err = %v", err)
	}
	public, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	via := make([]*http.Request, maxWebFetchRedirects)
	if err := policy.checkRedirect(public, via); err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxWebFetchRedirects)) {
		t.Fatalf("redirect cap err = %v", err)
	}
}

func TestPublicWebFetchIPClassification(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":            true,
		"2606:4700::1111":    true,
		"100.64.0.1":         false,
		"198.18.0.1":         false,
		"203.0.113.10":       false,
		"::ffff:127.0.0.1":   false,
		"64:ff9b::0808:0808": false,
		"2002:0808:0808::1":  false,
	}
	for raw, want := range tests {
		if got := isPublicWebFetchIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("isPublicWebFetchIP(%s) = %v, want %v", raw, got, want)
		}
	}
}
