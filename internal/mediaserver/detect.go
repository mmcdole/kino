package mediaserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

const detectTimeout = 10 * time.Second

// DetectServerType probes a server URL to determine if it's Plex or Jellyfin.
// Returns the detected SourceType or an error if detection fails.
func DetectServerType(ctx context.Context, serverURL string) (config.SourceType, error) {
	// Normalize URL (remove trailing slash)
	serverURL = strings.TrimRight(serverURL, "/")

	// Create a client with timeout
	client := &http.Client{
		Timeout: detectTimeout,
	}

	// Try Jellyfin first (/System/Info/Public is unauthenticated)
	jellyfinErr := probeJellyfin(ctx, client, serverURL)
	if jellyfinErr == nil {
		return config.SourceTypeJellyfin, nil
	}

	// Try Plex (/identity is unauthenticated)
	plexErr := probePlex(ctx, client, serverURL)
	if plexErr == nil {
		return config.SourceTypePlex, nil
	}

	// Neither worked
	return "", fmt.Errorf("could not detect server type: tried Jellyfin (%v), Plex (%v)", jellyfinErr, plexErr)
}

// probeJellyfin checks for a Jellyfin server
func probeJellyfin(ctx context.Context, client *http.Client, serverURL string) error {
	body, err := probe(ctx, client, serverURL+"/System/Info/Public")
	if err != nil {
		return err
	}

	var info jellyfin.SystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if ProductName indicates Jellyfin
	if !strings.Contains(strings.ToLower(info.ProductName), "jellyfin") {
		return fmt.Errorf("not a Jellyfin server (ProductName: %s)", info.ProductName)
	}
	return nil
}

// probePlex checks for a Plex server
func probePlex(ctx context.Context, client *http.Client, serverURL string) error {
	body, err := probe(ctx, client, serverURL+"/identity")
	if err != nil {
		return err
	}
	if _, err := plex.ParseIdentity(body); err != nil {
		return fmt.Errorf("not a Plex server: %w", err)
	}
	return nil
}

// probe fetches an unauthenticated endpoint and returns its body
func probe(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return body, nil
}
