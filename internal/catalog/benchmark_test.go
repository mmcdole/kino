package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/store"
)

// Run with -mutexprofile to measure contention separately from total decode cost.
func BenchmarkCachedLibraries(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			cache, err := store.OpenDisk(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer cache.Close()
			svc := NewService(context.Background(), fakeBackend{}, cache)
			defer svc.Close()
			items := make([]media.ListItem, size)
			for i := range items {
				items[i] = &media.Item{ID: fmt.Sprint(i), Title: "Movie title"}
			}
			r := Resource{Kind: Movies, ID: "lib", LibraryID: "lib"}
			if err := cache.Save(r.Key(), media.CachedList{Items: items, FetchedAt: time.Now()}); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := svc.Load(context.Background(), r, Browse); err != nil {
						b.Error(err)
					}
				}
			})
		})
	}
}
