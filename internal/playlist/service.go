package playlist

import (
	"context"
	"log/slog"

	"github.com/mmcdole/kino/internal/domain"
)

// Service orchestrates playlist client + store + CRUD operations.
type Service struct {
	client domain.PlaylistClient
	store  domain.Store
	logger *slog.Logger
}

// NewService creates a new playlist service.
func NewService(client domain.PlaylistClient, store domain.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{client: client, store: store, logger: logger}
}

func (s *Service) FetchPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	playlists, err := s.client.GetPlaylists(ctx)
	if err != nil {
		s.logger.Error("failed to fetch playlists", "error", err)
		return nil, err
	}
	if err := s.store.SavePlaylists(playlists); err != nil {
		s.logger.Error("failed to save playlists", "error", err)
	}
	s.logger.Debug("fetched playlists", "count", len(playlists))
	return playlists, nil
}

func (s *Service) FetchPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error) {
	items, err := s.client.GetPlaylistItems(ctx, playlistID)
	if err != nil {
		s.logger.Error("failed to fetch playlist items", "error", err, "playlistID", playlistID)
		return nil, err
	}
	if err := s.store.SavePlaylistItems(playlistID, items); err != nil {
		s.logger.Error("failed to save playlist items", "error", err, "playlistID", playlistID)
	}
	s.logger.Debug("fetched playlist items", "count", len(items), "playlistID", playlistID)
	return items, nil
}

func (s *Service) SyncPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	playlists, err := s.client.GetPlaylists(ctx)
	if err != nil {
		s.logger.Error("failed to fetch playlists", "error", err)
		return nil, err
	}
	if err := s.store.SavePlaylists(playlists); err != nil {
		s.logger.Error("failed to save playlists", "error", err)
	}

	// Fetch items for each playlist (two levels deep)
	for _, p := range playlists {
		items, err := s.client.GetPlaylistItems(ctx, p.ID)
		if err != nil {
			s.logger.Debug("failed to fetch playlist items during sync", "error", err, "playlistID", p.ID)
			continue
		}
		if err := s.store.SavePlaylistItems(p.ID, items); err != nil {
			s.logger.Debug("failed to save playlist items during sync", "error", err, "playlistID", p.ID)
		}
	}

	s.logger.Debug("synced playlists", "count", len(playlists))
	return playlists, nil
}

func (s *Service) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	playlist, err := s.client.CreatePlaylist(ctx, title, itemIDs)
	if err != nil {
		s.logger.Error("failed to create playlist", "error", err, "title", title)
		return nil, err
	}
	s.InvalidatePlaylists()
	s.logger.Info("created playlist", "title", title, "id", playlist.ID)
	return playlist, nil
}

func (s *Service) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	if err := s.client.AddToPlaylist(ctx, playlistID, itemIDs); err != nil {
		s.logger.Error("failed to add to playlist", "error", err, "playlistID", playlistID)
		return err
	}
	s.InvalidatePlaylists()
	s.InvalidatePlaylistItems(playlistID)
	s.logger.Info("added items to playlist", "playlistID", playlistID, "count", len(itemIDs))
	return nil
}

func (s *Service) RemoveFromPlaylist(ctx context.Context, playlistID, itemID string) error {
	if err := s.client.RemoveFromPlaylist(ctx, playlistID, itemID); err != nil {
		s.logger.Error("failed to remove from playlist", "error", err, "playlistID", playlistID)
		return err
	}
	s.InvalidatePlaylists()
	s.InvalidatePlaylistItems(playlistID)
	s.logger.Info("removed item from playlist", "playlistID", playlistID, "itemID", itemID)
	return nil
}

func (s *Service) DeletePlaylist(ctx context.Context, playlistID string) error {
	if err := s.client.DeletePlaylist(ctx, playlistID); err != nil {
		s.logger.Error("failed to delete playlist", "error", err, "playlistID", playlistID)
		return err
	}
	s.InvalidatePlaylists()
	s.InvalidatePlaylistItems(playlistID)
	s.logger.Info("deleted playlist", "playlistID", playlistID)
	return nil
}

func (s *Service) GetPlaylistMembership(ctx context.Context, itemID string) (map[string]bool, error) {
	playlists, ok := s.store.GetPlaylists()
	if !ok {
		// Cache miss - sync first
		var err error
		playlists, err = s.SyncPlaylists(ctx)
		if err != nil {
			return nil, err
		}
	}

	membership := make(map[string]bool)

	// Check each playlist's items, fetching from server if not cached
	for _, p := range playlists {
		items, ok := s.store.GetPlaylistItems(p.ID)
		if !ok {
			var err error
			items, err = s.FetchPlaylistItems(ctx, p.ID)
			if err != nil {
				s.logger.Error("failed to fetch playlist items for membership check", "error", err, "playlistID", p.ID)
				continue
			}
		}

		for _, item := range items {
			if item.ID == itemID {
				membership[p.ID] = true
				break
			}
		}
	}

	return membership, nil
}

func (s *Service) InvalidatePlaylists() {
	s.store.InvalidatePlaylists()
}

func (s *Service) InvalidatePlaylistItems(playlistID string) {
	s.store.InvalidatePlaylistItems(playlistID)
}
