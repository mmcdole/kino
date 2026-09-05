package catalog

import (
	"context"
	"errors"
	"github.com/mmcdole/kino/internal/domain"
	"testing"
)

func TestPaginationDeduplicatesAndHandlesUnknownTotal(t *testing.T) {
	for _, total := range []int{0, 4} {
		pages := [][]*domain.MediaItem{{{ID: "a"}, {ID: "b"}}, {{ID: "b"}, {ID: "c"}}}
		calls := 0
		items, err := fetchAll(context.Background(), func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			calls++
			if offset/limit >= len(pages) {
				return nil, total, nil
			}
			return pages[offset/limit], total, nil
		}, 2, nil)
		if err != nil || len(items) != 3 || calls != 3 {
			t.Fatalf("total=%d: items=%d calls=%d err=%v", total, len(items), calls, err)
		}
	}
}
func TestPaginationCancellationDoesNotReturnPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	items, err := fetchAll(ctx, func(context.Context, int, int) ([]*domain.MediaItem, int, error) {
		calls++
		return []*domain.MediaItem{{ID: "a"}}, 10, nil
	}, 2, func(int, int) { cancel() })
	if !errors.Is(err, context.Canceled) || items != nil || calls != 1 {
		t.Fatalf("canceled pagination returned partial success: %v %v", items, err)
	}
}
