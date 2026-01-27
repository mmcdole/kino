package search

import (
	"log/slog"
	"strings"

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

// Service handles fuzzy search across libraries
type Service struct {
	queries domain.LibraryQueries
	logger  *slog.Logger
}

// NewService creates a new search service
func NewService(queries domain.LibraryQueries, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		queries: queries,
		logger:  logger,
	}
}

// FilterLocal searches cached data directly
// types: filter by media types (nil = all types)
// libraries: which libraries to search
func (s *Service) FilterLocal(query string, types []domain.MediaType, libraries []domain.Library) []FilterResult {
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

func (s *Service) gatherLibraryItems(lib domain.Library, types map[domain.MediaType]bool) []FilterItem {
	var items []FilterItem

	isTypeAllowed := func(t domain.MediaType) bool {
		return len(types) == 0 || types[t]
	}

	switch lib.Type {
	case "movie":
		if isTypeAllowed(domain.MediaTypeMovie) {
			if movies, ok := s.queries.GetCachedMovies(lib.ID); ok {
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
			if shows, ok := s.queries.GetCachedShows(lib.ID); ok {
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
		if content, ok := s.queries.GetCachedMixedContent(lib.ID); ok {
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
