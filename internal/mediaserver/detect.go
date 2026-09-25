package mediaserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

const detectTimeout = 10 * time.Second

// DetectServerType probes a server URL to determine if it's Plex or Jellyfin.
// Returns the detected ServerType or an error if detection fails.
func DetectServerType(ctx context.Context, serverURL string) (config.ServerType, error) {
	client := &http.Client{Timeout: detectTimeout}

	// Both endpoints are unauthenticated. Jellyfin goes first because its
	// check is the more specific one.
	jellyfinErr := jellyfin.Probe(ctx, client, serverURL)
	if jellyfinErr == nil {
		return config.Jellyfin, nil
	}
	plexErr := plex.Probe(ctx, client, serverURL)
	if plexErr == nil {
		return config.Plex, nil
	}
	return "", fmt.Errorf("could not detect server type: tried Jellyfin (%v), Plex (%v)", jellyfinErr, plexErr)
}
