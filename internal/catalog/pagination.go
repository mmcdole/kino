package catalog

import (
	"context"
	"github.com/mmcdole/kino/internal/domain"
)

// fetchAll is a generic pagination helper. Items are deduplicated by ID:
// offset pagination under concurrent server-side mutation can shift pages
// and repeat items, and duplicates would otherwise be cached as truth. An
// empty page always terminates, so a server reporting total=0 alongside a
// non-empty page still gets fully paginated rather than truncated.
func fetchAll[T domain.ListItem](
	ctx context.Context,
	fetch func(ctx context.Context, offset, limit int) ([]T, int, error),
	chunkSize int,
	onProgress domain.ProgressFunc,
) ([]T, error) {
	if chunkSize <= 0 {
		chunkSize = 50
	}

	var all []T
	seen := make(map[string]bool)
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		items, total, err := fetch(ctx, offset, chunkSize)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			id := item.GetID()
			if id != "" && seen[id] {
				continue
			}
			seen[id] = true
			all = append(all, item)
		}

		if onProgress != nil {
			onProgress(len(all), total)
		}

		if len(items) == 0 || (total > 0 && len(all) >= total) {
			break
		}
		offset += chunkSize
	}

	return all, nil
}
