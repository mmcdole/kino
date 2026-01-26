package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the application
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Enter    key.Binding
	Back     key.Binding
	HalfUp   key.Binding
	HalfDown key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Actions
	Quit           key.Binding
	Help           key.Binding
	Escape         key.Binding
	Filter         key.Binding
	GlobalSearch   key.Binding
	Sort           key.Binding
	Refresh        key.Binding
	RefreshAll     key.Binding
	MarkWatched    key.Binding
	MarkUnwatched  key.Binding
	Play           key.Binding
	ToggleInspector key.Binding
	Logout         key.Binding
	PlaylistModal  key.Binding
	Delete         key.Binding
	NewPlaylist    key.Binding

	// Confirmations
	Confirm key.Binding
	Deny    key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Navigation
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left", "backspace"),
			key.WithHelp("h/←", "back"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/→", "expand"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select/play"),
		),
		Back: key.NewBinding(
			key.WithKeys("h", "left", "backspace"),
			key.WithHelp("h/←", "back"),
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
		Home: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "go to top"),
		),
		End: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "go to bottom"),
		),

		// Actions
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel/clear"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		GlobalSearch: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "global search"),
		),
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		RefreshAll: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "refresh all"),
		),
		MarkWatched: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "mark watched"),
		),
		MarkUnwatched: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "mark unwatched"),
		),
		Play: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "play"),
		),
		ToggleInspector: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "toggle inspector"),
		),
		Logout: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "logout"),
		),
		PlaylistModal: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "playlist"),
		),
		Delete: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "delete/remove"),
		),
		NewPlaylist: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),

		// Confirmations
		Confirm: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "confirm"),
		),
		Deny: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n/esc", "cancel"),
		),
	}
}

// Keys is the global key bindings instance
var Keys = DefaultKeyMap()
