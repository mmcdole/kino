package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mmcdole/kino/internal/mediaserver/httpx"
)

// Probe reports whether serverURL is a Jellyfin server, using the
// unauthenticated /System/Info/Public endpoint.
func Probe(ctx context.Context, hc *http.Client, serverURL string) error {
	body, err := httpx.Fetch(ctx, hc, strings.TrimRight(serverURL, "/")+"/System/Info/Public")
	if err != nil {
		return err
	}
	var info systemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	if !strings.Contains(strings.ToLower(info.ProductName), "jellyfin") {
		return fmt.Errorf("not a Jellyfin server (ProductName: %s)", info.ProductName)
	}
	return nil
}
