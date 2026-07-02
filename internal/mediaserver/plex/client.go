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
	maxRetries     = 3
	baseRetryDelay = 500 * time.Millisecond
	userAgent      = "Kino/1.0"
)

// normalizeClientID ensures a usable X-Plex-Client-Identifier.
// The identifier must be unique per install: plex.tv tracks devices by it,
// so a shared static ID makes every kino install look like the same device
// and re-linking anywhere can invalidate previously issued tokens.
func normalizeClientID(clientID string) string {
	if clientID == "" {
		return "kino-tui-client" // legacy fallback
	}
	return clientID
}

// Client implements domain.LibraryRepository, domain.SearchRepository,
// domain.MetadataRepository, and domain.Scrobbler for Plex
type Client struct {
	baseURL           string
	token             string
	clientID          string // unique per-install X-Plex-Client-Identifier
	machineIdentifier string // fetched from /identity on init
	httpClient        *http.Client
	logger            *slog.Logger
}

// NewClient creates a new Plex API client
func NewClient(baseURL, token, clientID string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		clientID: normalizeClientID(clientID),
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

// setHeaders applies the standard Plex request headers
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", c.clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)
}

// do performs an authenticated HTTP request to the Plex server. All error
// mapping lives here: 401 → domain.ErrAuthFailed, transport failures →
// domain.ErrServerOffline (wrapped with the cause), any 2xx → success.
// Idempotent requests (retry=true) are retried on network errors and 5xx
// responses with exponential backoff.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, retry bool) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	if query != nil {
		reqURL = fmt.Sprintf("%s?%s", reqURL, query.Encode())
	}

	attempts := 1
	if retry {
		attempts = maxRetries + 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1)) // 500ms, 1s, 2s
			c.logger.Debug("retrying request", "attempt", attempt, "delay", delay, "path", path)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		c.setHeaders(req)

		c.logger.Debug("plex request", "method", method, "path", path, "attempt", attempt)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%w: %v", domain.ErrServerOffline, err)
			c.logger.Warn("plex request failed", "error", err, "method", method, "path", path, "attempt", attempt)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, domain.ErrAuthFailed
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("server error: %d - %s", resp.StatusCode, truncateForLog(body))
			c.logger.Warn("plex server error",
				"status", resp.StatusCode,
				"attempt", attempt,
				"method", method,
				"path", path,
			)
			continue
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return body, nil
		default:
			c.logger.Error("plex request error", "status", resp.StatusCode, "path", path, "body", truncateForLog(body))
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}

	c.logger.Error("plex request failed", "error", lastErr, "method", method, "path", path)
	return nil, lastErr
}

// doRequest performs an idempotent (retried) GET-style request
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	return c.do(ctx, method, path, query, true)
}

// truncateForLog bounds response bodies before they reach the log file
// (a reverse proxy's 502 page can be arbitrarily large)
func truncateForLog(body []byte) string {
	const max = 512
	if len(body) > max {
		return string(body[:max]) + "...(truncated)"
	}
	return string(body)
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

// GetLibraryItemCount returns the total item count for a library section
// without fetching the items (X-Plex-Container-Size=0 returns only totalSize).
// libType is unused: /all already returns the section's native item type.
func (c *Client) GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error) {
	query := url.Values{}
	query.Set("X-Plex-Container-Start", "0")
	query.Set("X-Plex-Container-Size", "0")

	path := fmt.Sprintf("/library/sections/%s/all", libID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return 0, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return 0, err
	}

	return container.TotalSize, nil
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
	params.Set("limit", "50") // match the Jellyfin backend's result cap

	body, err := c.doRequest(ctx, http.MethodGet, "/search", params)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapSearchResults(container.Metadata, c.baseURL), nil
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

// MarkPlayed marks an item as fully watched
func (c *Client) MarkPlayed(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)
	query.Set("identifier", "com.plexapp.plugins.library") // required by some PMS versions

	_, err := c.doRequest(ctx, http.MethodGet, "/:/scrobble", query)
	return err
}

// MarkUnplayed marks an item as unwatched
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)
	query.Set("identifier", "com.plexapp.plugins.library") // required by some PMS versions

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

	return MapVideoItems(container.Metadata, c.baseURL), nil
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

	respBody, err := c.do(ctx, http.MethodPost, "/playlists", query, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create playlist: %w", err)
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

		if _, err := c.do(ctx, http.MethodPut, path, query, false); err != nil {
			return fmt.Errorf("failed to add item to playlist: %w", err)
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
	if _, err := c.do(ctx, http.MethodDelete, deletePath, nil, false); err != nil {
		return fmt.Errorf("failed to remove item from playlist: %w", err)
	}
	return nil
}

// DeletePlaylist deletes a playlist
func (c *Client) DeletePlaylist(ctx context.Context, playlistID string) error {
	path := fmt.Sprintf("/playlists/%s", playlistID)
	if _, err := c.do(ctx, http.MethodDelete, path, nil, false); err != nil {
		return fmt.Errorf("failed to delete playlist: %w", err)
	}
	return nil
}
