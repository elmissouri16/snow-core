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
	"unicode/utf8"

	bleve "github.com/blevesearch/bleve/v2"
	blevemapping "github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/snow-core/snow/internal/tools"
)

const (
	scoringModelBM25        = "bm25"
	namespaceSearchLimit    = 3
	globalRescueWindow      = 5
	rrfConstant             = 60.0
	globalRRFWeight         = 1.0
	namespaceRRFWeight      = 1.15
	namespaceSummaryMaxSize = 32 << 10

	// Fixed field budgets keep one very large namespace from starving the
	// process while retaining the most valuable identifiers before prose.
	namespaceFieldMaxSize        = 512
	namespaceNamesMaxSize        = 16 << 10
	namespaceKeywordsMaxSize     = 8 << 10
	namespaceDescriptionsMaxSize = namespaceSummaryMaxSize - namespaceFieldMaxSize - namespaceNamesMaxSize - namespaceKeywordsMaxSize
)

type indexDocument struct {
	NamespaceID string   `json:"namespace_id"`
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
}

type namespaceAccumulator struct {
	names        []string
	keywords     []string
	descriptions []string
}

type scoredHit struct {
	id    string
	score float64
}

type indexFactory func(blevemapping.IndexMapping) (bleve.Index, error)

// Bleve is an in-memory, namespace-first BM25 router. Initialization failures
// are retained so the agent can fail open on each turn instead of making
// application startup depend on optional discovery infrastructure.
type Bleve struct {
	mu             sync.RWMutex
	index          bleve.Index
	namespaceIndex bleve.Index
	metadata       map[string]tools.ToolMatch
	count          int
	namespaceCount int
	initErr        error
	closed         bool
	factory        indexFactory
}

// New builds in-memory namespace and tool indexes from the deferred descriptors
// in catalog.
func New(catalog []tools.ToolDescriptor) *Bleve {
	return newWithFactory(catalog, bleve.NewMemOnly)
}

func newWithFactory(catalog []tools.ToolDescriptor, factory indexFactory) *Bleve {
	if factory == nil {
		factory = bleve.NewMemOnly
	}
	r := &Bleve{metadata: make(map[string]tools.ToolMatch), factory: factory}
	deferred := make([]tools.ToolDescriptor, 0, len(catalog))
	for _, desc := range catalog {
		if tools.IsDeferred(desc) {
			deferred = append(deferred, desc)
		}
	}
	sort.SliceStable(deferred, func(i, j int) bool {
		return deferred[i].Schema.Name < deferred[j].Schema.Name
	})
	r.count = len(deferred)

	idx, err := factory(newIndexMapping())
	if err != nil {
		r.initErr = fmt.Errorf("tool router: create tool index: %w", err)
		return r
	}
	r.index = idx
	toolBatch := idx.NewBatch()
	for _, desc := range deferred {
		discovery := desc.Schema.Discovery
		name := strings.TrimSpace(strings.Join([]string{
			desc.Schema.Name,
			desc.OriginalName,
			normalizeIdentifier(desc.Schema.Name),
			normalizeIdentifier(desc.OriginalName),
		}, " "))
		doc := indexDocument{
			NamespaceID: discovery.Namespace,
			Namespace:   discovery.Namespace,
			Name:        name,
			Description: desc.Schema.Description,
			Keywords:    append([]string(nil), discovery.Keywords...),
		}
		if err := toolBatch.Index(desc.Schema.Name, doc); err != nil {
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
	if err := idx.Batch(toolBatch); err != nil {
		r.failInitialization(fmt.Errorf("tool router: commit tool index: %w", err))
		return r
	}

	namespaceDocs := buildNamespaceDocuments(deferred)
	r.namespaceCount = len(namespaceDocs)
	namespaceIdx, err := factory(newIndexMapping())
	if err != nil {
		r.failInitialization(fmt.Errorf("tool router: create namespace index: %w", err))
		return r
	}
	r.namespaceIndex = namespaceIdx
	namespaceBatch := namespaceIdx.NewBatch()
	namespaces := make([]string, 0, len(namespaceDocs))
	for namespace := range namespaceDocs {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		if err := namespaceBatch.Index(namespace, namespaceDocs[namespace]); err != nil {
			r.failInitialization(fmt.Errorf("tool router: index namespace %s: %w", namespace, err))
			return r
		}
	}
	if err := namespaceIdx.Batch(namespaceBatch); err != nil {
		r.failInitialization(fmt.Errorf("tool router: commit namespace index: %w", err))
	}
	return r
}

func newIndexMapping() blevemapping.IndexMapping {
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
	namespaceIDMapping := bleve.NewTextFieldMapping()
	namespaceIDMapping.Analyzer = "keyword"
	namespaceIDMapping.Store = false
	namespaceIDMapping.IncludeTermVectors = false
	documentMapping.AddFieldMappingsAt("namespace_id", namespaceIDMapping)
	mapping.DefaultMapping = documentMapping
	return mapping
}

func buildNamespaceDocuments(deferred []tools.ToolDescriptor) map[string]indexDocument {
	accumulators := make(map[string]*namespaceAccumulator)
	for _, desc := range deferred {
		if !tools.IsDeferred(desc) || desc.Schema.Discovery == nil {
			continue
		}
		namespace := strings.TrimSpace(desc.Schema.Discovery.Namespace)
		// Registry validation normally limits namespaces to 64 lowercase ASCII
		// bytes. Keep the summary builder independently bounded for callers that
		// supply raw descriptors; invalid/oversized namespaces remain globally
		// searchable through the tool index.
		if namespace == "" || len(namespace) > namespaceFieldMaxSize || !utf8.ValidString(namespace) {
			continue
		}
		acc := accumulators[namespace]
		if acc == nil {
			acc = &namespaceAccumulator{}
			accumulators[namespace] = acc
		}
		acc.names = append(acc.names,
			desc.Schema.Name,
			desc.OriginalName,
			normalizeIdentifier(desc.Schema.Name),
			normalizeIdentifier(desc.OriginalName),
		)
		acc.keywords = append(acc.keywords, desc.Schema.Discovery.Keywords...)
		acc.descriptions = append(acc.descriptions, desc.Schema.Description)
	}

	documents := make(map[string]indexDocument, len(accumulators))
	for namespace, acc := range accumulators {
		namespaceTextBudget := max(0, namespaceFieldMaxSize-len(namespace))
		documents[namespace] = indexDocument{
			NamespaceID: namespace,
			Namespace: boundedJoin(uniqueSorted([]string{
				namespace,
				normalizeIdentifier(namespace),
			}), namespaceTextBudget, false),
			Name: boundedJoin(uniqueSorted(acc.names), namespaceNamesMaxSize, false),
			Keywords: []string{boundedJoin(
				uniqueSorted(acc.keywords), namespaceKeywordsMaxSize, false,
			)},
			Description: boundedJoin(uniqueSorted(acc.descriptions), namespaceDescriptionsMaxSize, true),
		}
	}
	return documents
}

func uniqueSorted(values []string) []string {
	values = append([]string(nil), values...)
	sort.SliceStable(values, func(i, j int) bool {
		left, right := strings.ToLower(strings.TrimSpace(values[i])), strings.ToLower(strings.TrimSpace(values[j]))
		if left == right {
			return strings.TrimSpace(values[i]) < strings.TrimSpace(values[j])
		}
		return left < right
	})
	out := values[:0]
	last := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if len(out) > 0 && key == last {
			continue
		}
		out = append(out, value)
		last = key
	}
	return out
}

func boundedJoin(values []string, maxBytes int, truncateLast bool) string {
	if maxBytes <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(min(maxBytes, 1024))
	for _, value := range values {
		separator := 0
		if builder.Len() > 0 {
			separator = 1
		}
		remaining := maxBytes - builder.Len() - separator
		if remaining <= 0 {
			break
		}
		if len(value) > remaining {
			if !truncateLast {
				continue
			}
			value = truncateUTF8(value, remaining)
		}
		if value == "" {
			continue
		}
		if separator != 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(value)
		if builder.Len() == maxBytes {
			break
		}
	}
	return builder.String()
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}

func namespaceDocumentSize(doc indexDocument) int {
	size := len(doc.NamespaceID) + len(doc.Namespace) + len(doc.Name) + len(doc.Description)
	for _, keyword := range doc.Keywords {
		size += len(keyword)
	}
	return size
}

func (r *Bleve) failInitialization(err error) {
	r.initErr = err
	toolIndex, namespaceIndex := r.index, r.namespaceIndex
	r.index, r.namespaceIndex = nil, nil
	_ = closeIndexes(toolIndex, namespaceIndex)
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

// Search selects likely namespaces, runs global and namespace-scoped BM25 tool
// searches, and fuses both ranked lists. A namespace-only failure degrades to
// the original global ranking; a tool-index failure remains a total failure so
// the agent can preserve its existing fail-open behavior.
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

	var namespaces []string
	if r.namespaceCount > 1 && r.namespaceIndex != nil {
		var err error
		namespaces, err = searchNamespaceIndex(ctx, r.namespaceIndex, text, namespaceSearchLimit)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			namespaces = nil
		}
	}

	global, err := r.searchToolIndex(ctx, text, nil, limit)
	if err != nil {
		return nil, fmt.Errorf("tool router: search tools: %w", err)
	}
	if len(namespaces) == 0 {
		return global, nil
	}
	scoped, err := r.searchToolIndex(ctx, text, namespaces, limit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return global, nil
	}
	if len(scoped) == 0 {
		return global, nil
	}
	return fuseRankings(global, scoped, limit), nil
}

func searchNamespaceIndex(ctx context.Context, idx bleve.Index, text string, limit int) ([]string, error) {
	hits, err := searchIndex(ctx, idx, boostedMetadataQuery(text), limit)
	if err != nil {
		return nil, fmt.Errorf("tool router: search namespaces: %w", err)
	}
	namespaces := make([]string, 0, len(hits))
	for _, hit := range hits {
		namespaces = append(namespaces, hit.id)
	}
	return namespaces, nil
}

func (r *Bleve) searchToolIndex(ctx context.Context, text string, namespaces []string, limit int) ([]tools.ToolMatch, error) {
	metadataQuery := boostedMetadataQuery(text)
	var searchQuery query.Query = metadataQuery
	if len(namespaces) > 0 {
		filters := make([]query.Query, 0, len(namespaces))
		for _, namespace := range namespaces {
			filter := bleve.NewTermQuery(namespace)
			filter.SetField("namespace_id")
			// namespace_id is a filter, not a relevance signal. A zero boost
			// prevents namespace size/IDF from distorting metadata BM25 scores.
			filter.SetBoost(0)
			filters = append(filters, filter)
		}
		var namespaceFilter query.Query = filters[0]
		if len(filters) > 1 {
			namespaceFilter = bleve.NewDisjunctionQuery(filters...)
		}
		searchQuery = bleve.NewConjunctionQuery(metadataQuery, namespaceFilter)
	}
	hits, err := searchIndex(ctx, r.index, searchQuery, limit)
	if err != nil {
		return nil, err
	}
	matches := make([]tools.ToolMatch, 0, len(hits))
	for _, hit := range hits {
		match, ok := r.metadata[hit.id]
		if !ok {
			continue
		}
		match.Score = hit.score
		matches = append(matches, match)
	}
	return matches, nil
}

func boostedMetadataQuery(text string) query.Query {
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
	return bleve.NewDisjunctionQuery(nameQuery, namespaceQuery, keywordsQuery, descriptionQuery)
}

func searchIndex(ctx context.Context, idx bleve.Index, searchQuery query.Query, limit int) ([]scoredHit, error) {
	request := bleve.NewSearchRequestOptions(searchQuery, limit, 0, false)
	result, err := idx.SearchInContext(ctx, request)
	if err != nil {
		return nil, err
	}
	hits := make([]scoredHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, scoredHit{id: hit.ID, score: hit.Score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].id < hits[j].id
		}
		return hits[i].score > hits[j].score
	})
	return hits, nil
}

func fuseRankings(global, scoped []tools.ToolMatch, limit int) []tools.ToolMatch {
	type fusedMatch struct {
		match tools.ToolMatch
		score float64
	}
	fused := make(map[string]fusedMatch, len(global)+len(scoped))
	add := func(matches []tools.ToolMatch, weight float64) {
		for rank, match := range matches {
			entry := fused[match.ID]
			if entry.match.ID == "" {
				entry.match = match
			}
			entry.score += weight / (rrfConstant + float64(rank+1))
			fused[match.ID] = entry
		}
	}
	add(global, globalRRFWeight)
	add(scoped, namespaceRRFWeight)

	matches := make([]tools.ToolMatch, 0, len(fused))
	for _, entry := range fused {
		entry.match.Score = entry.score
		matches = append(matches, entry.match)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Score > matches[j].Score
	})
	// Routing consumers expose the first five candidates. Keep the strongest
	// global hit inside that window even when many scoped-only hits receive the
	// namespace weight, preserving the global search as a real rescue path.
	if len(global) > 0 && len(matches) > 0 && limit > 0 {
		window := min(globalRescueWindow, limit, len(matches))
		globalTop := global[0].ID
		for i := window; i < len(matches); i++ {
			if matches[i].ID != globalTop {
				continue
			}
			rescued := matches[i]
			copy(matches[window:i+1], matches[window-1:i])
			matches[window-1] = rescued
			break
		}
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// Refresh atomically replaces both in-memory indexes from the current registry.
func (r *Bleve) Refresh(catalog []tools.ToolDescriptor) error {
	if r == nil {
		return errors.New("tool router: unavailable")
	}
	fresh := newWithFactory(catalog, r.factory)
	if fresh.initErr != nil {
		return fresh.initErr
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = fresh.Close()
		return errors.New("tool router: closed")
	}
	oldIndex, oldNamespaceIndex := r.index, r.namespaceIndex
	r.index, r.namespaceIndex = fresh.index, fresh.namespaceIndex
	r.metadata = fresh.metadata
	r.count = fresh.count
	r.namespaceCount = fresh.namespaceCount
	r.initErr = nil
	fresh.index, fresh.namespaceIndex = nil, nil
	r.mu.Unlock()
	// The new pair is already committed. Old-index cleanup failure cannot be
	// reported as a refresh failure because callers would retain an old registry
	// while this router serves the new catalog.
	_ = closeIndexes(oldIndex, oldNamespaceIndex)
	return nil
}

// Close releases both memory indexes. It is safe to call more than once.
func (r *Bleve) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	toolIndex, namespaceIndex := r.index, r.namespaceIndex
	r.index, r.namespaceIndex = nil, nil
	r.mu.Unlock()
	return closeIndexes(toolIndex, namespaceIndex)
}

func closeIndexes(indexes ...bleve.Index) error {
	var errs []error
	for _, idx := range indexes {
		if idx != nil {
			if err := idx.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func normalizeIdentifier(value string) string {
	return strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(value)
}

var _ tools.Router = (*Bleve)(nil)
var _ tools.RefreshableRouter = (*Bleve)(nil)
