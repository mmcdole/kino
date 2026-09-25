package tui

// overlay is whatever is drawn over the columns and receives keys first.
// Only one is open at a time; key routing, message routing and drawing all
// switch on this one field.
type overlay uint8

const (
	overlayNone overlay = iota
	overlayHelp
	overlayConfirmLogout
	overlayConfirmDelete
	overlaySearch
	overlaySort
	overlayPlaylists
	overlayInput
)
