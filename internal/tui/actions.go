package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"time"
)

func (m *Model) beginMutation(change catalog.Mutation) tea.Cmd {
	owner := "mutation:playlist:" + change.PlaylistID
	if change.Kind == catalog.Watch {
		owner = "mutation:watch:" + change.ItemID
	}
	if change.Kind == catalog.CreatePlaylist {
		owner = "mutation:create:" + change.Title
	}
	// A second keypress cannot reorder two writes to the same item. Keep the
	// first operation pending until its result, rather than launching duplicates.
	if _, pending := m.requests.active[owner]; pending {
		return nil
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return MutationCmd(m.Catalog, req, change)
}

func (m *Model) beginPlayback(item domain.MediaItem, resume bool) tea.Cmd {
	owner := "playback:" + item.ID
	if _, pending := m.requests.active[owner]; pending {
		return nil
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return PlayItemCmd(m.PlaybackSvc, req, item, resume)
}

func (m *Model) cancelPendingModal() {
	if m.PlaylistModal.IsLoading() {
		m.PlaylistModal.Hide()
	}
	m.requests.stop("playlist-modal")
}

func (m *Model) scheduleSearch() tea.Cmd {
	m.requests.stop("search")
	m.searchSeq++
	seq, query := m.searchSeq, m.GlobalSearch.Query()
	if query == "" {
		m.GlobalSearch.SetResults(nil)
		return nil
	}
	m.GlobalSearch.SetLoading(true)
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return SearchDebounceMsg{Seq: seq, Query: query} })
}
