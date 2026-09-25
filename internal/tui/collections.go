package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/components"
)

// Collections hold the catalog's latest published state. The catalog decides
// ordering and freshness; the model only displays the newest state it has.

func (m *Model) collection(r catalog.Resource) catalog.State {
	return m.collections[r.Key()]
}

// track records a collection the model is about to request, so its feedback
// and navigation can resolve it before the catalog publishes anything.
func (m *Model) track(r catalog.Resource) {
	st := m.collections[r.Key()]
	st.Resource = r
	m.collections[r.Key()] = st
}

func (m Model) resource(key string) (catalog.Resource, bool) {
	st, ok := m.collections[key]
	return st.Resource, ok
}

// librariesKnown reports whether the model has a library list to check
// library-scoped collections against.
func (m *Model) librariesKnown() bool {
	return m.collection(catalog.Resource{Kind: catalog.Libraries}).Known
}

// applyState records a published state and projects content that changed.
func (m *Model) applyState(st catalog.State) tea.Cmd {
	r := st.Resource
	key := r.Key()
	if r.LibraryID != "" && m.librariesKnown() && m.findLibrary(r.LibraryID) == nil {
		return nil // the library was removed; late states cannot recreate it
	}
	prev := m.collections[key]
	m.collections[key] = st

	var cmds []tea.Cmd
	if st.Fetching && st.Attempt != prev.Attempt {
		cmds = append(cmds, showLoadingCmd(key, st.Attempt))
	}
	changed := st.Known && (!prev.Known || st.Snapshot.Revision != prev.Snapshot.Revision)
	if changed {
		cmds = append(cmds, m.project(st.Snapshot))
	}
	if r.Kind == catalog.Libraries && changed && (!prev.Known || st.Snapshot.Validated) {
		if st.Snapshot.Validated {
			m.pruneLibraryRequests()
		}
		cmds = append(cmds, m.syncLibraries(catalog.Revalidate))
	}
	if changed && st.Snapshot.Validated {
		m.pruneNavigation()
	}
	switch {
	case st.Known && st.Err == nil:
		cmds = append(cmds, m.advanceNavPlanAfterLoad(key, !st.Fetching))
	case st.Err != nil && !st.Fetching && m.navPlan != nil && m.navPlan.AwaitKey == key:
		m.clearNavPlan()
	}
	m.updateResourceFeedback(r)
	return tea.Batch(cmds...)
}

// project shows a snapshot in every column displaying its collection and
// feeds library contents to the search index.
func (m *Model) project(snapshot catalog.Snapshot) tea.Cmd {
	r := snapshot.Resource
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

// syncLibraries loads every library and the playlists in the background.
func (m *Model) syncLibraries(policy catalog.Policy) tea.Cmd {
	var cmds []tea.Cmd
	for _, lib := range m.Libraries {
		cmds = append(cmds, m.loadResource(catalog.LibraryResource(lib), policy, true))
	}
	cmds = append(cmds, m.loadResource(catalog.Resource{Kind: catalog.Playlists}, policy, true))
	return tea.Batch(cmds...)
}
