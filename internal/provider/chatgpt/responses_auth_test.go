package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
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
