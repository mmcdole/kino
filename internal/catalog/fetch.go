package catalog

import (
	"context"
	"fmt"

	"github.com/mmcdole/kino/internal/media"
)

// pageSize is how many library items each paginated request asks for.
const pageSize = 50

// fetch downloads r's complete item list from the server.
func (s *Service) fetch(ctx context.Context, r Resource, progress progressFunc) ([]media.ListItem, error) {
	switch r.Kind {
	case Libraries:
		libs, err := s.backend.GetLibraries(ctx)
		return media.LibraryItems(libs), err
	case Movies:
		items, err := fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]*media.Item, int, error) {
			return s.backend.GetMovies(ctx, r.LibraryID, offset, limit)
		}, pageSize, progress)
		return media.ListItems(items), err
	case Shows:
		items, err := fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]*media.Show, int, error) {
			return s.backend.GetShows(ctx, r.LibraryID, offset, limit)
		}, pageSize, progress)
		return media.ListItems(items), err
	case Mixed:
		return fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]media.ListItem, int, error) {
			return s.backend.GetMixedContent(ctx, r.LibraryID, offset, limit)
		}, pageSize, progress)
	case Seasons:
		items, err := s.backend.GetSeasons(ctx, r.ShowID)
		return media.ListItems(items), err
	case Episodes:
		items, err := s.backend.GetEpisodes(ctx, r.ID)
		return media.ListItems(items), err
	case Playlists:
		items, err := s.backend.GetPlaylists(ctx)
		return media.ListItems(items), err
	case PlaylistItems:
		items, err := s.backend.GetPlaylistItems(ctx, r.ID)
		return media.ListItems(items), err
	default:
		return nil, fmt.Errorf("unknown resource kind %d", r.Kind)
	}
}

// progressFunc reports pagination progress: (50, 500), (100, 500), ...
type progressFunc func(loaded, total int)

// fetchAll is a generic pagination helper. Items are deduplicated by ID:
// offset pagination under concurrent server-side mutation can shift pages
// and repeat items, and duplicates would otherwise be cached as truth. An
// empty page always terminates, so a server reporting total=0 alongside a
// non-empty page still gets fully paginated rather than truncated.
func fetchAll[T media.ListItem](
	ctx context.Context,
	fetch func(ctx context.Context, offset, limit int) ([]T, int, error),
	chunkSize int,
	onProgress progressFunc,
) ([]T, error) {
	var all []T
	seen := make(map[string]bool)
	offset := 0

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
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
		offset += len(items)
	}

	return all, nil
}
