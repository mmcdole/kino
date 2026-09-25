package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

// scheduleSearch debounces the current query and delays its activity
// indicator, abandoning any query still pending.
func (m *Model) scheduleSearch() tea.Cmd {
	m.requests.stop("search")
	m.searchSeq++
	seq, query := m.searchSeq, m.globalSearch.Query()
	if query == "" {
		m.globalSearch.SetResults(nil)
		return nil
	}
	m.globalSearch.SetLoading(true)
	return tea.Batch(
		tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return searchDebounceMsg{Seq: seq, Query: query} }),
		tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg { return showSearchLoadingMsg{Seq: seq} }),
	)
}

// runSearch queries the index once typing has paused, unless a newer query
// or a closed overlay made this one obsolete.
func (m *Model) runSearch(msg searchDebounceMsg) tea.Cmd {
	if m.overlay != overlaySearch || msg.Seq != m.searchSeq {
		return nil
	}
	req := m.requests.begin("search", catalog.Resource{}, catalog.Browse)
	libraries := append([]domain.Library(nil), m.libraries...)
	index := m.searchIndex
	return func() tea.Msg {
		return searchResultsMsg{Request: req, Results: index.Search(req.ctx, msg.Query, libraries)}
	}
}

// showSearchResults displays results for the query still being asked.
func (m *Model) showSearchResults(msg searchResultsMsg) {
	if !m.requests.owns(msg.Request) || m.overlay != overlaySearch {
		return
	}
	m.requests.finish(msg.Request)
	m.globalSearch.SetResults(msg.Results)
}

// closeSearch closes global search and abandons any pending query.
func (m *Model) closeSearch() {
	m.overlay = overlayNone
	m.requests.stop("search")
	m.searchSeq++
}

func (m *Model) navigateToSearchResult(item search.Entry) tea.Cmd {
	m.pendingSelect = nil
	m.cancelPendingModal()
	m.popTo(1)
	lib := m.findLibrary(item.LibraryID)
	if lib == nil {
		return m.notify(noticeError, "Library no longer available")
	}
	m.libraryColumn().SetSelectedByID(lib.ID)
	r := catalog.LibraryResource(*lib)
	m.pendingSelect = &pendingSelect{key: r.Key(), id: item.Item.GetID(), open: item.Type == domain.MediaTypeShow}
	return tea.Batch(m.pushColumn(r, lib.Name), m.trySelect(r.Key(), false))
}
