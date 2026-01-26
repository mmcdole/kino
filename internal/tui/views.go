package tui

import (
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Pane represents a UI pane (kept for compatibility but not used in Miller Columns)
type Pane int

const (
	PaneSidebar Pane = iota
	PaneBrowser
	PaneInspector
)

// BrowseLevel tracks where we are in hierarchical navigation
type BrowseLevel int

const (
	BrowseLevelLibrary BrowseLevel = iota // Movies or Shows list
	BrowseLevelShow                       // Seasons of a show
	BrowseLevelSeason                     // Episodes of a season
)

// NavContext tracks navigation breadcrumbs for TV shows
type NavContext struct {
	Level       BrowseLevel
	LibraryID   string
	LibraryName string
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int

	// State Restoration
	CursorPos  int
	PageOffset int
}

// RenderSpinner renders a loading spinner
func RenderSpinner(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return styles.SpinnerStyle.Render(frames[frame%len(frames)])
}

