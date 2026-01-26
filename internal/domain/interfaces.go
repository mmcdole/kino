package domain

// ListItem is the polymorphic interface for items that can be displayed in lists.
// It provides a common API for display, filtering, and sorting across all content types.
// Domain entities (MediaItem, Show, Season) implement this interface directly.
type ListItem interface {
	// GetID returns the unique identifier for this item
	GetID() string

	// GetTitle returns the display title
	GetTitle() string

	// GetSortTitle returns the title used for alphabetical sorting (handles "The", "A", etc.)
	GetSortTitle() string

	// GetYear returns the release/air year (0 if not applicable)
	GetYear() int

	// GetDescription returns secondary info for display (e.g., "2024" for movies, "3 Seasons" for shows)
	GetDescription() string

	// GetItemType returns the type identifier: "movie", "show", "season", "episode"
	GetItemType() string

	// GetWatchStatus returns the watch status for indicator rendering
	GetWatchStatus() WatchStatus

	// CanDrillDown returns true if this item can be drilled into (shows child content)
	CanDrillDown() bool

	// GetAddedAt returns the unix timestamp when added to library (0 if not applicable)
	GetAddedAt() int64

	// GetUpdatedAt returns the unix timestamp when last updated (0 if not applicable)
	GetUpdatedAt() int64
}
