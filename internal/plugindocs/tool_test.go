package plugindocs

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func runTool(t *testing.T, tool *Tool, args arguments) response {
	t.Helper()
	raw, err := jsonv2.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Run(t.Context(), raw, nil)
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("run %+v: %+v, %v", args, result, err)
	}
	text := result.Content[0].Text
	if len(text) > tool.opts.MaxOutputBytes {
		t.Fatalf("output exceeds cap: %d", len(text))
	}
	var out response
	if err := jsonv2.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSchemaAndOverview(t *testing.T) {
	tool := New(Options{BuildVersion: "test-build"})
	schema := tool.Schema()
	if schema.Name != ToolName || schema.Discovery == nil || schema.Discovery.Mode != protocol.ToolDiscoveryDeferred || schema.Discovery.Namespace != "snow_plugin_development" {
		t.Fatalf("schema=%+v", schema)
	}
	var parameters map[string]any
	if err := jsonv2.Unmarshal(schema.Parameters, &parameters); err != nil {
		t.Fatal(err)
	}
	out := runTool(t, tool, arguments{Action: "overview", Limit: 1000})
	if out.BuildVersion != "test-build" || out.Path != "GUIDE.md" || out.NextOffset != 0 {
		t.Fatalf("overview=%+v", out)
	}
	for _, want := range []string{"snow-plugin.json", "main.js", "tests/plugin.json", "API 2", "host_tools", "updat", "plugin_id", "read", "reload", "permission", "config", "state"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("overview missing %q", want)
		}
	}
	if strings.Contains(out.Content, "activate_skill") || strings.Contains(out.Content, "$snow-js-plugin") {
		t.Fatal("obsolete activation instructions in overview")
	}
}

func TestAllResourcesReadExactlyAcrossPages(t *testing.T) {
	tool := New(Options{})
	files, err := resourceFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 69 {
		t.Fatalf("lost bundled resources: %d", len(files))
	}
	for _, file := range files {
		t.Run(file.Path, func(t *testing.T) {
			expected, err := readResource(t.Context(), file.Path)
			if err != nil {
				t.Fatal(err)
			}
			var content strings.Builder
			for offset := 1; ; {
				page := runTool(t, tool, arguments{Action: "read", Path: file.Path, Offset: offset, Limit: 73})
				content.WriteString(page.Content)
				if page.NextOffset == 0 {
					break
				}
				if page.NextOffset <= offset {
					t.Fatal("pagination did not advance")
				}
				offset = page.NextOffset
			}
			if content.String() != expected {
				t.Fatal("paged content differs from bundled resource")
			}
		})
	}
}

func TestListAndSearchPagination(t *testing.T) {
	tool := New(Options{})
	files, err := resourceFiles()
	if err != nil {
		t.Fatal(err)
	}
	var listed []resource
	for offset := 1; ; {
		page := runTool(t, tool, arguments{Action: "list", Offset: offset, Limit: 7})
		listed = append(listed, page.Resources...)
		if page.Total != len(files) {
			t.Fatalf("total=%d", page.Total)
		}
		if page.NextOffset == 0 {
			break
		}
		offset = page.NextOffset
	}
	if len(listed) != len(files) {
		t.Fatal("list lost resources")
	}
	for i := range files {
		if files[i] != listed[i] {
			t.Fatal("unstable resource order")
		}
	}
	filtered := runTool(t, tool, arguments{Action: "list", Path: "api/"})
	if len(filtered.Resources) != 2 {
		t.Fatalf("filtered=%+v", filtered)
	}
	const path = "api/snow.d.ts"
	text, err := readResource(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	var expected []int
	for i, line := range resourceLines(text) {
		if strings.Contains(strings.ToLower(line), "workflow") {
			expected = append(expected, i+1)
		}
	}
	var found []match
	for offset := 1; ; {
		page := runTool(t, tool, arguments{Action: "search", Path: path, Query: "WoRkFlOw", Offset: offset, Limit: 3})
		if page.Total != len(expected) {
			t.Fatalf("total=%d expected=%d", page.Total, len(expected))
		}
		found = append(found, page.Matches...)
		if page.NextOffset == 0 {
			break
		}
		offset = page.NextOffset
	}
	if len(found) == 0 || len(found) != len(expected) {
		t.Fatalf("matches=%d expected=%d", len(found), len(expected))
	}
	for i, match := range found {
		if match.Path != path || match.Line != expected[i] {
			t.Fatalf("match=%+v", match)
		}
	}
	for _, action := range []string{"list", "search", "read"} {
		page := runTool(t, tool, arguments{Action: action, Path: path, Query: "workflow", Offset: math.MaxInt, Limit: 1000})
		if page.NextOffset != 0 || page.Content != "" || len(page.Resources)+len(page.Matches) != 0 {
			t.Fatalf("past end=%+v", page)
		}
	}
}

func TestResourcesRespectByteCaps(t *testing.T) {
	tool := New(Options{MaxOutputBytes: 4096})
	for _, action := range []string{"read", "list", "search"} {
		args := arguments{Action: action, Limit: 1000}
		if action == "read" {
			args.Path = "api/snow.d.ts"
		}
		if action == "search" {
			args.Query = "snow"
		}
		out := runTool(t, tool, args)
		if out.NextOffset <= 1 {
			t.Fatalf("missing byte-limit continuation: %+v", out)
		}
	}
	var full strings.Builder
	for offset := 1; ; {
		page := runTool(t, tool, arguments{Action: "read", Path: "api/snow.d.ts", Offset: offset, Limit: 1000})
		full.WriteString(page.Content)
		if page.NextOffset == 0 {
			break
		}
		offset = page.NextOffset
	}
	expected, err := readResource(t.Context(), "api/snow.d.ts")
	if err != nil || full.String() != expected {
		t.Fatal("byte-capped pagination lost content", err)
	}
}

func TestErrorOutputRespectsByteCap(t *testing.T) {
	tool := New(Options{MaxOutputBytes: 1024, Inventory: func(context.Context) ([]Plugin, error) { return nil, errors.New(strings.Repeat("不可信", 1000)) }})
	for _, raw := range []string{`{"action":"plugins"}`, `{"` + strings.Repeat("未知", 1000) + `":1}`} {
		out, err := tool.Run(t.Context(), []byte(raw), nil)
		if err != nil || !out.IsError || len(out.Content[0].Text) > 1024 || !utf8.ValidString(out.Content[0].Text) {
			t.Fatalf("unbounded or invalid error: %+v %v", out, err)
		}
	}
}

func TestInvalidRequestsAndCancellation(t *testing.T) {
	tool := New(Options{})
	for _, raw := range []string{
		`{`, `{}`, `null`, `{"action":"execute"}`, `{"action":"read"}`, `{"action":"search"}`,
		`{"action":"read","path":"../README.md"}`, `{"action":"read","path":"/etc/passwd"}`,
		`{"action":"read","path":"api/../GUIDE.md"}`, `{"action":"read","path":"api\\snow.d.ts"}`,
		`{"action":"read","path":"api"}`, `{"action":"read","path":"missing.md"}`,
		`{"action":"search","path":"../","query":"snow"}`, `{"action":"list","offset":-1}`,
		`{"action":"list","limit":1001}`, `{"action":"list","limit":-1}`,
		`{"action":"list","extra":"ignored?"}`, `{"action":"plugins"}`,
		`{"action":"search","query":"` + strings.Repeat("x", 257) + `"}`,
		strings.Repeat(" ", 16<<10) + `{}`,
	} {
		t.Run(raw[:min(len(raw), 80)], func(t *testing.T) {
			out, err := tool.Run(t.Context(), []byte(raw), nil)
			if err != nil || !out.IsError {
				t.Fatalf("accepted %q: %+v %v", raw, out, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, err := tool.Run(ctx, []byte(`{"action":"overview"}`), nil)
	if err != nil || !out.IsError || !strings.Contains(out.Content[0].Text, context.Canceled.Error()) {
		t.Fatalf("cancellation=%+v %v", out, err)
	}
	_, err = readResource(ctx, "GUIDE.md")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
