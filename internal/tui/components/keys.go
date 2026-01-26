package components

import "github.com/charmbracelet/bubbles/key"

// ListColumnKeyMap defines key bindings for list column navigation
type ListColumnKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Home     key.Binding
	End      key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Escape   key.Binding
	Enter    key.Binding
	Filter   key.Binding
}

// DefaultListColumnKeyMap returns the default list column key bindings
func DefaultListColumnKeyMap() ListColumnKeyMap {
	return ListColumnKeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Home: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "go to top"),
		),
		End: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "go to bottom"),
		),
		HalfUp: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("C-u", "half page up"),
		),
		HalfDown: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("C-d", "half page down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("PgUp", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("PgDn", "page down"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear filter"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "accept filter"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
	}
}

// GlobalSearchKeyMap defines key bindings for the global search component
type GlobalSearchKeyMap struct {
	Escape key.Binding
	Enter  key.Binding
	Up     key.Binding
	Down   key.Binding
}

// DefaultGlobalSearchKeyMap returns the default global search key bindings
func DefaultGlobalSearchKeyMap() GlobalSearchKeyMap {
	return GlobalSearchKeyMap{
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "ctrl+p"),
			key.WithHelp("↑/C-p", "previous"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "ctrl+n"),
			key.WithHelp("↓/C-n", "next"),
		),
	}
}

// PlaylistModalKeyMap defines key bindings for the playlist modal
type PlaylistModalKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	Create key.Binding
	Enter  key.Binding
	Escape key.Binding
}

// DefaultPlaylistModalKeyMap returns the default playlist modal key bindings
func DefaultPlaylistModalKeyMap() PlaylistModalKeyMap {
	return PlaylistModalKeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "toggle"),
		),
		Create: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new playlist"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// Package-level key map instances
var (
	ListColumnKeys    = DefaultListColumnKeyMap()
	GlobalSearchKeys  = DefaultGlobalSearchKeyMap()
	PlaylistModalKeys = DefaultPlaylistModalKeyMap()
)
