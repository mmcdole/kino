package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

const (
	plexTVBaseURL = "https://plex.tv"
	pinEndpoint   = "/api/v2/pins"
)

// AuthClient handles Plex authentication
type AuthClient struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAuthClient creates a new authentication client
func NewAuthClient(logger *slog.Logger) *AuthClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// GetPIN generates a new authentication PIN
func (a *AuthClient) GetPIN(ctx context.Context) (pin string, id int, err error) {
	reqURL := fmt.Sprintf("%s%s", plexTVBaseURL, pinEndpoint)

	data := url.Values{}
	data.Set("strong", "false")
	data.Set("X-Plex-Product", "Kino")
	data.Set("X-Plex-Client-Identifier", clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	// Add data as query params for POST
	req.URL.RawQuery = data.Encode()

	a.logger.Debug("requesting PIN", "url", reqURL)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.logger.Error("PIN request failed", "error", err)
		return "", 0, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		a.logger.Error("PIN request error", "status", resp.StatusCode, "body", string(body))
		return "", 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pinResp PINResponse
	if err := json.Unmarshal(body, &pinResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse PIN response: %w", err)
	}

	a.logger.Info("PIN generated", "pin", pinResp.Code, "id", pinResp.ID)
	return pinResp.Code, pinResp.ID, nil
}

// CheckPIN polls for PIN claim status and returns the auth token
func (a *AuthClient) CheckPIN(ctx context.Context, pinID int) (token string, claimed bool, err error) {
	reqURL := fmt.Sprintf("%s%s/%d", plexTVBaseURL, pinEndpoint, pinID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.logger.Error("PIN check failed", "error", err)
		return "", false, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", false, domain.ErrPINExpired
	}

	if resp.StatusCode != http.StatusOK {
		a.logger.Error("PIN check error", "status", resp.StatusCode, "body", string(body))
		return "", false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pinResp PINCheckResponse
	if err := json.Unmarshal(body, &pinResp); err != nil {
		return "", false, fmt.Errorf("failed to parse PIN response: %w", err)
	}

	if pinResp.AuthToken == "" {
		return "", false, nil // Not yet claimed
	}

	a.logger.Info("PIN claimed successfully")
	return pinResp.AuthToken, true, nil
}

// ValidateToken checks if a token is still valid by making a test request
func (a *AuthClient) ValidateToken(ctx context.Context, token string) error {
	reqURL := fmt.Sprintf("%s/api/v2/user", plexTVBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return domain.ErrServerOffline
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return domain.ErrAuthExpired
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// GetServers returns available Plex servers for the authenticated user
func (a *AuthClient) GetServers(ctx context.Context, token string) ([]ServerResource, error) {
	reqURL := fmt.Sprintf("%s/api/v2/resources?includeHttps=1&includeRelay=1", plexTVBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
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
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var servers []ServerResource
	if err := json.Unmarshal(body, &servers); err != nil {
		return nil, fmt.Errorf("failed to parse servers response: %w", err)
	}

	// Filter to only Plex Media Server resources
	var pmsServers []ServerResource
	for _, s := range servers {
		if s.Product == "Plex Media Server" {
			pmsServers = append(pmsServers, s)
		}
	}

	return pmsServers, nil
}

// WaitForPIN polls for PIN claim with exponential backoff
func (a *AuthClient) WaitForPIN(ctx context.Context, pinID int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	interval := 1 * time.Second
	maxInterval := 5 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
			token, claimed, err := a.CheckPIN(ctx, pinID)
			if err != nil {
				if err == domain.ErrPINExpired {
					return "", err
				}
				a.logger.Warn("PIN check error, retrying", "error", err)
				continue
			}

			if claimed {
				return token, nil
			}

			// Increase interval up to max
			interval = min(interval*2, maxInterval)
		}
	}

	return "", domain.ErrPINExpired
}
