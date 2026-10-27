package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// The host fetches the pinned model only on an explicit confirmed request,
// keeps nothing that fails verification, and then serves it to paired browsers.
func TestSpeechModelDownloadVerifiesAndServes(t *testing.T) {
	s, cookie, _ := projectShell(t)
	csrf := csrfFor(t, s, cookie)
	model := []byte(strings.Repeat("whistle-weights ", 64))
	served := model
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(served) }))
	t.Cleanup(source.Close)
	digest := sha256.Sum256(model)
	store := newSpeechStore(t.TempDir() + "/models")
	store.source, store.sha256, store.size = source.URL, hex.EncodeToString(digest[:]), int64(len(model))
	s.speech = store
	if w := request(t, s, "GET", "/speech/status", nil, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("status before download: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, s, "GET", "/speech/model", nil, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("model served before download: %d", w.Code)
	}
	if w := request(t, s, "POST", "/speech/download", url.Values{"csrf": {csrf}}, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("download without confirmation: %d", w.Code)
	}
	served = append([]byte("corrupt"), model[7:]...)
	if w := request(t, s, "POST", "/speech/download", url.Values{"csrf": {csrf}, "confirm": {"download"}}, cookie); w.Code != http.StatusBadGateway {
		t.Fatalf("corrupt download accepted: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(store.path()); err == nil {
		t.Fatal("corrupt model was kept")
	}
	if entries, _ := os.ReadDir(store.dir); len(entries) != 0 {
		t.Fatalf("partial files remained: %v", entries)
	}
	served = model
	if w := request(t, s, "POST", "/speech/download", url.Values{"csrf": {csrf}, "confirm": {"download"}}, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("download: %d %s", w.Code, w.Body.String())
	}
	w := request(t, s, "GET", "/speech/model", nil, cookie)
	if w.Code != 200 || w.Body.String() != string(model) || w.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("serve: %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"))
	}
	if w := request(t, s, "GET", "/speech/model", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unpaired browser read the model: %d", w.Code)
	}
}
