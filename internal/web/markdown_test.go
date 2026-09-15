package web

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/html"
)

func TestMarkdownDisplaySupportsFormattingWithoutActiveHTML(t *testing.T) {
	source := "# Heading\n\n**bold** and *italic* and ~~gone~~.\n\n- one\n- two\n\n```go\nfmt.Println(\"hello\")\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n[link](https://example.com/path)\n\n![remote](https://tracker.invalid/image)\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(1)) [data](data:text/html,evil)\n"
	result := string(markdownHTML(source))
	for _, want := range []string{"<h1>Heading</h1>", "<strong>bold</strong>", "<em>italic</em>", "<del>gone</del>", "<ul>", "<pre><code>", "<table>", `href="https://example.com/path"`} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing %q: %s", want, result)
		}
	}
	assertMarkdownSafe(t, result)
}
func assertMarkdownSafe(t *testing.T, markup string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "img", "svg", "math", "iframe", "object", "form", "input", "video", "audio":
				t.Errorf("active node %s", n.Data)
			}
			for _, attr := range n.Attr {
				if strings.HasPrefix(attr.Key, "on") || attr.Key == "style" || attr.Key == "src" || attr.Key == "id" {
					t.Errorf("unsafe attribute %+v", attr)
				}
				if attr.Key == "href" && !strings.HasPrefix(attr.Val, "https://") && !strings.HasPrefix(attr.Val, "http://") {
					t.Errorf("unsafe href %q", attr.Val)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
}
func TestMarkdownAdversarialInputsAndBounds(t *testing.T) {
	for _, text := range []string{
		"<img src=x onerror=alert(1)>",
		"[x](java&#x73;cript:alert(1))",
		"[x](https://user:password@example.com)",
		"[x](/access/pair) [x](//evil.example/a)",
		"<svg><a xlink:href='javascript:alert(1)'>x</a></svg>",
		"```html\n</code></pre><script>alert(1)</script>\n```",
		strings.Repeat("&", runtimeMessageBytes),
		strings.Repeat("<>&'\"", runtimeMessageBytes),
	} {
		result := string(markdownHTML(text))
		assertMarkdownSafe(t, result)
		if len(result) > markdownHTMLLimit {
			t.Fatalf("unbounded rendered bytes: %d", len(result))
		}
	}
}
func TestMarkdownCacheConcurrentBoundedAndSnapshotIsolation(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 80 {
		wg.Go(func() {
			text := fmt.Sprintf("# %d\n\n%s", i, strings.Repeat("long text ", 4000))
			assertMarkdownSafe(t, string(markdownHTML(text)))
		})
	}
	wg.Wait()
	markdownCache.Lock()
	if len(markdownCache.values) > 64 || markdownCache.bytes > markdownCacheLimit {
		t.Error("unbounded Markdown cache")
	}
	markdownCache.Unlock()
	original := RuntimeSnapshot{Messages: []RuntimeMessage{{Role: "assistant", Text: "**answer**", HTML: "<script>not trusted</script>"}, {Role: "user", Text: "**plain**", HTML: "<script>not trusted</script>"}}}
	projected := displaySnapshot(original)
	if !strings.Contains(projected.Messages[0].HTML, "<strong>answer</strong>") || projected.Messages[1].HTML != "" {
		t.Fatal("invalid HTML projection")
	}
	if original.Messages[0].HTML != "<script>not trusted</script>" {
		t.Fatal("presentation mutated backend snapshot")
	}
}
