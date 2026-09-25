package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// snapshotStore is what the catalog needs from both cache implementations.
type snapshotStore interface {
	Load(string) (domain.CachedList, bool)
	Save(string, domain.CachedList) error
	Update([]string, func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error)
	Close() error
}

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedStore[S snapshotStore](t *testing.T, s S) S {
	t.Helper()
	entries := map[string][]domain.ListItem{
		"movies":   {&domain.MediaItem{ID: "movie", Title: "Movie"}},
		"episodes": {&domain.MediaItem{ID: "episode", Type: domain.MediaTypeEpisode, ShowID: "show", ParentID: "season", ViewOffset: time.Minute}},
		"shows":    {&domain.Show{ID: "show", EpisodeCount: 10, UnwatchedCount: 5}},
		"seasons":  {&domain.Season{ID: "season", ShowID: "show", EpisodeCount: 10, UnwatchedCount: 5}},
		"playlist": {&domain.MediaItem{ID: "episode", Type: domain.MediaTypeEpisode, ShowID: "show", ParentID: "season"}},
	}
	for key, items := range entries {
		if err := s.Save(key, domain.CachedList{Items: items, Version: 100, FetchedAt: time.Now().Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// watch applies a watch change the way the catalog does.
func watch(s snapshotStore, played bool) ([]string, error) {
	change := domain.WatchChange{ItemID: "episode", ShowID: "show", SeasonID: "season", Played: played}
	return s.Update(change.IDs(), func(lists map[string]domain.CachedList) map[string]domain.CachedList {
		items := make(map[string][]domain.ListItem)
		for key, l := range lists {
			items[key] = l.Items
		}
		out := make(map[string]domain.CachedList)
		for key, patched := range change.Apply(items) {
			l := lists[key]
			l.Items = patched
			out[key] = l
		}
		return out
	})
}

func TestWatchStateSnapshotsAreAtomicAndIdempotent(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			var s snapshotStore = NewMemory()
			if persistent {
				s = openStore(t)
			}
			seedStore(t, s)
			original, _ := s.Load("episodes")
			var wg sync.WaitGroup
			for range 32 {
				wg.Go(func() {
					if _, err := watch(s, true); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			for _, key := range []string{"episodes", "playlist"} {
				entry, ok := s.Load(key)
				item := entry.Items[0].(*domain.MediaItem)
				if !ok || !item.IsPlayed || item.ViewOffset != 0 {
					t.Fatalf("unpatched %s", key)
				}
			}
			entry, _ := s.Load("episodes")
			if !entry.FetchedAt.Equal(original.FetchedAt) || entry.Version != 100 {
				t.Fatal("local patch changed server freshness")
			}
			shows, _ := s.Load("shows")
			seasons, _ := s.Load("seasons")
			if shows.Items[0].(*domain.Show).UnwatchedCount != 4 || seasons.Items[0].(*domain.Season).UnwatchedCount != 4 {
				t.Fatal("duplicate projections or callers double-counted the mutation")
			}
			saved, err := watch(s, false)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(saved) != "[episodes playlist seasons shows]" {
				t.Fatalf("saved %v; unrelated snapshots were rewritten", saved)
			}
			if err != nil {
				t.Fatal(err)
			}
			shows, _ = s.Load("shows")
			seasons, _ = s.Load("seasons")
			if shows.Items[0].(*domain.Show).UnwatchedCount != 5 || seasons.Items[0].(*domain.Season).UnwatchedCount != 5 {
				t.Fatal("unwatch did not restore parent counts")
			}
		})
	}
}

func TestSnapshotRoundTripPreservesExpiryAndDetachedEntities(t *testing.T) {
	s := seedStore(t, openStore(t))
	stale := domain.CachedList{Items: []domain.ListItem{&domain.Playlist{ID: "p"}}, FetchedAt: time.Now().Add(-24 * time.Hour), Version: 12}
	if err := s.Save("stale", stale); err != nil {
		t.Fatal(err)
	}
	loaded, ok := s.Load("stale")
	if !ok || !loaded.FetchedAt.Equal(stale.FetchedAt) {
		t.Fatal("store discarded stale data needed for offline fallback")
	}
	loaded.Items[0].(*domain.Playlist).Title = "consumer mutation"
	again, _ := s.Load("stale")
	if again.Items[0].GetTitle() != "" {
		t.Fatal("read leaked mutable cache state")
	}
}
