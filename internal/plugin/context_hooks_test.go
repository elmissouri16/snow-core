package plugin

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
	public "github.com/elmissouri16/snow-core/pkg/plugin"
)

func TestHookContextSatisfiesProviderContract(t *testing.T) {
	for _, text := range []string{"Source pattern: **/*.go", "  "} {
		t.Run(text, func(t *testing.T) {
			m := NewManager(tools.NewRegistry())
			defer m.Close(context.Background())
			encoded, _ := jsonv2.Marshal(text)
			p := &javascript.Package{
				Manifest: javascript.Manifest{ID: "context-test_2", Name: "Context", Version: "1", APIVersion: 2, Entry: "main.js", Capabilities: []string{"hooks"}},
				Config:   json.RawMessage(`{}`),
				Script:   []byte(`snow.registerHook("before_request",()=>({context:[{text:` + string(encoded) + `}]}));`),
			}
			if err := m.LoadJavaScript(javascript.New(p, javascript.Options{}), strings.Repeat("a", 64)); err != nil {
				t.Fatal(err)
			}
			if err := m.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			result, audit, err := m.RunHooks(t.Context(), public.HookRequest{Phase: "before_request"})
			if strings.TrimSpace(text) == "" {
				if err == nil {
					t.Fatal("empty context passed hook boundary")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Context) != 1 || len(audit) != 1 || audit[0].PluginID != p.Manifest.ID {
				t.Fatalf("lost context attribution: %+v %+v", result, audit)
			}
			fragment := result.Context[0]
			if err := fragment.Validate(); err != nil {
				t.Fatalf("provider would reject plugin context: %v", err)
			}
			if fragment.Source != "plugin-context-test_2" || fragment.Text != text {
				t.Fatalf("incorrect fragment: %+v", fragment)
			}
		})
	}
}
