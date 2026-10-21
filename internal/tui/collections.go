package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
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

func (m *Model) resource(key string) (catalog.Resource, bool) {
	st, ok := m.collections[key]
	return st.Resource, ok
}

// librariesKnown reports whether the model has a library list to check
// library-scoped collections against.
func (m *Model) librariesKnown() bool {
	return m.collection(catalog.Resource{Kind: catalog.Libraries}).Known
}

func (m *Model) loadResource(r catalog.Resource, policy catalog.Policy, background bool) tea.Cmd {
	owner := viewOwner(r)
	if background {
		owner = syncOwner(r)
	}
	if old, ok := m.requests.active[owner]; ok && policy != catalog.Refresh && old.Resource.Version == r.Version {
		return nil
	}
	req := m.requests.begin(owner, r, policy)
	m.track(r)
	m.updateResourceFeedback(r)
	return loadCmd(m.catalog, req)
}

// handleLoadDone reports the outcome of this model's own request. Content
// and loading state come from the catalog's published state instead.
func (m *Model) handleLoadDone(msg loadDoneMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	r := msg.Request.Resource
	var cmds []tea.Cmd
	if msg.Err != nil {
		if p := m.pendingSelect; p != nil && p.key == r.Key() {
			m.pendingSelect = nil
		}
		cmds = append(cmds, m.notifyError("Loading "+m.resourceName(r), msg.Err))
	} else {
		// The load has settled, including from a fresh cache that publishes
		// no new state, so a pending selection is decided now.
		cmds = append(cmds, m.trySelect(r.Key(), true))
	}
	if msg.Warning != nil {
		cmds = append(cmds, m.notifyError("Loaded "+m.resourceName(r), msg.Warning))
	}
	m.updateResourceFeedback(r)
	return tea.Batch(cmds...)
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
	if st.Known {
		cmds = append(cmds, m.trySelect(key, false))
	}
	m.updateResourceFeedback(r)
	return tea.Batch(cmds...)
}

// project shows a snapshot in every column displaying its collection and
// feeds library contents to the search index.
func (m *Model) project(snapshot catalog.Snapshot) tea.Cmd {
	r := snapshot.Resource
	if r.Kind == catalog.Libraries {
		m.libraries = nil
		for _, item := range snapshot.Items {
			if lib, ok := item.(*media.Library); ok {
				m.libraries = append(m.libraries, *lib)
			}
		}
		m.libraryColumn().ReplaceItems(media.LibraryItems(m.allLibraryEntries()))
	} else {
		for i := 0; i < m.columns.Len(); i++ {
			if col := m.columns.Get(i); col.ContentID() == r.Key() {
				col.ReplaceItems(media.CloneItems(snapshot.Items))
			}
		}
	}
	if r.Kind == catalog.Movies || r.Kind == catalog.Shows || r.Kind == catalog.Mixed {
		index := m.searchIndex
		return func() tea.Msg {
			index.ReplaceLibrary(r.LibraryID, snapshot.Revision, snapshot.Items)
			return searchIndexChangedMsg{}
		}
	}
	return nil
}

// syncLibraries loads every library, and the playlists when the server has
// them, in the background.
func (m *Model) syncLibraries(policy catalog.Policy) tea.Cmd {
	var cmds []tea.Cmd
	for _, lib := range m.libraries {
		cmds = append(cmds, m.loadResource(catalog.LibraryResource(lib), policy, true))
	}
	if !m.opts.NoPlaylists {
		cmds = append(cmds, m.loadResource(catalog.Resource{Kind: catalog.Playlists}, policy, true))
	}
	return tea.Batch(cmds...)
}

func (m *Model) topResource() (catalog.Resource, bool) {
	if col := m.columns.Top(); col != nil {
		r, ok := m.resource(col.ContentID())
		return r, ok
	}
	return catalog.Resource{}, false
}

func (m *Model) resourceName(r catalog.Resource) string {
	if r.Kind == catalog.Libraries {
		return "libraries"
	}
	if r.Kind == catalog.Playlists {
		return "playlists"
	}
	for i := 0; i < m.columns.Len(); i++ {
		if col := m.columns.Get(i); col.ContentID() == r.Key() {
			return col.Title()
		}
	}
	if lib := m.findLibrary(r.LibraryID); lib != nil {
		return lib.Name
	}
	return "items"
}
