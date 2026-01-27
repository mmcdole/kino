package mediaserver

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/config"
)

const detectTimeout = 10 * time.Second

// jellyfinSystemInfo represents the Jellyfin /System/Info/Public response
type jellyfinSystemInfo struct {
	ProductName string `json:"ProductName"`
	ServerName  string `json:"ServerName"`
	Version     string `json:"Version"`
	ID          string `json:"Id"`
}

// plexIdentity represents the Plex /identity response
type plexIdentity struct {
	XMLName           xml.Name `xml:"MediaContainer"`
	MachineIdentifier string   `xml:"machineIdentifier,attr"`
	Version           string   `xml:"version,attr"`
}

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
	jellyfinType, jellyfinErr := tryJellyfin(ctx, client, serverURL)
	if jellyfinErr == nil {
		return jellyfinType, nil
	}

	// Try Plex (/identity is unauthenticated)
	plexType, plexErr := tryPlex(ctx, client, serverURL)
	if plexErr == nil {
		return plexType, nil
	}

	// Neither worked
	return "", fmt.Errorf("could not detect server type: tried Jellyfin (%v), Plex (%v)", jellyfinErr, plexErr)
}

// tryJellyfin attempts to detect a Jellyfin server
func tryJellyfin(ctx context.Context, client *http.Client, serverURL string) (config.SourceType, error) {
	url := serverURL + "/System/Info/Public"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var info jellyfinSystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if ProductName indicates Jellyfin
	if strings.Contains(strings.ToLower(info.ProductName), "jellyfin") {
		return config.SourceTypeJellyfin, nil
	}

	return "", fmt.Errorf("not a Jellyfin server (ProductName: %s)", info.ProductName)
}

// tryPlex attempts to detect a Plex server
func tryPlex(ctx context.Context, client *http.Client, serverURL string) (config.SourceType, error) {
	url := serverURL + "/identity"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Try XML parsing (Plex default)
	var identity plexIdentity
	if err := xml.Unmarshal(body, &identity); err == nil {
		if identity.MachineIdentifier != "" {
			return config.SourceTypePlex, nil
		}
	}

	// Try JSON parsing (Plex with Accept: application/json)
	var jsonIdentity struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &jsonIdentity); err == nil {
		if jsonIdentity.MediaContainer.MachineIdentifier != "" {
			return config.SourceTypePlex, nil
		}
	}

	return "", fmt.Errorf("not a Plex server")
}
