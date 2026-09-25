package catalog

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

type gatedCache struct {
	Cache
	key              string
	started, release chan struct{}
	blocked          atomic.Bool
}

func (c *gatedCache) Load(key string) (domain.CachedList, bool) {
	entry, ok := c.Cache.Load(key)
	if key == c.key && c.blocked.CompareAndSwap(false, true) {
		close(c.started)
		<-c.release
	}
	return entry, ok
}

func TestCacheDecodeDoesNotBlockUnrelatedLoads(t *testing.T) {
	cache := store.NewMemory()
	defer cache.Close()
	a := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	b := Resource{Kind: Movies, ID: "b", LibraryID: "b"}
	for _, r := range []Resource{a, b} {
		if err := cache.Save(r.Key(), domain.CachedList{FetchedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	gate := &gatedCache{Cache: cache, key: a.Key(), started: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(context.Background(), fakeBackend{}, gate)
	defer svc.Close()
	defer close(gate.release)
	go func() { _, _ = svc.Load(context.Background(), a, Browse) }()
	<-gate.started
	done := make(chan error, 1)
	go func() { _, err := svc.Load(context.Background(), b, Browse); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("decoding one collection blocked another collection's cache hit")
	}
}

func TestMutationDuringCacheDecodeRejectsOldPayload(t *testing.T) {
	cache := store.NewMemory()
	defer cache.Close()
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	if err := cache.Save(r.Key(), domain.CachedList{FetchedAt: time.Now(), Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}}); err != nil {
		t.Fatal(err)
	}
	gate := &gatedCache{Cache: cache, key: r.Key(), started: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(context.Background(), watchBackend{watch: func(context.Context) error { return nil }}, gate)
	defer svc.Close()
	done := make(chan Snapshot, 1)
	go func() { snapshot, _ := svc.Load(context.Background(), r, Browse); done <- snapshot }()
	<-gate.started
	change, err := svc.Mutate(context.Background(), Mutation{Kind: Watch, ItemID: "movie", LibraryID: "a", Played: true})
	close(gate.release)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := <-done
	if !change.Applied || !snapshot.Items[0].(*domain.MediaItem).IsPlayed || snapshot.Revision != svc.state(r).Snapshot.Revision {
		t.Fatal("cache payload decoded before the mutation escaped its revision fence")
	}
}

type slowSaveCache struct {
	Cache
	started, release chan struct{}
}

func (c *slowSaveCache) Save(key string, l domain.CachedList) error {
	close(c.started)
	<-c.release
	return c.Cache.Save(key, l)
}

func TestSlowCacheWriteDoesNotBlockCacheHits(t *testing.T) {
	cache := store.NewMemory()
	a := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	b := Resource{Kind: Movies, ID: "b", LibraryID: "b"}
	if err := cache.Save(b.Key(), domain.CachedList{FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	slow := &slowSaveCache{Cache: cache, started: make(chan struct{}), release: make(chan struct{})}
	backend := fakeBackend{movies: func(context.Context) ([]*domain.MediaItem, int, error) {
		return []*domain.MediaItem{{ID: "m"}}, 1, nil
	}}
	svc := NewService(context.Background(), backend, slow)
	defer svc.Close()
	defer close(slow.release)
	go func() { _, _ = svc.Load(context.Background(), a, Refresh) }()
	<-slow.started

	done := make(chan error, 1)
	go func() { _, err := svc.Load(context.Background(), b, Browse); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("a cache hit waited on another collection's disk write")
	}
}
