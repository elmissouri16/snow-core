package tui

import (
	"context"
	"strings"
	"testing"
	"unsafe"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/internal/session"
	"github.com/snow-core/snow/pkg/protocol"
)

func TestJoinTranscriptContentReusesSingleInput(t *testing.T) {
	base := strings.Repeat("stable transcript\n", 1024)
	if got := joinTranscriptContent(base, ""); got != base {
		t.Fatalf("stable-only content changed")
	} else if unsafe.StringData(got) != unsafe.StringData(base) {
		t.Fatal("stable-only content copied its backing buffer")
	}

	live := strings.Repeat("live tail\n", 128)
	if got := joinTranscriptContent("", live); got != live {
		t.Fatalf("live-only content changed")
	}

	if got, want := joinTranscriptContent(base, live), base+"\n"+live; got != want {
		t.Fatalf("combined content = %q, want %q", got, want)
	}
}

func TestIdleTranscriptSnapshotReusesStableBase(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	m.width = 100
	m.height = 30
	m.layout()
	m.lines = []string{strings.Repeat("stable transcript row ", 1024)}
	m.transcriptBaseDirty = true
	m.refreshTranscriptWithForce(true)

	if m.transcriptBase == "" || m.transcriptContent != m.transcriptBase {
		t.Fatal("idle refresh did not publish the stable transcript")
	}
	if unsafe.StringData(m.transcriptContent) != unsafe.StringData(m.transcriptBase) {
		t.Fatal("idle refresh copied the stable transcript backing buffer")
	}
}

func TestMarkdownRendererClearCacheRetainsRenderer(t *testing.T) {
	r := newMarkdownRenderer()
	const input = "# Cached heading\n\nRendered body."
	first := r.render(input, 80)
	if r.renderer == nil || r.lastRaw == "" || r.lastOut == "" {
		t.Fatal("render did not populate the document cache")
	}
	termRenderer := r.renderer

	r.clearCache()
	if r.lastRaw != "" || r.lastOut != "" || r.lastW != 0 {
		t.Fatalf("cache retained raw=%d rendered=%d width=%d", len(r.lastRaw), len(r.lastOut), r.lastW)
	}
	if r.renderer != termRenderer {
		t.Fatal("clearing documents discarded the reusable terminal renderer")
	}
	if second := r.render(input, 80); second != first {
		t.Fatal("render output changed after clearing the document cache")
	}
	if r.renderer != termRenderer {
		t.Fatal("render recreated the terminal renderer at the same width")
	}
}

func TestFinalizedMarkdownCachesAreReleased(t *testing.T) {
	tests := []struct {
		name     string
		finalize func(*Model)
		cache    func(*Model) *mdRenderer
	}{
		{
			name: "assistant",
			finalize: func(m *Model) {
				m.assistantBuf.WriteString("# Assistant\n\nFinal response.")
				m.finalizeAssistant()
			},
			cache: func(m *Model) *mdRenderer { return m.md },
		},
		{
			name: "thinking",
			finalize: func(m *Model) {
				m.thinkingBuf.WriteString("**Inspecting** the repository.")
				m.finalizeThinking()
			},
			cache: func(m *Model) *mdRenderer { return m.thinkingMD },
		},
		{
			name: "plan",
			finalize: func(m *Model) {
				m.planBuf.WriteString("## Plan\n\n- Apply the change")
				m.finalizePlan()
			},
			cache: func(m *Model) *mdRenderer { return m.md },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := newModel(context.Background(), app.Options{})
			test.finalize(m)
			cache := test.cache(m)
			if cache.renderer == nil {
				t.Fatal("finalization did not render Markdown")
			}
			if cache.lastRaw != "" || cache.lastOut != "" || cache.lastW != 0 {
				t.Fatalf("finalization retained raw=%d rendered=%d width=%d", len(cache.lastRaw), len(cache.lastOut), cache.lastW)
			}
			if len(m.lines) == 0 {
				t.Fatal("finalization did not retain the rendered transcript row")
			}
		})
	}
}

func TestSessionHydrationReleasesMarkdownCaches(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	buildAppForTest(t, m)
	message := protocol.NewAssistantMessage(
		"cached-markdown",
		m.app.Session.BranchTip(),
		"fake",
		"fake-model",
		[]protocol.ContentBlock{
			{Type: protocol.BlockThinking, Text: "**Inspecting** files."},
			{Type: protocol.BlockText, Text: "# Result\n\nHydrated Markdown."},
		},
		protocol.StopStop,
		nil,
	)
	if err := m.app.Session.Append(session.Entry{Type: session.EntryMessage, ID: message.ID, Message: &message}); err != nil {
		t.Fatal(err)
	}

	m.hydrateSession()
	for name, cache := range map[string]*mdRenderer{"assistant": m.md, "thinking": m.thinkingMD} {
		if cache.renderer == nil {
			t.Fatalf("%s Markdown was not rendered during hydration", name)
		}
		if cache.lastRaw != "" || cache.lastOut != "" || cache.lastW != 0 {
			t.Fatalf("hydration retained %s raw=%d rendered=%d width=%d", name, len(cache.lastRaw), len(cache.lastOut), cache.lastW)
		}
	}
}

func TestCloseSubagentFleetReleasesMarkdownCache(t *testing.T) {
	m := newModel(context.Background(), app.Options{})
	_ = m.renderSubagentFleetMarkdown("# Child result", 80)
	if m.subagentFleetMD.lastRaw == "" || m.subagentFleetMD.lastOut == "" {
		t.Fatal("subagent detail render did not populate the cache")
	}

	m.closeSubagentFleet()
	if m.subagentFleetMD.lastRaw != "" || m.subagentFleetMD.lastOut != "" || m.subagentFleetMD.lastW != 0 {
		t.Fatal("closing subagent detail retained its Markdown cache")
	}
}
