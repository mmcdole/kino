package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/components"
)

// A collection owns the last accepted snapshot. RequiredRevision is a fence,
// not evidence that this model has received the corresponding content.
// Requests are separate: several subscribers may await the same collection.
type collectionState struct {
	Resource          catalog.Resource
	Snapshot          catalog.Snapshot
	Known             bool
	RequiredRevision  uint64
	Error             error
	BackgroundRequest uint64
}

func (s *collectionState) accepts(snapshot catalog.Snapshot) bool {
	return snapshot.Revision >= s.RequiredRevision
}

func (m *Model) collection(r catalog.Resource) *collectionState {
	state := m.collections[r.Key()]
	if state == nil {
		state = &collectionState{Resource: r}
		m.collections[r.Key()] = state
	}
	return state
}

func (m Model) resource(key string) (catalog.Resource, bool) {
	state, ok := m.collections[key]
	if !ok {
		return catalog.Resource{}, false
	}
	return state.Resource, true
}

// applySnapshot is shared by loads and mutation reconciliation. Equal revisions
// can update freshness feedback but never repeat content or index preparation.
func (m *Model) applySnapshot(snapshot catalog.Snapshot) tea.Cmd {
	r := snapshot.Resource
	state := m.collection(r)
	if !state.accepts(snapshot) {
		return nil
	}
	changed := !state.Known || snapshot.Revision != state.Snapshot.Revision
	state.RequiredRevision = max(state.RequiredRevision, snapshot.Revision)
	state.Known = true
	if !changed {
		state.Snapshot.Stale = snapshot.Stale
		return nil
	}
	state.Snapshot = snapshot
	if r.Kind == catalog.Libraries {
		m.Libraries = nil
		for _, item := range snapshot.Items {
			if lib, ok := item.(*domain.Library); ok {
				m.Libraries = append(m.Libraries, *lib)
			}
		}
		m.libraryColumn().ReplaceItems(components.WrapLibraries(m.allLibraryEntries()))
	} else {
		for i := 0; i < m.ColumnStack.Len(); i++ {
			if col := m.ColumnStack.Get(i); col.ContentID() == r.Key() {
				col.ReplaceItems(domain.CloneItems(snapshot.Items))
			}
		}
	}
	if r.Kind == catalog.Movies || r.Kind == catalog.Shows || r.Kind == catalog.Mixed {
		index := m.SearchIndex
		return func() tea.Msg {
			index.ReplaceLibrary(r.LibraryID, snapshot.Revision, snapshot.Items)
			return SearchIndexChangedMsg{}
		}
	}
	return nil
}
