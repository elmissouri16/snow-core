package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider"
	"github.com/elmissouri16/snow-core/internal/provider/chatgpt"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestChatGPTPickerDiscoversNewModelsAfterExpiry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		model := "gpt-5.6-sol"
		if calls.Add(1) > 1 {
			model = "gpt-6-astra"
		}
		fmt.Fprintf(w, `{"models":[{"slug":%q,"visibility":"list"}]}`, model)
	}))
	defer server.Close()
	now := time.Now()
	store := auth.NewMemoryStore()
	if err := store.Put("chatgpt", auth.Credential{Type: auth.CredentialOAuth, Access: "access", AccountID: "acct"}); err != nil {
		t.Fatal(err)
	}
	transport := chatgpt.New(chatgpt.Config{BaseURL: server.URL, HTTPClient: server.Client(), Store: store, CacheRoot: t.TempDir(), Now: func() time.Time { return now }})
	service := auth.NewService(store)
	if err := service.Register(chatgpt.NewAuthDriver(transport)); err != nil {
		t.Fatal(err)
	}
	p, err := provider.NewAuthenticated(transport, service)
	if err != nil {
		t.Fatal(err)
	}
	active := protocol.Model{Provider: "chatgpt", ID: "gpt-5.6-sol"}
	a := &App{ProviderID: "chatgpt", Model: active, runtimeSelection: &liveRuntimeSelection{
		provider: "chatgpt", model: active,
		providers: map[string]provider.Provider{"chatgpt": p}, catalogs: make(map[string][]protocol.Model),
	}}
	for range 3 {
		models, err := a.LoadProviderCatalogs(t.Context())
		if err != nil || len(models) != 1 || models[0].ID != active.ID || calls.Load() != 1 {
			t.Fatalf("initial picker=%+v calls=%d err=%v", models, calls.Load(), err)
		}
	}
	now = now.Add(15 * time.Minute)
	models, err := a.LoadProviderCatalogs(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "gpt-6-astra" || calls.Load() != 2 ||
		len(a.AllModels) != 1 || a.AllModels[0].ID != "gpt-6-astra" || a.Models[0].ID != "gpt-6-astra" {
		t.Fatalf("expired picker=%+v calls=%d err=%v", models, calls.Load(), err)
	}
	if a.Model.ID != active.ID {
		t.Fatal("catalog browsing changed the selected model")
	}
}
