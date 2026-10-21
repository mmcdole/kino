package kivo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/mediaserver/httpx"
)

// Probe reports whether serverURL is a Kivo server, using the
// unauthenticated GET /api/v1/server.
func Probe(ctx context.Context, hc *http.Client, serverURL string) error {
	body, err := httpx.Fetch(ctx, hc, strings.TrimRight(serverURL, "/")+apiPrefix+"/server")
	if err != nil {
		return err
	}
	var info serverInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if info.ID == "" || info.Version == "" {
		return fmt.Errorf("not a Kivo server")
	}
	return nil
}

// DeviceCode is a sign-in waiting for approval in Kivo's web client.
type DeviceCode struct {
	Code     string // what the user enters at /link
	LinkURL  string // /link with the code filled in
	secret   string
	interval time.Duration
	expires  time.Time
}

// ErrDeviceCodeDenied is a device code the user turned down.
var ErrDeviceCodeDenied = errors.New("sign-in was denied")

// ErrDeviceCodeExpired is a device code nobody approved in time.
var ErrDeviceCodeExpired = errors.New("the sign-in code expired before it was approved")

// AuthClient signs a device in by code.
type AuthClient struct {
	api httpx.Client
}

// NewAuthClient returns a client for signing in to the server at baseURL.
func NewAuthClient(baseURL string, logger *slog.Logger) *AuthClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthClient{api: httpx.Client{
		Name:    "kivo",
		BaseURL: strings.TrimRight(baseURL, "/") + apiPrefix,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Logger:  logger,
	}}
}

// StartDeviceCode starts signing in as name (shown when approving and as the
// client in Now playing). clientID is this install's own ID, so signing in
// again replaces its session rather than adding one.
func (a *AuthClient) StartDeviceCode(ctx context.Context, name, clientID string) (DeviceCode, error) {
	data, err := json.Marshal(deviceCodeStart{Name: name, Kind: "app", ClientID: clientID})
	if err != nil {
		return DeviceCode{}, err
	}
	body, err := a.api.Do(ctx, httpx.Request{Method: http.MethodPost, Path: "/device-codes", Body: data})
	if err != nil {
		return DeviceCode{}, err
	}
	var dc deviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return DeviceCode{}, fmt.Errorf("failed to parse response: %w", err)
	}
	return DeviceCode{
		Code:     dc.Code,
		LinkURL:  dc.LinkURL,
		secret:   dc.Secret,
		interval: time.Duration(max(dc.Interval, 1)) * time.Second,
		expires:  dc.ExpiresAt,
	}, nil
}

// WaitForDeviceCode polls until the code is approved, denied or expired, and
// returns the signed-in user's credentials.
func (a *AuthClient) WaitForDeviceCode(ctx context.Context, dc DeviceCode) (*media.Credentials, error) {
	data, err := json.Marshal(deviceCodePoll{Secret: dc.secret})
	if err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(dc.interval):
		}
		if !dc.expires.IsZero() && time.Now().After(dc.expires) {
			return nil, ErrDeviceCodeExpired
		}
		token, err := a.poll(ctx, data)
		if err != nil {
			return nil, err
		}
		if token == "" {
			continue
		}
		return a.credentials(ctx, token)
	}
}

// poll returns the session token once the code is approved, "" while it
// waits.
func (a *AuthClient) poll(ctx context.Context, data []byte) (string, error) {
	body, err := a.api.Do(ctx, httpx.Request{Method: http.MethodPost, Path: "/device-codes/poll", Body: data})
	var se *httpx.StatusError
	switch {
	case errors.As(err, &se) && se.Status == http.StatusForbidden:
		return "", ErrDeviceCodeDenied
	case errors.As(err, &se) && se.Status == http.StatusGone:
		return "", ErrDeviceCodeExpired
	case err != nil:
		return "", err
	case len(body) == 0:
		return "", nil // 202: still waiting
	}
	var s session
	if err := json.Unmarshal(body, &s); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if s.Token == "" {
		return "", fmt.Errorf("approved session carried no token")
	}
	return s.Token, nil
}

func (a *AuthClient) credentials(ctx context.Context, token string) (*media.Credentials, error) {
	api := a.api
	api.Header = func(h http.Header) { h.Set("Authorization", "Bearer "+token) }
	body, err := api.Do(ctx, httpx.Request{Method: http.MethodGet, Path: "/users/me"})
	if err != nil {
		return nil, err
	}
	var me userInfo
	if err := json.Unmarshal(body, &me); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &media.Credentials{Token: token, UserID: me.ID, Username: me.Username}, nil
}
