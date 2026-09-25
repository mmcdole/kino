package store

import (
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func TestFailedWritesDoNotLeaveReadableMemoryCopy(t *testing.T) {
	s := seedStore(t, openStore(t))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("movies", domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "new"}}}); err == nil {
		t.Fatal("closed database write succeeded")
	}
	if _, err := s.Update([]string{"episode"}, func(l map[string]domain.CachedList) map[string]domain.CachedList { return l }); err == nil {
		t.Fatal("closed database update succeeded")
	}
	if _, ok := s.Load("movies"); ok {
		t.Fatal("failed database read served divergent memory data")
	}
}
