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
func (b *browsingBackend) GetLibraryItemCount(context.Context, string, string) (int, error) {
	return 1, nil
}
func (b *browsingBackend) MarkPlayed(context.Context, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.played = true
	return nil
}

// start runs cmd in the background, as Bubble Tea would.
func start(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	return done
}

// pumpOnce applies the states the catalog publishes within a short wait.
func pumpOnce(m *Model, svc *catalog.Service) (*Model, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	states, err := svc.Updates(ctx)
	if err != nil {
		return m, false
	}
	return publish(m, states...), true
}

// pumpUntil applies published states until cond holds.
func pumpUntil(t *testing.T, m *Model, svc *catalog.Service, cond func(*Model) bool) *Model {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond(m) {
		if time.Now().After(deadline) {
			t.Fatal("model never reached the expected state")
		}
		m, _ = pumpOnce(m, svc)
	}
	return m
}

// await applies published states until the command's result arrives, then
// applies its result and any states that follow.
func await(t *testing.T, m *Model, svc *catalog.Service, done <-chan tea.Msg) *Model {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case msg := <-done:
			m = updateModel(m, msg)
			for more := true; more; {
				m, more = pumpOnce(m, svc)
			}
			return m
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not complete")
		}
		m, _ = pumpOnce(m, svc)
	}
}

func TestCatalogDiskCacheAndTUIRequestLifecycle(t *testing.T) {
	dir := t.TempDir()
	cache, err := store.Open(dir)
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

	view := start(m.pushColumn(r, "A"))
	col := m.ColumnStack.Top()
	m = pumpUntil(t, m, svc, func(*Model) bool { return col.HasContent() })
	if col.SelectedMediaItem().Title != "Cached title" || !col.IsRefreshing() {
		t.Fatal("cached snapshot not shown during network load")
	}

	// A second UI subscription joins the same catalog fetch.
	shared := start(m.loadResource(r, catalog.Revalidate, true))
	close(backend.gate)
	m = await(t, m, svc, view)
	m = await(t, m, svc, shared)
	if col.SelectedMediaItem().Title != "Fresh title" || col.IsRefreshing() {
		t.Fatal("shared completion did not update open view and stop loading")
	}

	mutation := catalog.Mutation{Kind: catalog.Watch, ItemID: "movie", LibraryID: r.LibraryID, Played: true}
	req := m.requests.begin("mutation:watch:movie", catalog.Resource{}, catalog.Browse)
	m = await(t, m, svc, start(mutateCmd(svc, req, mutation)))
	persisted, ok := cache.Load(r.Key())
	if !ok || !persisted.Items[0].(*domain.MediaItem).IsPlayed || !col.SelectedMediaItem().IsPlayed {
		t.Fatal("watch change did not reconcile persistence and view")
	}

	backend.mu.Lock()
	backend.offline = true
	backend.mu.Unlock()
	m = await(t, m, svc, start(m.loadResource(r, catalog.Refresh, false)))
	if !col.SelectedMediaItem().IsPlayed || !col.HasLoadFailed() || col.IsRefreshing() {
		t.Fatal("offline refresh lost watch change or retry state")
	}
	// Offline fallback must survive closing and reopening the actual database.
	svc.Close()
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, ok := reopened.Load(r.Key())
	if !ok || !saved.Items[0].(*domain.MediaItem).IsPlayed {
		t.Fatal("offline fallback did not survive reopening disk cache")
	}
}
