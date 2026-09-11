package plugindocs

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestInventoryPaginationAndDetails(t *testing.T) {
	plugins := make([]Plugin, 20)
	for i := range plugins {
		plugins[i] = Plugin{ID: fmt.Sprintf("plugin-%02d", len(plugins)-i-1), Registered: true, Enabled: true, Loaded: true, APIVersion: 2, Version: "1.0.0", Path: strings.Repeat("路径", 30), Commands: []Command{{ID: "run", Description: "fixture command"}}}
	}
	calls := 0
	tool := New(Options{MaxOutputBytes: 4096, Inventory: func(ctx context.Context) ([]Plugin, error) { calls++; return plugins, ctx.Err() }})
	var found []Plugin
	for offset := 1; ; {
		page := runTool(t, tool, arguments{Action: "plugins", Offset: offset, Limit: 1000})
		if page.Total != len(plugins) {
			t.Fatal("wrong inventory total")
		}
		found = append(found, page.Plugins...)
		if page.NextOffset == 0 {
			break
		}
		if page.NextOffset <= offset {
			t.Fatal("pagination stalled")
		}
		offset = page.NextOffset
	}
	if len(found) != len(plugins) || calls < 2 {
		t.Fatalf("plugins=%d calls=%d", len(found), calls)
	}
	for i, p := range found {
		if p.ID != fmt.Sprintf("plugin-%02d", i) || len(p.Commands) != 0 {
			t.Fatalf("summary=%+v", p)
		}
	}
	if plugins[0].ID != "plugin-19" || len(plugins[0].Commands) != 1 {
		t.Fatal("inventory provider's slice mutated")
	}
	detail := runTool(t, tool, arguments{Action: "plugins", PluginID: "plugin-05"})
	if detail.Total != 1 || len(detail.Plugins) != 1 || len(detail.Plugins[0].Commands) != 1 {
		t.Fatalf("detail=%+v", detail)
	}
	out, err := tool.Run(t.Context(), []byte(`{"action":"plugins","plugin_id":"missing"}`), nil)
	if err != nil || !out.IsError {
		t.Fatalf("missing plugin=%+v %v", out, err)
	}
	before := calls
	runTool(t, tool, arguments{Action: "overview", Limit: 3})
	if calls != before {
		t.Fatal("offline reference read touched runtime inventory")
	}
}

func TestInventoryErrorsBoundsAndCancellation(t *testing.T) {
	tool := New(Options{MaxOutputBytes: 4096, Inventory: func(context.Context) ([]Plugin, error) {
		return []Plugin{{ID: "oversized", Name: strings.Repeat("x", 64<<10)}}, nil
	}})
	result, err := tool.Run(t.Context(), []byte(`{"action":"plugins"}`), nil)
	if err != nil || !result.IsError || len(result.Content[0].Text) > tool.opts.MaxOutputBytes {
		t.Fatalf("unbounded inventory=%+v %v", result, err)
	}
	calls := 0
	tool = New(Options{Inventory: func(context.Context) ([]Plugin, error) { calls++; return nil, fmt.Errorf("inventory unavailable") }})
	result, err = tool.Run(t.Context(), []byte(`{"action":"plugins"}`), nil)
	if err != nil || !result.IsError || calls != 1 {
		t.Fatalf("inventory error=%+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err = tool.Run(ctx, []byte(`{"action":"plugins"}`), nil)
	if err != nil || !result.IsError || calls != 1 {
		t.Fatal("canceled request touched inventory")
	}
	tool = New(Options{Inventory: func(context.Context) ([]Plugin, error) { return nil, nil }})
	out := runTool(t, tool, arguments{Action: "plugins"})
	if out.Total != 0 || out.NextOffset != 0 || len(out.Plugins) != 0 {
		t.Fatalf("empty inventory=%+v", out)
	}
}
