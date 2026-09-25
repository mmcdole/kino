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
	col.SetShowWatchStatus(m.UIConfig.ShowWatchStatus)
	m.ColumnStack.Push(col)
	m.track(r)
	if st := m.collection(r); st.Known {
		col.ReplaceItems(domain.CloneItems(st.Snapshot.Items))
	}
	if m.navPlan != nil {
		m.navPlan.AwaitKey = r.Key()
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
	m.clearNavPlan()
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
	m.popTo(1)
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
	return m.pushColumn(catalog.LibraryResource(*lib), lib.Name)
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
		m.clearNavPlan()
		m.notify(NoticeAlert, "Item no longer exists in this view — navigation reset")
		return
	}
}
