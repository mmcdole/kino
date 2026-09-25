package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// MutationKind is the kind of write a Mutation makes.
type MutationKind uint8

const (
	Watch MutationKind = iota
	CreatePlaylist
	AddToPlaylist
	RemoveFromPlaylist
	DeletePlaylist
)

// Mutation is one write to the server. Each kind uses only the fields it
// needs; Title is also echoed back for the caller's messages.
type Mutation struct {
	Kind       MutationKind
	ItemID     string
	ShowID     string // parents of an episode, for watch count rollup
	SeasonID   string
	PlaylistID string
	LibraryID  string
	Title      string
	Played     bool
}

// itemIDs returns the mutation's item as the list playlist writes take.
func (m Mutation) itemIDs() []string {
	if m.ItemID == "" {
		return nil
	}
	return []string{m.ItemID}
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
	ctx, finish, startErr := s.operation(ctx, requestTimeout)
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
		change.Playlist, err = s.backend.CreatePlaylist(ctx, m.Title, m.itemIDs())
	case AddToPlaylist:
		err = s.backend.AddToPlaylist(ctx, m.PlaylistID, m.itemIDs())
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
// response fetched before a write cannot undo it. It returns the new
// revision. Callers hold s.mu.
func (s *Service) fence(key string) uint64 {
	s.cancelFlight(key)
	s.revisions[key]++
	return s.revisions[key]
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
		if s.cancelFlight(key) {
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
			s.fence(key)
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
		revised[key] = s.fence(key)
		entry, ok := entries[key]
		if s.invalid[key] || !ok {
			s.expire(key)
			change.Resources = append(change.Resources, r)
			continue
		}
		s.cacheRevisions[key] = revised[key]
		s.accept(Snapshot{Resource: r, CachedList: entry, Revision: revised[key], fromCache: true, Stale: !s.fresh(r, entry)})
	}
	for _, key := range interrupted {
		if _, done := revised[key]; !done {
			s.fence(key)
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
		revised[r.Key()] = s.fence(r.Key())
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
			s.accept(Snapshot{Resource: r, CachedList: entries[key], Revision: revised[key], fromCache: true, Stale: true})
		case ok:
			s.invalid[key] = true
			s.expire(key)
		default:
			s.expire(key)
		}
	}
	return change, err
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
