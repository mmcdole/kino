package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

const (
	authTimeout = 30 * time.Second
)

// Authenticate signs in to a Jellyfin server with a username and password.
// The device ID must be unique per install (see buildAuthHeader).
func Authenticate(ctx context.Context, serverURL, deviceID, username, password string, logger *slog.Logger) (*domain.Credentials, error) {
	if logger == nil {
		logger = slog.Default()
	}
	url := strings.TrimRight(serverURL, "/") + "/Users/AuthenticateByName"

	// Build request body
	body := map[string]string{
		"Username": username,
		"Pw":       password,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", buildAuthHeader("", deviceID)) // No token yet

	resp, err := (&http.Client{Timeout: authTimeout}).Do(req)
	if err != nil {
		logger.Error("Jellyfin auth request failed", "error", err)
		return nil, fmt.Errorf("%w: %v", domain.ErrServerOffline, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: check username and password", domain.ErrAuthFailed)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Error("Jellyfin auth error", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("authentication failed with status %d", resp.StatusCode)
	}

	var authResp authResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return nil, fmt.Errorf("failed to parse auth response: %w", err)
	}

	return &domain.Credentials{
		Token:    authResp.AccessToken,
		UserID:   authResp.User.ID,
		Username: authResp.User.Name,
	}, nil
}

// buildAuthHeader constructs the X-Emby-Authorization header.
// The device ID must be unique per install: Jellyfin revokes existing
// tokens when a new login reuses the same device ID, so a shared static
// ID causes intermittent token invalidation across installs.
func buildAuthHeader(token, deviceID string) string {
	if deviceID == "" {
		deviceID = "kino-tui-client" // legacy fallback
	}
	parts := []string{
		`MediaBrowser Client="Kino"`,
		`Device="CLI"`,
		fmt.Sprintf(`DeviceId="%s"`, deviceID),
		`Version="1.0.0"`,
	}

	if token != "" {
		parts = append(parts, fmt.Sprintf(`Token="%s"`, token))
	}

	return strings.Join(parts, ", ")
}
