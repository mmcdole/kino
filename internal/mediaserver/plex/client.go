package plex

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	baseURL           string
	token             string
	machineIdentifier string // fetched from /identity on init
	httpClient        *http.Client
	logger            *slog.Logger
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

// FetchIdentity fetches and stores the server's machineIdentifier
func (c *Client) FetchIdentity(ctx context.Context) error {
	reqURL := fmt.Sprintf("%s/identity", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse XML response
	var identity struct {
		XMLName           xml.Name `xml:"MediaContainer"`
		MachineIdentifier string   `xml:"machineIdentifier,attr"`
	}
	if err := xml.Unmarshal(body, &identity); err != nil {
		return err
	}

	c.machineIdentifier = identity.MachineIdentifier
	return nil
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

// GetMixedContent returns paginated content (movies AND shows) from a library.
// Note: Plex doesn't truly support "mixed" libraries at the API level like Jellyfin,
// so this method fetches all items and returns both types. For pure movie or show
// libraries, this still works but is less efficient than GetMovies/GetShows.
func (c *Client) GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]domain.ListItem, int, error) {
	query := url.Values{}
	query.Set("X-Plex-Container-Start", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("X-Plex-Container-Size", strconv.Itoa(limit))
	}

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
		totalSize = container.Size
	}

	return MapLibraryContent(container.Metadata, c.baseURL), totalSize, nil
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

// Search performs a search across all libraries
func (c *Client) Search(ctx context.Context, query string) ([]*domain.MediaItem, error) {
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

	return MapOnDeck(container.Metadata, c.baseURL), nil
}

// ResolvePlayableURL returns a direct playback URL for an item
func (c *Client) ResolvePlayableURL(ctx context.Context, itemID string) (string, error) {
	path := fmt.Sprintf("/library/metadata/%s", itemID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return "", err
	}

	if len(container.Metadata) == 0 {
		return "", domain.ErrItemNotFound
	}

	// Extract media URL from the metadata
	m := container.Metadata[0]
	if len(m.Media) == 0 || len(m.Media[0].Part) == 0 {
		return "", domain.ErrItemNotFound
	}

	mediaPath := m.Media[0].Part[0].Key
	if mediaPath == "" {
		return "", domain.ErrItemNotFound
	}

	// Add token to URL for direct play
	return fmt.Sprintf("%s%s?X-Plex-Token=%s", c.baseURL, mediaPath, c.token), nil
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

// CreatePlaylist creates a new playlist with the given title and initial items.
// Plex does not support creating empty playlists, so at least one itemID is required.
func (c *Client) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	if len(itemIDs) == 0 {
		return nil, fmt.Errorf("plex does not support creating empty playlists")
	}

	// Build canonical URI with machineIdentifier
	ids := strings.Join(itemIDs, ",")
	uri := fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s",
		c.machineIdentifier, ids)

	query := url.Values{}
	query.Set("type", "video")
	query.Set("title", title)
	query.Set("smart", "0")
	query.Set("uri", uri)

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

	return playlists[0], nil
}

// AddToPlaylist adds items to an existing playlist
func (c *Client) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}

	path := fmt.Sprintf("/playlists/%s/items", playlistID)

	// Add items one at a time for reliability
	for _, itemID := range itemIDs {
		// Use canonical Plex URI format with machineIdentifier
		uri := fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s",
			c.machineIdentifier, itemID)

		query := url.Values{}
		query.Set("uri", uri)

		reqURL := fmt.Sprintf("%s%s?%s", c.baseURL, path, query.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, nil)
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

	return nil
}

// RemoveFromPlaylist removes an item from a playlist.
// Plex requires the playlist-specific entry ID (playlistItemID), not the media's ratingKey.
// This method fetches playlist items to resolve the correct entry ID internally.
func (c *Client) RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error {
	// Fetch playlist items to find the playlistItemID for this ratingKey
	path := fmt.Sprintf("/playlists/%s/items", playlistID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return err
	}

	var entryID int
	found := false
	for _, m := range container.Metadata {
		if m.RatingKey == itemID && m.PlaylistItemID > 0 {
			entryID = m.PlaylistItemID
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("item %s not found in playlist %s", itemID, playlistID)
	}

	deletePath := fmt.Sprintf("/playlists/%s/items/%d", playlistID, entryID)
	reqURL := fmt.Sprintf("%s%s", c.baseURL, deletePath)

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
