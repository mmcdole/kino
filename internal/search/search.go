package search

import (
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
	store domain.Store
}

// NewService creates a new search service
func NewService(store domain.Store) *Service {
	return &Service{
		store: store,
	}
}

// FilterLocal searches cached data directly
func (s *Service) FilterLocal(query string, libraries []domain.Library) []FilterResult {
	if query == "" {
		return nil
	}

	var items []FilterItem
	for _, lib := range libraries {
		items = append(items, s.gatherLibraryItems(lib)...)
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

func (s *Service) gatherLibraryItems(lib domain.Library) []FilterItem {
	var items []FilterItem

	switch lib.Type {
	case "movie":
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
	case "show":
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
	case "mixed":
		if content, ok := s.store.GetMixedContent(lib.ID); ok {
			for _, item := range content {
				switch v := item.(type) {
				case *domain.MediaItem:
					items = append(items, FilterItem{
						Item:      v,
						Title:     v.Title,
						Type:      domain.MediaTypeMovie,
						LibraryID: lib.ID,
					})
				case *domain.Show:
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

	return items
}
