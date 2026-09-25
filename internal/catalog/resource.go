// Package catalog coordinates browsing, cache freshness, and media mutations.
// It has no dependency on the terminal UI or a particular media server.
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

// Policy says how much Load trusts cached data.
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

// Snapshot is one complete item list for a collection, with where it came
// from and how far it can be trusted.
type Snapshot struct {
	Resource Resource
	domain.CachedList
	Revision  uint64
	fromCache bool
	// Validated is true only when this result was checked against the server.
	// A count check can validate a payload that still comes from cache.
	Validated bool
	Stale     bool
	// Warning means usable data was fetched but could not be persisted.
	Warning error
}

// Clone copies the items so the caller can keep or change them.
func (s Snapshot) Clone() Snapshot { s.Items = domain.CloneItems(s.Items); return s }

// Progress reports pagination of a large collection.
type Progress struct{ Loaded, Total int }

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
