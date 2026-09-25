package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/httpx"
)

const (
	defaultTimeout = 60 * time.Second
	maxRetries     = 3
	baseRetryDelay = 500 * time.Millisecond
)

// Client implements the MediaSource interface for Jellyfin
type Client struct {
	baseURL  string
	token    string
	userID   string
	deviceID string
	api      httpx.Client
	logger   *slog.Logger
}

// NewClient creates a new Jellyfin API client
func NewClient(baseURL, token, userID, deviceID string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    token,
		userID:   userID,
		deviceID: deviceID,
		logger:   logger,
	}
	c.api = httpx.Client{
		Name:    "jellyfin",
		BaseURL: c.baseURL,
		HTTP:    &http.Client{Timeout: defaultTimeout},
		Header: func(h http.Header) {
			h.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.deviceID))
		},
		Retries:    maxRetries,
		RetryDelay: baseRetryDelay,
		Logger:     logger,
	}
	return c
}

// get performs an idempotent request, retried on transient failures.
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.api.Do(ctx, httpx.Request{Method: http.MethodGet, Path: path, Query: query, Retry: true})
}

// send performs a mutation with an optional JSON body. It is never retried.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	r := httpx.Request{Method: method, Path: path, Query: query}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		r.Body = data
	}
	return c.api.Do(ctx, r)
}

// GetLibraries returns all available libraries (Views)
func (c *Client) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	path := fmt.Sprintf("/Users/%s/Views", c.userID)
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapLibraries(resp.Items), nil
}

// GetMovies returns paginated movies from a movie library
func (c *Client) GetMovies(ctx context.Context, libID string, offset, limit int) ([]*domain.MediaItem, int, error) {
	query := url.Values{}
	query.Set("ParentId", libID)
	query.Set("IncludeItemTypes", "Movie")
	query.Set("Recursive", "true")
	query.Set("Fields", "Overview,DateCreated,MediaSources,MediaStreams")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, 0, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse response: %w", err)
	}

	movies := MapMovies(resp.Items, c.baseURL)
	// Set library ID for all movies
	for _, m := range movies {
		m.LibraryID = libID
	}

	return movies, resp.TotalRecordCount, nil
}

// GetShows returns paginated TV shows from a show library
func (c *Client) GetShows(ctx context.Context, libID string, offset, limit int) ([]*domain.Show, int, error) {
	query := url.Values{}
	query.Set("ParentId", libID)
	query.Set("IncludeItemTypes", "Series")
	query.Set("Recursive", "true")
	query.Set("Fields", "Overview,ChildCount,RecursiveItemCount,DateCreated,DateLastMediaAdded,MediaSources,MediaStreams")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, 0, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse response: %w", err)
	}

	shows := MapShows(resp.Items, c.baseURL)
	// Set library ID for all shows
	for _, s := range shows {
		s.LibraryID = libID
	}

	return shows, resp.TotalRecordCount, nil
}

// GetMixedContent returns paginated content (movies AND shows) from a mixed library.
// This fetches both types in a single API call with server-side sorting.
func (c *Client) GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]domain.ListItem, int, error) {
	query := url.Values{}
	query.Set("ParentId", libID)
	query.Set("IncludeItemTypes", "Movie,Series")
	query.Set("Recursive", "true")
	query.Set("Fields", "Overview,ChildCount,RecursiveItemCount,DateCreated,DateLastMediaAdded,MediaSources,MediaStreams")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, 0, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse response: %w", err)
	}

	items := MapLibraryContent(resp.Items, c.baseURL)
	// Set library ID for all items
	for _, item := range items {
		switch v := item.(type) {
		case *domain.MediaItem:
			v.LibraryID = libID
		case *domain.Show:
			v.LibraryID = libID
		}
	}

	return items, resp.TotalRecordCount, nil
}

// GetLibraryItemCount returns the total item count for a library without
// fetching the items. Limit=1 keeps the response tiny while still populating
// TotalRecordCount.
func (c *Client) GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error) {
	query := url.Values{}
	query.Set("ParentId", libID)
	switch libType {
	case "movie":
		query.Set("IncludeItemTypes", "Movie")
	case "show":
		query.Set("IncludeItemTypes", "Series")
	default: // mixed
		query.Set("IncludeItemTypes", "Movie,Series")
	}
	query.Set("Recursive", "true")
	query.Set("Limit", "1")
	query.Set("EnableTotalRecordCount", "true")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return 0, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.TotalRecordCount, nil
}

// GetSeasons returns all seasons for a TV show
func (c *Client) GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error) {
	query := url.Values{}
	query.Set("Fields", "ChildCount,RecursiveItemCount")

	path := fmt.Sprintf("/Shows/%s/Seasons", showID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapSeasons(resp.Items, c.baseURL), nil
}

// GetEpisodes returns all episodes for a season.
// Queried by ParentId directly — no need for the extra round-trip that
// looked up the season's series ID first.
func (c *Client) GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error) {
	query := url.Values{}
	query.Set("ParentId", seasonID)
	query.Set("Fields", "Overview,MediaSources,MediaStreams,DateCreated")
	query.Set("SortBy", "IndexNumber")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapEpisodes(resp.Items, c.baseURL), nil
}

// ResolvePlayableURL returns a direct playback URL for an item
func (c *Client) ResolvePlayableURL(ctx context.Context, itemID string) (string, error) {
	// Get playback info to get the stream URL
	query := url.Values{}
	query.Set("UserId", c.userID)
	query.Set("MaxStreamingBitrate", "140000000") // High bitrate for direct play

	path := fmt.Sprintf("/Items/%s/PlaybackInfo", itemID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return "", err
	}

	var resp PlaybackInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.MediaSources) == 0 {
		return "", domain.ErrItemNotFound
	}

	source := resp.MediaSources[0]

	// Build direct stream URL
	// Format: /Videos/{itemId}/stream.{container}?static=true&api_key={token}
	streamURL := fmt.Sprintf("%s/Videos/%s/stream.%s?Static=true&api_key=%s",
		c.baseURL, itemID, source.Container, c.token)

	return streamURL, nil
}

// MarkPlayed marks an item as fully watched
func (c *Client) MarkPlayed(ctx context.Context, itemID string) error {
	path := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.userID, itemID)
	if _, err := c.send(ctx, http.MethodPost, path, nil, nil); err != nil {
		return fmt.Errorf("failed to mark as played: %w", err)
	}
	return nil
}

// MarkUnplayed marks an item as unwatched
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	path := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.userID, itemID)
	if _, err := c.send(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("failed to mark as unplayed: %w", err)
	}
	return nil
}

// GetPlaylists returns all user playlists
func (c *Client) GetPlaylists(ctx context.Context) ([]*domain.Playlist, error) {
	query := url.Values{}
	query.Set("IncludeItemTypes", "Playlist")
	query.Set("Recursive", "true")
	query.Set("Fields", "ChildCount,DateCreated")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapPlaylists(resp.Items, c.baseURL), nil
}

// GetPlaylistItems returns all items in a playlist
func (c *Client) GetPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error) {
	query := url.Values{}
	query.Set("UserId", c.userID)
	query.Set("Fields", "Overview,MediaSources,DateCreated")

	path := fmt.Sprintf("/Playlists/%s/Items", playlistID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Map items (could be movies or episodes)
	items := make([]*domain.MediaItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		switch item.Type {
		case "Movie":
			movie := mapMovie(item, c.baseURL)
			items = append(items, &movie)
		case "Episode":
			episode := mapEpisode(item, c.baseURL)
			items = append(items, &episode)
		}
	}

	return items, nil
}

// CreatePlaylist creates a new playlist with the given title and optional initial items
func (c *Client) CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error) {
	reqBody := map[string]interface{}{
		"Name":   title,
		"UserId": c.userID,
	}
	if len(itemIDs) > 0 {
		reqBody["Ids"] = itemIDs
	}

	respBody, err := c.send(ctx, http.MethodPost, "/Playlists", nil, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create playlist: %w", err)
	}

	// Parse the response to get the created playlist
	var createResp struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Return a minimal playlist object - caller can refresh for full details
	return &domain.Playlist{
		ID:           createResp.ID,
		Title:        title,
		PlaylistType: "video",
		ItemCount:    len(itemIDs),
	}, nil
}

// AddToPlaylist adds items to an existing playlist
func (c *Client) AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}

	query := url.Values{}
	query.Set("Ids", strings.Join(itemIDs, ","))
	query.Set("UserId", c.userID)

	path := fmt.Sprintf("/Playlists/%s/Items", playlistID)
	if _, err := c.send(ctx, http.MethodPost, path, query, nil); err != nil {
		return fmt.Errorf("failed to add items to playlist: %w", err)
	}
	return nil
}

// RemoveFromPlaylist removes an item from a playlist.
// Jellyfin's EntryIds parameter takes the playlist-specific entry ID
// (PlaylistItemId), not the media item's ID — passing an item ID is silently
// ignored (204 with no change). Resolve the entry ID first.
func (c *Client) RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error {
	entryID, err := c.resolvePlaylistEntryID(ctx, playlistID, itemID)
	if err != nil {
		return err
	}

	query := url.Values{}
	query.Set("EntryIds", entryID)

	path := fmt.Sprintf("/Playlists/%s/Items", playlistID)
	if _, err := c.send(ctx, http.MethodDelete, path, query, nil); err != nil {
		return fmt.Errorf("failed to remove item from playlist: %w", err)
	}
	return nil
}

// resolvePlaylistEntryID fetches the playlist's items and returns the
// playlist entry ID (PlaylistItemId) for the given media item ID.
func (c *Client) resolvePlaylistEntryID(ctx context.Context, playlistID, itemID string) (string, error) {
	query := url.Values{}
	query.Set("UserId", c.userID)

	path := fmt.Sprintf("/Playlists/%s/Items", playlistID)
	body, err := c.get(ctx, path, query)
	if err != nil {
		return "", err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	for _, item := range resp.Items {
		if item.ID == itemID {
			if item.PlaylistItemID != "" {
				return item.PlaylistItemID, nil
			}
			// Very old servers predate PlaylistItemId; fall back to the
			// item ID, which those versions accepted
			return itemID, nil
		}
	}

	return "", fmt.Errorf("item %s not found in playlist %s", itemID, playlistID)
}

// DeletePlaylist deletes a playlist
func (c *Client) DeletePlaylist(ctx context.Context, playlistID string) error {
	path := fmt.Sprintf("/Items/%s", playlistID)
	if _, err := c.send(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete playlist: %w", err)
	}
	return nil
}
