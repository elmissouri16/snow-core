package rpc

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/internal/app"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func discoveryTestApp(t *testing.T, inactive http.HandlerFunc) (*app.App, *atomic.Int32) {
	t.Helper()
	t.Setenv("SNOW_HOME", t.TempDir())
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/active/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"active-model"}]}`)
		case "/inactive/models":
			calls.Add(1)
			inactive(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remote.Close)
	path := filepath.Join(t.TempDir(), "config.json")
	config := fmt.Sprintf(`{"providers":{"openai-compatible":{"base_url":%q},"opencode-go":{"base_url":%q},"chatgpt":{"base_url":%q},"inactive":{"type":"openai-compatible","base_url":%q}}}`, remote.URL+"/active", remote.URL+"/go", remote.URL+"/chatgpt", remote.URL+"/inactive")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(t.Context(), app.Options{Provider: "openai-compatible", ConfigPath: path, NoSession: true, Permission: "deny", NoPlugins: true, NoMCP: true, NoSkills: true, CWD: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, &calls
}

func discoveryResponse(t *testing.T, wire []byte) protocol.RPCModelDiscovery {
	t.Helper()
	var response struct {
		Success bool                       `json:"success"`
		Data    protocol.RPCModelDiscovery `json:"data"`
	}
	if err := json.Unmarshal(wire, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatalf("discovery failed: %s", wire)
	}
	if err := resolveWireSchema(t, "output.schema.json").Validate(decodedJSON(t, wire)); err != nil {
		t.Fatalf("discovery output schema: %v: %s", err, wire)
	}
	return response.Data
}

func TestModelsDiscoverIsLazyAndPreservesSelection(t *testing.T) {
	a, calls := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"inactive-model"}]}`)
	})
	beforeProvider, beforeModel, beforeModels := a.ActiveModelsSnapshot()
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	if err := srv.Serve(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := srv.handle(t.Context(), Request{Type: "models_list"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("startup/models_list eagerly discovered inactive models")
	}
	output.Reset()
	if err := srv.handle(t.Context(), Request{ID: "discover", Type: "models_discover"}); err != nil {
		t.Fatal(err)
	}
	result := discoveryResponse(t, output.Bytes())
	if calls.Load() != 1 || !slices.ContainsFunc(result.Models, func(m protocol.Model) bool { return m.Provider == "inactive" && m.ID == "inactive-model" }) {
		t.Fatalf("discovery did not load inactive app catalog: calls=%d result=%+v", calls.Load(), result)
	}
	if result.Partial || result.Truncated {
		t.Fatalf("complete discovery marked partial/truncated: %+v", result)
	}
	// LoadProviderCatalogs also publishes the combined App mirror. Checking it
	// proves the command used that facade rather than a separate provider loop.
	if !slices.ContainsFunc(a.AllModels, func(m protocol.Model) bool { return m.Provider == "inactive" }) {
		t.Fatal("app facade did not publish catalog")
	}
	afterProvider, afterModel, afterModels := a.ActiveModelsSnapshot()
	if beforeProvider != afterProvider || !reflect.DeepEqual(beforeModel, afterModel) || !reflect.DeepEqual(beforeModels, afterModels) {
		t.Fatal("discovery changed selected provider/model or active catalog")
	}
}

func TestModelsDiscoverUnavailableInactiveProviderIsPartial(t *testing.T) {
	a, _ := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "private-auth-account-secret", http.StatusServiceUnavailable)
	})
	var output bytes.Buffer
	srv := New(t.Context(), a, strings.NewReader(""), &output)
	if err := srv.handle(t.Context(), Request{Type: "models_discover"}); err != nil {
		t.Fatal(err)
	}
	result := discoveryResponse(t, output.Bytes())
	if !result.Partial || result.Truncated || !slices.ContainsFunc(result.Models, func(m protocol.Model) bool { return m.ID == "active-model" }) {
		t.Fatalf("partial discovery lost available models: %+v", result)
	}
	for _, private := range []string{"private-auth-account-secret", "HTTP 503", "error", "account", "auth"} {
		if strings.Contains(output.String(), private) {
			t.Fatalf("discovery exposed private failure detail %q", private)
		}
	}
}

func TestModelsDiscoverCancellationAndFixedTimeout(t *testing.T) {
	for _, cancelEarly := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelEarly), func(t *testing.T) {
			started := make(chan struct{})
			a, _ := discoveryTestApp(t, func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var output bytes.Buffer
			srv := New(t.Context(), a, strings.NewReader(""), &output)
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- srv.handle(ctx, Request{Type: "models_discover"}) }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("discovery did not start")
			}
			if cancelEarly {
				cancel()
			}
			wait := modelDiscoveryTimeout + 2*time.Second
			if cancelEarly {
				wait = time.Second
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(wait):
				t.Fatal("discovery exceeded cancellation/timeout bound")
			}
			if !cancelEarly && time.Since(start) < modelDiscoveryTimeout-time.Second {
				t.Fatal("discovery returned before fixed timeout")
			}
			result := discoveryResponse(t, output.Bytes())
			if !result.Partial || !slices.ContainsFunc(result.Models, func(m protocol.Model) bool { return m.ID == "active-model" }) {
				t.Fatalf("canceled discovery lost partial models: %+v", result)
			}
		})
	}
}

func TestModelsDiscoverBoundsAndDefensiveCopies(t *testing.T) {
	model := protocol.Model{Provider: "provider", ID: "model", DisplayName: strings.Repeat("界", 512), Description: strings.Repeat("d", 5000), ContextWindow: -1,
		DefaultThinking: "private-invalid", ThinkingLevels: []protocol.ThinkingLevel{"high", "high", "invalid"},
		Upgrade: &protocol.ModelUpgrade{Model: "next", Message: strings.Repeat("m", 5000)},
		Pricing: &protocol.ModelPricing{Currency: strings.Repeat("c", 30)}, SupportsReasoningSummary: new(true)}
	result := boundedModelDiscovery([]protocol.Model{model})
	if !result.Truncated || result.Partial || len(result.Models) != 1 {
		t.Fatalf("bounded result=%+v", result)
	}
	got := result.Models[0]
	if len(got.DisplayName) > 512 || !utf8.ValidString(got.DisplayName) || len(got.Description) != 4096 || got.ContextWindow != 0 || got.DefaultThinking != "" || !slices.Equal(got.ThinkingLevels, []protocol.ThinkingLevel{"high"}) || len(got.Upgrade.Message) != 4096 || len(got.Pricing.Currency) != 16 {
		t.Fatalf("unbounded/invalid model=%+v", got)
	}
	got.Upgrade.Model = "changed"
	got.Pricing.Currency = "USD"
	*got.SupportsReasoningSummary = false
	got.ThinkingLevels[0] = "low"
	if model.Upgrade.Model != "next" || len(model.Pricing.Currency) != 30 || !*model.SupportsReasoningSummary || model.ThinkingLevels[0] != "high" {
		t.Fatal("result aliases source metadata")
	}
	many := make([]protocol.Model, 513)
	for i := range many {
		many[i] = protocol.Model{Provider: "p", ID: fmt.Sprint(i)}
	}
	if result := boundedModelDiscovery(many); len(result.Models) != 512 || !result.Truncated {
		t.Fatalf("model cap=%+v", result)
	}
	for _, bad := range []protocol.Model{{Provider: "p", ID: ""}, {Provider: strings.Repeat("p", 129), ID: "m"}, {Provider: "p", ID: strings.Repeat("m", 513)}, {Provider: "p", ID: "bad\xff"}, {Provider: "p", ID: "bad\n"}} {
		if result := boundedModelDiscovery([]protocol.Model{bad}); len(result.Models) != 0 || !result.Truncated {
			t.Fatalf("invalid identity survived: %+v", result)
		}
	}
	model.Pricing.InputPerMillion = math.Inf(1)
	model.Upgrade.Model = strings.Repeat("m", 513)
	result = boundedModelDiscovery([]protocol.Model{model})
	if result.Models[0].Pricing != nil || result.Models[0].Upgrade != nil || !result.Truncated {
		t.Fatal("invalid nested metadata survived")
	}
}
