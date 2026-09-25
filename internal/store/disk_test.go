package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// snapshotStore is what the catalog needs from both cache implementations.
type snapshotStore interface {
	Load(string) (media.CachedList, bool)
	Save(string, media.CachedList) error
	Update([]string, func(map[string]media.CachedList) map[string]media.CachedList) ([]string, error)
	Close() error
}

func openStore(t *testing.T) *Disk {
	t.Helper()
	s, err := OpenDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedStore[S snapshotStore](t *testing.T, s S) S {
	t.Helper()
	entries := map[string][]media.ListItem{
		"movies":   {&media.Item{ID: "movie", Title: "Movie"}},
		"episodes": {&media.Item{ID: "episode", Type: media.TypeEpisode, ShowID: "show", ParentID: "season", ViewOffset: time.Minute}},
		"shows":    {&media.Show{ID: "show", EpisodeCount: 10, UnwatchedCount: 5}},
		"seasons":  {&media.Season{ID: "season", ShowID: "show", EpisodeCount: 10, UnwatchedCount: 5}},
		"playlist": {&media.Item{ID: "episode", Type: media.TypeEpisode, ShowID: "show", ParentID: "season"}},
	}
	for key, items := range entries {
		if err := s.Save(key, media.CachedList{Items: items, Version: 100, FetchedAt: time.Now().Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// watch applies a watch change the way the catalog does.
func watch(s snapshotStore, played bool) ([]string, error) {
	change := media.WatchChange{ItemID: "episode", ShowID: "show", SeasonID: "season", Played: played}
	return s.Update(change.IDs(), func(lists map[string]media.CachedList) map[string]media.CachedList {
		items := make(map[string][]media.ListItem)
		for key, l := range lists {
			items[key] = l.Items
		}
		out := make(map[string]media.CachedList)
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
				item := entry.Items[0].(*media.Item)
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
			if shows.Items[0].(*media.Show).UnwatchedCount != 4 || seasons.Items[0].(*media.Season).UnwatchedCount != 4 {
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
			if shows.Items[0].(*media.Show).UnwatchedCount != 5 || seasons.Items[0].(*media.Season).UnwatchedCount != 5 {
				t.Fatal("unwatch did not restore parent counts")
			}
		})
	}
}

func TestSnapshotRoundTripPreservesExpiryAndDetachedEntities(t *testing.T) {
	s := seedStore(t, openStore(t))
	stale := media.CachedList{Items: []media.ListItem{&media.Playlist{ID: "p"}}, FetchedAt: time.Now().Add(-24 * time.Hour), Version: 12}
	if err := s.Save("stale", stale); err != nil {
		t.Fatal(err)
	}
	loaded, ok := s.Load("stale")
	if !ok || !loaded.FetchedAt.Equal(stale.FetchedAt) {
		t.Fatal("store discarded stale data needed for offline fallback")
	}
	loaded.Items[0].(*media.Playlist).Title = "consumer mutation"
	again, _ := s.Load("stale")
	if again.Items[0].GetTitle() != "" {
		t.Fatal("read leaked mutable cache state")
	}
}

func TestFailedWritesDoNotLeaveReadableMemoryCopy(t *testing.T) {
	s := seedStore(t, openStore(t))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("movies", media.CachedList{Items: []media.ListItem{&media.Item{ID: "new"}}}); err == nil {
		t.Fatal("closed database write succeeded")
	}
	if _, err := s.Update([]string{"episode"}, func(l map[string]media.CachedList) map[string]media.CachedList { return l }); err == nil {
		t.Fatal("closed database update succeeded")
	}
	if _, ok := s.Load("movies"); ok {
		t.Fatal("failed database read served divergent memory data")
	}
}
