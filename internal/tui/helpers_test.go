package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(context.Background(), nil, nil, nil, search.NewIndex(), Options{})
	t.Cleanup(m.requests.cancel)
	m.libraries = []domain.Library{{ID: "a", Name: "A", Type: "movie"}, {ID: "b", Name: "B", Type: "movie"}}
	m.libraryColumn().SetItems(domain.LibraryItems(m.allLibraryEntries()))
	return m
}

func snapshot(r catalog.Resource, revision uint64, ids ...string) catalog.Snapshot {
	result := catalog.Snapshot{Resource: r, Revision: revision, Validated: true, CachedList: domain.CachedList{FetchedAt: time.Now()}}
	for _, id := range ids {
		result.Items = append(result.Items, &domain.MediaItem{ID: id, Title: id})
	}
	return result
}

// state is a published collection state holding a validated snapshot.
func state(r catalog.Resource, revision uint64, ids ...string) catalog.State {
	return catalog.State{Resource: r, Known: true, Snapshot: snapshot(r, revision, ids...)}
}

func updateModel(m *Model, msg tea.Msg) *Model { m.Update(msg); return m }

func publish(m *Model, states ...catalog.State) *Model {
	return updateModel(m, statesMsg(states))
}
