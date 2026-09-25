package tui

import (
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

// StatesMsg carries collection states published by the catalog.
type StatesMsg []catalog.State

// LoadDoneMsg reports that a load request settled.
type LoadDoneMsg struct {
	Request request
	Warning error // usable data could not be persisted
	Err     error
}

type ActionMsg struct {
	Request  request
	Change   catalog.Change
	Item     domain.MediaItem
	Playback bool
	Err      error
}

type PlaylistModalDataMsg struct {
	Request    request
	Membership catalog.Membership
	Item       domain.MediaItem
	Err        error
}

type TickMsg struct{}
type ShowLoadingMsg struct {
	Key     string
	Attempt uint64
}
type LogoutCompleteMsg struct{ Error error }
type SearchDebounceMsg struct {
	Seq   uint64
	Query string
}
type ShowSearchLoadingMsg struct{ Seq uint64 }
type SearchResultsMsg struct {
	Request request
	Results []search.Result
}
type SearchIndexChangedMsg struct{}
