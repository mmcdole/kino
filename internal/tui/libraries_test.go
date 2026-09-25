package tui

import (
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

func TestRemovedLibraryDetachesRequestsAndNavigation(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	m.loadResource(r, catalog.Revalidate, true)
	late := m.requests.active[syncOwner(r)]
	root := catalog.Resource{Kind: catalog.Libraries}
	m = publish(m, catalog.State{Resource: root, Known: true, Snapshot: snapshot(root, 1)})
	if m.columns.Len() != 1 {
		t.Fatal("removed library remains open")
	}
	if _, ok := m.libraryStates[r.LibraryID]; ok {
		t.Fatal("removed library remains in sync status")
	}
	if m.requests.owns(late) || late.ctx.Err() == nil {
		t.Fatal("removed subscription remains active")
	}
	m = publish(m, state(r, 2, "late"))
	m = updateModel(m, loadDoneMsg{Request: late})
	if _, ok := m.libraryStates[r.LibraryID]; ok {
		t.Fatal("late state recreated removed library")
	}
	if _, ok := m.collections[r.Key()]; ok {
		t.Fatal("late state recreated removed collection")
	}
}

func TestAllLibraryEntriesDoesNotAliasLibraries(t *testing.T) {
	m := &Model{libraries: make([]domain.Library, 1, 2)}
	m.libraries[0] = domain.Library{ID: "movies"}

	entries := m.allLibraryEntries()
	entries[0].Name = "changed"
	if m.libraries[0].Name != "" {
		t.Fatal("allLibraryEntries shares its backing array with m.Libraries")
	}
	if got := m.libraries[:2][1].ID; got != "" {
		t.Fatalf("Playlists entry written into m.Libraries spare capacity: %q", got)
	}
}
