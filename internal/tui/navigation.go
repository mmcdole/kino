package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

// pendingSelect selects id in the column showing key once that column has
// content, then opens the item if open is set. Search results use it to reach
// an item whose library may still be loading.
type pendingSelect struct {
	key, id string
	open    bool
}

// columnOptions describes what a column of each collection kind offers.
func columnOptions(kind catalog.Kind) components.ColumnOptions {
	switch kind {
	case catalog.Movies:
		return components.ColumnOptions{SortFields: components.MovieSortOptions(), DefaultSort: components.SortTitle}
	case catalog.Shows:
		return components.ColumnOptions{SortFields: components.ShowSortOptions(), DefaultSort: components.SortTitle}
	case catalog.Mixed:
		return components.ColumnOptions{SortFields: components.MixedSortOptions(), DefaultSort: components.SortTitle}
	case catalog.Episodes:
		return components.ColumnOptions{SortFields: components.EpisodeSortOptions(), DefaultSort: components.SortEpisodeNum}
	case catalog.PlaylistItems:
		return components.ColumnOptions{ShowParent: true}
	default:
		return components.ColumnOptions{}
	}
}

func (m *Model) pushColumn(r catalog.Resource, title string) tea.Cmd {
	col := components.NewListColumn(title, columnOptions(r.Kind))
	col.SetContentID(r.Key())
	col.SetShowWatchStatus(m.Options.ShowWatchStatus)
	m.ColumnStack.Push(col)
	m.track(r)
	if st := m.collection(r); st.Known {
		col.ReplaceItems(domain.CloneItems(st.Snapshot.Items))
	}
	m.updateLayout()
	return m.loadResource(r, catalog.Browse, false)
}

func (m *Model) drillSelected() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil || !top.CanDrillInto() {
		return nil
	}
	parent, _ := m.topResource()
	switch item := top.SelectedItem().(type) {
	case *domain.Library:
		if item.ID == playlistsLibraryID {
			return m.pushColumn(catalog.Resource{Kind: catalog.Playlists}, "Playlists")
		}
		return m.pushColumn(catalog.LibraryResource(*item), item.Name)
	case *domain.Show:
		return m.pushColumn(catalog.Resource{Kind: catalog.Seasons, ID: item.ID, LibraryID: parent.LibraryID, ShowID: item.ID}, item.Title)
	case *domain.Season:
		title := fmt.Sprintf("%s - S%02d", item.ShowTitle, item.SeasonNum)
		if item.SeasonNum == 0 {
			title = item.ShowTitle + " - Specials"
		}
		return m.pushColumn(catalog.Resource{Kind: catalog.Episodes, ID: item.ID, LibraryID: parent.LibraryID, ShowID: parent.ShowID}, title)
	case *domain.Playlist:
		return m.pushColumn(catalog.Resource{Kind: catalog.PlaylistItems, ID: item.ID}, item.Title)
	}
	return nil
}

func (m *Model) handleBack() tea.Cmd {
	m.pendingSelect = nil
	m.cancelPendingModal()
	m.popTo(m.ColumnStack.Len() - 1)
	return nil
}

// popTo closes the columns above depth, never the root, and detaches their
// foreground loads.
func (m *Model) popTo(depth int) {
	for m.ColumnStack.Len() > max(depth, 1) {
		col := m.ColumnStack.Pop()
		if r, ok := m.resource(col.ContentID()); ok {
			m.requests.stop(viewOwner(r))
			m.updateResourceFeedback(r)
		}
	}
	m.updateLayout()
}

// trySelect completes a pending selection in the column showing key. Until
// the load is final the item may still arrive, so a miss keeps waiting.
func (m *Model) trySelect(key string, final bool) tea.Cmd {
	p := m.pendingSelect
	col := m.ColumnStack.Top()
	if p == nil || p.key != key || col == nil || col.ContentID() != key {
		return nil
	}
	if !col.SetSelectedByID(p.id) {
		if !final {
			return nil
		}
		m.pendingSelect = nil
		return m.notify(NoticeError, "Item not found (library may have changed)")
	}
	m.pendingSelect = nil
	if !p.open {
		return nil
	}
	if cmd := m.drillSelected(); cmd != nil {
		return cmd
	}
	return m.notify(NoticeError, "Navigation failed")
}

func (m *Model) navigateToSearchResult(item search.Entry) tea.Cmd {
	m.pendingSelect = nil
	m.cancelPendingModal()
	m.popTo(1)
	lib := m.findLibrary(item.LibraryID)
	if lib == nil {
		return m.notify(NoticeError, "Library no longer available")
	}
	m.libraryColumn().SetSelectedByID(lib.ID)
	r := catalog.LibraryResource(*lib)
	m.pendingSelect = &pendingSelect{key: r.Key(), id: item.Item.GetID(), open: item.Type == domain.MediaTypeShow}
	return tea.Batch(m.pushColumn(r, lib.Name), m.trySelect(r.Key(), false))
}

// Revalidate the navigation ancestry when an authoritative parent snapshot
// changes. Retained columns cannot silently acquire a different parent.
func (m *Model) pruneNavigation() {
	for i := 1; i < m.ColumnStack.Len(); i++ {
		r, exists := m.resource(m.ColumnStack.Get(i).ContentID())
		expected := r.ID
		if r.Kind == catalog.Playlists {
			expected = playlistsLibraryID
		}
		parent := m.ColumnStack.Get(i - 1)
		if !parent.HasContent() {
			continue
		}
		if exists && parent.SetSelectedByID(expected) {
			continue
		}
		m.popTo(i)
		m.pendingSelect = nil
		m.notify(NoticeAlert, "Item no longer exists in this view — navigation reset")
		return
	}
}
