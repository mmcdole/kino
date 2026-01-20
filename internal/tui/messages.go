package tui

import (
	"github.com/drake/goplex/internal/domain"
)

// Message types for the TUI

// ErrMsg represents an error
type ErrMsg struct {
	Err     error
	Context string
}

// Error implements the error interface
func (e ErrMsg) Error() string {
	if e.Context != "" {
		return e.Context + ": " + e.Err.Error()
	}
	return e.Err.Error()
}

// LibrariesLoadedMsg signals that libraries have been loaded
type LibrariesLoadedMsg struct {
	Libraries []domain.Library
}

// MoviesLoadedMsg signals that movies have been loaded
type MoviesLoadedMsg struct {
	Movies    []*domain.MediaItem
	LibraryID string
}

// ShowsLoadedMsg signals that shows have been loaded
type ShowsLoadedMsg struct {
	Shows     []*domain.Show
	LibraryID string
}

// SeasonsLoadedMsg signals that seasons have been loaded
type SeasonsLoadedMsg struct {
	Seasons []*domain.Season
	ShowID  string
}

// EpisodesLoadedMsg signals that episodes have been loaded
type EpisodesLoadedMsg struct {
	Episodes []*domain.MediaItem
	SeasonID string
}

// SearchResultsMsg signals that search results are ready
type SearchResultsMsg struct {
	Results []domain.MediaItem
	Query   string
}

// PlaybackStartedMsg signals that playback has started (player launched)
type PlaybackStartedMsg struct {
	Item domain.MediaItem
}

// ItemMarkedPlayedMsg signals that an item was marked as played
type ItemMarkedPlayedMsg struct {
	ItemID string
}

// MarkWatchedMsg signals a request to mark an item as watched
type MarkWatchedMsg struct {
	ItemID string
	Title  string
}

// MarkUnwatchedMsg signals a request to mark an item as unwatched
type MarkUnwatchedMsg struct {
	ItemID string
	Title  string
}

// AuthRequiredMsg signals that authentication is needed
type AuthRequiredMsg struct{}

// PINGeneratedMsg contains a new PIN for authentication
type PINGeneratedMsg struct {
	PIN string
	ID  int
}

// AuthSuccessMsg signals successful authentication
type AuthSuccessMsg struct {
	Token string
}

// RefreshMsg triggers a refresh of the current view
type RefreshMsg struct{}

// ResizeMsg signals a terminal resize
type ResizeMsg struct {
	Width  int
	Height int
}

// FocusChangeMsg signals a focus change between panes
type FocusChangeMsg struct {
	Pane Pane
}

// NavigateMsg signals navigation to a new level
type NavigateMsg struct {
	Level     BrowseLevel
	ID        string
	Title     string
	ParentCtx *NavContext
}

// NavigateBackMsg signals navigation back one level
type NavigateBackMsg struct{}

// ShowHelpMsg shows the help screen
type ShowHelpMsg struct{}

// HideHelpMsg hides the help screen
type HideHelpMsg struct{}

// ShowSearchMsg shows the search modal
type ShowSearchMsg struct{}

// HideSearchMsg hides the search modal
type HideSearchMsg struct{}

// SelectItemMsg signals item selection in the browser
type SelectItemMsg struct {
	Index int
}

// TickMsg is a general tick message for animations
type TickMsg struct{}

// ClearStatusMsg clears the status bar message
type ClearStatusMsg struct{}

// StatusMsg sets a temporary status message
type StatusMsg struct {
	Message string
	IsError bool
}

// GlobalSearchReadyMsg signals that all content is loaded for global search
type GlobalSearchReadyMsg struct {
	MovieCount       int
	ShowCount        int
	EpisodeCount     int
	SkippedLibraries int // Libraries not yet cached (still syncing)
}

// LibraryStatus represents the sync status of a library
type LibraryStatus int

const (
	StatusIdle LibraryStatus = iota
	StatusSyncing
	StatusSynced
	StatusError
)

// LibrarySyncState tracks sync progress for a single library
type LibrarySyncState struct {
	Status   LibraryStatus
	Loaded   int    // Items loaded so far
	Total    int    // Total items expected
	FromDisk bool   // Whether loaded from cache
	Error    error  // Error if any
}

// LibrarySyncProgressMsg sent for each chunk during streaming sync
type LibrarySyncProgressMsg struct {
	LibraryID   string
	LibraryType string
	Loaded      int
	Total       int
	Items       interface{} // Current chunk for indexing
	Done        bool
	FromDisk    bool
	Error       error
	NextCmd     interface{} // Continuation command (tea.Cmd) for streaming
}

// ClearLibraryStatusMsg signals that the success indicator should be removed
type ClearLibraryStatusMsg struct {
	LibraryID string
}
