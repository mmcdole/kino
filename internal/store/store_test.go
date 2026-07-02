package store

import (
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func seedStore(t *testing.T, dir string) *LibraryStore {
	t.Helper()
	s, err := NewLibraryStore(dir, "http://test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	movie := &domain.MediaItem{ID: "mov1", Title: "Movie One", Type: domain.MediaTypeMovie}
	episode := &domain.MediaItem{
		ID: "ep1", Title: "Episode One", Type: domain.MediaTypeEpisode,
		ShowID: "show1", ParentID: "season1", ViewOffset: 300,
	}
	show := &domain.Show{ID: "show1", Title: "Show One", EpisodeCount: 10, UnwatchedCount: 5}
	season := &domain.Season{ID: "season1", ShowID: "show1", EpisodeCount: 10, UnwatchedCount: 5}

	if err := s.SaveMovies("lib1", []*domain.MediaItem{movie}, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveShows("lib2", []*domain.Show{show}, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeasons("lib2", "show1", []*domain.Season{season}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEpisodes("lib2", "show1", "season1", []*domain.MediaItem{episode}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlaylistItems("pl1", []*domain.MediaItem{
		{ID: "mov1", Title: "Movie One", Type: domain.MediaTypeMovie},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func testWatchState(t *testing.T, s *LibraryStore) {
	t.Helper()

	// Toggle an episode watched: item patched, season + show counters drop
	s.SetWatchState("ep1", true)

	eps, ok := s.GetEpisodes("lib2", "show1", "season1")
	if !ok || len(eps) != 1 {
		t.Fatal("episodes missing after patch")
	}
	if !eps[0].IsPlayed || eps[0].ViewOffset != 0 {
		t.Fatalf("episode not patched: %+v", eps[0])
	}

	seasons, _ := s.GetSeasons("lib2", "show1")
	if seasons[0].UnwatchedCount != 4 {
		t.Fatalf("season unwatched = %d, want 4", seasons[0].UnwatchedCount)
	}
	shows, _ := s.GetShows("lib2")
	if shows[0].UnwatchedCount != 4 {
		t.Fatalf("show unwatched = %d, want 4", shows[0].UnwatchedCount)
	}

	// Toggling the same state again must not shift counters
	s.SetWatchState("ep1", true)
	seasons, _ = s.GetSeasons("lib2", "show1")
	if seasons[0].UnwatchedCount != 4 {
		t.Fatalf("idempotency broken: season unwatched = %d", seasons[0].UnwatchedCount)
	}

	// Unwatch restores the counters
	s.SetWatchState("ep1", false)
	seasons, _ = s.GetSeasons("lib2", "show1")
	shows, _ = s.GetShows("lib2")
	if seasons[0].UnwatchedCount != 5 || shows[0].UnwatchedCount != 5 {
		t.Fatalf("counters not restored: season=%d show=%d",
			seasons[0].UnwatchedCount, shows[0].UnwatchedCount)
	}

	// A movie is patched in the library list AND its playlist copy
	s.SetWatchState("mov1", true)
	movies, _ := s.GetMovies("lib1")
	if !movies[0].IsPlayed {
		t.Fatal("movie not patched in library list")
	}
	plItems, _ := s.GetPlaylistItems("pl1")
	if !plItems[0].IsPlayed {
		t.Fatal("movie not patched in playlist items")
	}

	// Freshness timestamp untouched — no invalidation happened
	if !s.IsValid("lib1", 100) {
		t.Fatal("watch-state patch must not invalidate the library")
	}
}

func TestSetWatchStateBolt(t *testing.T) {
	testWatchState(t, seedStore(t, t.TempDir()))
}

func TestSetWatchStateMemoryOnly(t *testing.T) {
	testWatchState(t, seedStore(t, ""))
}
