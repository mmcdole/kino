package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

// mutationOwner identifies what a write changes. Writes to different items in
// the same playlist are independent; the catalog serializes them.
func mutationOwner(change catalog.Mutation) string {
	switch change.Kind {
	case catalog.Watch:
		return "mutation:watch:" + change.ItemID
	case catalog.CreatePlaylist:
		return "mutation:create:" + change.Title
	case catalog.DeletePlaylist:
		return "mutation:playlist:" + change.PlaylistID
	default:
		return "mutation:playlist:" + change.PlaylistID + ":" + change.ItemID + strings.Join(change.ItemIDs, ",")
	}
}

func (m *Model) beginMutation(change catalog.Mutation) tea.Cmd {
	owner := mutationOwner(change)
	// A second keypress cannot reorder two writes to the same item. Keep the
	// first operation pending until its result, rather than launching duplicates.
	if _, pending := m.requests.active[owner]; pending {
		return m.notify(NoticeInfo, "Already updating — waiting for the server")
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return mutateCmd(m.Catalog, req, change)
}

func (m *Model) beginPlayback(item domain.MediaItem, resume bool) tea.Cmd {
	owner := "playback:" + item.ID
	if _, pending := m.requests.active[owner]; pending {
		return nil
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return playCmd(m.PlaybackSvc, req, item, resume)
}

func (m *Model) cancelPendingModal() {
	if m.overlay == overlayPlaylists && m.PlaylistModal.IsLoading() {
		m.overlay = overlayNone
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
	return tea.Batch(
		tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return SearchDebounceMsg{Seq: seq, Query: query} }),
		tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg { return ShowSearchLoadingMsg{Seq: seq} }),
	)
}
