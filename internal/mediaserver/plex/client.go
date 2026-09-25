package plex

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/httpx"
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
	identityMu        sync.Mutex
	machineIdentifier string // resolved lazily by playlist writes
	api               httpx.Client
	logger            *slog.Logger
}

// NewClient creates a new Plex API client
func NewClient(baseURL, token, clientID string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		clientID: normalizeClientID(clientID),
		logger:   logger,
	}
	c.api = httpx.Client{
		Name:       "plex",
		BaseURL:    c.baseURL,
		HTTP:       &http.Client{Timeout: defaultTimeout},
		Header:     c.setHeaders,
		Retries:    maxRetries,
		RetryDelay: baseRetryDelay,
		Logger:     logger,
	}
	return c
}

// serverIdentity is lazy so startup and offline browsing never wait for it.
// Failed lookups remain retryable; only a validated identity is cached.
func (c *Client) serverIdentity(ctx context.Context) (string, error) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.machineIdentifier != "" {
		return c.machineIdentifier, nil
	}
	body, err := c.get(ctx, "/identity", nil)
	if err != nil {
		return "", fmt.Errorf("resolve Plex identity: %w", err)
	}
	id, err := ParseIdentity(body)
	if err != nil {
		return "", err
	}
	c.machineIdentifier = id
	return id, nil
}

// ParseIdentity returns the machine identifier from a /identity response,
// which Plex sends as XML or JSON depending on the Accept header.
func ParseIdentity(body []byte) (string, error) {
	var identity struct {
		MachineIdentifier string `xml:"machineIdentifier,attr" json:"machineIdentifier"`
	}
	var err error
	if strings.HasPrefix(strings.TrimSpace(string(body)), "<") {
		err = xml.Unmarshal(body, &identity)
	} else {
		var response struct {
			MediaContainer struct {
				MachineIdentifier string `json:"machineIdentifier"`
			} `json:"MediaContainer"`
		}
		err = json.Unmarshal(body, &response)
		identity.MachineIdentifier = response.MediaContainer.MachineIdentifier
	}
	if err != nil {
		return "", fmt.Errorf("parse Plex identity: %w", err)
	}
	if identity.MachineIdentifier == "" {
		return "", fmt.Errorf("Plex identity missing machineIdentifier")
	}
	return identity.MachineIdentifier, nil
}

// setHeaders applies the standard Plex request headers
func (c *Client) setHeaders(h http.Header) {
	h.Set("X-Plex-Token", c.token)
	h.Set("X-Plex-Client-Identifier", c.clientID)
	h.Set("X-Plex-Product", "Kino")
	h.Set("X-Plex-Version", "1.0")
	h.Set("User-Agent", userAgent)
}

// get performs an idempotent request, retried on transient failures.
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.api.Do(ctx, httpx.Request{Method: http.MethodGet, Path: path, Query: query, Retry: true})
}

// send performs a mutation. It is never retried.
func (c *Client) send(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	return c.api.Do(ctx, httpx.Request{Method: method, Path: path, Query: query})
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
	body, err := c.get(ctx, "/library/sections", nil)
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
	body, err := c.get(ctx, path, query)
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
	body, err := c.get(ctx, path, query)
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
	body, err := c.get(ctx, path, query)
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
	body, err := c.get(ctx, path, query)
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
	body, err := c.get(ctx, path, nil)
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
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	container, err := c.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return MapEpisodes(container.Metadata, c.baseURL), nil
}

// ResolvePlayableURL returns a direct playback URL for an item
func (c *Client) ResolvePlayableURL(ctx context.Context, itemID string) (string, error) {
	path := fmt.Sprintf("/library/metadata/%s", itemID)
	body, err := c.get(ctx, path, nil)
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

	_, err := c.get(ctx, "/:/scrobble", query)
	return err
}

// MarkUnplayed marks an item as unwatched
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	query := url.Values{}
	query.Set("key", itemID)
	query.Set("identifier", "com.plexapp.plugins.library") // required by some PMS versions

	_, err := c.get(ctx, "/:/unscrobble", query)
	return err
}

// GetPlaylists returns all user playlists
func (c *Client) GetPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	body, err := c.get(ctx, "/playlists", nil)
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
	body, err := c.get(ctx, path, nil)
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

	identity, err := c.serverIdentity(ctx)
	if err != nil {
		return nil, err
	}

	// Build canonical URI with machineIdentifier
	ids := strings.Join(itemIDs, ",")
	uri := fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s",
		identity, ids)

	query := url.Values{}
	query.Set("type", "video")
	query.Set("title", title)
	query.Set("smart", "0")
	query.Set("uri", uri)

	respBody, err := c.send(ctx, http.MethodPost, "/playlists", query)
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

	identity, err := c.serverIdentity(ctx)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/playlists/%s/items", playlistID)

	// Add items one at a time for reliability
	for _, itemID := range itemIDs {
		// Use canonical Plex URI format with machineIdentifier
		uri := fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s",
			identity, itemID)

		query := url.Values{}
		query.Set("uri", uri)

		if _, err := c.send(ctx, http.MethodPut, path, query); err != nil {
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
	body, err := c.get(ctx, path, nil)
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
	if _, err := c.send(ctx, http.MethodDelete, deletePath, nil); err != nil {
		return fmt.Errorf("failed to remove item from playlist: %w", err)
	}
	return nil
}

// DeletePlaylist deletes a playlist
func (c *Client) DeletePlaylist(ctx context.Context, playlistID string) error {
	path := fmt.Sprintf("/playlists/%s", playlistID)
	if _, err := c.send(ctx, http.MethodDelete, path, nil); err != nil {
		return fmt.Errorf("failed to delete playlist: %w", err)
	}
	return nil
}
