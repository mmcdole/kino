package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

const (
	defaultTimeout = 30 * time.Second
	userAgent      = "Kino/1.0"
	clientID       = "kino-tui-client"
)

// Client implements domain.LibraryRepository, domain.SearchRepository,
// domain.MetadataRepository, and domain.Scrobbler for Plex
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new Plex API client
func NewClient(baseURL, token string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// SetToken updates the authentication token
func (c *Client) SetToken(token string) {
	c.token = token
}

// doRequest performs an authenticated HTTP request
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	if query != nil {
		reqURL = fmt.Sprintf("%s?%s", reqURL, query.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	c.logger.Debug("plex request", "method", method, "url", reqURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("plex request failed", "error", err)
		return nil, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, domain.ErrAuthFailed
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Error("plex request error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return body, nil
}

// parseResponse parses a JSON response into APIResponse
func (c *Client) parseResponse(body []byte) (*MediaContainer, error) {
	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// Log raw body and full error to file for debugging
		errMsg := fmt.Sprintf("ERROR: %v\n\nBODY:\n%s", err, string(body))
		_ = os.WriteFile("/tmp/plex_parse_error.txt", []byte(errMsg), 0644)
		c.logger.Error("JSON parse error", "error", err, "bodyLen", len(body))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp.MediaContainer, nil
}

// GetLibraries returns all available libraries
func (c *Client) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	body, err := c.doRequest(ctx, http.MethodGet, "/library/sections", nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapLibraries(container.Directory), nil
}

// GetLibraryDetails returns details for a specific library (lightweight)
func (c *Client) GetLibraryDetails(ctx context.Context, libID string) (*domain.Library, error) {
	path := fmt.Sprintf("/library/sections/%s", libID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	if len(container.Directory) == 0 {
		return nil, domain.ErrItemNotFound
	}

	lib := MapLibrary(container.Directory[0])
	if lib == nil {
		return nil, domain.ErrItemNotFound
	}
	return lib, nil
}

// GetMovies returns movies from a movie library with pagination support
// Returns (items, totalSize, error)
// Note: If limit=0, Plex uses its default page size (typically 50-100).
// The SERVICE layer is responsible for pagination loops if "all" items are needed.
func (c *Client) GetMovies(ctx context.Context, libID string, offset, limit int) ([]*domain.MediaItem, int, error) {
	query := url.Values{}
	query.Set("X-Plex-Container-Start", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("X-Plex-Container-Size", strconv.Itoa(limit))
	}
	// NO hardcoded fallback - let Plex use its natural default if limit=0

	path := fmt.Sprintf("/library/sections/%s/all", libID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, 0, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, 0, err
	}

	totalSize := container.TotalSize
	if totalSize == 0 {
		totalSize = container.Size // Fallback if TotalSize not provided
	}

	return MapMovies(container.Metadata, c.baseURL), totalSize, nil
}

// GetShows returns TV shows from a show library with pagination support
// Returns (items, totalSize, error)
// Note: If limit=0, Plex uses its default page size (typically 50-100).
// The SERVICE layer is responsible for pagination loops if "all" items are needed.
func (c *Client) GetShows(ctx context.Context, libID string, offset, limit int) ([]*domain.Show, int, error) {
	query := url.Values{}
	query.Set("X-Plex-Container-Start", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("X-Plex-Container-Size", strconv.Itoa(limit))
	}
	// NO hardcoded fallback - let Plex use its natural default if limit=0

	path := fmt.Sprintf("/library/sections/%s/all", libID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, 0, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, 0, err
	}

	totalSize := container.TotalSize
	if totalSize == 0 {
		totalSize = container.Size // Fallback if TotalSize not provided
	}

	return MapShows(container.Metadata, c.baseURL), totalSize, nil
}

const defaultBatchSize = 100

// GetAllMovies fetches all movies, handling pagination internally
func (c *Client) GetAllMovies(ctx context.Context, libID string) ([]*domain.MediaItem, error) {
	var allMovies []*domain.MediaItem
	offset := 0

	for {
		movies, total, err := c.GetMovies(ctx, libID, offset, defaultBatchSize)
		if err != nil {
			return nil, err
		}

		allMovies = append(allMovies, movies...)

		if len(allMovies) >= total || len(movies) == 0 {
			break
		}
		offset += defaultBatchSize
	}

	return allMovies, nil
}

// GetAllShows fetches all shows, handling pagination internally
func (c *Client) GetAllShows(ctx context.Context, libID string) ([]*domain.Show, error) {
	var allShows []*domain.Show
	offset := 0

	for {
		shows, total, err := c.GetShows(ctx, libID, offset, defaultBatchSize)
		if err != nil {
			return nil, err
		}

		allShows = append(allShows, shows...)

		if len(allShows) >= total || len(shows) == 0 {
			break
		}
		offset += defaultBatchSize
	}

	return allShows, nil
}

// GetMoviesWithProgress fetches movies with progress callback for UI updates
func (c *Client) GetMoviesWithProgress(ctx context.Context, libID string, progress func([]*domain.MediaItem, int, int)) error {
	offset := 0
	loaded := 0

	for {
		movies, total, err := c.GetMovies(ctx, libID, offset, defaultBatchSize)
		if err != nil {
			return err
		}

		loaded += len(movies)
		progress(movies, loaded, total)

		if loaded >= total || len(movies) == 0 {
			break
		}
		offset += defaultBatchSize
	}

	return nil
}

// GetShowsWithProgress fetches shows with progress callback for UI updates
func (c *Client) GetShowsWithProgress(ctx context.Context, libID string, progress func([]*domain.Show, int, int)) error {
	offset := 0
	loaded := 0

	for {
		shows, total, err := c.GetShows(ctx, libID, offset, defaultBatchSize)
		if err != nil {
			return err
		}

		loaded += len(shows)
		progress(shows, loaded, total)

		if loaded >= total || len(shows) == 0 {
			break
		}
		offset += defaultBatchSize
	}

	return nil
}

// GetSeasons returns all seasons for a TV show
func (c *Client) GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error) {
	path := fmt.Sprintf("/library/metadata/%s/children", showID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapSeasons(container.Metadata, c.baseURL), nil
}

// GetEpisodes returns all episodes for a season
func (c *Client) GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error) {
	path := fmt.Sprintf("/library/metadata/%s/children", seasonID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapEpisodes(container.Metadata, c.baseURL), nil
}

// GetRecentlyAdded returns recently added items from a library
func (c *Client) GetRecentlyAdded(ctx context.Context, libID string, limit int) ([]*domain.MediaItem, error) {
	query := url.Values{}
	query.Set("X-Plex-Container-Size", strconv.Itoa(limit))

	path := fmt.Sprintf("/library/sections/%s/recentlyAdded", libID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapOnDeck(container.Metadata, c.baseURL), nil
}

// Search performs a search across all libraries
func (c *Client) Search(ctx context.Context, query string) ([]domain.MediaItem, error) {
	params := url.Values{}
	params.Set("query", query)

	body, err := c.doRequest(ctx, http.MethodGet, "/search", params)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	// Convert pointer slice to value slice for the Search interface
	ptrs := MapOnDeck(container.Metadata, c.baseURL)
	results := make([]domain.MediaItem, len(ptrs))
	for i, p := range ptrs {
		results[i] = *p
	}
	return results, nil
}

// ResolvePlayableURL returns a direct playback URL for an item
func (c *Client) ResolvePlayableURL(ctx context.Context, itemID string) (string, error) {
	item, err := c.GetMediaItem(ctx, itemID)
	if err != nil {
		return "", err
	}

	if item.MediaURL == "" {
		return "", domain.ErrItemNotFound
	}

	// Add token to URL for direct play
	return fmt.Sprintf("%s?X-Plex-Token=%s", item.MediaURL, c.token), nil
}

// GetMediaItem returns detailed metadata for a specific item
func (c *Client) GetMediaItem(ctx context.Context, itemID string) (*domain.MediaItem, error) {
	path := fmt.Sprintf("/library/metadata/%s", itemID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	if len(container.Metadata) == 0 {
		return nil, domain.ErrItemNotFound
	}

	item := MapMediaItem(container.Metadata[0], c.baseURL)
	return &item, nil
}

// GetNextEpisode returns the next episode in a series
func (c *Client) GetNextEpisode(ctx context.Context, episodeID string) (*domain.MediaItem, error) {
	// First get the current episode to find its position
	current, err := c.GetMediaItem(ctx, episodeID)
	if err != nil {
		return nil, err
	}

	if current.Type != domain.MediaTypeEpisode {
		return nil, domain.ErrNoNextEpisode
	}

	// Get all episodes in the season
	episodes, err := c.GetEpisodes(ctx, current.ParentID)
	if err != nil {
		return nil, err
	}

	// Find the next episode
	for i, ep := range episodes {
		if ep.ID == episodeID && i+1 < len(episodes) {
			return episodes[i+1], nil
		}
	}

	// No next episode in this season, try next season
	// This would require additional logic to fetch next season
	return nil, domain.ErrNoNextEpisode
}

// MarkPlaying indicates playback has started
func (c *Client) MarkPlaying(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)
	query.Set("state", "playing")

	_, err := c.doRequest(ctx, http.MethodGet, "/:/timeline", query)
	return err
}

// ReportProgress updates the watch position
func (c *Client) ReportProgress(ctx context.Context, itemID string, status domain.PlayerStatus) error {
	query := url.Values{}
	query.Set("key", itemID)
	query.Set("time", strconv.FormatInt(status.CurrentTime.Milliseconds(), 10))
	query.Set("duration", strconv.FormatInt(status.TotalTime.Milliseconds(), 10))
	if status.IsPaused {
		query.Set("state", "paused")
	} else {
		query.Set("state", "playing")
	}

	_, err := c.doRequest(ctx, http.MethodGet, "/:/timeline", query)
	return err
}

// MarkPlayed marks an item as fully watched
func (c *Client) MarkPlayed(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)

	_, err := c.doRequest(ctx, http.MethodGet, "/:/scrobble", query)
	return err
}

// MarkUnplayed marks an item as unwatched
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)

	_, err := c.doRequest(ctx, http.MethodGet, "/:/unscrobble", query)
	return err
}

// GetPlaylists returns all user playlists
func (c *Client) GetPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	body, err := c.doRequest(ctx, http.MethodGet, "/playlists", nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapPlaylists(container.Metadata, c.baseURL), nil
}

// GetPlaylistItems returns all items in a playlist
func (c *Client) GetPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error) {
	path := fmt.Sprintf("/playlists/%s/items", playlistID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapOnDeck(container.Metadata, c.baseURL), nil
}

// CreatePlaylist creates a new playlist with the given title and optional initial items
func (c *Client) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	query := url.Values{}
	query.Set("type", "video")
	query.Set("title", title)
	query.Set("smart", "0")

	// If items are provided, build the URI parameter
	if len(itemIDs) > 0 {
		// Plex expects a URI in the format: server://machineID/com.plexapp.plugins.library/library/metadata/itemID
		// For simplicity, we'll create an empty playlist and add items separately
	}

	reqURL := fmt.Sprintf("%s/playlists?%s", c.baseURL, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("plex create playlist failed", "error", err)
		return nil, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		c.logger.Error("plex create playlist error", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("failed to create playlist: status %d", resp.StatusCode)
	}

	container, err := c.parseResponse(respBody)
	if err != nil {
		return nil, err
	}

	if len(container.Metadata) == 0 {
		return nil, fmt.Errorf("no playlist returned from server")
	}

	playlists := MapPlaylists(container.Metadata, c.baseURL)
	if len(playlists) == 0 {
		return nil, fmt.Errorf("failed to parse created playlist")
	}

	// If items were provided, add them to the playlist
	if len(itemIDs) > 0 {
		if err := c.AddToPlaylist(ctx, playlists[0].ID, itemIDs); err != nil {
			// Playlist was created but items couldn't be added
			c.logger.Warn("playlist created but failed to add items", "error", err)
		}
	}

	return playlists[0], nil
}

// AddToPlaylist adds items to an existing playlist
func (c *Client) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}

	// Build the URI for each item
	// Plex expects: library://libraryUUID/item//itemID
	// We use a simplified format that works: library:///item/itemID
	uris := make([]string, len(itemIDs))
	for i, id := range itemIDs {
		uris[i] = fmt.Sprintf("library:///item/%%2Flibrary%%2Fmetadata%%2F%s", id)
	}

	query := url.Values{}
	query.Set("uri", uris[0]) // Plex accepts one URI at a time in most cases

	path := fmt.Sprintf("/playlists/%s/items", playlistID)
	reqURL := fmt.Sprintf("%s%s?%s", c.baseURL, path, query.Encode())

	// Add items one at a time for reliability
	for _, itemID := range itemIDs {
		itemQuery := url.Values{}
		itemQuery.Set("uri", fmt.Sprintf("library:///item/%%2Flibrary%%2Fmetadata%%2F%s", itemID))

		itemURL := fmt.Sprintf("%s%s?%s", c.baseURL, path, itemQuery.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, itemURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Plex-Token", c.token)
		req.Header.Set("X-Plex-Client-Identifier", clientID)
		req.Header.Set("X-Plex-Product", "Kino")
		req.Header.Set("X-Plex-Version", "1.0")
		req.Header.Set("User-Agent", userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logger.Error("plex add to playlist failed", "error", err)
			return domain.ErrServerOffline
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("failed to add item to playlist: status %d", resp.StatusCode)
		}
	}

	_ = reqURL // unused in loop version
	return nil
}

// RemoveFromPlaylist removes an item from a playlist
func (c *Client) RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error {
	path := fmt.Sprintf("/playlists/%s/items/%s", playlistID, itemID)
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("plex remove from playlist failed", "error", err)
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to remove item from playlist: status %d", resp.StatusCode)
	}

	return nil
}

// DeletePlaylist deletes a playlist
func (c *Client) DeletePlaylist(ctx context.Context, playlistID string) error {
	path := fmt.Sprintf("/playlists/%s", playlistID)
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("plex delete playlist failed", "error", err)
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete playlist: status %d", resp.StatusCode)
	}

	return nil
}
