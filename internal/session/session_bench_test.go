package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
