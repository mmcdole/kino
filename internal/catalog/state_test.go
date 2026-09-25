package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// latest reads updates until pred holds for r's state or the deadline passes.
func latest(t *testing.T, svc *Service, r Resource, pred func(State) bool) State {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var last State
	for {
		states, err := svc.Updates(ctx)
		if err != nil {
			t.Fatalf("no matching state for %s; last %+v", r.Key(), last)
		}
		for _, st := range states {
			if st.Resource.Key() == r.Key() {
				last = st
			}
		}
		if last.Resource.Key() == r.Key() && pred(last) {
			return last
		}
	}
}

func TestSlowConsumerStillSeesFinalState(t *testing.T) {
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	backend := fakeBackend{}
	backend.movies = func(context.Context) ([]*domain.MediaItem, int, error) {
		return []*domain.MediaItem{{ID: "m"}}, 1, nil
	}
	svc, _ := testService(t, backend)
	for range 50 {
		if _, err := svc.Load(context.Background(), r, Refresh, Observer{}); err != nil {
			t.Fatal(err)
		}
	}
	st := latest(t, svc, r, func(st State) bool { return !st.Fetching })
	if !st.Known || !st.Snapshot.Validated || len(st.Snapshot.Items) != 1 || st.Snapshot.Revision != 50 {
		t.Fatalf("final state lost: %+v", st)
	}
}

func TestCacheHitDoesNotClearErrorButValidatedResultDoes(t *testing.T) {
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	fail := true
	backend := fakeBackend{movies: func(context.Context) ([]*domain.MediaItem, int, error) {
		if fail {
			return nil, 0, domain.ErrServerOffline
		}
		return []*domain.MediaItem{{ID: "m"}}, 1, nil
	}}
	svc, cache := testService(t, backend)
	if err := cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "old"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Load(context.Background(), r, Refresh, Observer{}); !errors.Is(err, domain.ErrServerOffline) {
		t.Fatalf("expected offline error, got %v", err)
	}
	st := latest(t, svc, r, func(st State) bool { return !st.Fetching })
	if !st.Known || !errors.Is(st.Err, domain.ErrServerOffline) {
		t.Fatalf("failed refresh lost cached data or error: %+v", st)
	}

	fail = false
	if _, err := svc.Load(context.Background(), r, Refresh, Observer{}); err != nil {
		t.Fatal(err)
	}
	st = latest(t, svc, r, func(st State) bool { return !st.Fetching && st.Snapshot.Validated })
	if st.Err != nil || st.Snapshot.Items[0].GetID() != "m" {
		t.Fatalf("validated result did not replace the error: %+v", st)
	}
}

func TestWatchPublishesPatchedSnapshot(t *testing.T) {
	svc, cache := testService(t, watchBackend{watch: func(context.Context) error { return nil }})
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	if err := cache.Save(r.Key(), domain.CachedList{FetchedAt: time.Now(), Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Load(context.Background(), r, Browse, Observer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Mutate(context.Background(), Mutation{Kind: Watch, ItemID: "movie", LibraryID: "a", Played: true}); err != nil {
		t.Fatal(err)
	}
	latest(t, svc, r, func(st State) bool { return st.Snapshot.Items[0].(*domain.MediaItem).IsPlayed })
}
