package mediaserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

// AuthFlow defines a generic authentication flow for any media server.
// Different backends implement this differently:
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
type AuthFlow interface {
	// Run executes the authentication flow and returns credentials.
	// The serverURL parameter is the base URL of the media server.
	// Implementations handle their own user interaction (prompting for credentials, etc.)
	Run(ctx context.Context, serverURL string) (*domain.Credentials, error)
}

// NewAuthFlow creates the appropriate AuthFlow based on server type.
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
// The deviceID uniquely identifies this install to the server.
func NewAuthFlow(serverType config.SourceType, deviceID string, logger *slog.Logger) (AuthFlow, error) {
	switch serverType {
	case config.SourceTypePlex:
		return plex.NewAuthFlow(deviceID, logger), nil

	case config.SourceTypeJellyfin:
		return jellyfin.NewAuthFlow(deviceID, logger), nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", serverType)
	}
}
