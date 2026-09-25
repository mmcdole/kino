package search

import (
	"context"
	"strings"
	"sync"

	"github.com/mmcdole/kino/internal/domain"
)

// Entry is one searchable title in the index.
type Entry struct {
	Item      domain.ListItem // *MediaItem or *Show
	Title     string
	Type      domain.MediaType
	LibraryID string
}

// Result is an entry that matched a query, with match metadata.
type Result struct {
	Entry
	MatchedIndexes []int
	Score          int
}

type indexedLibrary struct {
	revision uint64
	items    []Entry
	titles   []string
}

// Index contains detached immutable snapshots. Updating it and running fuzzy
// queries happen in commands; typing never reads or decodes the disk cache.
type Index struct {
	mu        sync.RWMutex
	libraries map[string]indexedLibrary
}

func NewIndex() *Index { return &Index{libraries: make(map[string]indexedLibrary)} }

func (s *Index) ReplaceLibrary(id string, revision uint64, items []domain.ListItem) {
	s.mu.RLock()
	old, exists := s.libraries[id]
	s.mu.RUnlock()
	if exists && old.revision >= revision {
		return
	}
	entry := indexedLibrary{revision: revision}
	for _, item := range domain.CloneItems(items) {
		var kind domain.MediaType
		switch v := item.(type) {
		case *domain.MediaItem:
			kind = v.Type
		case *domain.Show:
			kind = domain.MediaTypeShow
		default:
			continue
		}
		entry.items = append(entry.items, Entry{Item: item, Title: item.GetTitle(), Type: kind, LibraryID: id})
		entry.titles = append(entry.titles, strings.ToLower(item.GetTitle()))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.libraries[id]; ok && old.revision >= revision {
		return
	}
	s.libraries[id] = entry
}

func (s *Index) Search(ctx context.Context, query string, libraries []domain.Library) []Result {
	if query == "" || ctx.Err() != nil {
		return nil
	}
	var items []Entry
	var titles []string
	s.mu.RLock()
	for _, lib := range libraries {
		entry := s.libraries[lib.ID]
		items = append(items, entry.items...)
		titles = append(titles, entry.titles...)
	}
	s.mu.RUnlock()
	matches := FuzzySearch(query, titles)
	results := make([]Result, 0, len(matches))
	for _, match := range matches {
		if ctx.Err() != nil {
			return nil
		}
		item := items[match.Index]
		item.Item = domain.CloneItems([]domain.ListItem{item.Item})[0]
		results = append(results, Result{Entry: item, MatchedIndexes: match.MatchedIndexes, Score: match.Score})
	}
	return results
}
