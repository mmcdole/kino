package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/mmcdole/kino/internal/domain"
)

// FilterItem represents a searchable item
type FilterItem struct {
	Item      domain.ListItem // *MediaItem or *Show
	Title     string
	Type      domain.MediaType
	LibraryID string
}

// FilterResult represents a search result with match metadata
type FilterResult struct {
	FilterItem
	MatchedIndexes []int
	Score          int
}

// SearchService handles fuzzy search across libraries
type SearchService struct {
	repo   domain.SearchRepository
	store  domain.LibraryStore
	logger *slog.Logger
}

// NewSearchService creates a new search service
func NewSearchService(repo domain.SearchRepository, store domain.LibraryStore, logger *slog.Logger) *SearchService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SearchService{
		repo:   repo,
		store:  store,
		logger: logger,
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

	// Boost movies over episodes for single-word queries
	if len(strings.Fields(query)) == 1 && item.Type == domain.MediaTypeMovie {
		score -= 10
	}

	return score
}

// FilterLocal searches cached data directly
// types: filter by media types (nil = all types)
// libraries: which libraries to search
func (s *SearchService) FilterLocal(query string, types []domain.MediaType, libraries []domain.Library) []FilterResult {
	if query == "" {
		return nil
	}

	var items []FilterItem
	typeSet := makeTypeSet(types)

	for _, lib := range libraries {
		items = append(items, s.gatherLibraryItems(lib, typeSet)...)
	}

	if len(items) == 0 {
		return nil
	}

	// Build lowercase titles for fuzzy search
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = strings.ToLower(item.Title)
	}

	matches := FuzzySearch(query, titles)

	results := make([]FilterResult, len(matches))
	for i, match := range matches {
		results[i] = FilterResult{
			FilterItem:     items[match.Index],
			MatchedIndexes: match.MatchedIndexes,
			Score:          match.Score,
		}
	}

	return results
}

func (s *SearchService) gatherLibraryItems(lib domain.Library, types map[domain.MediaType]bool) []FilterItem {
	var items []FilterItem

	isTypeAllowed := func(t domain.MediaType) bool {
		return len(types) == 0 || types[t]
	}

	switch lib.Type {
	case "movie":
		if isTypeAllowed(domain.MediaTypeMovie) {
			if movies, ok := s.store.GetMovies(lib.ID); ok {
				for _, m := range movies {
					items = append(items, FilterItem{
						Item:      m,
						Title:     m.Title,
						Type:      domain.MediaTypeMovie,
						LibraryID: lib.ID,
					})
				}
			}
		}
	case "show":
		if isTypeAllowed(domain.MediaTypeShow) {
			if shows, ok := s.store.GetShows(lib.ID); ok {
				for _, sh := range shows {
					items = append(items, FilterItem{
						Item:      sh,
						Title:     sh.Title,
						Type:      domain.MediaTypeShow,
						LibraryID: lib.ID,
					})
				}
			}
		}
	case "mixed":
		if content, ok := s.store.GetMixedContent(lib.ID); ok {
			for _, item := range content {
				switch v := item.(type) {
				case *domain.MediaItem:
					if isTypeAllowed(domain.MediaTypeMovie) {
						items = append(items, FilterItem{
							Item:      v,
							Title:     v.Title,
							Type:      domain.MediaTypeMovie,
							LibraryID: lib.ID,
						})
					}
				case *domain.Show:
					if isTypeAllowed(domain.MediaTypeShow) {
						items = append(items, FilterItem{
							Item:      v,
							Title:     v.Title,
							Type:      domain.MediaTypeShow,
							LibraryID: lib.ID,
						})
					}
				}
			}
		}
	}

	return items
}

func makeTypeSet(types []domain.MediaType) map[domain.MediaType]bool {
	if len(types) == 0 {
		return nil
	}
	set := make(map[domain.MediaType]bool)
	for _, t := range types {
		set[t] = true
	}
	return set
}
