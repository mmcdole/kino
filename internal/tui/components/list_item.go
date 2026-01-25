package components

import (
	"fmt"
	"strings"

	"github.com/mmcdole/kino/internal/domain"
)

// ListItem is the interface for items that can be displayed in a ListColumn.
// It provides a common API for display, filtering, and sorting across all content types.
type ListItem interface {
	// ItemID returns the unique identifier for this item
	ItemID() string

	// ItemTitle returns the display title
	ItemTitle() string

	// ItemSubtitle returns secondary text (e.g., year, episode count)
	ItemSubtitle() string

	// FilterValue returns the string used for fuzzy filtering (usually same as title)
	FilterValue() string

	// SortTitle returns the title used for alphabetical sorting (handles "The", "A", etc.)
	SortTitle() string

	// SortableYear returns the year for release date sorting (0 if not applicable)
	SortableYear() int

	// SortableAddedAt returns the unix timestamp for date added sorting (0 if not applicable)
	SortableAddedAt() int64

	// SortableUpdatedAt returns the unix timestamp for last updated sorting (0 if not applicable)
	SortableUpdatedAt() int64

	// WatchStatus returns the watch status for indicator rendering
	WatchStatus() domain.WatchStatus

	// CanDrillInto returns true if this item can be drilled into (shows child content)
	CanDrillInto() bool

	// Unwrap returns the underlying domain object
	Unwrap() interface{}
}

// LibraryListItem wraps domain.Library to implement ListItem
type LibraryListItem struct {
	Library domain.Library
}

func (i LibraryListItem) ItemID() string           { return i.Library.ID }
func (i LibraryListItem) ItemTitle() string        { return i.Library.Name }
func (i LibraryListItem) ItemSubtitle() string     { return i.Library.Type }
func (i LibraryListItem) FilterValue() string      { return i.Library.Name }
func (i LibraryListItem) SortTitle() string        { return strings.ToLower(i.Library.Name) }
func (i LibraryListItem) SortableYear() int        { return 0 }
func (i LibraryListItem) SortableAddedAt() int64   { return 0 }
func (i LibraryListItem) SortableUpdatedAt() int64 { return i.Library.UpdatedAt }
func (i LibraryListItem) WatchStatus() domain.WatchStatus {
	return domain.WatchStatusUnwatched // Libraries don't have watch status
}
func (i LibraryListItem) CanDrillInto() bool { return true }
func (i LibraryListItem) Unwrap() interface{} { return i.Library }

// MovieListItem wraps *domain.MediaItem (movie) to implement ListItem
type MovieListItem struct {
	Movie *domain.MediaItem
}

func (i MovieListItem) ItemID() string { return i.Movie.ID }
func (i MovieListItem) ItemTitle() string {
	if i.Movie.Year > 0 {
		return fmt.Sprintf("%s (%d)", i.Movie.Title, i.Movie.Year)
	}
	return i.Movie.Title
}
func (i MovieListItem) ItemSubtitle() string { return i.Movie.FormattedDuration() }
func (i MovieListItem) FilterValue() string  { return i.Movie.Title }
func (i MovieListItem) SortTitle() string {
	if i.Movie.SortTitle != "" {
		return strings.ToLower(i.Movie.SortTitle)
	}
	return strings.ToLower(i.Movie.Title)
}
func (i MovieListItem) SortableYear() int          { return i.Movie.Year }
func (i MovieListItem) SortableAddedAt() int64     { return i.Movie.AddedAt }
func (i MovieListItem) SortableUpdatedAt() int64   { return i.Movie.UpdatedAt }
func (i MovieListItem) WatchStatus() domain.WatchStatus { return i.Movie.WatchStatus() }
func (i MovieListItem) CanDrillInto() bool         { return false }
func (i MovieListItem) Unwrap() interface{}        { return i.Movie }

// ShowListItem wraps *domain.Show to implement ListItem
type ShowListItem struct {
	Show *domain.Show
}

func (i ShowListItem) ItemID() string { return i.Show.ID }
func (i ShowListItem) ItemTitle() string {
	if i.Show.Year > 0 {
		return fmt.Sprintf("%s (%d)", i.Show.Title, i.Show.Year)
	}
	return i.Show.Title
}
func (i ShowListItem) ItemSubtitle() string {
	return fmt.Sprintf("%d seasons, %d episodes", i.Show.SeasonCount, i.Show.EpisodeCount)
}
func (i ShowListItem) FilterValue() string { return i.Show.Title }
func (i ShowListItem) SortTitle() string {
	if i.Show.SortTitle != "" {
		return strings.ToLower(i.Show.SortTitle)
	}
	return strings.ToLower(i.Show.Title)
}
func (i ShowListItem) SortableYear() int              { return i.Show.Year }
func (i ShowListItem) SortableAddedAt() int64         { return i.Show.AddedAt }
func (i ShowListItem) SortableUpdatedAt() int64       { return i.Show.UpdatedAt }
func (i ShowListItem) WatchStatus() domain.WatchStatus { return i.Show.WatchStatus() }
func (i ShowListItem) CanDrillInto() bool             { return true }
func (i ShowListItem) Unwrap() interface{}            { return i.Show }

// SeasonListItem wraps *domain.Season to implement ListItem
type SeasonListItem struct {
	Season *domain.Season
}

func (i SeasonListItem) ItemID() string               { return i.Season.ID }
func (i SeasonListItem) ItemTitle() string            { return i.Season.DisplayTitle() }
func (i SeasonListItem) ItemSubtitle() string         { return fmt.Sprintf("%d episodes", i.Season.EpisodeCount) }
func (i SeasonListItem) FilterValue() string          { return i.Season.DisplayTitle() }
func (i SeasonListItem) SortTitle() string            { return fmt.Sprintf("%03d", i.Season.SeasonNum) }
func (i SeasonListItem) SortableYear() int            { return 0 }
func (i SeasonListItem) SortableAddedAt() int64       { return 0 }
func (i SeasonListItem) SortableUpdatedAt() int64     { return 0 }
func (i SeasonListItem) WatchStatus() domain.WatchStatus { return i.Season.WatchStatus() }
func (i SeasonListItem) CanDrillInto() bool           { return true }
func (i SeasonListItem) Unwrap() interface{}          { return i.Season }

// EpisodeListItem wraps *domain.MediaItem (episode) to implement ListItem
type EpisodeListItem struct {
	Episode *domain.MediaItem
}

func (i EpisodeListItem) ItemID() string { return i.Episode.ID }
func (i EpisodeListItem) ItemTitle() string {
	return fmt.Sprintf("%s %s", i.Episode.EpisodeCode(), i.Episode.Title)
}
func (i EpisodeListItem) ItemSubtitle() string        { return i.Episode.FormattedDuration() }
func (i EpisodeListItem) FilterValue() string         { return i.Episode.Title }
func (i EpisodeListItem) SortTitle() string           { return fmt.Sprintf("%03d%03d", i.Episode.SeasonNum, i.Episode.EpisodeNum) }
func (i EpisodeListItem) SortableYear() int           { return 0 }
func (i EpisodeListItem) SortableAddedAt() int64      { return i.Episode.AddedAt }
func (i EpisodeListItem) SortableUpdatedAt() int64    { return i.Episode.UpdatedAt }
func (i EpisodeListItem) WatchStatus() domain.WatchStatus { return i.Episode.WatchStatus() }
func (i EpisodeListItem) CanDrillInto() bool          { return false }
func (i EpisodeListItem) Unwrap() interface{}         { return i.Episode }

// PlaylistListItem wraps *domain.Playlist to implement ListItem
type PlaylistListItem struct {
	Playlist *domain.Playlist
}

func (i PlaylistListItem) ItemID() string         { return i.Playlist.ID }
func (i PlaylistListItem) ItemTitle() string      { return i.Playlist.Title }
func (i PlaylistListItem) ItemSubtitle() string   { return fmt.Sprintf("%d items", i.Playlist.ItemCount) }
func (i PlaylistListItem) FilterValue() string    { return i.Playlist.Title }
func (i PlaylistListItem) SortTitle() string      { return strings.ToLower(i.Playlist.Title) }
func (i PlaylistListItem) SortableYear() int      { return 0 }
func (i PlaylistListItem) SortableAddedAt() int64 { return 0 }
func (i PlaylistListItem) SortableUpdatedAt() int64 { return i.Playlist.UpdatedAt }
func (i PlaylistListItem) WatchStatus() domain.WatchStatus {
	return domain.WatchStatusUnwatched // Playlists don't have watch status
}
func (i PlaylistListItem) CanDrillInto() bool  { return true }
func (i PlaylistListItem) Unwrap() interface{} { return i.Playlist }

// PlaylistMediaListItem wraps *domain.MediaItem (playlist item) to implement ListItem
// This is separate from MovieListItem/EpisodeListItem because playlist items
// may need different display formatting (e.g., showing show title for episodes)
type PlaylistMediaListItem struct {
	Item *domain.MediaItem
}

func (i PlaylistMediaListItem) ItemID() string { return i.Item.ID }
func (i PlaylistMediaListItem) ItemTitle() string {
	if i.Item.Type == domain.MediaTypeEpisode && i.Item.ShowTitle != "" {
		return fmt.Sprintf("%s - %s %s", i.Item.ShowTitle, i.Item.EpisodeCode(), i.Item.Title)
	}
	if i.Item.Year > 0 {
		return fmt.Sprintf("%s (%d)", i.Item.Title, i.Item.Year)
	}
	return i.Item.Title
}
func (i PlaylistMediaListItem) ItemSubtitle() string      { return i.Item.FormattedDuration() }
func (i PlaylistMediaListItem) FilterValue() string       { return i.Item.Title }
func (i PlaylistMediaListItem) SortTitle() string         { return strings.ToLower(i.Item.Title) }
func (i PlaylistMediaListItem) SortableYear() int         { return i.Item.Year }
func (i PlaylistMediaListItem) SortableAddedAt() int64    { return i.Item.AddedAt }
func (i PlaylistMediaListItem) SortableUpdatedAt() int64  { return i.Item.UpdatedAt }
func (i PlaylistMediaListItem) WatchStatus() domain.WatchStatus { return i.Item.WatchStatus() }
func (i PlaylistMediaListItem) CanDrillInto() bool        { return false }
func (i PlaylistMediaListItem) Unwrap() interface{}       { return i.Item }

// WrapLibraries converts a slice of domain.Library to []ListItem
func WrapLibraries(libs []domain.Library) []ListItem {
	items := make([]ListItem, len(libs))
	for i, lib := range libs {
		items[i] = LibraryListItem{Library: lib}
	}
	return items
}

// WrapMovies converts a slice of *domain.MediaItem (movies) to []ListItem
func WrapMovies(movies []*domain.MediaItem) []ListItem {
	items := make([]ListItem, len(movies))
	for i, m := range movies {
		items[i] = MovieListItem{Movie: m}
	}
	return items
}

// WrapShows converts a slice of *domain.Show to []ListItem
func WrapShows(shows []*domain.Show) []ListItem {
	items := make([]ListItem, len(shows))
	for i, s := range shows {
		items[i] = ShowListItem{Show: s}
	}
	return items
}

// WrapSeasons converts a slice of *domain.Season to []ListItem
func WrapSeasons(seasons []*domain.Season) []ListItem {
	items := make([]ListItem, len(seasons))
	for i, s := range seasons {
		items[i] = SeasonListItem{Season: s}
	}
	return items
}

// WrapEpisodes converts a slice of *domain.MediaItem (episodes) to []ListItem
func WrapEpisodes(episodes []*domain.MediaItem) []ListItem {
	items := make([]ListItem, len(episodes))
	for i, e := range episodes {
		items[i] = EpisodeListItem{Episode: e}
	}
	return items
}

// WrapPlaylists converts a slice of *domain.Playlist to []ListItem
func WrapPlaylists(playlists []*domain.Playlist) []ListItem {
	items := make([]ListItem, len(playlists))
	for i, p := range playlists {
		items[i] = PlaylistListItem{Playlist: p}
	}
	return items
}

// WrapPlaylistItems converts a slice of *domain.MediaItem (playlist items) to []ListItem
func WrapPlaylistItems(items []*domain.MediaItem) []ListItem {
	result := make([]ListItem, len(items))
	for i, item := range items {
		result[i] = PlaylistMediaListItem{Item: item}
	}
	return result
}
