package catalog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

type playlistBackend struct {
	playlistServer
	items     func(context.Context, string) ([]*media.Item, error)
	playlists []*media.Playlist
}

func (b playlistBackend) GetPlaylists(context.Context) ([]*media.Playlist, error) {
	return b.playlists, nil
}
func (b playlistBackend) GetPlaylistItems(ctx context.Context, id string) ([]*media.Item, error) {
	return b.items(ctx, id)
}

func TestMembershipNeverReportsUnknownAsAbsent(t *testing.T) {
	svc, _ := testService(t, playlistBackend{playlists: []*media.Playlist{{ID: "one", Title: "One"}, {ID: "two", Title: "Two"}}, items: func(ctx context.Context, id string) ([]*media.Item, error) {
		if id == "two" {
			return nil, media.ErrAuthFailed
		}
		return []*media.Item{{ID: "movie"}}, nil
	}})
	result, err := svc.PlaylistMembership(context.Background(), "movie")
	if !errors.Is(err, media.ErrAuthFailed) || result.Present != nil {
		t.Fatalf("incomplete membership published as editable: %+v %v", result, err)
	}
}

func TestMembershipRevalidatesEvenFreshCachedItems(t *testing.T) {
	var calls atomic.Int32
	svc, cache := testService(t, playlistBackend{playlists: []*media.Playlist{{ID: "p"}}, items: func(context.Context, string) ([]*media.Item, error) {
		calls.Add(1)
		return []*media.Item{{ID: "movie"}}, nil
	}})
	cache.Save((Resource{Kind: PlaylistItems, ID: "p"}).Key(), media.CachedList{FetchedAt: time.Now()})
	result, err := svc.PlaylistMembership(context.Background(), "movie")
	if err != nil || !result.Present["p"] || calls.Load() != 1 {
		t.Fatalf("membership trusted stale cached absence: %+v %v", result, err)
	}
}

// A server without playlists says so on every playlist load and write rather
// than looking like one with none.
func TestServerWithoutPlaylists(t *testing.T) {
	svc, _ := testService(t, withoutPlaylists{})
	if svc.HasPlaylists() {
		t.Fatal("backend without playlist methods reported playlists")
	}
	if _, err := svc.Load(context.Background(), Resource{Kind: Playlists}, Revalidate); !errors.Is(err, ErrNoPlaylists) {
		t.Fatalf("load: %v", err)
	}
	if _, err := svc.Mutate(context.Background(), Mutation{Kind: MutationCreatePlaylist, Title: "x"}); !errors.Is(err, ErrNoPlaylists) {
		t.Fatalf("create: %v", err)
	}
}

type withoutPlaylists struct{ Backend }
