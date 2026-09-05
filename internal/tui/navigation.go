package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

type NavPlan struct {
	Targets  []string
	Step     int
	AwaitKey string
}

func (m *Model) clearNavPlan() { m.navPlan = nil }

func columnType(kind catalog.Kind) components.ColumnType {
	switch kind {
	case catalog.Libraries:
		return components.ColumnTypeLibraries
	case catalog.Movies:
		return components.ColumnTypeMovies
	case catalog.Shows:
		return components.ColumnTypeShows
	case catalog.Mixed:
		return components.ColumnTypeMixed
	case catalog.Seasons:
		return components.ColumnTypeSeasons
	case catalog.Episodes:
		return components.ColumnTypeEpisodes
	case catalog.Playlists:
		return components.ColumnTypePlaylists
	default:
		return components.ColumnTypePlaylistItems
	}
}

func (m *Model) pushColumn(r catalog.Resource, title string, cursor int) tea.Cmd {
	col := components.NewListColumn(columnType(r.Kind), title)
	col.SetContentID(r.Key())
	col.SetShowWatchStatus(m.UIConfig.ShowWatchStatus)
	m.ColumnStack.Push(col, cursor)
	m.resources[r.Key()] = r
	if m.navPlan != nil {
		m.navPlan.AwaitKey = r.Key()
	}
	m.updateLayout()
	m.updateInspector()
	return m.loadResource(r, catalog.Browse, false)
}

func (m *Model) drillSelected() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil || !top.CanDrillInto() {
		return nil
	}
	parent, _ := m.topResource()
	cursor := top.SelectedIndex()
	switch item := top.SelectedItem().(type) {
	case *domain.Library:
		if item.ID == playlistsLibraryID {
			return m.pushColumn(catalog.Resource{Kind: catalog.Playlists}, "Playlists", cursor)
		}
		return m.pushColumn(catalog.LibraryResource(*item), item.Name, cursor)
	case *domain.Show:
		return m.pushColumn(catalog.Resource{Kind: catalog.Seasons, ID: item.ID, LibraryID: parent.LibraryID, ShowID: item.ID}, item.Title, cursor)
	case *domain.Season:
		title := fmt.Sprintf("%s - S%02d", item.ShowTitle, item.SeasonNum)
		if item.SeasonNum == 0 {
			title = item.ShowTitle + " - Specials"
		}
		return m.pushColumn(catalog.Resource{Kind: catalog.Episodes, ID: item.ID, LibraryID: parent.LibraryID, ShowID: parent.ShowID}, title, cursor)
	case *domain.Playlist:
		return m.pushColumn(catalog.Resource{Kind: catalog.PlaylistItems, ID: item.ID}, item.Title, cursor)
	}
	return nil
}

func (m Model) drillIntoSelection() (tea.Model, tea.Cmd) { return m, m.drillSelected() }

func (m Model) handleBack() (tea.Model, tea.Cmd) {
	m.clearNavPlan()
	m.cancelPendingModal()
	if !m.ColumnStack.CanGoBack() {
		return m, nil
	}
	top := m.ColumnStack.Top()
	if r, ok := m.resources[top.ContentID()]; ok {
		m.requests.stop(viewOwner(r))
	}
	m.ColumnStack.Pop()
	m.updateLayout()
	m.updateInspector()
	return m, nil
}

func (m *Model) advanceNavPlanAfterLoad(key string, final bool) tea.Cmd {
	p := m.navPlan
	if p == nil || p.AwaitKey != key {
		return nil
	}
	col := m.ColumnStack.Top()
	if col == nil || col.ContentID() != key {
		m.clearNavPlan()
		return nil
	}
	target := p.Targets[p.Step]
	if target != "" && !col.SetSelectedByID(target) {
		if !final {
			return nil
		} // The fresh response may contain a newly added item.
		m.clearNavPlan()
		return m.notify(NoticeError, "Item not found (library may have changed)")
	}
	p.Step++
	if p.Step == len(p.Targets) {
		m.clearNavPlan()
		m.updateInspector()
		return nil
	}
	cmd := m.drillSelected()
	if cmd == nil {
		m.clearNavPlan()
		return m.notify(NoticeError, "Navigation failed")
	}
	return cmd
}

func (m *Model) navigateToSearchResult(item search.FilterItem) tea.Cmd {
	m.clearNavPlan()
	m.cancelPendingModal()
	for m.ColumnStack.CanGoBack() {
		col := m.ColumnStack.Top()
		m.requests.stop("view:" + col.ContentID())
		m.ColumnStack.Pop()
	}
	lib := m.findLibrary(item.LibraryID)
	if lib == nil {
		return m.notify(NoticeError, "Library no longer available")
	}
	m.libraryColumn().SetSelectedByID(lib.ID)
	targets := []string{item.Item.GetID()}
	if item.Type == domain.MediaTypeShow {
		targets = append(targets, "")
	}
	m.navPlan = &NavPlan{Targets: targets}
	return m.pushColumn(catalog.LibraryResource(*lib), lib.Name, m.libraryColumn().SelectedIndex())
}

// Revalidate the navigation ancestry when an authoritative parent snapshot
// changes. Retained columns cannot silently acquire a different parent.
func (m *Model) pruneNavigation() {
	for i := 1; i < m.ColumnStack.Len(); i++ {
		r := m.resources[m.ColumnStack.Get(i).ContentID()]
		expected := r.ID
		if r.Kind == catalog.Playlists {
			expected = playlistsLibraryID
		}
		parent := m.ColumnStack.Get(i - 1)
		if !parent.HasContent() {
			continue
		}
		if parent.SetSelectedByID(expected) {
			continue
		}
		for m.ColumnStack.Len() > i {
			col := m.ColumnStack.Top()
			m.requests.stop("view:" + col.ContentID())
			m.ColumnStack.Pop()
		}
		m.clearNavPlan()
		m.notify(NoticeAlert, "Item no longer exists in this view — navigation reset")
		m.updateLayout()
		m.updateInspector()
		return
	}
}
