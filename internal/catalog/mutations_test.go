package catalog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

type playlistBackend struct {
	Backend
	items     func(context.Context, string) ([]*domain.MediaItem, error)
	playlists []*domain.Playlist
}

func (b playlistBackend) GetPlaylists(context.Context) ([]*domain.Playlist, error) {
	return b.playlists, nil
}
func (b playlistBackend) GetPlaylistItems(ctx context.Context, id string) ([]*domain.MediaItem, error) {
	return b.items(ctx, id)
}

func TestMembershipNeverReportsUnknownAsAbsent(t *testing.T) {
	svc, _ := testService(t, playlistBackend{playlists: []*domain.Playlist{{ID: "one", Title: "One"}, {ID: "two", Title: "Two"}}, items: func(ctx context.Context, id string) ([]*domain.MediaItem, error) {
		if id == "two" {
			return nil, domain.ErrAuthFailed
		}
		return []*domain.MediaItem{{ID: "movie"}}, nil
	}})
	result, err := svc.PlaylistMembership(context.Background(), "movie")
	if !errors.Is(err, domain.ErrAuthFailed) || result.Present != nil {
		t.Fatalf("incomplete membership published as editable: %+v %v", result, err)
	}
}

func TestMembershipRevalidatesEvenFreshCachedItems(t *testing.T) {
	var calls atomic.Int32
	svc, cache := testService(t, playlistBackend{playlists: []*domain.Playlist{{ID: "p"}}, items: func(context.Context, string) ([]*domain.MediaItem, error) {
		calls.Add(1)
		return []*domain.MediaItem{{ID: "movie"}}, nil
	}})
	cache.Save((Resource{Kind: PlaylistItems, ID: "p"}).Key(), domain.CachedList{FetchedAt: time.Now()})
	result, err := svc.PlaylistMembership(context.Background(), "movie")
	if err != nil || !result.Present["p"] || calls.Load() != 1 {
		t.Fatalf("membership trusted stale cached absence: %+v %v", result, err)
	}
}

func TestPlaylistFreshnessExpiresWithoutDiscardingOfflineFallback(t *testing.T) {
	svc, cache := testService(t, playlistBackend{items: func(context.Context, string) ([]*domain.MediaItem, error) { return nil, domain.ErrServerOffline }})
	r := Resource{Kind: PlaylistItems, ID: "p"}
	cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}, FetchedAt: time.Now().Add(-MaxAge - time.Second)})
	result, err := svc.Load(context.Background(), r, Browse, Observer{})
	if !errors.Is(err, domain.ErrServerOffline) || !result.Stale || len(result.Items) != 1 {
		t.Fatalf("lost offline playlist: %+v %v", result, err)
	}
}

type watchBackend struct {
	Backend
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
		_, err := svc.Mutate(context.Background(), Mutation{Kind: Watch, ItemID: "movie", Played: true})
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
