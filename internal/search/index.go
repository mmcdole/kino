package search

import (
	"context"
	"strings"
	"sync"

	"github.com/mmcdole/kino/internal/domain"
)

type indexedLibrary struct {
	revision uint64
	items    []FilterItem
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
		entry.items = append(entry.items, FilterItem{Item: item, Title: item.GetTitle(), Type: kind, LibraryID: id})
		entry.titles = append(entry.titles, strings.ToLower(item.GetTitle()))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.libraries[id]; ok && old.revision >= revision {
		return
	}
	s.libraries[id] = entry
}

func (s *Index) Search(ctx context.Context, query string, libraries []domain.Library) []FilterResult {
	if query == "" || ctx.Err() != nil {
		return nil
	}
	var items []FilterItem
	var titles []string
	s.mu.RLock()
	for _, lib := range libraries {
		entry := s.libraries[lib.ID]
		items = append(items, entry.items...)
		titles = append(titles, entry.titles...)
	}
	s.mu.RUnlock()
	matches := FuzzySearch(query, titles)
	results := make([]FilterResult, 0, len(matches))
	for _, match := range matches {
		if ctx.Err() != nil {
			return nil
		}
		item := items[match.Index]
		item.Item = domain.CloneItems([]domain.ListItem{item.Item})[0]
		results = append(results, FilterResult{FilterItem: item, MatchedIndexes: match.MatchedIndexes, Score: match.Score})
	}
	return results
}
