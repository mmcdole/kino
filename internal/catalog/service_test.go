package catalog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

type fakeBackend struct {
	Backend
	movies    func(context.Context) ([]*domain.MediaItem, int, error)
	libraries func(context.Context) ([]domain.Library, error)
	count     func(context.Context) (int, error)
}

func (f fakeBackend) GetMovies(ctx context.Context, _ string, _, _ int) ([]*domain.MediaItem, int, error) {
	return f.movies(ctx)
}
func (f fakeBackend) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	return f.libraries(ctx)
}
func (f fakeBackend) GetLibraryItemCount(ctx context.Context, _, _ string) (int, error) {
	return f.count(ctx)
}

func testService(t *testing.T, backend Backend) (*Service, *store.Store) {
	t.Helper()
	cache, err := store.Open("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(context.Background(), backend, cache)
	t.Cleanup(service.Close)
	return service, cache
}

func TestRefreshFencesOlderFetchAndSharesReplacement(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc, cache := testService(t, fakeBackend{movies: func(ctx context.Context) ([]*domain.MediaItem, int, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return []*domain.MediaItem{{ID: "old"}}, 1, nil
		}
		return []*domain.MediaItem{{ID: "new"}}, 1, nil
	}})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
	oldDone := make(chan Snapshot, 1)
	go func() { snap, _ := svc.Load(context.Background(), r, Browse, Observer{}); oldDone <- snap }()
	<-started
	fresh, err := svc.Load(context.Background(), r, Refresh, Observer{})
	close(release)
	old := <-oldDone
	if err != nil {
		t.Fatal(err)
	}
	cached, _ := cache.Load(r.Key())
	if fresh.Items[0].GetID() != "new" || cached.Items[0].GetID() != "new" || old.Items[0].GetID() != "new" {
		t.Fatal("superseded work escaped its commit fence")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one old fetch and one replacement, got %d", calls.Load())
	}
}

func TestConcurrentBrowseSharesFetchAndDetachedResults(t *testing.T) {
	registered, release := make(chan struct{}, 2), make(chan struct{})
	var calls, networkObservers atomic.Int32
	svc, cache := testService(t, fakeBackend{movies: func(ctx context.Context) ([]*domain.MediaItem, int, error) {
		calls.Add(1)
		<-release
		return []*domain.MediaItem{{ID: "new"}}, 1, nil
	}})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
	cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "cached"}}})
	result := make(chan Snapshot, 2)
	for range 2 {
		go func() {
			snap, _ := svc.Load(context.Background(), r, Browse, Observer{Cached: func(Snapshot) { registered <- struct{}{} }, Network: func() { networkObservers.Add(1) }})
			result <- snap
		}()
	}
	<-registered
	<-registered
	close(release)
	a, b := <-result, <-result
	if networkObservers.Load() != 2 {
		t.Fatal("shared fetch did not report network activity to each subscriber")
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate requests: %d", calls.Load())
	}
	a.Items[0].(*domain.MediaItem).Title = "changed by consumer"
	if b.Items[0].GetTitle() != "" {
		t.Fatal("consumers share mutable entities")
	}
}

func TestExpiredContentRefetchesDespiteUnchangedCountAndVersion(t *testing.T) {
	var calls atomic.Int32
	svc, cache := testService(t, fakeBackend{movies: func(context.Context) ([]*domain.MediaItem, int, error) {
		calls.Add(1)
		return []*domain.MediaItem{{ID: "movie", IsPlayed: true}}, 1, nil
	}})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib", Version: 100}
	cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}, Version: 100, FetchedAt: time.Now().Add(-MaxAge - time.Second)})
	result, err := svc.Load(context.Background(), r, Revalidate, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !result.Items[0].(*domain.MediaItem).IsPlayed {
		t.Fatal("expired user state was accepted as fresh")
	}
}

func TestFailedStartupAndRefreshPreserveCacheAndError(t *testing.T) {
	for _, failure := range []error{domain.ErrServerOffline, domain.ErrAuthFailed, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			svc, cache := testService(t, fakeBackend{libraries: func(context.Context) ([]domain.Library, error) { return nil, failure }})
			r := Resource{Kind: Libraries}
			cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.Library{ID: "lib"}}})
			var cachedPublished bool
			result, err := svc.Load(context.Background(), r, Refresh, Observer{Cached: func(Snapshot) { cachedPublished = true }})
			if !errors.Is(err, failure) || !cachedPublished || !result.Stale || len(result.Items) != 1 {
				t.Fatalf("fallback lost context: %+v, %v", result, err)
			}
			if data, ok := cache.Load(r.Key()); !ok || len(data.Items) != 1 {
				t.Fatal("failed refresh deleted cache")
			}
		})
	}
}

func TestCountValidationDoesNotExtendFreshness(t *testing.T) {
	svc, cache := testService(t, fakeBackend{count: func(context.Context) (int, error) { return 1, nil }})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
	fetched := time.Now().Add(-time.Minute)
	cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}, FetchedAt: fetched})
	result, err := svc.Load(context.Background(), r, Revalidate, Observer{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromCache || !result.FetchedAt.Equal(fetched) {
		t.Fatal("count check renewed stale user data")
	}
}

func TestLastSubscriberCancellationStopsNetworkWork(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	svc, _ := testService(t, fakeBackend{movies: func(ctx context.Context) ([]*domain.MediaItem, int, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil, 0, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := svc.Load(ctx, Resource{Kind: Movies}, Browse, Observer{}); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("orphaned network request")
	}
}

type failingCache struct {
	Cache
	fail bool
}

func (c *failingCache) Save(key string, entry domain.CachedList) error {
	if c.fail {
		return errors.New("disk full")
	}
	return c.Cache.Save(key, entry)
}

func TestFailedPersistenceCannotMakeOldPayloadFresh(t *testing.T) {
	var calls atomic.Int32
	svc, cache := testService(t, fakeBackend{
		movies: func(context.Context) ([]*domain.MediaItem, int, error) {
			calls.Add(1)
			return []*domain.MediaItem{{ID: "new"}, {ID: "second"}}, 2, nil
		},
		count: func(context.Context) (int, error) { return 2, nil },
	})
	r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
	cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "old"}}, FetchedAt: time.Now()})
	svc.cache = &failingCache{Cache: cache, fail: true}
	result, err := svc.Load(context.Background(), r, Revalidate, Observer{})
	if err != nil || result.Warning == nil || result.Items[0].GetID() != "new" {
		t.Fatalf("usable network result or persistence warning lost: %+v %v", result, err)
	}
	next, err := svc.Load(context.Background(), r, Browse, Observer{})
	if err != nil || next.Items[0].GetID() != "new" || calls.Load() != 2 {
		t.Fatal("old disk data was accepted as a fresh replacement")
	}
}

func TestLoadReportsNetworkWorkSeparatelyFromPayloadSource(t *testing.T) {
	for _, policy := range []Policy{Browse, Revalidate, Refresh} {
		t.Run(map[Policy]string{Browse: "cache", Revalidate: "count check", Refresh: "full fetch"}[policy], func(t *testing.T) {
			svc, cache := testService(t, fakeBackend{
				count: func(context.Context) (int, error) { return 1, nil },
				movies: func(context.Context) ([]*domain.MediaItem, int, error) {
					return []*domain.MediaItem{{ID: "movie"}}, 1, nil
				},
			})
			r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
			if err := cache.Save(r.Key(), domain.CachedList{Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}, FetchedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			network := 0
			result, err := svc.Load(context.Background(), r, policy, Observer{Network: func() { network++ }})
			if err != nil {
				t.Fatal(err)
			}
			wantNetwork := policy != Browse
			if result.Validated != wantNetwork || (network == 1) != wantNetwork {
				t.Fatalf("policy %v: network=%d validated=%v", policy, network, result.Validated)
			}
			if result.FromCache != (policy != Refresh) {
				t.Fatal("payload source conflated with validation")
			}
		})
	}
}
