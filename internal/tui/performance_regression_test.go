package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestTranscriptBaseIncrementalMatchesFullRebuild(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"seed"})
	for i := 0; i < 50; i++ {
		m.appendTranscriptLine(fmt.Sprintf("line %d\nwide 界 content", i))
		m.transcriptBaseDirty = true
		m.transcriptDirty = true
		m.refreshTranscriptForced()
	}
	incremental := m.transcriptBase
	m.transcriptBase = ""
	m.transcriptBaseSynced = 0
	m.transcriptBaseAppend = false
	m.transcriptBaseDirty = true
	m.transcriptDirty = true
	m.refreshTranscriptForced()
	if m.transcriptBase != incremental {
		t.Fatal("incremental transcript base differs from full rebuild")
	}
}

func TestTranscriptRetentionIsBounded(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, nil)
	for i := 0; i < maxTranscriptEntries+100; i++ {
		m.appendTranscriptLine(fmt.Sprintf("line-%04d", i))
	}
	if len(m.lines) > maxTranscriptEntries {
		t.Fatalf("lines=%d, want <=%d", len(m.lines), maxTranscriptEntries)
	}
	if m.transcriptDropped < 100 || !strings.Contains(stripANSI(m.lines[0]), "older transcript entries omitted") {
		t.Fatalf("dropped=%d first=%q", m.transcriptDropped, stripANSI(m.lines[0]))
	}
	if got := stripANSI(m.lines[len(m.lines)-1]); got != fmt.Sprintf("line-%04d", maxTranscriptEntries+99) {
		t.Fatalf("tail=%q", got)
	}
}

func TestTranscriptRetentionTruncatesSingleOversizedEntry(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, nil)
	m.appendTranscriptLine(strings.Repeat("x", maxTranscriptBytes+1024))
	if m.transcriptBytes > maxTranscriptBytes || len(m.lines) != 1 || !strings.Contains(stripANSI(m.lines[0]), "entry truncated") {
		t.Fatalf("bytes=%d lines=%d", m.transcriptBytes, len(m.lines))
	}
}

func TestTranscriptSelectionLinesAreLazy(t *testing.T) {
	m := newTranscriptSelectionTestModel(t, []string{"one", "two"})
	if m.transcriptSelectionLines != nil {
		t.Fatal("refresh eagerly split selection rows")
	}
	if got := m.transcriptSelectionSourceLines(); len(got) != 2 {
		t.Fatalf("selection rows=%v", got)
	}
}

func TestAgentEventMailboxBoundsFlood(t *testing.T) {
	q := newAgentEventMailbox()
	defer q.Close()
	for i := 0; i < maxMailboxQueuedItems*2; i++ {
		typ := protocol.EvTextDelta
		if i%2 != 0 {
			typ = protocol.EvThinkingDelta
		}
		q.Push(protocol.AgentEvent{Type: typ, Text: "x"})
	}
	q.Push(protocol.AgentEvent{Type: protocol.EvTurnDone})
	if got := q.len(); got > maxMailboxQueuedItems {
		t.Fatalf("mailbox items=%d, want <=%d", got, maxMailboxQueuedItems)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bytes > maxMailboxQueuedBytes || q.dropped == 0 {
		t.Fatalf("bytes=%d dropped=%d", q.bytes, q.dropped)
	}
	foundDone := false
	for _, item := range q.items {
		foundDone = foundDone || item.event.Type == protocol.EvTurnDone
	}
	if !foundDone {
		t.Fatal("bounded mailbox dropped newest lifecycle event")
	}
}
