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
	ShowID     string // parents of an episode, for watch count rollup
	SeasonID   string
	ItemIDs    []string
	PlaylistID string
	LibraryID  string
	Title      string
	Played     bool
}

// Change reports a write. Reconciled snapshots are published through Updates;
// Resources lists collections that must be revalidated against the server.
type Change struct {
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
	change := Change{Mutation: m}
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
	s.commit.Lock()
	defer s.commit.Unlock()
	if m.Kind == Watch {
		return s.reconcileWatch(m, change, err)
	}
	return s.reconcilePlaylists(m, change, err)
}

// fence cancels in-flight fetches for key and advances its revision, so a
// response fetched before a write cannot undo it. It records the new revision
// in revised. Callers hold s.mu.
func (s *Service) fence(key string, revised map[string]uint64) {
	if f := s.active[key]; f != nil {
		f.cancel()
		delete(s.active, key)
	}
	s.revisions[key]++
	revised[key] = s.revisions[key]
}

// expire republishes key as needing revalidation. Callers hold s.mu.
func (s *Service) expire(key string) {
	s.refreshState(key, func(st *State) { st.Snapshot.Stale = true })
}

// watchScope returns the collections an item's watch state can appear in:
// its library's collections and every playlist. Callers hold s.mu.
func (s *Service) watchScope(m Mutation) []string {
	var keys []string
	for key, r := range s.known {
		if r.Kind == Libraries || r.Kind == Playlists {
			continue
		}
		if m.LibraryID != "" && r.Kind != PlaylistItems && r.LibraryID != m.LibraryID {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// reconcileWatch patches the cached projections that contain the item and
// publishes only those. In-flight fetches in scope restart, since they may
// carry the old state. If the write failed or the cache could not be
// patched, everything in scope revalidates. Callers hold s.commit; s.mu is
// only taken for bookkeeping around the cache I/O.
func (s *Service) reconcileWatch(m Mutation, change Change, err error) (Change, error) {
	revised := make(map[string]uint64)
	s.mu.Lock()
	scope := s.watchScope(m)
	var interrupted []string
	for _, key := range scope {
		if f := s.active[key]; f != nil {
			f.cancel()
			delete(s.active, key)
			interrupted = append(interrupted, key)
		}
	}
	s.mu.Unlock()

	var saved []string
	if err == nil {
		watch := domain.WatchChange{ItemID: m.ItemID, ShowID: m.ShowID, SeasonID: m.SeasonID, Played: m.Played}
		saved, change.Warning = s.cache.Update(watch.IDs(), func(lists map[string]domain.CachedList) map[string]domain.CachedList {
			return patchLists(lists, watch.Apply)
		})
	}
	if err != nil || change.Warning != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, key := range scope {
			s.fence(key, revised)
			s.invalid[key] = true
			s.expire(key)
			change.Resources = append(change.Resources, s.known[key])
		}
		return change, err
	}

	entries := make(map[string]domain.CachedList, len(saved))
	for _, key := range saved {
		if entry, ok := s.cache.Load(key); ok {
			entries[key] = entry
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range saved {
		r, known := s.known[key]
		if !known {
			continue // patched on disk; nothing in this session shows it
		}
		s.fence(key, revised)
		entry, ok := entries[key]
		if s.invalid[key] || !ok {
			s.expire(key)
			change.Resources = append(change.Resources, r)
			continue
		}
		s.cacheRevisions[key] = revised[key]
		s.accept(Snapshot{Resource: r, CachedList: entry, Revision: revised[key], FromCache: true, Stale: !s.fresh(r, entry)})
	}
	for _, key := range interrupted {
		if _, done := revised[key]; !done {
			s.fence(key, revised)
			s.expire(key)
			change.Resources = append(change.Resources, s.known[key])
		}
	}
	return change, nil
}

// reconcilePlaylists expires the affected playlist snapshots. Usable data is
// kept for offline browsing, but a known or uncertain remote change forces
// revalidation. A local change never renews a snapshot's age.
func (s *Service) reconcilePlaylists(m Mutation, change Change, err error) (Change, error) {
	revised := make(map[string]uint64)
	change.Resources = []Resource{{Kind: Playlists}}
	if m.PlaylistID != "" {
		change.Resources = append(change.Resources, Resource{Kind: PlaylistItems, ID: m.PlaylistID})
	}
	s.mu.Lock()
	for _, r := range change.Resources {
		s.fence(r.Key(), revised)
	}
	s.mu.Unlock()

	entries := make(map[string]domain.CachedList)
	saved := make(map[string]error)
	for _, r := range change.Resources {
		if entry, ok := s.cache.Load(r.Key()); ok {
			entry.FetchedAt = time.Time{}
			entries[r.Key()] = entry
			saved[r.Key()] = s.cache.Save(r.Key(), entry)
			change.Warning = errors.Join(change.Warning, saved[r.Key()])
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range change.Resources {
		key := r.Key()
		saveErr, ok := saved[key]
		switch {
		case ok && saveErr == nil:
			s.cacheRevisions[key] = revised[key]
			s.accept(Snapshot{Resource: r, CachedList: entries[key], Revision: revised[key], FromCache: true, Stale: true})
		case ok:
			s.invalid[key] = true
			s.expire(key)
		default:
			s.expire(key)
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

	snapshot, err := s.Load(ctx, Resource{Kind: Playlists}, Revalidate)
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
				items, err := s.Load(ctx, Resource{Kind: PlaylistItems, ID: playlist.ID}, Revalidate)
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

// patchLists adapts a domain rule over item lists to cached snapshots,
// keeping each snapshot's fetch time and server version.
func patchLists(lists map[string]domain.CachedList, apply func(map[string][]domain.ListItem) map[string][]domain.ListItem) map[string]domain.CachedList {
	items := make(map[string][]domain.ListItem, len(lists))
	for key, l := range lists {
		items[key] = l.Items
	}
	changed := make(map[string]domain.CachedList)
	for key, patched := range apply(items) {
		l := lists[key]
		l.Items = patched
		changed[key] = l
	}
	return changed
}
