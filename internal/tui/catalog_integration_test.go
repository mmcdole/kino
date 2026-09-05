package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

// Embed the unused backend contract; unexpected calls fail the test immediately.
type browsingBackend struct {
	catalog.Backend
	gate    chan struct{}
	mu      sync.Mutex
	played  bool
	offline bool
}

func (b *browsingBackend) GetMovies(ctx context.Context, _ string, _, _ int) ([]*domain.MediaItem, int, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-b.gate:
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.offline {
		return nil, 0, domain.ErrServerOffline
	}
	return []*domain.MediaItem{{ID: "movie", Title: "Fresh title", IsPlayed: b.played}}, 1, nil
}
func (b *browsingBackend) MarkPlayed(context.Context, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.played = true
	return nil
}

func awaitResource(t *testing.T, cmd tea.Cmd) ResourceMsg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg.(ResourceMsg)
	case <-time.After(3 * time.Second):
		t.Fatal("resource command did not complete")
	}
	return ResourceMsg{}
}

func TestCatalogDiskCacheAndTUIRequestLifecycle(t *testing.T) {
	dir := t.TempDir()
	cache, err := store.Open(dir, "server", "user")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	backend := &browsingBackend{gate: make(chan struct{})}
	svc := catalog.NewService(context.Background(), backend, cache)
	defer svc.Close()
	m := testModel(t)
	m.Catalog = svc
	r := catalog.LibraryResource(m.Libraries[0])
	if err := cache.Save(r.Key(), domain.CachedList{
		Items:     []domain.ListItem{&domain.MediaItem{ID: "movie", Title: "Cached title"}},
		FetchedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	msg := awaitResource(t, m.pushColumn(r, "A"))
	for msg.Stage == loadNetwork {
		m = updateModel(m, msg)
		msg = awaitResource(t, msg.Next)
	}
	if msg.Stage != loadCached {
		t.Fatalf("expected cached data while server waits: %v", msg.Stage)
	}
	m = updateModel(m, msg)
	col := m.ColumnStack.Top()
	if col.SelectedMediaItem().Title != "Cached title" || !col.IsRefreshing() {
		t.Fatal("cached snapshot not shown during network load")
	}

	// A second UI subscription joins the same service fetch.
	shared := awaitResource(t, m.loadResource(r, catalog.Revalidate, true))
	for shared.Stage == loadNetwork {
		m = updateModel(m, shared)
		shared = awaitResource(t, shared.Next)
	}
	if shared.Stage != loadCached {
		t.Fatal("background request did not expose cached data")
	}
	m = updateModel(m, shared)
	close(backend.gate)
	for _, pending := range []ResourceMsg{msg, shared} {
		for pending.Stage != loadFinished {
			pending = awaitResource(t, pending.Next)
			m = updateModel(m, pending)
		}
	}
	if col.SelectedMediaItem().Title != "Fresh title" || col.IsRefreshing() {
		t.Fatal("shared completion did not update open view and stop loading")
	}

	mutation := catalog.Mutation{Kind: catalog.Watch, ItemID: "movie", LibraryID: r.LibraryID, Played: true}
	req := m.requests.begin("mutation:watch:movie", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, MutationCmd(svc, req, mutation)())
	persisted, ok := cache.Load(r.Key())
	if !ok || !persisted.Items[0].(*domain.MediaItem).IsPlayed || !col.SelectedMediaItem().IsPlayed {
		t.Fatal("watch change did not reconcile persistence and view")
	}

	backend.mu.Lock()
	backend.offline = true
	backend.mu.Unlock()
	pending := awaitResource(t, m.loadResource(r, catalog.Refresh, false))
	for {
		m = updateModel(m, pending)
		if pending.Stage == loadFinished {
			break
		}
		pending = awaitResource(t, pending.Next)
	}
	if !col.SelectedMediaItem().IsPlayed || !col.HasLoadFailed() || col.IsRefreshing() {
		t.Fatal("offline refresh lost watch change or retry state")
	}
	// Offline fallback must survive closing and reopening the actual database.
	svc.Close()
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dir, "server", "user")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, ok := reopened.Load(r.Key())
	if !ok || !saved.Items[0].(*domain.MediaItem).IsPlayed {
		t.Fatal("offline fallback did not survive reopening disk cache")
	}
}
