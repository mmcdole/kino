package tui

import (
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

// The library column lists the server's libraries plus a synthetic Playlists
// row, which shows playlist load state as if it were a library.
const playlistsLibraryID = "__playlists__"

// playlistsLibraryEntry returns the synthetic library entry for playlists
func playlistsLibraryEntry() domain.Library {
	return domain.Library{
		ID:   playlistsLibraryID,
		Name: "Playlists",
		Type: "playlist",
	}
}

// allLibraryEntries returns libraries plus the synthetic Playlists entry
func (m *Model) allLibraryEntries() []domain.Library {
	return append(m.Libraries, playlistsLibraryEntry())
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
