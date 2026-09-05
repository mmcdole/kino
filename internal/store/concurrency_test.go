package store

import (
	"fmt"
	"sync"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func TestConcurrentWatchStateIsIdempotent(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			dir := ""
			if persistent {
				dir = t.TempDir()
			}
			s := seedStore(t, dir)
			var wg sync.WaitGroup
			for range 32 {
				wg.Go(func() {
					if err := s.SetWatchState("ep1", true); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			shows, _ := s.GetShows("lib2")
			seasons, _ := s.GetSeasons("lib2", "show1")
			if shows[0].UnwatchedCount != 4 || seasons[0].UnwatchedCount != 4 {
				t.Fatalf("concurrent duplicate updates changed counts more than once: show=%d season=%d", shows[0].UnwatchedCount, seasons[0].UnwatchedCount)
			}
		})
	}
}

func TestReadsCannotResurrectInvalidatedData(t *testing.T) {
	s := seedStore(t, t.TempDir())
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 100 {
				s.GetMovies("lib1")
			}
		})
	}
	if err := s.InvalidateLibrary("lib1"); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	if _, ok := s.GetMovies("lib1"); ok {
		t.Fatal("read resurrected invalidated movies")
	}
	if s.IsValid("lib1", 100) {
		t.Fatal("invalidation retained freshness metadata")
	}
}

func TestFailedWritesDoNotLeaveReadableMemoryCopy(t *testing.T) {
	s := seedStore(t, t.TempDir())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMovies("lib1", []*domain.MediaItem{{ID: "new"}}, 200); err == nil {
		t.Fatal("closed database write succeeded")
	}
	if err := s.SetWatchState("ep1", true); err == nil {
		t.Fatal("closed database patch succeeded")
	}
	if err := s.InvalidateAll(); err == nil {
		t.Fatal("closed database deletion succeeded")
	}
	if _, ok := s.GetMovies("lib1"); ok {
		t.Fatal("failed database read served a divergent memory copy")
	}
}
