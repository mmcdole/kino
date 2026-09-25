package tui

import (
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

// statesMsg carries collection states published by the catalog.
type statesMsg []catalog.State

// loadDoneMsg reports that a load request settled.
type loadDoneMsg struct {
	Request request
	Warning error // usable data could not be persisted
	Err     error
}

type actionMsg struct {
	Request  request
	Change   catalog.Change
	Item     domain.MediaItem
	Playback bool
	Err      error
}

type playlistModalDataMsg struct {
	Request    request
	Membership catalog.Membership
	Item       domain.MediaItem
	Err        error
}

type tickMsg struct{}
type showLoadingMsg struct {
	Key     string
	Attempt uint64
}
type logoutCompleteMsg struct{ Error error }
type searchDebounceMsg struct {
	Seq   uint64
	Query string
}
type showSearchLoadingMsg struct{ Seq uint64 }
type searchResultsMsg struct {
	Request request
	Results []search.Result
}
type searchIndexChangedMsg struct{}
