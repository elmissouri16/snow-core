package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/app"
	"github.com/snow-core/snow/pkg/protocol"
)

func BenchmarkMailboxIngestion(b *testing.B) {
	for i := 0; i < b.N; i++ {
		q := newAgentEventMailbox()
		for j := 0; j < 10000; j++ {
			q.Push(protocol.AgentEvent{Type: protocol.EvTextDelta, Text: "token "})
		}
		_ = q.popBatch(maxAgentEventsPerUpdate)
	}
}

func BenchmarkTranscriptRefresh10K(b *testing.B) {
	m := newModel(context.Background(), app.Options{})
	m.width, m.height = 120, 40
	m.layout()
	m.lines = make([]string, 10000)
	for i := range m.lines {
		m.lines[i] = fmt.Sprintf("line %d: stable transcript content", i)
	}
	for i := 0; i < b.N; i++ {
		m.transcriptBaseDirty = true
		m.transcriptDirty = true
		m.refreshTranscriptForced()
	}
}

func BenchmarkViewNormalAndNarrow(b *testing.B) {
	for _, width := range []int{40, 120} {
		b.Run(fmt.Sprintf("width-%d", width), func(b *testing.B) {
			m := newModel(context.Background(), app.Options{})
			m.width, m.height = width, 30
			m.lines = make([]string, 500)
			for i := range m.lines {
				m.lines[i] = "assistant output " + strings.Repeat("x", 24)
			}
			m.layout()
			m.refreshTranscriptForced()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
	}
}

func BenchmarkMentionMatching(b *testing.B) {
	files := make([]string, 2000)
	for i := range files {
		files[i] = fmt.Sprintf("internal/package-%03d/file.go", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = matchMentionFiles(files, "file")
	}
}

func BenchmarkMentionDiscovery(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 200; i++ {
		path := filepath.Join(root, fmt.Sprintf("pkg-%03d", i), "file.go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = discoverMentionFiles(root)
	}
}
