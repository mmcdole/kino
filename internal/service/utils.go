package service

import (
	"context"
)

const defaultChunkSize = 50

// fetchAll is a private helper that handles pagination.
// Only used by LibraryService - not exported.
func fetchAll[T any](
	ctx context.Context,
	fetch func(ctx context.Context, offset, limit int) ([]T, int, error),
	chunkSize int,
	onProgress func(loaded, total int),
) ([]T, error) {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	var all []T
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

		all = append(all, items...)

		if onProgress != nil {
			onProgress(len(all), total)
		}

		if len(all) >= total || len(items) == 0 {
			break
		}
		offset += chunkSize
	}

	return all, nil
}
