package playlist

import (
	"context"
	"log/slog"

	"github.com/mmcdole/kino/internal/domain"
)

// Commands provides asynchronous operations (includes CRUD).
// Implements domain.PlaylistCommands.
type Commands struct {
	repo   domain.PlaylistRepository
	store  domain.Store
	logger *slog.Logger
}

// NewCommands creates a new Commands instance.
func NewCommands(repo domain.PlaylistRepository, store domain.Store, logger *slog.Logger) *Commands {
	if logger == nil {
		logger = slog.Default()
	}
	return &Commands{repo: repo, store: store, logger: logger}
}

func (c *Commands) FetchPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	playlists, err := c.repo.GetPlaylists(ctx)
	if err != nil {
		c.logger.Error("failed to fetch playlists", "error", err)
		return nil, err
	}
	if err := c.store.SavePlaylists(playlists); err != nil {
		c.logger.Error("failed to save playlists", "error", err)
	}
	c.logger.Debug("fetched playlists", "count", len(playlists))
	return playlists, nil
}

func (c *Commands) FetchPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error) {
	items, err := c.repo.GetPlaylistItems(ctx, playlistID)
	if err != nil {
		c.logger.Error("failed to fetch playlist items", "error", err, "playlistID", playlistID)
		return nil, err
	}
	if err := c.store.SavePlaylistItems(playlistID, items); err != nil {
		c.logger.Error("failed to save playlist items", "error", err, "playlistID", playlistID)
	}
	c.logger.Debug("fetched playlist items", "count", len(items), "playlistID", playlistID)
	return items, nil
}

func (c *Commands) SyncPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	playlists, err := c.repo.GetPlaylists(ctx)
	if err != nil {
		c.logger.Error("failed to fetch playlists", "error", err)
		return nil, err
	}
	if err := c.store.SavePlaylists(playlists); err != nil {
		c.logger.Error("failed to save playlists", "error", err)
	}

	// Fetch items for each playlist (two levels deep)
	for _, p := range playlists {
		items, err := c.repo.GetPlaylistItems(ctx, p.ID)
		if err != nil {
			c.logger.Debug("failed to fetch playlist items during sync", "error", err, "playlistID", p.ID)
			continue
		}
		if err := c.store.SavePlaylistItems(p.ID, items); err != nil {
			c.logger.Debug("failed to save playlist items during sync", "error", err, "playlistID", p.ID)
		}
	}

	c.logger.Debug("synced playlists", "count", len(playlists))
	return playlists, nil
}

func (c *Commands) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	playlist, err := c.repo.CreatePlaylist(ctx, title, itemIDs)
	if err != nil {
		c.logger.Error("failed to create playlist", "error", err, "title", title)
		return nil, err
	}
	c.InvalidatePlaylists()
	c.logger.Info("created playlist", "title", title, "id", playlist.ID)
	return playlist, nil
}

func (c *Commands) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	if err := c.repo.AddToPlaylist(ctx, playlistID, itemIDs); err != nil {
		c.logger.Error("failed to add to playlist", "error", err, "playlistID", playlistID)
		return err
	}
	c.InvalidatePlaylists()
	c.InvalidatePlaylistItems(playlistID)
	c.logger.Info("added items to playlist", "playlistID", playlistID, "count", len(itemIDs))
	return nil
}

func (c *Commands) RemoveFromPlaylist(ctx context.Context, playlistID, itemID string) error {
	if err := c.repo.RemoveFromPlaylist(ctx, playlistID, itemID); err != nil {
		c.logger.Error("failed to remove from playlist", "error", err, "playlistID", playlistID)
		return err
	}
	c.InvalidatePlaylists()
	c.InvalidatePlaylistItems(playlistID)
	c.logger.Info("removed item from playlist", "playlistID", playlistID, "itemID", itemID)
	return nil
}

func (c *Commands) DeletePlaylist(ctx context.Context, playlistID string) error {
	if err := c.repo.DeletePlaylist(ctx, playlistID); err != nil {
		c.logger.Error("failed to delete playlist", "error", err, "playlistID", playlistID)
		return err
	}
	c.InvalidatePlaylists()
	c.InvalidatePlaylistItems(playlistID)
	c.logger.Info("deleted playlist", "playlistID", playlistID)
	return nil
}

func (c *Commands) GetPlaylistMembership(ctx context.Context, itemID string) (map[string]bool, error) {
	playlists, ok := c.store.GetPlaylists()
	if !ok {
		// Cache miss - sync first
		var err error
		playlists, err = c.SyncPlaylists(ctx)
		if err != nil {
			return nil, err
		}
	}

	membership := make(map[string]bool)

	// Check each playlist's cached items
	for _, p := range playlists {
		items, ok := c.store.GetPlaylistItems(p.ID)
		if !ok {
			// Items not cached for this playlist, skip
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

func (c *Commands) InvalidatePlaylists() {
	c.store.InvalidatePlaylists()
}

func (c *Commands) InvalidatePlaylistItems(playlistID string) {
	c.store.InvalidatePlaylistItems(playlistID)
}
