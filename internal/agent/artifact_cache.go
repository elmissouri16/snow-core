package agent

import (
	"context"

	"github.com/elmissouri16/snow-core/internal/artifact"
)

// saveHistoricalToolArtifact caches only successfully persisted references.
// Every reuse is verified through the artifact store, preserving deletion and
// tamper detection while avoiding repeated hashing and idempotent saves of the
// same immutable durable tool result.
func (a *Agent) saveHistoricalToolArtifact(ctx context.Context, sessionID, key, text string, cacheable bool) string {
	cacheKey := sessionID + "\x00" + key
	if cacheable {
		a.mu.RLock()
		cached := a.artifactRefs[cacheKey]
		a.mu.RUnlock()
		if cached != "" {
			if verifier, ok := a.opts.Artifacts.(artifact.ReferenceVerifier); ok {
				if verified, err := verifier.TextReferenceVerified(ctx, sessionID, cached); err == nil && verified {
					return cached
				}
			}
			a.mu.Lock()
			delete(a.artifactRefs, cacheKey)
			a.mu.Unlock()
		}
	}
	ref, err := a.opts.Artifacts.SaveText(ctx, sessionID, key, text)
	if err != nil {
		return ""
	}
	if cacheable {
		a.mu.Lock()
		if a.artifactRefs == nil {
			a.artifactRefs = make(map[string]string)
		}
		if _, exists := a.artifactRefs[cacheKey]; !exists && len(a.artifactRefs) >= maxCachedArtifactReferences {
			// A small whole-cache reset keeps eviction work bounded and cannot
			// affect artifact correctness because every reuse is reverified.
			clear(a.artifactRefs)
		}
		a.artifactRefs[cacheKey] = ref.ID
		a.mu.Unlock()
	}
	return ref.ID
}
