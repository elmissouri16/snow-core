package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/elmissouri16/snow-core/internal/tools"
)

func TestSelectedChildToolsAreIsolatedAndPinned(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg)
	defer m.Close(context.Background())
	p := &javascript.Package{Manifest: javascript.Manifest{ID: "child", Name: "Child", Version: "1", APIVersion: 2, Entry: "main.js"}, Config: json.RawMessage(`{}`), Script: []byte(`let count=0;snow.registerTool({name:"count",description:"count",child:true,parameters:{type:"object"},execute(){return String(++count)}});snow.registerTool({name:"private",description:"private",parameters:{type:"object"},execute(){return "private"}});`)}
	fingerprint := strings.Repeat("a", 64)
	if err := m.LoadJavaScript(javascript.New(p, javascript.Options{}), fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := m.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	selected, err := m.SelectChildTools([]string{"plugin_child_count"}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		childReg := tools.NewRegistry()
		child, err := m.CloneChild(t.Context(), childReg, selected, ManagerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		tool, ok := childReg.Get("plugin_child_count")
		if !ok {
			t.Fatal("missing child tool")
		}
		result, err := tool.Run(t.Context(), []byte(`{}`), nil)
		if err != nil || result.Content[0].Text != "1" {
			t.Fatalf("not isolated %+v %v", result, err)
		}
		if _, ok := childReg.Get("plugin_child_private"); ok {
			t.Fatal("unselected tool leaked")
		}
		_ = child.Close(context.Background())
	}
	selected["plugin_child_count"] = strings.Repeat("b", 64)
	if _, err := m.CloneChild(t.Context(), tools.NewRegistry(), selected, ManagerOptions{}); err == nil {
		t.Fatal("changed package restored")
	}
	if _, err := m.SelectChildTools([]string{"plugin_child_private"}, func(string) bool { return true }); err == nil {
		t.Fatal("non-child tool selected")
	}
}
