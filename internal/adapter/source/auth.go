package source

import (
	"fmt"
	"log/slog"

	"github.com/mmcdole/kino/internal/adapter"
	"github.com/mmcdole/kino/internal/adapter/source/jellyfin"
	"github.com/mmcdole/kino/internal/adapter/source/plex"
	"github.com/mmcdole/kino/internal/domain"
)

// NewAuthFlow creates the appropriate AuthFlow based on server type.
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
func NewAuthFlow(serverType adapter.SourceType, logger *slog.Logger) (domain.AuthFlow, error) {
	switch serverType {
	case adapter.SourceTypePlex:
		return plex.NewAuthFlow(logger), nil

	case adapter.SourceTypeJellyfin:
		return jellyfin.NewAuthFlow(logger), nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", serverType)
	}
}
