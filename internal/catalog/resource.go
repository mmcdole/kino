package catalog

import (
	"fmt"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// Kind is the type of collection a Resource names.
type Kind uint8

const (
	Libraries Kind = iota
	Movies
	Shows
	Mixed
	Seasons
	Episodes
	Playlists
	PlaylistItems
)

// Resource identifies a collection and its ancestry. Version is an optional
// server revision, not part of the collection's identity.
type Resource struct {
	Kind      Kind
	ID        string
	LibraryID string
	ShowID    string
	Version   int64
}

// Key identifies r's collection in the cache and in published states. It
// leaves out Version, which is not part of the identity.
func (r Resource) Key() string {
	return fmt.Sprintf("%d/%q/%q/%q", r.Kind, r.LibraryID, r.ShowID, r.ID)
}

// LibraryResource names a library's contents, picking the kind from its type.
func LibraryResource(lib domain.Library) Resource {
	kind := Mixed
	switch lib.Type {
	case "movie":
		kind = Movies
	case "show":
		kind = Shows
	}
	return Resource{Kind: kind, ID: lib.ID, LibraryID: lib.ID, Version: lib.UpdatedAt}
}

// Operation deadlines. Library contents paginate, so they get longer.
const (
	requestTimeout = 30 * time.Second
	contentTimeout = 10 * time.Minute
)

// Timeout bounds one server fetch of r.
func (r Resource) Timeout() time.Duration {
	switch r.Kind {
	case Movies, Shows, Mixed:
		return contentTimeout
	default:
		return requestTimeout
	}
}
