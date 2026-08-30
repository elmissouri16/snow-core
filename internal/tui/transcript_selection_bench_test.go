package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/internal/app"
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
	frame := 0
	for b.Loop() {
		point := transcriptSelectionPoint{row: 5000 + frame%(m.transcript.Height-1), col: frame % m.transcript.Width}
		m.updateTranscriptSelectionFocus(point)
		_ = m.View()
		frame++
	}
}
