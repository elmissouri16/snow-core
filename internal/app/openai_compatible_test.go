package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/provider/openaicompat"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func compatibleConfig(t *testing.T, serverURL, defaultModel string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body := map[string]any{
		"default_provider": openaicompat.ProviderID,
		"permission_mode":  "allow",
		"providers": map[string]any{
			openaicompat.ProviderID: map[string]any{"base_url": serverURL + "/v1", "default_model": defaultModel},
			"opencode-go":           map[string]any{"base_url": serverURL + "/opencode"},
		},
	}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNamedOpenAICompatibleProfileStartupAndAuthIsolation(t *testing.T) {
	var modelsAuth, chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelsAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "x-model"}}})
		case "/v1/responses":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{}}}\n\n")
		case "/opencode/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := fmt.Sprintf(`{"default_provider":"x-provider","default_model":"x-model","permission_mode":"allow","providers":{"x-provider":{"type":"openai-compatible","base_url":%q},"opencode-go":{"base_url":%q}}}`, server.URL+"/v1", server.URL+"/opencode")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(t.TempDir(), "auth.json")
	store, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("x-provider", auth.Credential{Type: auth.CredentialAPIKey, Key: "x-secret"}); err != nil {
		t.Fatal(err)
	}

	a, err := New(context.Background(), Options{Provider: "x-provider", Model: "x-model", ConfigPath: configPath, AuthPath: authPath, NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.ProviderID != "x-provider" || a.Agent.Model().Provider != "x-provider" {
		t.Fatalf("provider=%q model=%+v", a.ProviderID, a.Agent.Model())
	}
	if err := a.Agent.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if modelsAuth != "Bearer x-secret" || chatAuth != "Bearer x-secret" {
		t.Fatalf("models auth=%q chat auth=%q", modelsAuth, chatAuth)
	}
	if _, legacy := a.Auth.Get(openaicompat.ProviderID); legacy {
		t.Fatal("named profile credential crossed into legacy profile")
	}
}

func TestOpenAICompatibleStartupRequiresEndpointAndModel(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	if _, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, Model: "m", NoSession: true, Permission: "allow", CWD: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "base URL is required") {
		t.Fatalf("missing endpoint error=%v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		case "/opencode/models":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("compatible API key leaked to OpenCode discovery: %q", got)
			}
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	cfg := compatibleConfig(t, server.URL, "")
	if _, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, ConfigPath: cfg, NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true}); err == nil || !strings.Contains(err.Error(), "pass --model") {
		t.Fatalf("missing model error=%v", err)
	}

	a, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, Model: "explicit", APIKey: "compatible-secret", ConfigPath: cfg, NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatalf("explicit model startup: %v", err)
	}
	defer a.Close()
	if a.Model.ID != "explicit" {
		t.Fatalf("model=%+v", a.Model)
	}
}

func TestOpenAICompatibleStoredKeyLogoutTakesEffect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	t.Setenv(openaicompat.EnvAPIKey, "")
	store, err := auth.NewFileStore(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(openaicompat.ProviderID, auth.Credential{Type: auth.CredentialAPIKey, Key: "stored-key"}); err != nil {
		t.Fatal(err)
	}
	var discoveryAuth, chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			discoveryAuth = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
		case "/opencode/models":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/v1/responses":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, ConfigPath: compatibleConfig(t, server.URL, "m"), NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Auth.Delete(openaicompat.ProviderID); err != nil {
		t.Fatal(err)
	}
	if err := a.Agent.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if discoveryAuth != "Bearer stored-key" || chatAuth != "" {
		t.Fatalf("discovery auth=%q chat auth after logout=%q", discoveryAuth, chatAuth)
	}
}

func TestConfigureOpenAICompatibleAtRuntime(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	var modelsAuth, chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelsAuth = r.Header.Get("Authorization")
			_, _ = fmt.Fprint(w, `{"data":[{"id":"runtime-model"}]}`)
		case "/v1/responses":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Auth.Put(openaicompat.ProviderID, auth.Credential{Type: auth.CredentialAPIKey, Key: "runtime-key"}); err != nil {
		t.Fatal(err)
	}
	pc := a.Cfg.Providers[openaicompat.ProviderID]
	pc.BaseURL = server.URL + "/v1"
	a.Cfg.Providers[openaicompat.ProviderID] = pc
	if err := a.ConfigureOpenAICompatible(pc.BaseURL); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshProviderModels(context.Background(), openaicompat.ProviderID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProvider(openaicompat.ProviderID); err != nil {
		t.Fatal(err)
	}
	if a.Agent.Model().ID != "runtime-model" {
		t.Fatalf("model=%+v", a.Agent.Model())
	}
	if err := a.Agent.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if modelsAuth != "Bearer runtime-key" || chatAuth != "Bearer runtime-key" {
		t.Fatalf("models auth=%q chat auth=%q", modelsAuth, chatAuth)
	}
}

func TestConfigureOpenAICompatiblePreservesExplicitDiscoveryKey(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	var mu sync.Mutex
	modelsAuth := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			mu.Lock()
			modelsAuth = r.Header.Get("Authorization")
			mu.Unlock()
			_, _ = fmt.Fprint(w, `{"data":[{"id":"m"}]}`)
		case "/opencode/models":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, Model: "m", APIKey: "explicit-key", ConfigPath: compatibleConfig(t, server.URL, "m"), NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Auth.Put(openaicompat.ProviderID, auth.Credential{Type: auth.CredentialAPIKey, Key: "stored-key"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfigureOpenAICompatible(server.URL + "/v1"); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshProviderModels(context.Background(), openaicompat.ProviderID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := modelsAuth
	mu.Unlock()
	if got != "Bearer explicit-key" {
		t.Fatalf("discovery authorization=%q", got)
	}
}

func TestRefreshProviderModelsRejectsStaleCompatibleEndpoint(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	started := make(chan struct{})
	release := make(chan struct{})
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = fmt.Fprint(w, `{"data":[{"id":"model-a"}]}`)
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"model-b"}]}`)
	}))
	defer serverB.Close()

	a, err := New(context.Background(), Options{Provider: "fake", NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.ConfigureOpenAICompatible(serverA.URL); err != nil {
		t.Fatal(err)
	}
	staleDone := make(chan error, 1)
	go func() { staleDone <- a.RefreshProviderModels(context.Background(), openaicompat.ProviderID) }()
	<-started
	if err := a.ConfigureOpenAICompatible(serverB.URL); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshProviderModels(context.Background(), openaicompat.ProviderID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-staleDone; err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("stale refresh error=%v", err)
	}
	models := a.modelCatalog[openaicompat.ProviderID]
	if len(models) != 1 || models[0].ID != "model-b" {
		t.Fatalf("catalog after stale refresh=%+v", models)
	}
}

func TestOpenAICompatibleAppToolRoundTrip(t *testing.T) {
	t.Setenv("SNOW_HOME", t.TempDir())
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "fixture.txt"), []byte("fixture contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	posts := 0
	var secondBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"discovered"}]}`)
		case "/opencode/models":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/v1/responses":
			data, _ := io.ReadAll(r.Body)
			mu.Lock()
			posts++
			call := posts
			if call == 2 {
				secondBody = string(data)
			}
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			if call == 1 {
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"id\":\"item-1\",\"call_id\":\"call-1\",\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"fixture.txt\\\"}\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
			} else {
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":4,\"output_tokens\":1}}}\n\n")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, err := New(context.Background(), Options{Provider: openaicompat.ProviderID, ConfigPath: compatibleConfig(t, server.URL, ""), NoSession: true, Permission: "allow", CWD: cwd, NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Model.ID != "discovered" {
		t.Fatalf("model=%+v", a.Model)
	}
	var text strings.Builder
	unsubscribe := a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvTextDelta {
			text.WriteString(event.Text)
		}
	})
	defer unsubscribe()
	if err := a.Agent.Prompt(context.Background(), "read the fixture"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotPosts, gotSecond := posts, secondBody
	mu.Unlock()
	if gotPosts != 2 || text.String() != "done" {
		t.Fatalf("posts=%d text=%q", gotPosts, text.String())
	}
	if !strings.Contains(gotSecond, `"type":"function_call_output"`) || !strings.Contains(gotSecond, "fixture contents") {
		t.Fatalf("second body missing tool result: %s", gotSecond)
	}
}
