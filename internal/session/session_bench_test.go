package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// BenchmarkSQLiteContextMessages compares the recursive SQL/JSON decode miss
// with the defensive projection-only cache hit.
func BenchmarkSQLiteContextMessages(b *testing.B) {
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "context.db"), b.TempDir(), Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	batch := make([]Entry, 1500)
	body := strings.Repeat("context ", 128)
	for i := range batch {
		batch[i] = msg(fmt.Sprintf("m%d", i), "", body)
	}
	if err := store.AppendBatch(batch); err != nil {
		b.Fatal(err)
	}
	if _, err := store.ContextMessages(); err != nil {
		b.Fatal(err)
	}
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			store.mu.Lock()
			store.invalidateContextCacheLocked()
			store.mu.Unlock()
			if _, err := store.ContextMessages(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("warm", func(b *testing.B) {
		if _, err := store.ContextMessages(); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.ContextMessages(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryContextMessagesAfterCompaction(b *testing.B) {
	store := NewMemoryStore(Options{})
	defer store.Close()
	batch := make([]Entry, 5000)
	body := strings.Repeat("context ", 128)
	for i := range batch {
		batch[i] = msg(fmt.Sprintf("m%d", i), "", body)
	}
	if err := store.AppendBatch(batch); err != nil {
		b.Fatal(err)
	}
	if err := store.Append(Entry{Type: EntryCompaction, ID: "checkpoint", Summary: "bounded working state", CompactedThrough: "m4899"}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		messages, err := store.ContextMessages()
		if err != nil {
			b.Fatal(err)
		}
		if len(messages) != 101 {
			b.Fatalf("messages=%d", len(messages))
		}
	}
}

func BenchmarkSQLiteAppendBatch1500(b *testing.B) {
	b.StopTimer()
	dir := b.TempDir()
	batch := make([]Entry, 1500)
	body := strings.Repeat("batch ", 64)
	for i := range batch {
		message := protocol.NewAssistantMessage(fmt.Sprintf("batch-%d", i), "", "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: body}}, protocol.StopStop, nil)
		batch[i] = Entry{Type: EntryMessage, ID: message.ID, Message: &message}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store, err := NewSQLiteStore(filepath.Join(dir, fmt.Sprintf("append-%d.db", i)), dir, Options{})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		err = store.AppendBatch(batch)
		b.StopTimer()
		if closeErr := store.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSQLiteBranchHydration5000(b *testing.B) {
	store, err := NewSQLiteStore(filepath.Join(b.TempDir(), "hydration.db"), b.TempDir(), Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	batch := make([]Entry, 5000)
	body := strings.Repeat("x", 2400)
	for i := range batch {
		message := protocol.NewAssistantMessage(fmt.Sprintf("h%d", i), "", "fake", "model", []protocol.ContentBlock{{Type: protocol.BlockText, Text: body}}, protocol.StopStop, nil)
		batch[i] = Entry{Type: EntryMessage, ID: message.ID, Message: &message}
	}
	if err := store.AppendBatch(batch); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		snapshot, err := store.BranchHydration()
		if err != nil {
			b.Fatal(err)
		}
		if len(snapshot.Entries) != 5001 {
			b.Fatalf("entries=%d", len(snapshot.Entries))
		}
	}
}

// BenchmarkLargeSessionReload loads a session with 10k entries and measures
// the reload time (target < 500ms per IMPLEMENTATION.md §13.4).
func BenchmarkLargeSessionReload(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "big.jsonl")

	// Build the file quickly (setup only; Append's per-entry fsync would
	// dominate benchmark time).
	hdr, _ := json.Marshal(map[string]any{
		"header": Header{Version: SessionVersion, ID: "big", CWD: "/tmp"},
	})
	buf := append([]byte{}, hdr...)
	buf = append(buf, '\n')
	for i := 0; i < 10000; i++ {
		e := msg(fmt.Sprintf("m%d", i), "", fmt.Sprintf("message %d", i))
		line, _ := json.Marshal(e)
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s2, err := NewJSONLStore(path, "", Options{})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := s2.Messages(); err != nil {
			b.Fatal(err)
		}
		s2.Close()
	}
}
