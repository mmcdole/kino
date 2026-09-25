package tui

import (
	"context"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

// These are the application operations the TUI consumes. There is no cache or
// backend access here: all data reaches the model through a scoped result.
type Catalog interface {
	Load(context.Context, catalog.Resource, catalog.Policy) (catalog.Snapshot, error)
	Updates(context.Context) ([]catalog.State, error)
	Mutate(context.Context, catalog.Mutation) (catalog.Change, error)
	PlaylistMembership(context.Context, string) (catalog.Membership, error)
}

// Session ends the signed-in session. The cache is cleared by the caller
// after catalog work has drained.
type Session interface {
	Logout() error
}

// Options are the user's display preferences.
type Options struct {
	ShowWatchStatus   bool
	ShowLibraryCounts bool
}

type Playback interface {
	Play(context.Context, domain.MediaItem) error
	Resume(context.Context, domain.MediaItem) error
}
