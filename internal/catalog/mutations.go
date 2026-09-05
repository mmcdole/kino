package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

type MutationKind uint8

const (
	Watch MutationKind = iota
	CreatePlaylist
	AddToPlaylist
	RemoveFromPlaylist
	DeletePlaylist
)

type Mutation struct {
	Kind       MutationKind
	ItemID     string
	ItemIDs    []string
	PlaylistID string
	LibraryID  string
	Title      string
	Played     bool
}

type Change struct {
	Revisions map[string]uint64
	Mutation  Mutation
	Applied   bool
	Playlist  *domain.Playlist
	Resources []Resource
	Warning   error
}

// Mutate serializes remote writes and reconciles the cache before publishing
// success. The change remains explicit when a server may have partly applied a
// multi-item request: affected views revalidate even when an error is returned.
func (s *Service) Mutate(ctx context.Context, m Mutation) (Change, error) {
	ctx, finish, startErr := s.operation(ctx, 30*time.Second)
	if startErr != nil {
		return Change{}, startErr
	}
	defer finish()
	select {
	case s.mutations <- struct{}{}:
	case <-ctx.Done():
		return Change{}, ctx.Err()
	}
	defer func() { <-s.mutations }()
	if err := ctx.Err(); err != nil {
		return Change{}, err
	}
	change := Change{Mutation: m, Revisions: make(map[string]uint64)}
	var err error
	switch m.Kind {
	case Watch:
		if m.Played {
			err = s.backend.MarkPlayed(ctx, m.ItemID)
		} else {
			err = s.backend.MarkUnplayed(ctx, m.ItemID)
		}
	case CreatePlaylist:
		change.Playlist, err = s.backend.CreatePlaylist(ctx, m.Title, m.ItemIDs)
	case AddToPlaylist:
		err = s.backend.AddToPlaylist(ctx, m.PlaylistID, m.ItemIDs)
	case RemoveFromPlaylist:
		err = s.backend.RemoveFromPlaylist(ctx, m.PlaylistID, m.ItemID)
	case DeletePlaylist:
		err = s.backend.DeletePlaylist(ctx, m.PlaylistID)
	default:
		return change, fmt.Errorf("unknown mutation %d", m.Kind)
	}
	change.Applied = err == nil
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Kind == Watch {
		// Watch data can appear in several projections. Fence active reads before
		// patching, so a response fetched before this write cannot undo it.
		for key, r := range s.known {
			if r.Kind == Libraries || r.Kind == Playlists {
				continue
			}
			if m.LibraryID != "" && r.Kind != PlaylistItems && r.LibraryID != m.LibraryID {
				continue
			}
			if f := s.active[key]; f != nil {
				f.cancel()
				delete(s.active, key)
			}
			s.revisions[key]++
			change.Revisions[key] = s.revisions[key]
		}

		if err == nil {
			change.Warning = s.cache.PatchWatchState(m.ItemID, m.Played)
		}
		for key, revision := range change.Revisions {
			if err == nil && change.Warning == nil {
				s.cacheRevisions[key] = revision
			} else {
				s.invalid[key] = true
			}
		}
		return change, err
	}
	change.Resources = []Resource{{Kind: Playlists}}
	if m.PlaylistID != "" {
		change.Resources = append(change.Resources, Resource{Kind: PlaylistItems, ID: m.PlaylistID})
	}
	for _, r := range change.Resources {
		key := r.Key()
		if f := s.active[key]; f != nil {
			f.cancel()
			delete(s.active, key)
		}
		s.revisions[key]++
		change.Revisions[key] = s.revisions[key]
		// Retain usable data, but force revalidation after a known or uncertain
		// remote change. Never renew its age just because we changed it locally.
		if entry, ok := s.cache.Load(key); ok {
			entry.FetchedAt = time.Time{}
			saveErr := s.cache.Save(key, entry)
			change.Warning = errors.Join(change.Warning, saveErr)
			if saveErr != nil {
				s.invalid[key] = true
			} else {
				s.cacheRevisions[key] = s.revisions[key]
			}
		}
	}
	return change, err
}

type Membership struct {
	Playlists []*domain.Playlist
	Present   map[string]bool
}

// PlaylistMembership fetches the same playlist snapshot that the modal will
// display. Every membership is verified; unknown never means absent.
func (s *Service) PlaylistMembership(ctx context.Context, itemID string) (Membership, error) {
	ctx, finish, startErr := s.operation(ctx, 30*time.Second)
	if startErr != nil {
		return Membership{}, startErr
	}
	defer finish()

	snapshot, err := s.Load(ctx, Resource{Kind: Playlists}, Revalidate, Observer{})
	if err != nil {
		return Membership{}, err
	}
	result := Membership{Present: make(map[string]bool)}
	for _, item := range snapshot.Items {
		if p, ok := item.(*domain.Playlist); ok {
			result.Playlists = append(result.Playlists, p)
		}
	}
	var mu sync.Mutex
	var failures []error
	jobs := make(chan *domain.Playlist)
	var workers sync.WaitGroup
	for range min(4, len(result.Playlists)) {
		workers.Go(func() {
			for playlist := range jobs {
				if ctx.Err() != nil {
					continue
				}
				items, err := s.Load(ctx, Resource{Kind: PlaylistItems, ID: playlist.ID}, Revalidate, Observer{})
				mu.Lock()
				if err != nil {
					failures = append(failures, fmt.Errorf("playlist %q: %w", playlist.Title, err))
				} else {
					result.Present[playlist.ID] = false
					for _, item := range items.Items {
						if item.GetID() == itemID {
							result.Present[playlist.ID] = true
							break
						}
					}
				}
				mu.Unlock()
			}
		})
	}
	for _, playlist := range result.Playlists {
		select {
		case jobs <- playlist:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	if len(failures) > 0 {
		return Membership{}, errors.Join(failures...)
	}
	return result, nil
}
