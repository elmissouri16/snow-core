package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestResponses401RefreshesAndRetriesOnce(t *testing.T) {
	newAccess := testJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"}})
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: newAccess, RefreshToken: "new-refresh", ExpiresIn: 3600})
	}))
	defer authServer.Close()
	var calls atomic.Int32
	responseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+newAccess {
			t.Errorf("authorization not refreshed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer responseServer.Close()
	store := auth.NewMemoryStoreForTest()
	old := auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "old-refresh", AccountID: "acct"}
	_ = store.Put(ProviderID, old)
	p := New(Config{BaseURL: responseServer.URL, AuthBaseURL: authServer.URL, HTTPClient: responseServer.Client(), Store: store})
	// One client must reach both local servers.
	p.client = http.DefaultClient
	stream, err := p.Chat(context.Background(), old, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for {
		ev, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		if ev.Type == protocol.EvStreamError {
			t.Fatal(ev.Err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("response calls=%d", calls.Load())
	}
	stored, _ := store.Get(ProviderID)
	if stored.Refresh != "new-refresh" {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestResponses401AndTransientFailuresShareThreeAttemptCap(t *testing.T) {
	newAccess := testJWT(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct"}})
	var refreshes atomic.Int32
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: newAccess, RefreshToken: "new-refresh", ExpiresIn: 3600})
	}))
	defer authServer.Close()
	var calls atomic.Int32
	responseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","code":"service_unavailable"}}`))
	}))
	defer responseServer.Close()
	store := auth.NewMemoryStoreForTest()
	old := auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "old-refresh", AccountID: "acct"}
	_ = store.Put(ProviderID, old)
	p := New(Config{BaseURL: responseServer.URL, AuthBaseURL: authServer.URL, HTTPClient: http.DefaultClient, Store: store})
	p.wait = func(_, _ context.Context, _ time.Duration) error { return nil }
	stream, err := p.Chat(context.Background(), old, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || event.Err == nil {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if calls.Load() != 3 || refreshes.Load() != 1 {
		t.Fatalf("response calls=%d refreshes=%d", calls.Load(), refreshes.Load())
	}
	if !strings.Contains(event.Err.Error(), "3 attempts") {
		t.Fatalf("error=%v", event.Err)
	}
}

func TestResponsesLate401DoesNotExceedThreeAttemptCap(t *testing.T) {
	var refreshes atomic.Int32
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "unused", RefreshToken: "unused", ExpiresIn: 3600})
	}))
	defer authServer.Close()
	var calls atomic.Int32
	responseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable","code":"service_unavailable"}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer responseServer.Close()
	store := auth.NewMemoryStoreForTest()
	credential := auth.Credential{Type: auth.CredentialOAuth, Access: "old", Refresh: "old-refresh", AccountID: "acct"}
	_ = store.Put(ProviderID, credential)
	p := New(Config{BaseURL: responseServer.URL, AuthBaseURL: authServer.URL, HTTPClient: http.DefaultClient, Store: store})
	p.wait = func(_, _ context.Context, _ time.Duration) error { return nil }
	stream, err := p.Chat(context.Background(), credential, protocol.ChatRequest{Model: protocol.Model{Provider: ProviderID, ID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next(context.Background())
	if err != nil || event.Type != protocol.EvStreamError || event.Err == nil {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if calls.Load() != 3 || refreshes.Load() != 0 {
		t.Fatalf("response calls=%d refreshes=%d", calls.Load(), refreshes.Load())
	}
	if !strings.Contains(event.Err.Error(), "HTTP 401") || !strings.Contains(event.Err.Error(), "3 attempts") {
		t.Fatalf("error=%v", event.Err)
	}
}
