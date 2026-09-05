package search

import "github.com/mmcdole/kino/internal/domain"

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
