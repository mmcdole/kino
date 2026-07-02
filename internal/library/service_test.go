package library

import (
	"context"
	"errors"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

// fakeClient implements domain.LibraryClient with canned data
type fakeClient struct {
	movies     []*domain.MediaItem
	count      int
	countErr   error
	fetchCalls int
	countCalls int
}

func (f *fakeClient) GetLibraries(ctx context.Context) ([]domain.Library, error) { return nil, nil }

func (f *fakeClient) GetMovies(ctx context.Context, libID string, offset, limit int) ([]*domain.MediaItem, int, error) {
	f.fetchCalls++
	return f.movies, len(f.movies), nil
}

func (f *fakeClient) GetShows(ctx context.Context, libID string, offset, limit int) ([]*domain.Show, int, error) {
	return nil, 0, nil
}

func (f *fakeClient) GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]domain.ListItem, int, error) {
	return nil, 0, nil
}

func (f *fakeClient) GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error) {
	return nil, nil
}

func (f *fakeClient) GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error) {
	return nil, nil
}

func (f *fakeClient) GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error) {
	f.countCalls++
	return f.count, f.countErr
}

func newTestService(t *testing.T, client *fakeClient) (*Service, domain.Store) {
	t.Helper()
	st, err := store.NewLibraryStore("", "", "") // memory-only
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(client, st, nil), st
}

func movie(id string) *domain.MediaItem {
	return &domain.MediaItem{ID: id, Title: id, Type: domain.MediaTypeMovie}
}

// TestSyncLibraryDetectsNewItems reproduces issue #25: the server's library
// timestamp doesn't change when items are added (Jellyfin never bumps it),
// so the count check must catch the new item.
func TestSyncLibraryDetectsNewItems(t *testing.T) {
	client := &fakeClient{movies: []*domain.MediaItem{movie("a"), movie("b")}, count: 2}
	svc, _ := newTestService(t, client)

	lib := domain.Library{ID: "lib1", Type: "movie", UpdatedAt: 100}

	// Initial sync populates the cache
	res, err := svc.SyncLibrary(context.Background(), lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache || res.Count != 2 {
		t.Fatalf("initial sync: got %+v", res)
	}

	// Second sync, nothing changed: timestamp valid, counts match -> cache
	res, err = svc.SyncLibrary(context.Background(), lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromCache || res.Count != 2 {
		t.Fatalf("unchanged sync: got %+v", res)
	}
	if client.fetchCalls != 1 {
		t.Fatalf("expected no refetch, got %d fetch calls", client.fetchCalls)
	}

	// New movie added on the server, but timestamp UNCHANGED (the bug)
	client.movies = append(client.movies, movie("c"))
	client.count = 3

	res, err = svc.SyncLibrary(context.Background(), lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("stale cache served despite item count change")
	}
	if res.Count != 3 {
		t.Fatalf("expected 3 items after refetch, got %d", res.Count)
	}
}

// TestSyncLibraryTimestampInvalidates verifies the original timestamp path
// still triggers a refetch without needing the count check.
func TestSyncLibraryTimestampInvalidates(t *testing.T) {
	client := &fakeClient{movies: []*domain.MediaItem{movie("a")}, count: 1}
	svc, _ := newTestService(t, client)

	if _, err := svc.SyncLibrary(context.Background(), domain.Library{ID: "lib1", Type: "movie", UpdatedAt: 100}, nil); err != nil {
		t.Fatal(err)
	}

	countCallsBefore := client.countCalls
	res, err := svc.SyncLibrary(context.Background(), domain.Library{ID: "lib1", Type: "movie", UpdatedAt: 200}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("expected refetch for newer server timestamp")
	}
	if client.countCalls != countCallsBefore {
		t.Fatal("count check should be skipped when timestamp already invalidates")
	}
}

// pagedClient serves distinct pages so pagination behavior is testable.
type pagedClient struct {
	fakeClient
	pages [][]*domain.MediaItem
	total int
}

func (p *pagedClient) GetMovies(ctx context.Context, libID string, offset, limit int) ([]*domain.MediaItem, int, error) {
	idx := offset / limit
	if idx >= len(p.pages) {
		return nil, p.total, nil
	}
	return p.pages[idx], p.total, nil
}

// Offset pagination under concurrent server mutation can repeat items across
// pages; duplicates must not be cached as truth.
func TestFetchMoviesDeduplicatesAcrossPages(t *testing.T) {
	client := &pagedClient{
		pages: [][]*domain.MediaItem{
			{movie("a"), movie("b")},
			{movie("b"), movie("c")}, // "b" repeated by a page shift
		},
		total: 4,
	}
	svc := NewService(client, mustStore(t), nil)

	movies, err := svc.FetchMovies(context.Background(), "lib1", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 3 {
		t.Fatalf("got %d movies, want 3 deduplicated", len(movies))
	}
	seen := map[string]bool{}
	for _, m := range movies {
		if seen[m.ID] {
			t.Fatalf("duplicate item %s cached", m.ID)
		}
		seen[m.ID] = true
	}
}

// A server reporting total=0 alongside non-empty pages must still be fully
// paginated instead of stopping after one page.
func TestFetchMoviesZeroTotalStillPaginates(t *testing.T) {
	client := &pagedClient{
		pages: [][]*domain.MediaItem{
			{movie("a"), movie("b")},
			{movie("c")},
		},
		total: 0,
	}
	svc := NewService(client, mustStore(t), nil)

	movies, err := svc.FetchMovies(context.Background(), "lib1", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 3 {
		t.Fatalf("got %d movies, want 3 (stopped early on total=0?)", len(movies))
	}
}

func mustStore(t *testing.T) domain.Store {
	t.Helper()
	st, err := store.NewLibraryStore("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestSyncLibraryCountCheckFailureServesCache verifies we degrade gracefully:
// if the count request fails, the cache is served rather than erroring.
func TestSyncLibraryCountCheckFailureServesCache(t *testing.T) {
	client := &fakeClient{movies: []*domain.MediaItem{movie("a")}, count: 1}
	svc, _ := newTestService(t, client)

	lib := domain.Library{ID: "lib1", Type: "movie", UpdatedAt: 100}
	if _, err := svc.SyncLibrary(context.Background(), lib, nil); err != nil {
		t.Fatal(err)
	}

	client.countErr = errors.New("server offline")
	res, err := svc.SyncLibrary(context.Background(), lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromCache || res.Count != 1 {
		t.Fatalf("expected cache to be served on count failure, got %+v", res)
	}
	if client.fetchCalls != 1 {
		t.Fatalf("expected no refetch, got %d fetch calls", client.fetchCalls)
	}
}
