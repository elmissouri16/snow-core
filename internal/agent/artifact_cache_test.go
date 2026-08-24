package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/elmissouri16/snow-core/internal/artifact"
	"github.com/elmissouri16/snow-core/internal/compact"
	"github.com/elmissouri16/snow-core/internal/session"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type countingArtifactStore struct {
	artifact.Store
	mu              sync.Mutex
	saves           int
	verifiedChecks  int
	forceUnverified bool
}

func (s *countingArtifactStore) SaveText(ctx context.Context, sessionID, key, text string) (artifact.Ref, error) {
	s.mu.Lock()
	s.saves++
	s.mu.Unlock()
	return s.Store.SaveText(ctx, sessionID, key, text)
}

func (s *countingArtifactStore) TextReferenceVerified(ctx context.Context, sessionID, id string) (bool, error) {
	s.mu.Lock()
	s.verifiedChecks++
	forceUnverified := s.forceUnverified
	s.mu.Unlock()
	if forceUnverified {
		return false, nil
	}
	verifier, ok := s.Store.(artifact.ReferenceVerifier)
	if !ok {
		return false, nil
	}
	return verifier.TextReferenceVerified(ctx, sessionID, id)
}

func TestHistoricalToolArtifactCacheReverifiesAndRepairs(t *testing.T) {
	local, err := artifact.NewLocalStore(t.TempDir(), artifact.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingArtifactStore{Store: local}
	store := session.NewMemoryStore(session.Options{ID: "session-one"})
	t.Cleanup(func() {
		_ = store.Close()
		_ = counting.Close()
	})
	a := &Agent{opts: Options{Session: store, Artifacts: counting, Compaction: CompactionOptions{HistoricalToolResultThreshold: compact.HistoricalToolResultThreshold}}}
	message := protocol.NewToolResultMessage("result", "assistant", "call", "read", []protocol.ContentBlock{protocol.NewTextBlock(string(make([]byte, compact.HistoricalToolResultThreshold+1024)))}, false)

	first := a.pruneHistoricalToolResults(context.Background(), []protocol.Message{message})
	second := a.pruneHistoricalToolResults(context.Background(), []protocol.Message{message})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("cached artifact reference changed provider projection")
	}
	counting.mu.Lock()
	if counting.saves != 1 || counting.verifiedChecks != 1 {
		t.Fatalf("saves=%d verified=%d", counting.saves, counting.verifiedChecks)
	}
	counting.forceUnverified = true
	counting.mu.Unlock()
	third := a.pruneHistoricalToolResults(context.Background(), []protocol.Message{message})
	if !reflect.DeepEqual(first, third) {
		t.Fatal("artifact repair changed provider projection")
	}
	counting.mu.Lock()
	defer counting.mu.Unlock()
	if counting.saves != 2 || counting.verifiedChecks != 2 {
		t.Fatalf("repair saves=%d verified=%d", counting.saves, counting.verifiedChecks)
	}
}
