package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

// Run with -mutexprofile to measure contention separately from total decode cost.
func BenchmarkCachedLibraries(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			cache, err := store.Open(b.TempDir(), "server", "user")
			if err != nil {
				b.Fatal(err)
			}
			defer cache.Close()
			svc := NewService(context.Background(), fakeBackend{}, cache)
			defer svc.Close()
			items := make([]domain.ListItem, size)
			for i := range items {
				items[i] = &domain.MediaItem{ID: fmt.Sprint(i), Title: "Movie title"}
			}
			r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
			if err := cache.Save(r.Key(), domain.CachedList{Items: items, FetchedAt: time.Now()}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := svc.Load(context.Background(), r, Browse, Observer{}); err != nil {
						b.Error(err)
					}
				}
			})
		})
	}
}
