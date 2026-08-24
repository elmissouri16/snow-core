package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/elmissouri16/snow-core/internal/app"
)

func TestTranscriptViewportCacheTracksContentScrollAndSize(t *testing.T) {
	m := newModel(t.Context(), app.Options{})
	m.width, m.height = 80, 18
	m.lines = make([]string, 80)
	for i := range m.lines {
		m.lines[i] = fmt.Sprintf("line %02d %s", i, strings.Repeat("x", 20))
	}
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.layout()
	m.refreshTranscriptForced()

	first := m.transcriptViewportView()
	if first == "" || !m.transcriptViewCacheValid {
		t.Fatal("initial transcript viewport was not cached")
	}
	revision := m.transcriptViewRevision
	if second := m.transcriptViewportView(); second != first || m.transcriptViewRevision != revision {
		t.Fatalf("stable viewport cache changed: revision=%d want=%d", m.transcriptViewRevision, revision)
	}

	m.transcript.GotoTop()
	top := m.transcriptViewportView()
	if m.transcriptViewCacheOffset != m.transcript.YOffset || top == first {
		t.Fatalf("scroll did not refresh viewport cache: offset=%d viewport=%d", m.transcriptViewCacheOffset, m.transcript.YOffset)
	}

	m.appendTranscriptLine("new cached content sentinel")
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.transcript.GotoBottom()
	m.refreshTranscriptForced()
	if m.transcriptViewRevision <= revision || m.transcriptViewCacheValid {
		t.Fatalf("content refresh did not invalidate viewport cache: revision=%d valid=%t", m.transcriptViewRevision, m.transcriptViewCacheValid)
	}
	updated := m.transcriptViewportView()
	if !strings.Contains(updated, "new cached content sentinel") {
		t.Fatalf("updated viewport omitted new content: %q", updated)
	}

	m.width = 60
	m.layout()
	m.refreshTranscriptForced()
	_ = m.transcriptViewportView()
	if m.transcriptViewCacheWidth != m.transcript.Width || m.transcriptViewCacheHeight != m.transcript.Height {
		t.Fatalf("resize cache dimensions=(%d,%d) viewport=(%d,%d)", m.transcriptViewCacheWidth, m.transcriptViewCacheHeight, m.transcript.Width, m.transcript.Height)
	}
}

func TestTranscriptViewportCacheDoesNotRetainOversizedView(t *testing.T) {
	m := &Model{transcript: viewport.New(maxTranscriptViewportCacheBytes+1, 1)}
	m.transcript.SetContent("x")
	view := m.transcriptViewportView()
	if len(view) <= maxTranscriptViewportCacheBytes {
		t.Fatalf("oversized viewport rendered %d bytes", len(view))
	}
	if m.transcriptViewCacheValid || m.transcriptViewCache != "" {
		t.Fatal("oversized viewport remained retained")
	}
}

func TestManagedFrameCacheKeysExactInputAndDimensions(t *testing.T) {
	m := &Model{}
	first := m.fitManagedFrame("alpha", 20, 4)
	if !m.managedFrameCacheValid || first == "" {
		t.Fatal("managed frame was not cached")
	}
	if second := m.fitManagedFrame("alpha", 20, 4); second != first {
		t.Fatalf("stable managed frame changed: first=%q second=%q", first, second)
	}
	changed := m.fitManagedFrame("beta", 20, 4)
	if changed == first || m.managedFrameCacheInput != "beta" {
		t.Fatalf("content change reused stale frame: %q", changed)
	}
	resized := m.fitManagedFrame("beta", 12, 2)
	if resized == changed || m.managedFrameCacheWidth != 12 || m.managedFrameCacheHeight != 2 {
		t.Fatalf("dimension change reused stale frame: %q", resized)
	}
	_ = m.fitManagedFrame(strings.Repeat("x", maxManagedFrameCacheBytes+1), 12, 2)
	if m.managedFrameCacheValid || m.managedFrameCacheInput != "" || m.managedFrameCacheOutput != "" {
		t.Fatal("oversized managed frame remained retained")
	}
}
