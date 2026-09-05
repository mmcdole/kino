package store

import (
	"github.com/mmcdole/kino/internal/domain"
	"sync"
	"testing"
)

func TestReadsCannotResurrectInvalidatedData(t *testing.T) {
	s := seedStore(t, t.TempDir())
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 100 {
				s.Load("movies")
			}
		})
	}
	if err := s.Remove("movies"); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	if _, ok := s.Load("movies"); ok {
		t.Fatal("read resurrected removed snapshot")
	}
}
func TestFailedWritesDoNotLeaveReadableMemoryCopy(t *testing.T) {
	s := seedStore(t, t.TempDir())
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("movies", domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "new"}}}); err == nil {
		t.Fatal("closed database write succeeded")
	}
	if err := s.PatchWatchState("episode", true); err == nil {
		t.Fatal("closed database patch succeeded")
	}
	if err := s.Remove("movies"); err == nil {
		t.Fatal("closed database removal succeeded")
	}
	if _, ok := s.Load("movies"); ok {
		t.Fatal("failed database read served divergent memory data")
	}
}
