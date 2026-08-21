package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestOpenCodeZenAnonymousEndToEndAndFreeCatalog(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	var modelsAuth, chatAuth string
	var emptyCompletion atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zen/models":
			modelsAuth = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"big-pickle"},{"id":"muse-spark-1.2-contributor-free"}]}`)
		case "/zen/chat/completions":
			chatAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "text/event-stream")
			if emptyCompletion.Load() {
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			} else {
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"zen ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}
		case "/go/models":
			_, _ = io.WriteString(w, `{"data":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("SNOW_HOME", home)
	configPath := filepath.Join(home, "config.json")
	body := fmt.Sprintf(`{"default_provider":"opencode-zen","permission_mode":"allow","providers":{"opencode-zen":{"base_url":%q},"opencode-go":{"base_url":%q}}}`, server.URL+"/zen", server.URL+"/go")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), Options{ConfigPath: configPath, NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.ProviderID != "opencode-zen" || a.Model.ID != "big-pickle" || a.Model.ContextWindow != 160000 || a.Model.MaxContextWindow != 200000 {
		t.Fatalf("provider=%s model=%+v", a.ProviderID, a.Model)
	}
	if len(a.Models) != 2 || a.Models[0].ID != "big-pickle" || a.Models[1].ID != "muse-spark-1.2-contributor-free" {
		t.Fatalf("models=%+v", a.Models)
	}
	var text strings.Builder
	a.Agent.Subscribe(func(event protocol.AgentEvent) {
		if event.Type == protocol.EvTextDelta {
			text.WriteString(event.Text)
		}
	})
	if err := a.Agent.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if text.String() != "zen ok" || modelsAuth != "" || chatAuth != "" {
		t.Fatalf("text=%q models auth=%q chat auth=%q", text.String(), modelsAuth, chatAuth)
	}
	emptyCompletion.Store(true)
	if err := a.Agent.Prompt(context.Background(), "empty"); err == nil || !strings.Contains(err.Error(), "returned an empty completion") {
		t.Fatalf("empty completion error=%v", err)
	}
	if err := a.SetModel(protocol.Model{Provider: "opencode-zen", ID: "gpt-5.4"}); err == nil {
		t.Fatal("authoritative Zen catalog accepted paid model")
	}
	bad, err := New(context.Background(), Options{Provider: "opencode-zen", Model: "gpt-5.4", ConfigPath: configPath, NoSession: true, Permission: "allow", CWD: t.TempDir(), NoPlugins: true, NoMCP: true, NoSkills: true})
	if err == nil {
		_ = bad.Close()
		t.Fatal("startup accepted explicit paid Zen model")
	}
}
