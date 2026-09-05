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
	cache, err := store.Open("", "", "")
	if err != nil {
		t.Fatal(err)
	}
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
	go func() { _, _ = svc.Load(context.Background(), a, Browse, Observer{}) }()
	<-gate.started
	done := make(chan error, 1)
	go func() { _, err := svc.Load(context.Background(), b, Browse, Observer{}); done <- err }()
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
	cache, err := store.Open("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	if err := cache.Save(r.Key(), domain.CachedList{FetchedAt: time.Now(), Items: []domain.ListItem{&domain.MediaItem{ID: "movie"}}}); err != nil {
		t.Fatal(err)
	}
	gate := &gatedCache{Cache: cache, key: r.Key(), started: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(context.Background(), watchBackend{watch: func(context.Context) error { return nil }}, gate)
	defer svc.Close()
	done := make(chan Snapshot, 1)
	go func() { snapshot, _ := svc.Load(context.Background(), r, Browse, Observer{}); done <- snapshot }()
	<-gate.started
	change, err := svc.Mutate(context.Background(), Mutation{Kind: Watch, ItemID: "movie", LibraryID: "a", Played: true})
	close(gate.release)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := <-done
	if len(change.Snapshots) != 1 || !snapshot.Items[0].(*domain.MediaItem).IsPlayed || snapshot.Revision != change.Revisions[r.Key()] {
		t.Fatal("cache payload decoded before the mutation escaped its revision fence")
	}
}

func TestCachedObserverCannotMutateValidatedPayload(t *testing.T) {
	release := make(chan struct{})
	svc, cache := testService(t, fakeBackend{count: func(context.Context) (int, error) {
		<-release
		return 1, nil
	}})
	r := Resource{Kind: Movies, ID: "a", LibraryID: "a"}
	if err := cache.Save(r.Key(), domain.CachedList{FetchedAt: time.Now(), Items: []domain.ListItem{&domain.MediaItem{ID: "movie", Title: "Original"}}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Load(context.Background(), r, Revalidate, Observer{Cached: func(snapshot Snapshot) {
		snapshot.Items[0].(*domain.MediaItem).Title = "Observer edit"
		close(release)
	}})
	if err != nil || snapshot.Items[0].GetTitle() != "Original" {
		t.Fatal("cached observation shared mutable entities with the validated result")
	}
}
