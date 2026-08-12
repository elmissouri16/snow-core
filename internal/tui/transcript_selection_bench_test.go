package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/snow-core/snow/internal/app"
)

func BenchmarkTranscriptSelectionDragFrame(b *testing.B) {
	m := newModel(context.Background(), app.Options{})
	m.width, m.height = 160, 50
	m.layout()
	lines := make([]string, 10000)
	for i := range lines {
		lines[i] = fmt.Sprintf("%05d %s", i, strings.Repeat("selection benchmark text ", 5))
	}
	m.lines = lines
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	m.transcript.SetYOffset(5000)
	m.transcriptSelection.anchor = &transcriptSelectionPoint{row: 5000, col: 0}
	m.transcriptSelection.focus = &transcriptSelectionPoint{row: 5000, col: 1}
	m.transcriptSelection.pressActive = true
	m.cacheTranscriptSelectionView()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		point := transcriptSelectionPoint{row: 5000 + i%(m.transcript.Height-1), col: i % m.transcript.Width}
		m.updateTranscriptSelectionFocus(point)
		_ = m.View()
	}
}
