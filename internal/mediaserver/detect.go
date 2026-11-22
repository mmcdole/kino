package mediaserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/kivo"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

const detectTimeout = 10 * time.Second

// DetectServerType probes a server URL to determine if it's Kivo, Jellyfin or
// Plex. Returns the detected ServerType or an error if detection fails.
func DetectServerType(ctx context.Context, serverURL string) (config.ServerType, error) {
	client := &http.Client{Timeout: detectTimeout}

	// All three endpoints are unauthenticated. They go from the most specific
	// check to the least.
	kivoErr := kivo.Probe(ctx, client, serverURL)
	if kivoErr == nil {
		return config.Kivo, nil
	}
	jellyfinErr := jellyfin.Probe(ctx, client, serverURL)
	if jellyfinErr == nil {
		return config.Jellyfin, nil
	}
	plexErr := plex.Probe(ctx, client, serverURL)
	if plexErr == nil {
		return config.Plex, nil
	}
	return "", fmt.Errorf("could not detect server type: tried Kivo (%v), Jellyfin (%v), Plex (%v)", kivoErr, jellyfinErr, plexErr)
}
