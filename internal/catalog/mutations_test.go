package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

func TestPlaylistFreshnessExpiresWithoutDiscardingOfflineFallback(t *testing.T) {
	svc, cache := testService(t, playlistBackend{items: func(context.Context, string) ([]*media.Item, error) { return nil, media.ErrServerOffline }})
	r := Resource{Kind: PlaylistItems, ID: "p"}
	cache.Save(r.Key(), media.CachedList{Items: []media.ListItem{&media.Item{ID: "movie"}}, FetchedAt: time.Now().Add(-MaxAge - time.Second)})
	result, err := svc.Load(context.Background(), r, Browse)
	if !errors.Is(err, media.ErrServerOffline) || !result.Stale || len(result.Items) != 1 {
		t.Fatalf("lost offline playlist: %+v %v", result, err)
	}
}

type watchBackend struct {
	playlistServer
	watch func(context.Context) error
}

func (b watchBackend) MarkPlayed(ctx context.Context, _ string) error { return b.watch(ctx) }

func TestCloseWaitsForMutationReconciliation(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	svc, _ := testService(t, watchBackend{watch: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return ctx.Err()
	}})
	result := make(chan error, 1)
	go func() {
		_, err := svc.Mutate(context.Background(), Mutation{Kind: MutationWatch, ItemID: "movie", Played: true})
		result <- err
	}()
	<-started
	closed := make(chan struct{})
	go func() { svc.Close(); close(closed) }()
	<-canceled
	select {
	case <-closed:
		t.Fatal("Close returned while mutation still used the cache")
	default:
	}
	close(release)
	<-closed
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestUncertainWatchWriteRequiresRevalidation(t *testing.T) {
	svc, cache := testService(t, watchBackend{watch: func(context.Context) error { return media.ErrServerOffline }})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
	if err := cache.Save(r.Key(), media.CachedList{FetchedAt: time.Now(), Items: []media.ListItem{&media.Item{ID: "movie"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Load(context.Background(), r, Browse); err != nil {
		t.Fatal(err)
	}
	change, err := svc.Mutate(context.Background(), Mutation{Kind: MutationWatch, ItemID: "movie", LibraryID: "lib", Played: true})
	if !errors.Is(err, media.ErrServerOffline) || len(change.Resources) != 1 || change.Resources[0] != r || change.Applied {
		t.Fatalf("uncertain remote write was not scheduled for reconciliation: %+v %v", change, err)
	}
}

func TestWatchPatchDoesNotPromoteInvalidCachedPayload(t *testing.T) {
	svc, cache := testService(t, fakeBackend{
		playlistServer: watchBackend{watch: func(context.Context) error { return nil }},
		movies: func(context.Context) ([]*media.Item, int, error) {
			return []*media.Item{{ID: "movie", Title: "Current"}}, 1, nil
		},
	})
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	if err := cache.Save(r.Key(), media.CachedList{FetchedAt: time.Now(), Items: []media.ListItem{&media.Item{ID: "movie", Title: "Outdated"}}}); err != nil {
		t.Fatal(err)
	}
	svc.cache = &failingCache{Cache: cache, fail: true}
	snapshot, err := svc.Load(context.Background(), r, Refresh)
	if err != nil || snapshot.Warning == nil {
		t.Fatal("expected usable content with a persistence warning")
	}
	change, err := svc.Mutate(context.Background(), Mutation{Kind: MutationWatch, ItemID: "movie", LibraryID: "a", Played: true})
	if err != nil || !change.Applied || len(change.Resources) != 1 || svc.state(r).Snapshot.Items[0].(*media.Item).IsPlayed {
		t.Fatal("watch patch promoted invalid cache data instead of requiring revalidation")
	}
}

func TestWatchPublishesOnlyCollectionsContainingTheItem(t *testing.T) {
	svc, cache := testService(t, watchBackend{watch: func(context.Context) error { return nil }})
	holds := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	other := Resource{Kind: Episodes, ID: "season", LibraryID: "a", ShowID: "show"}
	for r, id := range map[Resource]string{holds: "movie", other: "episode"} {
		if err := cache.Save(r.Key(), media.CachedList{FetchedAt: time.Now(), Items: []media.ListItem{&media.Item{ID: id}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Load(context.Background(), r, Browse); err != nil {
			t.Fatal(err)
		}
	}
	before := svc.state(other)
	change, err := svc.Mutate(context.Background(), Mutation{Kind: MutationWatch, ItemID: "movie", LibraryID: "a", Played: true})
	if err != nil || len(change.Resources) != 0 {
		t.Fatalf("unexpected revalidation: %+v %v", change, err)
	}
	if !svc.state(holds).Snapshot.Items[0].(*media.Item).IsPlayed {
		t.Fatal("published snapshot is not patched")
	}
	if after := svc.state(other); after.Snapshot.Revision != before.Snapshot.Revision || svc.collections[other.Key()].revision != 0 {
		t.Fatal("an unrelated collection was revised")
	}
}
