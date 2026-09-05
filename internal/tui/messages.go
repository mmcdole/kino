package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

type loadStage uint8

const (
	loadCached loadStage = iota
	loadProgress
	loadNetwork
	loadFinished
)

type ResourceMsg struct {
	Request  request
	Stage    loadStage
	Snapshot catalog.Snapshot
	Progress catalog.Progress
	Err      error
	Next     tea.Cmd
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
type ShowLoadingMsg struct{ Request request }
type LogoutCompleteMsg struct{ Error error }
type SearchDebounceMsg struct {
	Seq   uint64
	Query string
}
type ShowSearchLoadingMsg struct{ Seq uint64 }
type SearchResultsMsg struct {
	Request request
	Results []search.FilterResult
}
type SearchIndexChangedMsg struct{}
