// Package httpx is the HTTP transport shared by the media server backends.
// It owns retries and the mapping from transport failures to domain errors.
package httpx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// Request describes one API call. Only idempotent requests may set Retry:
// repeating a mutation can apply it twice.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte // sent as JSON, and again on each retry
	Retry  bool
}

// Client sends requests to one server.
type Client struct {
	Name       string // backend name for log messages
	BaseURL    string
	HTTP       *http.Client
	Header     func(http.Header) // authentication and client identification
	Retries    int
	RetryDelay time.Duration // first backoff; doubles per attempt
	Logger     *slog.Logger
}

// Do sends r and returns the response body of a 2xx response. A 401 maps to
// domain.ErrAuthFailed, 404 to domain.ErrItemNotFound, and a transport failure
// to domain.ErrServerOffline wrapping its cause. Retried requests repeat on
// transport failures and 5xx responses. Cancellation is returned as-is.
func (c *Client) Do(ctx context.Context, r Request) ([]byte, error) {
	target := c.BaseURL + r.Path
	if r.Query != nil {
		target += "?" + r.Query.Encode()
	}
	attempts := 1
	if r.Retry {
		attempts += c.Retries
	}

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			delay := c.RetryDelay << (attempt - 1)
			c.Logger.Debug("retrying request", "backend", c.Name, "attempt", attempt, "delay", delay, "path", r.Path)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		body, status, err := c.send(ctx, target, r)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			lastErr = fmt.Errorf("%w: %w", domain.ErrServerOffline, err)
			c.Logger.Warn("request failed", "backend", c.Name, "error", err, "method", r.Method, "path", r.Path, "attempt", attempt)
		case status == http.StatusUnauthorized:
			return nil, domain.ErrAuthFailed
		case status == http.StatusNotFound:
			return nil, domain.ErrItemNotFound
		case status >= 500:
			lastErr = fmt.Errorf("server error: %d - %s", status, truncate(body))
			c.Logger.Warn("server error", "backend", c.Name, "status", status, "method", r.Method, "path", r.Path, "attempt", attempt)
		case status >= 200 && status < 300:
			return body, nil
		default:
			c.Logger.Error("request error", "backend", c.Name, "status", status, "path", r.Path, "body", truncate(body))
			return nil, fmt.Errorf("unexpected status code: %d", status)
		}
	}
	c.Logger.Error("request failed", "backend", c.Name, "error", lastErr, "method", r.Method, "path", r.Path)
	return nil, lastErr
}

func (c *Client) send(ctx context.Context, target string, r Request) ([]byte, int, error) {
	var body io.Reader
	if r.Body != nil {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Header != nil {
		c.Header(req.Header)
	}
	c.Logger.Debug("request", "backend", c.Name, "method", r.Method, "path", r.Path)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}
	return data, resp.StatusCode, nil
}

// truncate bounds response bodies before they reach the log file; a reverse
// proxy's error page can be arbitrarily large.
func truncate(body []byte) string {
	const max = 512
	if len(body) > max {
		return string(body[:max]) + "...(truncated)"
	}
	return string(body)
}
