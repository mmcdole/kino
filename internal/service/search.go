package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/drake/goplex/internal/domain"
	"github.com/lithammer/fuzzysearch/fuzzy"
)

// NavigationContext contains information needed to navigate to an item
type NavigationContext struct {
	LibraryID   string
	LibraryName string
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int
	ItemIndex   int // Cursor position in the list
}

// FilterItem represents an item in the global filter index
type FilterItem struct {
	Item       interface{}       // domain.MediaItem or domain.Show
	Title      string            // Searchable title (display title)
	Type       domain.MediaType
	NavContext NavigationContext
}

// FilterResult represents a search result with match metadata for highlighting
type FilterResult struct {
	FilterItem
	MatchedIndexes []int // Character positions that matched
	Score          int   // Match score (lower is better)
}

// FilterIndex implements sahilm/fuzzy.Source for zero-allocation fuzzy matching
type FilterIndex struct {
	items       []FilterItem
	lowerTitles []string // Pre-computed lowercase titles
}

// String returns the lowercase title at index i (implements fuzzy.Source)
func (idx *FilterIndex) String(i int) string { return idx.lowerTitles[i] }

// Len returns the number of items (implements fuzzy.Source)
func (idx *FilterIndex) Len() int { return len(idx.items) }

// SearchService handles fuzzy search across libraries
type SearchService struct {
	repo   domain.SearchRepository
	logger *slog.Logger

	// Local index for faster fuzzy matching
	indexMu    sync.RWMutex
	titleIndex map[string]domain.MediaItem // title -> item
	indexed    bool

	// Filter index for global filter feature (using FilterIndex for zero-allocation search)
	filterMu      sync.RWMutex
	filterIndex   *FilterIndex
	filterIndexed map[string]bool // Track indexed item IDs to avoid duplicates
}

// NewSearchService creates a new search service
func NewSearchService(repo domain.SearchRepository, logger *slog.Logger) *SearchService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SearchService{
		repo:          repo,
		logger:        logger,
		titleIndex:    make(map[string]domain.MediaItem),
		filterIndex:   &FilterIndex{},
		filterIndexed: make(map[string]bool),
	}
}

// Search performs a fuzzy search across all libraries
func (s *SearchService) Search(ctx context.Context, query string) ([]domain.MediaItem, error) {
	if query == "" {
		return nil, nil
	}

	s.logger.Debug("searching", "query", query)

	// First try server-side search
	results, err := s.repo.Search(ctx, query)
	if err != nil {
		s.logger.Warn("server search failed, falling back to local", "error", err)
		// Fall back to local fuzzy search if indexed
		return s.localSearch(query), nil
	}

	// Apply local fuzzy ranking to server results
	ranked := s.rankResults(results, query)
	s.logger.Debug("search complete", "query", query, "results", len(ranked))

	return ranked, nil
}

// SearchLocal performs fuzzy search against the local index only
func (s *SearchService) SearchLocal(query string) []domain.MediaItem {
	return s.localSearch(query)
}

// IndexItems adds items to the local search index
func (s *SearchService) IndexItems(items []domain.MediaItem) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for _, item := range items {
		// Index by lowercase title for case-insensitive matching
		key := strings.ToLower(item.Title)
		s.titleIndex[key] = item
	}

	s.indexed = true
	s.logger.Debug("indexed items", "count", len(items), "total", len(s.titleIndex))
}

// ClearIndex removes all items from the local index
func (s *SearchService) ClearIndex() {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	s.titleIndex = make(map[string]domain.MediaItem)
	s.indexed = false
	s.logger.Debug("cleared search index")
}

// localSearch performs fuzzy search against the local index
func (s *SearchService) localSearch(query string) []domain.MediaItem {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	if !s.indexed || len(s.titleIndex) == 0 {
		return nil
	}

	query = strings.ToLower(query)

	// Collect all titles for fuzzy matching
	titles := make([]string, 0, len(s.titleIndex))
	for title := range s.titleIndex {
		titles = append(titles, title)
	}

	// Perform fuzzy search
	matches := fuzzy.RankFindFold(query, titles)

	// Sort by score (lower is better)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Distance < matches[j].Distance
	})

	// Convert back to items
	results := make([]domain.MediaItem, 0, len(matches))
	for _, match := range matches {
		if item, ok := s.titleIndex[match.Target]; ok {
			results = append(results, item)
		}
	}

	return results
}

// rankResults applies fuzzy ranking to search results
func (s *SearchService) rankResults(items []domain.MediaItem, query string) []domain.MediaItem {
	if len(items) == 0 {
		return items
	}

	query = strings.ToLower(query)

	type rankedItem struct {
		item  domain.MediaItem
		score int
	}

	ranked := make([]rankedItem, 0, len(items))

	for _, item := range items {
		title := strings.ToLower(item.Title)
		score := calculateMatchScore(title, query, item)
		ranked = append(ranked, rankedItem{item: item, score: score})
	}

	// Sort by score (lower is better)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score < ranked[j].score
	})

	// Extract sorted items
	results := make([]domain.MediaItem, len(ranked))
	for i, r := range ranked {
		results[i] = r.item
	}

	return results
}

// calculateMatchScore calculates a match score for ranking
// Lower score = better match
func calculateMatchScore(title, query string, item domain.MediaItem) int {
	score := 0

	// Exact match is best
	if title == query {
		return 0
	}

	// Prefix match is very good
	if strings.HasPrefix(title, query) {
		return 10
	}

	// Contains match is good
	if strings.Contains(title, query) {
		return 50
	}

	// Fuzzy distance
	distance := fuzzy.LevenshteinDistance(query, title)
	score = 100 + distance

	// Boost recently added items
	// (This would require addedAt timestamp)

	// Boost movies over episodes for single-word queries
	if len(strings.Fields(query)) == 1 && item.Type == domain.MediaTypeMovie {
		score -= 10
	}

	return score
}

// FilterByType filters search results by media type
func FilterByType(items []domain.MediaItem, mediaType domain.MediaType) []domain.MediaItem {
	filtered := make([]domain.MediaItem, 0)
	for _, item := range items {
		if item.Type == mediaType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// FilterByLibrary filters search results by library ID
func FilterByLibrary(items []domain.MediaItem, libID string) []domain.MediaItem {
	filtered := make([]domain.MediaItem, 0)
	for _, item := range items {
		if item.LibraryID == libID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// FilterUnwatched filters to only unwatched items
func FilterUnwatched(items []domain.MediaItem) []domain.MediaItem {
	filtered := make([]domain.MediaItem, 0)
	for _, item := range items {
		if !item.IsPlayed {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// IndexForFilter adds items to the global filter index, deduplicating by item ID
// Pre-computes lowercase titles at index time for zero-allocation search
func (s *SearchService) IndexForFilter(items []FilterItem) {
	s.filterMu.Lock()
	defer s.filterMu.Unlock()

	added := 0
	for _, item := range items {
		// Generate unique key based on item type and ID
		var key string
		switch v := item.Item.(type) {
		case domain.MediaItem:
			key = "media:" + v.ID
		case *domain.MediaItem:
			key = "media:" + v.ID
		case domain.Show:
			key = "show:" + v.ID
		case *domain.Show:
			key = "show:" + v.ID
		default:
			key = item.Title // fallback
		}

		// Skip if already indexed
		if s.filterIndexed[key] {
			continue
		}

		s.filterIndexed[key] = true
		s.filterIndex.items = append(s.filterIndex.items, item)
		s.filterIndex.lowerTitles = append(s.filterIndex.lowerTitles, strings.ToLower(item.Title))
		added++
	}

	s.logger.Debug("indexed items for filter", "added", added, "skipped", len(items)-added, "total", len(s.filterIndex.items))
}

// FilterLocal performs fuzzy search against the filter index using custom fuzzy matching
// Returns FilterResult with match metadata for highlighting
func (s *SearchService) FilterLocal(query string) []FilterResult {
	s.filterMu.RLock()
	defer s.filterMu.RUnlock()

	if query == "" || s.filterIndex.Len() == 0 {
		return nil
	}

	// Use our custom fuzzy search (already returns sorted results)
	matches := FuzzySearch(query, s.filterIndex.lowerTitles)

	// Convert to FilterResult
	results := make([]FilterResult, len(matches))
	for i, match := range matches {
		results[i] = FilterResult{
			FilterItem:     s.filterIndex.items[match.Index],
			MatchedIndexes: match.MatchedIndexes,
			Score:          match.Score,
		}
	}

	return results
}

// ClearFilterIndex removes all items from the filter index
func (s *SearchService) ClearFilterIndex() {
	s.filterMu.Lock()
	defer s.filterMu.Unlock()

	s.filterIndex = &FilterIndex{}
	s.filterIndexed = make(map[string]bool)
	s.logger.Debug("cleared filter index")
}

// FilterIndexCount returns the number of items in the filter index
func (s *SearchService) FilterIndexCount() int {
	s.filterMu.RLock()
	defer s.filterMu.RUnlock()
	return s.filterIndex.Len()
}
