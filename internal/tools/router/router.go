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

	"github.com/elmissouri16/snow-core/internal/tools"
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

type routeDescriptor struct {
	name        string
	original    string
	description string
	namespace   string
	keywords    []string
}

type scoredHit struct {
	id    string
	score float64
}

type indexFactory func(blevemapping.IndexMapping) (bleve.Index, error)

type indexInitialization struct {
	done       chan struct{}
	generation uint64
	once       sync.Once
}

func (i *indexInitialization) finish() {
	if i != nil {
		i.once.Do(func() { close(i.done) })
	}
}

// Bleve is an in-memory, namespace-first BM25 router. Initialization failures
// are retained so the agent can use its bounded metadata fallback on each turn
// instead of making application startup depend on discovery infrastructure.
type Bleve struct {
	mu                sync.RWMutex
	refreshMu         sync.Mutex
	index             bleve.Index
	namespaceIndex    bleve.Index
	metadata          map[string]tools.ToolMatch
	catalog           []routeDescriptor
	count             int
	namespaceCount    int
	init              *indexInitialization
	catalogGeneration uint64
	initErr           error
	closed            bool
	factory           indexFactory
}

// New retains compact deferred metadata. Bleve indexes are built lazily by the
// first real search so startup does not pay indexing cost merely because a
// deferred tool exists.
func New(catalog []tools.ToolDescriptor) *Bleve {
	metadata := make([]tools.DescriptorMetadata, 0, len(catalog))
	for _, desc := range catalog {
		metadata = append(metadata, tools.MetadataFromDescriptor(desc))
	}
	return NewMetadata(metadata)
}

// NewMetadata avoids cloning parameter schemas when the caller has a projected
// registry view.
func NewMetadata(catalog []tools.DescriptorMetadata) *Bleve {
	return newWithMetadataFactory(catalog, bleve.NewMemOnly)
}

func newWithFactory(catalog []tools.ToolDescriptor, factory indexFactory) *Bleve {
	metadata := make([]tools.DescriptorMetadata, 0, len(catalog))
	for _, desc := range catalog {
		metadata = append(metadata, tools.MetadataFromDescriptor(desc))
	}
	return newWithMetadataFactory(metadata, factory)
}

func newWithMetadataFactory(catalog []tools.DescriptorMetadata, factory indexFactory) *Bleve {
	if factory == nil {
		factory = bleve.NewMemOnly
	}
	deferred := deferredCatalog(catalog)
	return &Bleve{catalog: deferred, count: len(deferred), factory: factory}
}

func deferredCatalog(catalog []tools.DescriptorMetadata) []routeDescriptor {
	deferred := make([]routeDescriptor, 0, len(catalog))
	for _, desc := range catalog {
		if !desc.Deferred {
			continue
		}
		deferred = append(deferred, routeDescriptor{
			name: desc.Name, original: desc.OriginalName, description: desc.Description,
			namespace: desc.Namespace, keywords: append([]string(nil), desc.Keywords...),
		})
	}
	sort.SliceStable(deferred, func(i, j int) bool {
		return deferred[i].name < deferred[j].name
	})
	return deferred
}

func buildIndexes(catalog []routeDescriptor, factory indexFactory) (bleve.Index, bleve.Index, map[string]tools.ToolMatch, int, error) {
	return buildIndexesContext(context.Background(), catalog, factory)
}

func buildIndexesContext(ctx context.Context, catalog []routeDescriptor, factory indexFactory) (bleve.Index, bleve.Index, map[string]tools.ToolMatch, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, 0, err
	}
	metadata := make(map[string]tools.ToolMatch, len(catalog))
	idx, err := factory(newIndexMapping())
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("tool router: create tool index: %w", err)
	}
	toolBatch := idx.NewBatch()
	for _, desc := range catalog {
		if err := ctx.Err(); err != nil {
			_ = idx.Close()
			return nil, nil, nil, 0, err
		}
		name := strings.TrimSpace(strings.Join([]string{desc.name, desc.original, normalizeIdentifier(desc.name), normalizeIdentifier(desc.original)}, " "))
		doc := indexDocument{NamespaceID: desc.namespace, Namespace: desc.namespace, Name: name, Description: desc.description, Keywords: append([]string(nil), desc.keywords...)}
		if err := toolBatch.Index(desc.name, doc); err != nil {
			_ = idx.Close()
			return nil, nil, nil, 0, fmt.Errorf("tool router: index %s: %w", desc.name, err)
		}
		metadata[desc.name] = tools.ToolMatch{ID: desc.name, Namespace: desc.namespace, Name: desc.original, Description: desc.description}
	}
	if err := ctx.Err(); err != nil {
		_ = idx.Close()
		return nil, nil, nil, 0, err
	}
	if err := idx.Batch(toolBatch); err != nil {
		_ = idx.Close()
		return nil, nil, nil, 0, fmt.Errorf("tool router: commit tool index: %w", err)
	}
	namespaceDocs, err := buildNamespaceRouteDocumentsContext(ctx, catalog)
	if err != nil {
		_ = idx.Close()
		return nil, nil, nil, 0, err
	}
	namespaceIdx, err := factory(newIndexMapping())
	if err != nil {
		_ = idx.Close()
		return nil, nil, nil, 0, fmt.Errorf("tool router: create namespace index: %w", err)
	}
	namespaceBatch := namespaceIdx.NewBatch()
	namespaces := make([]string, 0, len(namespaceDocs))
	for namespace := range namespaceDocs {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		if err := ctx.Err(); err != nil {
			_ = closeIndexes(idx, namespaceIdx)
			return nil, nil, nil, 0, err
		}
		if err := namespaceBatch.Index(namespace, namespaceDocs[namespace]); err != nil {
			_ = closeIndexes(idx, namespaceIdx)
			return nil, nil, nil, 0, fmt.Errorf("tool router: index namespace %s: %w", namespace, err)
		}
	}
	if err := ctx.Err(); err != nil {
		_ = closeIndexes(idx, namespaceIdx)
		return nil, nil, nil, 0, err
	}
	if err := namespaceIdx.Batch(namespaceBatch); err != nil {
		_ = closeIndexes(idx, namespaceIdx)
		return nil, nil, nil, 0, fmt.Errorf("tool router: commit namespace index: %w", err)
	}
	return idx, namespaceIdx, metadata, len(namespaceDocs), nil
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
	metadata := make([]tools.DescriptorMetadata, 0, len(deferred))
	for _, desc := range deferred {
		metadata = append(metadata, tools.MetadataFromDescriptor(desc))
	}
	return buildNamespaceRouteDocuments(deferredCatalog(metadata))
}

func buildNamespaceRouteDocuments(deferred []routeDescriptor) map[string]indexDocument {
	documents, _ := buildNamespaceRouteDocumentsContext(context.Background(), deferred)
	return documents
}

func buildNamespaceRouteDocumentsContext(ctx context.Context, deferred []routeDescriptor) (map[string]indexDocument, error) {
	accumulators := make(map[string]*namespaceAccumulator)
	for _, desc := range deferred {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		namespace := strings.TrimSpace(desc.namespace)
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
			desc.name,
			desc.original,
			normalizeIdentifier(desc.name),
			normalizeIdentifier(desc.original),
		)
		acc.keywords = append(acc.keywords, desc.keywords...)
		acc.descriptions = append(acc.descriptions, desc.description)
	}

	documents := make(map[string]indexDocument, len(accumulators))
	for namespace, acc := range accumulators {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		names, err := uniqueSortedContext(ctx, acc.names)
		if err != nil {
			return nil, err
		}
		keywords, err := uniqueSortedContext(ctx, acc.keywords)
		if err != nil {
			return nil, err
		}
		descriptions, err := uniqueSortedContext(ctx, acc.descriptions)
		if err != nil {
			return nil, err
		}
		namespaceText, err := uniqueSortedContext(ctx, []string{namespace, normalizeIdentifier(namespace)})
		if err != nil {
			return nil, err
		}
		namespaceTextBudget := max(0, namespaceFieldMaxSize-len(namespace))
		documents[namespace] = indexDocument{
			NamespaceID: namespace,
			Namespace:   boundedJoin(namespaceText, namespaceTextBudget, false),
			Name:        boundedJoin(names, namespaceNamesMaxSize, false),
			Keywords: []string{boundedJoin(
				keywords, namespaceKeywordsMaxSize, false,
			)},
			Description: boundedJoin(descriptions, namespaceDescriptionsMaxSize, true),
		}
	}
	return documents, nil
}

func uniqueSortedContext(ctx context.Context, values []string) ([]string, error) {
	type sortableValue struct {
		value string
		key   string
	}
	items := make([]sortableValue, 0, len(values))
	for _, value := range values {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		items = append(items, sortableValue{value: value, key: strings.ToLower(value)})
	}
	less := func(left, right sortableValue) bool {
		if left.key == right.key {
			return left.value < right.value
		}
		return left.key < right.key
	}
	const sortChunk = 256
	for start := 0; start < len(items); start += sortChunk {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+sortChunk, len(items))
		sort.SliceStable(items[start:end], func(i, j int) bool {
			return less(items[start+i], items[start+j])
		})
	}
	buffer := make([]sortableValue, len(items))
	for width := sortChunk; width < len(items); width *= 2 {
		for start := 0; start < len(items); start += 2 * width {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			middle := min(start+width, len(items))
			end := min(start+2*width, len(items))
			left, right, output := start, middle, start
			for left < middle && right < end {
				if output%64 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				if less(items[right], items[left]) {
					buffer[output] = items[right]
					right++
				} else {
					buffer[output] = items[left]
					left++
				}
				output++
			}
			output += copy(buffer[output:end], items[left:middle])
			copy(buffer[output:end], items[right:end])
		}
		items, buffer = buffer, items
	}
	out := make([]string, 0, len(items))
	last := ""
	for i, item := range items {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if item.value == "" || (len(out) > 0 && item.key == last) {
			continue
		}
		out = append(out, item.value)
		last = item.key
	}
	return out, nil
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

func (r *Bleve) ensureInitialized(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return errors.New("tool router: closed")
		}
		if r.index != nil || r.count == 0 {
			r.mu.Unlock()
			return nil
		}
		if r.initErr != nil {
			err := r.initErr
			r.mu.Unlock()
			return err
		}
		generation := r.catalogGeneration
		if current := r.init; current != nil && current.generation == generation {
			done := current.done
			r.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		initialization := &indexInitialization{done: make(chan struct{}), generation: generation}
		r.init = initialization
		catalog := append([]routeDescriptor(nil), r.catalog...)
		factory := r.factory
		r.mu.Unlock()

		index, namespaceIndex, metadata, namespaceCount, err := buildIndexesContext(ctx, catalog, factory)
		r.mu.Lock()
		if r.init == initialization {
			r.init = nil
		}
		initialization.finish()
		if err != nil {
			stale := r.catalogGeneration != generation
			closed := r.closed
			if !stale && !closed && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				r.initErr = err
			}
			r.mu.Unlock()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if stale {
				continue
			}
			if closed {
				return errors.New("tool router: closed")
			}
			return err
		}
		if r.closed || r.catalogGeneration != generation || r.index != nil {
			closed := r.closed
			initialized := r.index != nil
			r.mu.Unlock()
			_ = closeIndexes(index, namespaceIndex)
			if closed {
				return errors.New("tool router: closed")
			}
			if initialized {
				return nil
			}
			continue
		}
		r.index = index
		r.namespaceIndex = namespaceIndex
		r.metadata = metadata
		r.namespaceCount = namespaceCount
		r.initErr = nil
		r.mu.Unlock()
		return nil
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

// Search selects likely namespaces, runs global and namespace-scoped BM25 tool
// searches, and fuses both ranked lists. A namespace-only failure degrades to
// the original global ranking; a tool-index failure remains a total failure so
// the agent can invoke its bounded schema-free metadata fallback.
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
	if err := r.ensureInitialized(ctx); err != nil {
		return nil, err
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

// Refresh atomically replaces deferred metadata. An uninitialized router stays
// lazy; once searches have begun, refreshed indexes are built before swapping.
func (r *Bleve) Refresh(catalog []tools.ToolDescriptor) error {
	metadata := make([]tools.DescriptorMetadata, 0, len(catalog))
	for _, desc := range catalog {
		metadata = append(metadata, tools.MetadataFromDescriptor(desc))
	}
	return r.RefreshMetadata(metadata)
}

// RefreshMetadata replaces the compact catalog without retaining schemas.
func (r *Bleve) RefreshMetadata(catalog []tools.DescriptorMetadata) error {
	if r == nil {
		return errors.New("tool router: unavailable")
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	deferred := deferredCatalog(catalog)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("tool router: closed")
	}
	initialized := r.index != nil || r.initErr != nil
	factory := r.factory
	if !initialized {
		initialization := r.init
		r.init = nil
		initialization.finish()
		r.catalog = deferred
		r.catalogGeneration++
		r.count = len(deferred)
		r.namespaceCount = 0
		r.metadata = nil
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	var index, namespaceIndex bleve.Index
	var metadata map[string]tools.ToolMatch
	var namespaceCount int
	var err error
	if len(deferred) > 0 {
		index, namespaceIndex, metadata, namespaceCount, err = buildIndexes(deferred, factory)
		if err != nil {
			return err
		}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = closeIndexes(index, namespaceIndex)
		return errors.New("tool router: closed")
	}
	oldIndex, oldNamespaceIndex := r.index, r.namespaceIndex
	r.catalogGeneration++
	r.index, r.namespaceIndex = index, namespaceIndex
	r.metadata = metadata
	r.catalog = deferred
	r.count = len(deferred)
	r.namespaceCount = namespaceCount
	r.initErr = nil
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
	initialization := r.init
	r.init = nil
	initialization.finish()
	toolIndex, namespaceIndex := r.index, r.namespaceIndex
	r.index, r.namespaceIndex = nil, nil
	r.catalog = nil
	r.metadata = nil
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
