package plex

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmcdole/kino/internal/mediaserver/httpx"
)

// Probe reports whether serverURL is a Plex server, using the
// unauthenticated /identity endpoint.
func Probe(ctx context.Context, hc *http.Client, serverURL string) error {
	body, err := httpx.Fetch(ctx, hc, strings.TrimRight(serverURL, "/")+"/identity")
	if err != nil {
		return err
	}
	if _, err := parseIdentity(body); err != nil {
		return fmt.Errorf("not a Plex server: %w", err)
	}
	return nil
}
