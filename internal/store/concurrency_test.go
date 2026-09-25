package store

import (
	"github.com/mmcdole/kino/internal/domain"
	"testing"
)

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
	if _, ok := s.Load("movies"); ok {
		t.Fatal("failed database read served divergent memory data")
	}
}
