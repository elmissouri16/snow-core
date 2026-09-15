package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestSavedHistoryToolsTemplates(t *testing.T) {
	const dangerous = `<script>alert("tool")</script><img src=x onerror="alert(1)"> **literal** & text`
	tools := []protocol.RPCHistoryTool{
		{ID: "tool-public", OwnerID: "owner", ResultID: "result", Tool: "read", Status: "completed", OutputAvailable: true, Output: dangerous},
		{ID: "tool-private", OwnerID: "owner", Tool: "private", Status: "completed", Output: "PRIVATE_SENTINEL"},
		{ID: "tool-unknown", OwnerID: "owner", Tool: "unknown", Status: "unresolved", OutputAvailable: true, Output: "UNKNOWN_SENTINEL"},
		{ID: "tool-failed", OwnerID: "owner", Tool: "bash", Status: "failed", OutputAvailable: true, Output: "failed output", Truncated: true},
		{ID: "tool-empty", OwnerID: "owner", Tool: "empty", Status: "completed", OutputAvailable: true},
	}
	for _, name := range []string{"runtime-messages", "projects"} {
		t.Run(name, func(t *testing.T) {
			var data any = []RuntimeMessage{
				{ID: "owner", Role: "assistant", Tools: tools, Truncated: true},
				{ID: "later", Role: "assistant", Text: "Later answer"},
			}
			if name == "projects" {
				data = pageData{Project: &Project{ID: "project", Name: "Project"}, History: &CatalogMessages{Messages: []HistoryMessage{
					{ID: "owner", Role: "assistant", Tools: tools, Truncated: true},
					{ID: "later", Role: "assistant", Text: "Later answer"},
				}}}
			}
			var output bytes.Buffer
			if err := templates.ExecuteTemplate(&output, name, data); err != nil {
				t.Fatal(err)
			}
			html := output.String()
			for _, want := range []string{
				`data-message-id="owner"`, `data-message-truncated="true"`, `data-history-tool-id="tool-public"`,
				`&lt;script&gt;alert(&#34;tool&#34;)&lt;/script&gt;`, `**literal** &amp; text`,
				`Public output was not recorded`, `No result recorded; execution outcome unknown`,
				`data-status="failed"`, `Tool output truncated for bounded display.`,
				`<pre class="activity-output"></pre>`,
			} {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q in rendered saved tool history", want)
				}
			}
			for _, forbidden := range []string{dangerous, "PRIVATE_SENTINEL", "UNKNOWN_SENTINEL", "<script>", "<img ", "Retry", "Running", "Canceled"} {
				if strings.Contains(html, forbidden) {
					t.Errorf("unexpected %q in rendered saved tool history", forbidden)
				}
			}
			if name == "projects" && strings.Count(html, "data-react-messages") != 1 {
				t.Fatal("saved history lacks its single message-only React owner")
			}
			ownerStart := strings.Index(html, `data-message-id="owner"`)
			ownerEnd := ownerStart + strings.Index(html[ownerStart:], "</article>")
			owner := html[ownerStart:ownerEnd]
			if strings.Count(owner, "data-history-tool-id=") != len(tools) || strings.Count(html, "data-history-tool-id=") != len(tools) {
				t.Fatal("tool-only assistant does not exclusively own all saved disclosures")
			}
			if name == "runtime-messages" && strings.Index(owner, `class="message-tools`) > strings.Index(owner, `class="message-actions`) {
				t.Fatal("saved tools must precede message copy actions")
			}
		})
	}
}
