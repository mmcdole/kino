package components

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Column is the interface for all navigable columns in Miller Columns layout.
// The Inspector is NOT a Column - it's a view projection of the selected item.
type Column interface {
	// Bubble Tea lifecycle
	Init() tea.Cmd
	Update(msg tea.Msg) (Column, tea.Cmd)
	View() string

	// Sizing
	SetSize(width, height int)
	Width() int
	Height() int

	// Focus management
	SetFocused(focused bool)
	IsFocused() bool

	// Column metadata
	Title() string // Column header (replaces breadcrumb)

	// Item selection
	SelectedItem() interface{}   // Returns the currently selected item
	SelectedIndex() int          // Returns cursor position
	SetSelectedIndex(idx int)    // Sets cursor position
	ItemCount() int              // Total items in column

	// Navigation
	CanDrillInto() bool // True if selected item can be drilled into (Show, Season, Library)
	IsEmpty() bool      // True if no items

	// Loading state for async data loading
	SetLoading(loading bool)
	IsLoading() bool

	// Data population (called after async load completes)
	SetItems(items interface{})
}

// ColumnType identifies the type of content in a column
type ColumnType int

const (
	ColumnTypeLibraries ColumnType = iota
	ColumnTypeMovies
	ColumnTypeShows
	ColumnTypeSeasons
	ColumnTypeEpisodes
	ColumnTypePlaylists
	ColumnTypePlaylistItems
	ColumnTypeEmpty
)
