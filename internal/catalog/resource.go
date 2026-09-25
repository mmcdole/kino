// Package catalog coordinates browsing, cache freshness, and media mutations.
// It has no dependency on the terminal UI or a particular media server.
package catalog

import (
	"fmt"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

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

func (r Resource) Key() string {
	return fmt.Sprintf("%d/%q/%q/%q", r.Kind, r.LibraryID, r.ShowID, r.ID)
}

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

type Policy uint8

const (
	// Browse serves a fresh snapshot; otherwise it revalidates after publishing
	// any stale snapshot. It joins an existing fetch for the same collection.
	Browse Policy = iota
	// Revalidate checks the server even when the snapshot has not expired.
	Revalidate
	// Refresh supersedes in-flight work and fetches a complete replacement.
	Refresh
)

// MaxAge bounds stale watch state and metadata even on servers with no useful
// revision. Reads, count checks, and local patches never extend this age.
const MaxAge = 5 * time.Minute

// ContentMaxAge bounds how long a library's item list is trusted on a count
// and server version check alone. Opening the library still refreshes it once
// MaxAge has passed, so watch state stays current where it is shown.
const ContentMaxAge = 24 * time.Hour

type Snapshot struct {
	Resource Resource
	domain.CachedList
	Revision  uint64
	FromCache bool
	// Validated is true only when this result was checked against the server.
	// A count check can validate a payload that still comes from cache.
	Validated bool
	Stale     bool
	// Warning means usable data was fetched but could not be persisted.
	Warning error
}

func (s Snapshot) Clone() Snapshot { s.Items = domain.CloneItems(s.Items); return s }

// Progress reports pagination of a large collection.
type Progress struct{ Loaded, Total int }

func (r Resource) Timeout() time.Duration {
	switch r.Kind {
	case Movies, Shows, Mixed:
		return 10 * time.Minute
	default:
		return 30 * time.Second
	}
}
