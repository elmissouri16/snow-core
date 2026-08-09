// Package router provides local progressive disclosure for model-callable
// tools. It indexes compact retrieval metadata while the authoritative
// registry retains full schemas, handlers, and permission information.
package router

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	bleve "github.com/blevesearch/bleve/v2"

	"github.com/snow-core/snow/internal/tools"
)

const scoringModelBM25 = "bm25"

type indexDocument struct {
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
}

// Bleve is an in-memory BM25 router. Initialization failures are retained so
// the agent can fail open on each turn instead of making application startup
// depend on optional discovery infrastructure.
type Bleve struct {
	mu       sync.RWMutex
	index    bleve.Index
	metadata map[string]tools.ToolMatch
	count    int
	initErr  error
	closed   bool
}

// New builds an in-memory index from the deferred descriptors in catalog.
func New(catalog []tools.ToolDescriptor) *Bleve {
	r := &Bleve{metadata: make(map[string]tools.ToolMatch)}
	deferred := make([]tools.ToolDescriptor, 0, len(catalog))
	for _, desc := range catalog {
		if tools.IsDeferred(desc) {
			deferred = append(deferred, desc)
		}
	}
	r.count = len(deferred)

	mapping := bleve.NewIndexMapping()
	mapping.DefaultAnalyzer = "standard"
	mapping.ScoringModel = scoringModelBM25
	documentMapping := bleve.NewDocumentStaticMapping()
	for _, field := range []string{"namespace", "name", "description", "keywords"} {
		fieldMapping := bleve.NewTextFieldMapping()
		fieldMapping.Analyzer = "standard"
		fieldMapping.Store = false
		fieldMapping.IncludeTermVectors = false
		documentMapping.AddFieldMappingsAt(field, fieldMapping)
	}
	mapping.DefaultMapping = documentMapping

	idx, err := bleve.NewMemOnly(mapping)
	if err != nil {
		r.initErr = fmt.Errorf("tool router: create index: %w", err)
		return r
	}
	r.index = idx
	batch := idx.NewBatch()
	for _, desc := range deferred {
		discovery := desc.Schema.Discovery
		name := strings.TrimSpace(strings.Join([]string{
			desc.Schema.Name,
			desc.OriginalName,
			normalizeIdentifier(desc.Schema.Name),
			normalizeIdentifier(desc.OriginalName),
		}, " "))
		doc := indexDocument{
			Namespace:   discovery.Namespace,
			Name:        name,
			Description: desc.Schema.Description,
			Keywords:    append([]string(nil), discovery.Keywords...),
		}
		if err := batch.Index(desc.Schema.Name, doc); err != nil {
			r.failInitialization(fmt.Errorf("tool router: index %s: %w", desc.Schema.Name, err))
			return r
		}
		r.metadata[desc.Schema.Name] = tools.ToolMatch{
			ID:          desc.Schema.Name,
			Namespace:   discovery.Namespace,
			Name:        desc.OriginalName,
			Description: desc.Schema.Description,
		}
	}
	if err := idx.Batch(batch); err != nil {
		r.failInitialization(fmt.Errorf("tool router: commit index: %w", err))
	}
	return r
}

func (r *Bleve) failInitialization(err error) {
	r.initErr = err
	if r.index != nil {
		_ = r.index.Close()
		r.index = nil
	}
}

// DeferredCount returns the number of opt-in tools represented by the router.
func (r *Bleve) DeferredCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Search runs a boosted multi-field BM25 query and returns compact metadata.
func (r *Bleve) Search(ctx context.Context, text string, limit int) ([]tools.ToolMatch, error) {
	if r == nil {
		return nil, errors.New("tool router: unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("tool router: closed")
	}
	if r.initErr != nil {
		return nil, r.initErr
	}
	if r.index == nil || r.count == 0 {
		return nil, nil
	}

	nameQuery := bleve.NewMatchQuery(text)
	nameQuery.SetField("name")
	nameQuery.SetBoost(4)
	namespaceQuery := bleve.NewMatchQuery(text)
	namespaceQuery.SetField("namespace")
	namespaceQuery.SetBoost(3)
	keywordsQuery := bleve.NewMatchQuery(text)
	keywordsQuery.SetField("keywords")
	keywordsQuery.SetBoost(2)
	descriptionQuery := bleve.NewMatchQuery(text)
	descriptionQuery.SetField("description")
	descriptionQuery.SetBoost(1)
	query := bleve.NewDisjunctionQuery(nameQuery, namespaceQuery, keywordsQuery, descriptionQuery)
	request := bleve.NewSearchRequestOptions(query, limit, 0, false)
	result, err := r.index.SearchInContext(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tool router: search: %w", err)
	}

	matches := make([]tools.ToolMatch, 0, len(result.Hits))
	for _, hit := range result.Hits {
		match, ok := r.metadata[hit.ID]
		if !ok {
			continue
		}
		match.Score = hit.Score
		matches = append(matches, match)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Score > matches[j].Score
	})
	return matches, nil
}

// Refresh atomically replaces the in-memory index from the current registry.
func (r *Bleve) Refresh(catalog []tools.ToolDescriptor) error {
	if r == nil {
		return errors.New("tool router: unavailable")
	}
	fresh := New(catalog)
	if fresh.initErr != nil {
		return fresh.initErr
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = fresh.Close()
		return errors.New("tool router: closed")
	}
	old := r.index
	r.index = fresh.index
	r.metadata = fresh.metadata
	r.count = fresh.count
	r.initErr = nil
	fresh.index = nil
	r.mu.Unlock()
	if old != nil {
		return old.Close()
	}
	return nil
}

// Close releases the memory index. It is safe to call more than once.
func (r *Bleve) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.index == nil {
		return nil
	}
	return r.index.Close()
}

func normalizeIdentifier(value string) string {
	return strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(value)
}

var _ tools.Router = (*Bleve)(nil)
var _ tools.RefreshableRouter = (*Bleve)(nil)
