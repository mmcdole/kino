package catalog

import (
	"context"
	"fmt"

	"github.com/mmcdole/kino/internal/domain"
)

func (s *Service) fetch(ctx context.Context, r Resource, progress domain.ProgressFunc) ([]domain.ListItem, error) {
	switch r.Kind {
	case Libraries:
		libs, err := s.backend.GetLibraries(ctx)
		items := make([]domain.ListItem, len(libs))
		for i := range libs {
			items[i] = &libs[i]
		}
		return items, err
	case Movies:
		items, err := fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			return s.backend.GetMovies(ctx, r.LibraryID, offset, limit)
		}, 50, progress)
		return domain.ListItems(items), err
	case Shows:
		items, err := fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]*domain.Show, int, error) {
			return s.backend.GetShows(ctx, r.LibraryID, offset, limit)
		}, 50, progress)
		return domain.ListItems(items), err
	case Mixed:
		return fetchAll(ctx, func(ctx context.Context, offset, limit int) ([]domain.ListItem, int, error) {
			return s.backend.GetMixedContent(ctx, r.LibraryID, offset, limit)
		}, 50, progress)
	case Seasons:
		items, err := s.backend.GetSeasons(ctx, r.ShowID)
		return domain.ListItems(items), err
	case Episodes:
		items, err := s.backend.GetEpisodes(ctx, r.ID)
		return domain.ListItems(items), err
	case Playlists:
		items, err := s.backend.GetPlaylists(ctx)
		return domain.ListItems(items), err
	case PlaylistItems:
		items, err := s.backend.GetPlaylistItems(ctx, r.ID)
		return domain.ListItems(items), err
	default:
		return nil, fmt.Errorf("unknown resource kind %d", r.Kind)
	}
}
