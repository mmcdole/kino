package tui

import (
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/tui/components"
)

// The library column lists the server's libraries plus a synthetic Playlists
// row, which shows playlist load state as if it were a library.
const playlistsLibraryID = "__playlists__"

// playlistsLibraryEntry returns the synthetic library entry for playlists
func playlistsLibraryEntry() media.Library {
	return media.Library{
		ID:   playlistsLibraryID,
		Name: "Playlists",
		Type: "playlist",
	}
}

// allLibraryEntries returns libraries plus the synthetic Playlists entry in a
// new slice. Appending to m.libraries directly could write into its spare
// capacity, and the library column keeps pointers into the returned slice.
func (m *Model) allLibraryEntries() []media.Library {
	entries := make([]media.Library, 0, len(m.libraries)+1)
	entries = append(entries, m.libraries...)
	return append(entries, playlistsLibraryEntry())
}

// libraryStateID returns the library row that shows a resource's load state,
// or "" when the resource is not shown in the library column.
func libraryStateID(r catalog.Resource) string {
	switch r.Kind {
	case catalog.Movies, catalog.Shows, catalog.Mixed:
		return r.LibraryID
	case catalog.Playlists:
		return playlistsLibraryID
	default:
		return ""
	}
}

func (m *Model) libraryColumn() *components.ListColumn { return m.columns.Get(0) }

func (m *Model) findLibrary(id string) *media.Library {
	for _, lib := range m.libraries {
		if lib.ID == id {
			return &lib
		}
	}
	return nil
}

func (m *Model) updateLibraryStates() {
	if col := m.libraryColumn(); col != nil {
		col.SetLibraryStates(m.libraryStates)
	}
	m.inspector.SetLibraryStates(m.libraryStates)
}

func (m *Model) activeSyncCount() int {
	n := 0
	for _, state := range m.libraryStates {
		if state.Activity.Visible {
			n++
		}
	}
	return n
}

// pruneLibraryRequests detaches removed libraries after an authoritative root
// refresh. Queued results cannot recreate removed rows or keep sync active.
func (m *Model) pruneLibraryRequests() {
	allowed := make(map[string]bool, len(m.libraries))
	for _, lib := range m.libraries {
		allowed[lib.ID] = true
	}
	for key, st := range m.collections {
		r := st.Resource
		if r.LibraryID == "" || allowed[r.LibraryID] {
			continue
		}
		m.requests.stop(viewOwner(r))
		m.requests.stop(syncOwner(r))
		delete(m.collections, key)
		delete(m.indicators, key)
		delete(m.libraryStates, r.LibraryID)
	}
	m.updateLibraryStates()
}
