package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/lithammer/fuzzysearch/fuzzy"
)

// NavigationContext contains information needed to navigate to an item
type NavigationContext struct {
	LibraryID   string
	LibraryName string
	MovieID     string // For movie deep links
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int
	EpisodeID   string // For episode deep links
}

// FilterItem represents an item in the global filter index
type FilterItem struct {
	Item       domain.ListItem   // *domain.MediaItem or *domain.Show
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
		filterIndex:   &FilterIndex{},
		filterIndexed: make(map[string]bool),
	}
}

// Search performs a fuzzy search across all libraries
func (s *SearchService) Search(ctx context.Context, query string) ([]*domain.MediaItem, error) {
	if query == "" {
		return nil, nil
	}

	s.logger.Debug("searching", "query", query)

	// Perform server-side search
	results, err := s.repo.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	// Apply local fuzzy ranking to server results
	ranked := s.rankResults(results, query)
	s.logger.Debug("search complete", "query", query, "results", len(ranked))

	return ranked, nil
}

// rankResults applies fuzzy ranking to search results
func (s *SearchService) rankResults(items []*domain.MediaItem, query string) []*domain.MediaItem {
	if len(items) == 0 {
		return items
	}

	query = strings.ToLower(query)

	type rankedItem struct {
		item  *domain.MediaItem
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
	results := make([]*domain.MediaItem, len(ranked))
	for i, r := range ranked {
		results[i] = r.item
	}

	return results
}

// calculateMatchScore calculates a match score for ranking
// Lower score = better match
func calculateMatchScore(title, query string, item *domain.MediaItem) int {
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

// IndexForFilter adds items to the global filter index, deduplicating by item ID
// Pre-computes lowercase titles at index time for zero-allocation search
func (s *SearchService) IndexForFilter(items []FilterItem) {
	s.filterMu.Lock()
	defer s.filterMu.Unlock()

	added := 0
	for _, item := range items {
		// Generate unique key based on item type and ID
		key := item.Item.GetItemType() + ":" + item.Item.GetID()

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
