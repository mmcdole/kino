// Package kivo is the client for a Kivo server's /api/v1.
package kivo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/mediaserver/httpx"
)

const (
	defaultTimeout = 60 * time.Second
	maxRetries     = 3
	baseRetryDelay = 500 * time.Millisecond
	apiPrefix      = "/api/v1"
)

// Client talks to one Kivo server as one user.
type Client struct {
	baseURL string
	token   string
	api     httpx.Client
	logger  *slog.Logger

	// totals remembers each library's item count from its first page; Kivo
	// sends the total only there.
	mu     sync.Mutex
	totals map[string]int
}

// NewClient creates a Kivo client authenticating with an app session token.
func NewClient(baseURL, token string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		logger:  logger,
		totals:  make(map[string]int),
	}
	c.api = httpx.Client{
		Name:    "kivo",
		BaseURL: c.baseURL + apiPrefix,
		HTTP:    &http.Client{Timeout: defaultTimeout},
		Header: func(h http.Header) {
			h.Set("Authorization", "Bearer "+c.token)
		},
		Retries:    maxRetries,
		RetryDelay: baseRetryDelay,
		Logger:     logger,
	}
	return c
}

// get performs an idempotent request, retried on transient failures.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	body, err := c.api.Do(ctx, httpx.Request{Method: http.MethodGet, Path: path, Query: query, Retry: true})
	if err != nil {
		return problemError(err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// send performs a mutation with a JSON body, decoding the response into out
// when it isn't nil. It is never retried.
func (c *Client) send(ctx context.Context, method, path string, in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	body, err := c.api.Do(ctx, httpx.Request{Method: method, Path: path, Body: data})
	if err != nil {
		return problemError(err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// GetLibraries returns the movies and shows libraries.
func (c *Client) GetLibraries(ctx context.Context) ([]media.Library, error) {
	var resp libraryList
	if err := c.get(ctx, "/libraries", nil, &resp); err != nil {
		return nil, err
	}
	for _, l := range resp.Items {
		if _, ok := libraryType(l.Kind); !ok {
			c.logger.Info("leaving out a library kino can't show", "library", l.Name, "kind", l.Kind)
		}
	}
	return mapLibraries(resp.Items), nil
}

// browse returns one page of a library's top-level works and the library's
// total.
func (c *Client) browse(ctx context.Context, libID string, offset, limit int) ([]work, int, error) {
	query := url.Values{}
	query.Set("libraryId", libID)
	query.Set("offset", strconv.Itoa(offset))
	if limit > 0 {
		query.Set("limit", strconv.Itoa(min(limit, 200)))
	}
	var resp workPage
	if err := c.get(ctx, "/works", query, &resp); err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if resp.Total != nil {
		c.totals[libID] = *resp.Total
	}
	total, known := c.totals[libID]
	if resp.Next == "" {
		total = offset + len(resp.Items) // the last page
	} else if !known {
		total = 0
	}
	return resp.Items, total, nil
}

// children returns all of a series' seasons or a season's episodes.
func (c *Client) children(ctx context.Context, parentID string) ([]work, error) {
	query := url.Values{}
	query.Set("parentId", parentID)
	var resp workPage
	if err := c.get(ctx, "/works", query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetMovies returns a page of a movies library.
func (c *Client) GetMovies(ctx context.Context, libID string, offset, limit int) ([]*media.Item, int, error) {
	works, total, err := c.browse(ctx, libID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]*media.Item, 0, len(works))
	for _, w := range works {
		items = append(items, mapItem(w, libID))
	}
	return items, total, nil
}

// GetShows returns a page of a shows library.
func (c *Client) GetShows(ctx context.Context, libID string, offset, limit int) ([]*media.Show, int, error) {
	works, total, err := c.browse(ctx, libID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	shows := make([]*media.Show, 0, len(works))
	for _, w := range works {
		shows = append(shows, mapShow(w, libID))
	}
	return shows, total, nil
}

// GetMixedContent returns a page of movies and series. GetLibraries lists no
// mixed libraries for Kivo; this maps whatever cards a library holds.
func (c *Client) GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]media.ListItem, int, error) {
	works, total, err := c.browse(ctx, libID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]media.ListItem, 0, len(works))
	for _, w := range works {
		switch w.Kind {
		case "movie", "video":
			items = append(items, mapItem(w, libID))
		case "series":
			items = append(items, mapShow(w, libID))
		}
	}
	return items, total, nil
}

// GetLibraryItemCount returns the number of top-level items in a library.
func (c *Client) GetLibraryItemCount(ctx context.Context, libID, _ string) (int, error) {
	var resp libraryList
	if err := c.get(ctx, "/libraries", nil, &resp); err != nil {
		return 0, err
	}
	for _, l := range resp.Items {
		if l.ID == libID {
			return l.ItemCount, nil
		}
	}
	return 0, media.ErrItemNotFound
}

// GetSeasons returns a series' seasons, Specials last.
func (c *Client) GetSeasons(ctx context.Context, showID string) ([]*media.Season, error) {
	works, err := c.children(ctx, showID)
	if err != nil {
		return nil, err
	}
	seasons := make([]*media.Season, 0, len(works))
	for _, w := range works {
		seasons = append(seasons, mapSeason(w))
	}
	return seasons, nil
}

// GetEpisodes returns a season's episodes in order.
func (c *Client) GetEpisodes(ctx context.Context, seasonID string) ([]*media.Item, error) {
	works, err := c.children(ctx, seasonID)
	if err != nil {
		return nil, err
	}
	episodes := make([]*media.Item, 0, len(works))
	for _, w := range works {
		episodes = append(episodes, mapItem(w, ""))
	}
	return episodes, nil
}

// MarkPlayed marks a work played; a series or season marks each episode.
func (c *Client) MarkPlayed(ctx context.Context, itemID string) error {
	return c.setPlayed(ctx, itemID, true)
}

// MarkUnplayed marks a work unplayed and clears its resume position.
func (c *Client) MarkUnplayed(ctx context.Context, itemID string) error {
	return c.setPlayed(ctx, itemID, false)
}

func (c *Client) setPlayed(ctx context.Context, itemID string, played bool) error {
	path := "/works/" + url.PathEscape(itemID) + "/user-state"
	if err := c.send(ctx, http.MethodPatch, path, userStateUpdate{Played: played}, nil); err != nil {
		return fmt.Errorf("failed to mark as played=%t: %w", played, err)
	}
	return nil
}

// decodeProblem returns the problem document in err's response, if any.
func decodeProblem(err error) (problem, bool) {
	var se *httpx.StatusError
	if !errors.As(err, &se) {
		return problem{}, false
	}
	var p problem
	if json.Unmarshal(se.Body, &p) != nil || p.Code == "" {
		return problem{}, false
	}
	return p, true
}
