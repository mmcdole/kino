package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// PlaylistService provides playlist management with memory caching
type PlaylistService struct {
	repo    domain.PlaylistRepository
	logger  *slog.Logger

	// Memory-only cache (playlists change frequently, no disk persistence)
	cacheMu   sync.RWMutex
	playlists []*domain.Playlist
	cacheTime time.Time
}

// NewPlaylistService creates a new playlist service
func NewPlaylistService(repo domain.PlaylistRepository, logger *slog.Logger) *PlaylistService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaylistService{
		repo:   repo,
		logger: logger,
	}
}

// GetPlaylists returns all playlists (cached)
func (s *PlaylistService) GetPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	s.cacheMu.RLock()
	if s.playlists != nil && time.Since(s.cacheTime) < 30*time.Second {
		cached := s.playlists
		s.cacheMu.RUnlock()
		return cached, nil
	}
	s.cacheMu.RUnlock()

	// Fetch from server
	playlists, err := s.repo.GetPlaylists(ctx)
	if err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.playlists = playlists
	s.cacheTime = time.Now()
	s.cacheMu.Unlock()

	return playlists, nil
}

// GetPlaylistItems returns items in a playlist
func (s *PlaylistService) GetPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error) {
	return s.repo.GetPlaylistItems(ctx, playlistID)
}

// CreatePlaylist creates a new playlist
func (s *PlaylistService) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	playlist, err := s.repo.CreatePlaylist(ctx, title, itemIDs)
	if err != nil {
		return nil, err
	}

	s.InvalidateCache()
	return playlist, nil
}

// AddToPlaylist adds items to an existing playlist
func (s *PlaylistService) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	err := s.repo.AddToPlaylist(ctx, playlistID, itemIDs)
	if err != nil {
		return err
	}

	s.InvalidateCache()
	return nil
}

// RemoveFromPlaylist removes an item from a playlist
func (s *PlaylistService) RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error {
	err := s.repo.RemoveFromPlaylist(ctx, playlistID, itemID)
	if err != nil {
		return err
	}

	s.InvalidateCache()
	return nil
}

// DeletePlaylist deletes a playlist
func (s *PlaylistService) DeletePlaylist(ctx context.Context, playlistID string) error {
	err := s.repo.DeletePlaylist(ctx, playlistID)
	if err != nil {
		return err
	}

	s.InvalidateCache()
	return nil
}

// GetPlaylistMembership returns a map of playlist IDs where the item is a member
// Key is playlist ID, value is true if item is in that playlist
func (s *PlaylistService) GetPlaylistMembership(ctx context.Context, itemID string) (map[string]bool, error) {
	playlists, err := s.GetPlaylists(ctx)
	if err != nil {
		return nil, err
	}

	membership := make(map[string]bool)

	// Check each playlist for the item
	for _, p := range playlists {
		items, err := s.repo.GetPlaylistItems(ctx, p.ID)
		if err != nil {
			s.logger.Warn("failed to get playlist items for membership check",
				"playlistID", p.ID, "error", err)
			continue
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

// InvalidateCache clears the playlist cache
func (s *PlaylistService) InvalidateCache() {
	s.cacheMu.Lock()
	s.playlists = nil
	s.cacheTime = time.Time{}
	s.cacheMu.Unlock()
}

// GetCachedPlaylists returns cached playlists without fetching (for UI)
func (s *PlaylistService) GetCachedPlaylists() []*domain.Playlist {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.playlists
}
