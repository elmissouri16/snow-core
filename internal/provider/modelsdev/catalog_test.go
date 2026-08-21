package modelsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestFetchProviderReturnsOnlyRequestedProviderWithoutCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization=%q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept=%q", got)
		}
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("User-Agent=%q", got)
		}
		_, _ = fmt.Fprint(w, `{
			"opencode":{"models":{"zen-model":{"id":"zen-model","reasoning":true}}},
			"other":{"models":{"paid-model":{"id":"paid-model"}}}
		}`)
	}))
	defer server.Close()

	models, ok := FetchProvider(context.Background(), server.Client(), server.URL+"/catalog", "opencode")
	if !ok || len(models) != 1 || models["zen-model"].ID != "zen-model" {
		t.Fatalf("models=%+v ok=%v", models, ok)
	}
	if _, found := models["paid-model"]; found {
		t.Fatalf("unrequested provider model leaked into result: %+v", models)
	}
}

func TestFetchProviderFailureAndValidMissingProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			_, _ = fmt.Fprint(w, `{"other":{"models":{}}}`)
			return
		}
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if models, ok := FetchProvider(context.Background(), server.Client(), server.URL+"/offline", "opencode"); ok || models != nil {
		t.Fatalf("failure models=%+v ok=%v", models, ok)
	}
	if models, ok := FetchProvider(context.Background(), server.Client(), server.URL+"/missing", "opencode"); !ok || len(models) != 0 {
		t.Fatalf("missing provider models=%+v ok=%v", models, ok)
	}
}

func TestReasoningMetadataNormalizesAdvertisedEfforts(t *testing.T) {
	reasoning := false
	model := Model{
		Reasoning: &reasoning,
		ReasoningOptions: []ReasoningOption{
			{Type: "toggle"},
			{Type: " EFFORT ", Values: []string{" LOW ", "high", "off", "future", "low", "xhigh"}},
		},
	}
	supports, levels := ReasoningMetadata(model)
	want := []protocol.ThinkingLevel{protocol.ThinkingLow, protocol.ThinkingHigh, protocol.ThinkingXHigh}
	if !supports || !slices.Equal(levels, want) {
		t.Fatalf("supports=%v levels=%v want=%v", supports, levels, want)
	}

	reasoning = true
	supports, levels = ReasoningMetadata(Model{Reasoning: &reasoning})
	if !supports || len(levels) != 0 {
		t.Fatalf("reasoning-only supports=%v levels=%v", supports, levels)
	}
}
