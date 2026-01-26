package jellyfin

import (
	"context"
	"encoding/json"
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
	defaultTimeout   = 60 * time.Second
	defaultBatchSize = 100
	maxRetries       = 3
	baseRetryDelay   = 500 * time.Millisecond
)

// Client implements the MediaSource interface for Jellyfin
type Client struct {
	baseURL    string
	token      string
	userID     string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new Jellyfin API client
func NewClient(baseURL, token, userID string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		userID:  userID,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// doRequest performs an authenticated HTTP request to the Jellyfin API
// Includes retry logic with exponential backoff for 5xx server errors
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	if query != nil {
		reqURL = fmt.Sprintf("%s?%s", reqURL, query.Encode())
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context before each attempt
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Wait before retry (exponential backoff)
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(1<<(attempt-1)) // 500ms, 1s, 2s
			c.logger.Debug("retrying request", "attempt", attempt, "delay", delay, "url", reqURL)
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

		// Set Jellyfin auth headers
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

		c.logger.Debug("jellyfin request", "method", method, "url", reqURL, "attempt", attempt)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logger.Error("jellyfin request failed", "error", err)
			return nil, domain.ErrServerOffline
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, domain.ErrAuthFailed
		}

		// Retry on 5xx server errors
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			lastErr = fmt.Errorf("server error: %d - %s", resp.StatusCode, string(body))
			queryStr := ""
			if query != nil {
				queryStr = query.Encode()
			}
			c.logger.Warn("jellyfin server error, will retry",
				"status", resp.StatusCode,
				"body", string(body),
				"attempt", attempt,
				"maxRetries", maxRetries,
				"path", path,
				"query", queryStr,
			)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			c.logger.Error("jellyfin request error", "status", resp.StatusCode, "body", string(body))
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		return body, nil
	}

	queryStr := ""
	if query != nil {
		queryStr = query.Encode()
	}
	c.logger.Error("jellyfin request failed after retries",
		"error", lastErr,
		"url", reqURL,
		"path", path,
		"query", queryStr,
	)
	return nil, lastErr
}

// GetLibraries returns all available libraries (Views)
func (c *Client) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	path := fmt.Sprintf("/Users/%s/Views", c.userID)
	body, err := c.doRequest(ctx, http.MethodGet, path, nil)
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
	query.Set("Fields", "Overview,DateCreated")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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
	query.Set("Fields", "Overview,ChildCount,RecursiveItemCount,DateCreated,DateLastMediaAdded")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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

// GetAllMovies returns all movies in a library (handles pagination internally)
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

// GetAllShows returns all TV shows in a library (handles pagination internally)
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

// GetLibraryContent returns paginated content (movies AND shows) from a mixed library.
// This fetches both types in a single API call with server-side sorting.
func (c *Client) GetLibraryContent(ctx context.Context, libID string, offset, limit int) ([]domain.ListItem, int, error) {
	query := url.Values{}
	query.Set("ParentId", libID)
	query.Set("IncludeItemTypes", "Movie,Series")
	query.Set("Recursive", "true")
	query.Set("Fields", "Overview,ChildCount,RecursiveItemCount,DateCreated,DateLastMediaAdded")
	query.Set("StartIndex", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("Limit", strconv.Itoa(limit))
	}
	query.Set("SortBy", "SortName")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Users/%s/Items", c.userID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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

// GetAllLibraryContent returns all content from a mixed library (handles pagination internally)
func (c *Client) GetAllLibraryContent(ctx context.Context, libID string) ([]domain.ListItem, error) {
	var allItems []domain.ListItem
	offset := 0

	for {
		items, total, err := c.GetLibraryContent(ctx, libID, offset, defaultBatchSize)
		if err != nil {
			return nil, err
		}

		allItems = append(allItems, items...)

		if len(allItems) >= total || len(items) == 0 {
			break
		}
		offset += defaultBatchSize
	}

	return allItems, nil
}

// GetSeasons returns all seasons for a TV show
func (c *Client) GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error) {
	query := url.Values{}
	query.Set("Fields", "ChildCount,RecursiveItemCount")

	path := fmt.Sprintf("/Shows/%s/Seasons", showID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapSeasons(resp.Items, c.baseURL), nil
}

// GetEpisodes returns all episodes for a season
func (c *Client) GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error) {
	// First, get the season to find the show ID
	seasonPath := fmt.Sprintf("/Users/%s/Items/%s", c.userID, seasonID)
	seasonBody, err := c.doRequest(ctx, http.MethodGet, seasonPath, nil)
	if err != nil {
		return nil, err
	}

	var season Item
	if err := json.Unmarshal(seasonBody, &season); err != nil {
		return nil, fmt.Errorf("failed to parse season: %w", err)
	}

	// Get episodes for this season
	query := url.Values{}
	query.Set("SeasonId", seasonID)
	query.Set("Fields", "Overview,MediaSources,MediaStreams,DateCreated")
	query.Set("SortBy", "IndexNumber")
	query.Set("SortOrder", "Ascending")

	path := fmt.Sprintf("/Shows/%s/Episodes", season.SeriesID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	var resp ItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapEpisodes(resp.Items, c.baseURL), nil
}

// Search performs a search across all libraries
func (c *Client) Search(ctx context.Context, query string) ([]domain.MediaItem, error) {
	params := url.Values{}
	params.Set("searchTerm", query)
	params.Set("IncludeItemTypes", "Movie,Episode,Series")
	params.Set("Limit", "50")

	path := "/Search/Hints"
	body, err := c.doRequest(ctx, http.MethodGet, path, params)
	if err != nil {
		return nil, err
	}

	var resp SearchHintsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return MapSearchResults(resp.SearchHints, c.baseURL), nil
}

// ResolvePlayableURL returns a direct playback URL for an item
func (c *Client) ResolvePlayableURL(ctx context.Context, itemID string) (string, error) {
	// Get playback info to get the stream URL
	query := url.Values{}
	query.Set("UserId", c.userID)
	query.Set("MaxStreamingBitrate", "140000000") // High bitrate for direct play

	path := fmt.Sprintf("/Items/%s/PlaybackInfo", itemID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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

// GetMediaItem returns detailed metadata for a specific item
func (c *Client) GetMediaItem(ctx context.Context, itemID string) (*domain.MediaItem, error) {
	query := url.Values{}
	query.Set("Fields", "Overview,MediaSources,MediaStreams,DateCreated")

	path := fmt.Sprintf("/Users/%s/Items/%s", c.userID, itemID)
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	var item Item
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var result domain.MediaItem
	switch item.Type {
	case "Movie":
		result = mapMovie(item, c.baseURL)
	case "Episode":
		result = mapEpisode(item, c.baseURL)
	default:
		return nil, domain.ErrItemNotFound
	}

	return &result, nil
}

// MarkPlayed marks an item as fully watched
func (c *Client) MarkPlayed(ctx context.Context, itemID string) error {
	path := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.userID, itemID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to mark as played: status %d", resp.StatusCode)
	}

	return nil
}

// MarkUnplayed marks an item as unwatched
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	path := fmt.Sprintf("/Users/%s/PlayedItems/%s", c.userID, itemID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to mark as unplayed: status %d", resp.StatusCode)
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
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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
	body, err := c.doRequest(ctx, http.MethodGet, path, query)
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

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/Playlists",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to create playlist: status %d - %s", resp.StatusCode, string(respBody))
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

	path := fmt.Sprintf("/Playlists/%s/Items?%s", playlistID, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to add items to playlist: status %d", resp.StatusCode)
	}

	return nil
}

// RemoveFromPlaylist removes an item from a playlist
func (c *Client) RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error {
	query := url.Values{}
	query.Set("EntryIds", itemID)

	path := fmt.Sprintf("/Playlists/%s/Items?%s", playlistID, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
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
	path := fmt.Sprintf("/Items/%s", playlistID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Authorization", buildAuthHeader(c.token, c.userID))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete playlist: status %d", resp.StatusCode)
	}

	return nil
}
