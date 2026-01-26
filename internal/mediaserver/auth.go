package mediaserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

// AuthResult contains the result of a successful authentication
type AuthResult struct {
	Token    string // Access token for API calls
	UserID   string // User identifier (required for Jellyfin)
	Username string // Display username
}

// AuthFlow defines a generic authentication flow for any media server.
// Different backends implement this differently:
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
type AuthFlow interface {
	// Run executes the authentication flow and returns credentials.
	// The serverURL parameter is the base URL of the media server.
	// Implementations handle their own user interaction (prompting for credentials, etc.)
	Run(ctx context.Context, serverURL string) (*AuthResult, error)
}

// NewAuthFlow creates the appropriate AuthFlow based on server type.
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
func NewAuthFlow(serverType config.SourceType, logger *slog.Logger) (AuthFlow, error) {
	switch serverType {
	case config.SourceTypePlex:
		return &plexAuthAdapter{inner: plex.NewAuthFlow(logger)}, nil

	case config.SourceTypeJellyfin:
		return &jellyfinAuthAdapter{inner: jellyfin.NewAuthFlow(logger)}, nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", serverType)
	}
}

// plexAuthAdapter wraps plex.AuthFlow to satisfy the AuthFlow interface
type plexAuthAdapter struct {
	inner *plex.AuthFlow
}

func (a *plexAuthAdapter) Run(ctx context.Context, serverURL string) (*AuthResult, error) {
	result, err := a.inner.Run(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		Token:    result.Token,
		UserID:   result.UserID,
		Username: result.Username,
	}, nil
}

// jellyfinAuthAdapter wraps jellyfin.AuthFlow to satisfy the AuthFlow interface
type jellyfinAuthAdapter struct {
	inner *jellyfin.AuthFlow
}

func (a *jellyfinAuthAdapter) Run(ctx context.Context, serverURL string) (*AuthResult, error) {
	result, err := a.inner.Run(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		Token:    result.Token,
		UserID:   result.UserID,
		Username: result.Username,
	}, nil
}
