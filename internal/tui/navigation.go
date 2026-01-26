package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
	"github.com/mmcdole/kino/internal/tui/components"
)

// NavAwaitKind specifies what async load the plan is waiting for
type NavAwaitKind int

const (
	AwaitNone     NavAwaitKind = iota
	AwaitMovies                // AwaitID = LibraryID
	AwaitShows                 // AwaitID = LibraryID
	AwaitMixed                 // AwaitID = LibraryID (mixed content library)
	AwaitSeasons               // AwaitID = ShowID
	AwaitEpisodes              // AwaitID = SeasonID
)

// NavTarget represents a single navigation step
type NavTarget struct {
	ID string // item ID to select (empty = no-op, just land)
}

// NavPlan represents a multi-step navigation flow
type NavPlan struct {
	Targets     []NavTarget
	CurrentStep int
	AwaitKind   NavAwaitKind
	AwaitID     string
}

func (p *NavPlan) IsComplete() bool {
	return p == nil || p.CurrentStep >= len(p.Targets)
}

func (p *NavPlan) Current() *NavTarget {
	if p.IsComplete() {
		return nil
	}
	return &p.Targets[p.CurrentStep]
}

func (p *NavPlan) Advance() {
	if p != nil {
		p.CurrentStep++
	}
}

// drillResult contains the result of drilling into an item
type drillResult struct {
	AwaitKind NavAwaitKind
	AwaitID   string
	Cmd       tea.Cmd
}

// NavigationContext contains information needed to navigate to an item
// This is purely a TUI concern - the service layer provides FilterItem with LibraryID,
// and the TUI decides how to navigate based on that.
type NavigationContext struct {
	LibraryID   string
	LibraryName string
	MovieID     string
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int
	EpisodeID   string
}

// buildNavContext constructs navigation context from a filter result
func (m *Model) buildNavContext(item service.FilterItem) NavigationContext {
	lib := m.findLibrary(item.LibraryID)
	libName := ""
	if lib != nil {
		libName = lib.Name
	}

	ctx := NavigationContext{
		LibraryID:   item.LibraryID,
		LibraryName: libName,
	}

	switch item.Type {
	case domain.MediaTypeMovie:
		ctx.MovieID = item.Item.GetID()
	case domain.MediaTypeShow:
		ctx.ShowID = item.Item.GetID()
		if show, ok := item.Item.(*domain.Show); ok {
			ctx.ShowTitle = show.Title
		}
	}

	return ctx
}

// clearNavPlan clears the current navigation plan
func (m *Model) clearNavPlan() {
	m.navPlan = nil
}

// drillSelected pushes a new column for the selected item and returns await info
func (m *Model) drillSelected() *drillResult {
	top := m.ColumnStack.Top()
	if top == nil || !top.CanDrillInto() {
		return nil
	}
	item := top.SelectedItem()
	if item == nil {
		return nil
	}
	cursor := top.SelectedIndex()

	switch v := item.(type) {
	case domain.Library:
		// Handle synthetic "Playlists" entry
		if v.ID == "__playlists__" {
			col := components.NewListColumn(components.ColumnTypePlaylists, "Playlists")
			col.SetLoading(true)
			m.ColumnStack.Push(col, cursor)
			m.Loading = true
			m.updateLayout()
			return &drillResult{
				AwaitKind: AwaitNone,
				Cmd:       LoadPlaylistsCmd(m.PlaylistSvc),
			}
		}

		switch v.Type {
		case "movie":
			col := components.NewListColumn(components.ColumnTypeMovies, v.Name)
			m.ColumnStack.Push(col, cursor)
			m.updateLayout()

			// Use cached data if available for instant display
			if cached := m.LibrarySvc.GetCachedMovies(v.ID); cached != nil {
				col.SetItems(cached)
				m.updateInspector()
				// If NavPlan active, advance it immediately
				if m.navPlan != nil {
					return &drillResult{
						AwaitKind: AwaitMovies,
						AwaitID:   v.ID,
						Cmd:       m.advanceNavPlanAfterLoad(AwaitMovies, v.ID),
					}
				}
				return &drillResult{AwaitKind: AwaitNone}
			}

			// Not cached - show loading and fetch async
			col.SetLoading(true)
			m.Loading = true
			return &drillResult{
				AwaitKind: AwaitMovies,
				AwaitID:   v.ID,
				Cmd:       LoadMoviesCmd(m.LibrarySvc, v.ID),
			}

		case "show":
			col := components.NewListColumn(components.ColumnTypeShows, v.Name)
			m.ColumnStack.Push(col, cursor)
			m.updateLayout()

			// Use cached data if available for instant display
			if cached := m.LibrarySvc.GetCachedShows(v.ID); cached != nil {
				col.SetItems(cached)
				m.updateInspector()
				// If NavPlan active, advance it immediately
				if m.navPlan != nil {
					return &drillResult{
						AwaitKind: AwaitShows,
						AwaitID:   v.ID,
						Cmd:       m.advanceNavPlanAfterLoad(AwaitShows, v.ID),
					}
				}
				return &drillResult{AwaitKind: AwaitNone}
			}

			// Not cached - show loading and fetch async
			col.SetLoading(true)
			m.Loading = true
			return &drillResult{
				AwaitKind: AwaitShows,
				AwaitID:   v.ID,
				Cmd:       LoadShowsCmd(m.LibrarySvc, v.ID),
			}

		case "mixed":
			col := components.NewListColumn(components.ColumnTypeMixed, v.Name)
			m.ColumnStack.Push(col, cursor)
			m.updateLayout()

			// Use cached data if available for instant display
			if cached := m.LibrarySvc.GetCachedLibraryContent(v.ID); cached != nil {
				col.SetItems(cached)
				m.updateInspector()
				if m.navPlan != nil {
					return &drillResult{
						AwaitKind: AwaitMixed,
						AwaitID:   v.ID,
						Cmd:       m.advanceNavPlanAfterLoad(AwaitMixed, v.ID),
					}
				}
				return &drillResult{AwaitKind: AwaitNone}
			}

			// Not cached - show loading and fetch async
			col.SetLoading(true)
			m.Loading = true
			return &drillResult{
				AwaitKind: AwaitMixed,
				AwaitID:   v.ID,
				Cmd:       LoadMixedLibraryCmd(m.LibrarySvc, v.ID),
			}

		default:
			// Unknown library type - treat as mixed
			col := components.NewListColumn(components.ColumnTypeMixed, v.Name)
			col.SetLoading(true)
			m.ColumnStack.Push(col, cursor)
			m.Loading = true
			m.updateLayout()
			return &drillResult{
				AwaitKind: AwaitMixed,
				AwaitID:   v.ID,
				Cmd:       LoadMixedLibraryCmd(m.LibrarySvc, v.ID),
			}
		}

	case *domain.Show:
		col := components.NewListColumn(components.ColumnTypeSeasons, v.Title)
		col.SetLoading(true)
		m.ColumnStack.Push(col, cursor)
		m.Loading = true
		m.updateLayout()
		return &drillResult{
			AwaitKind: AwaitSeasons,
			AwaitID:   v.ID,
			Cmd:       LoadSeasonsCmd(m.LibrarySvc, v.ID),
		}

	case *domain.Season:
		title := v.ShowTitle
		if v.SeasonNum == 0 {
			title += " - Specials"
		} else {
			title += fmt.Sprintf(" - S%02d", v.SeasonNum)
		}
		col := components.NewListColumn(components.ColumnTypeEpisodes, title)
		col.SetLoading(true)
		m.ColumnStack.Push(col, cursor)
		m.Loading = true
		m.updateLayout()
		return &drillResult{
			AwaitKind: AwaitEpisodes,
			AwaitID:   v.ID,
			Cmd:       LoadEpisodesCmd(m.LibrarySvc, v.ID),
		}

	case *domain.Playlist:
		col := components.NewListColumn(components.ColumnTypePlaylistItems, v.Title)
		col.SetLoading(true)
		m.ColumnStack.Push(col, cursor)
		m.Loading = true
		m.currentPlaylistID = v.ID
		m.updateLayout()
		return &drillResult{
			AwaitKind: AwaitNone, // Playlists don't use the NavPlan system
			AwaitID:   v.ID,
			Cmd:       LoadPlaylistItemsCmd(m.PlaylistSvc, v.ID),
		}
	}
	return nil
}

// drillIntoSelection pushes a new column for the selected item
func (m Model) drillIntoSelection() (tea.Model, tea.Cmd) {
	result := m.drillSelected()
	if result == nil {
		return m, nil
	}
	return m, result.Cmd
}

// handleBack handles navigation back (h/backspace)
func (m Model) handleBack() (tea.Model, tea.Cmd) {
	if !m.ColumnStack.CanGoBack() {
		return m, nil
	}

	// Check if we're leaving playlist items view
	if top := m.ColumnStack.Top(); top != nil && top.ColumnType() == components.ColumnTypePlaylistItems {
		m.currentPlaylistID = ""
	}

	_, savedCursor := m.ColumnStack.Pop()

	// Restore cursor position on the new top
	if top := m.ColumnStack.Top(); top != nil {
		top.SetSelectedIndex(savedCursor)
	}

	m.updateLayout()
	m.updateInspector()
	return m, nil
}

// advanceNavPlanAfterLoad advances the navigation plan after an async load completes
func (m *Model) advanceNavPlanAfterLoad(kind NavAwaitKind, id string) tea.Cmd {
	p := m.navPlan
	if p == nil || p.IsComplete() {
		m.navPlan = nil
		return nil
	}
	// Only advance if this is the awaited load
	if p.AwaitKind != kind || p.AwaitID != id {
		return nil
	}

	top := m.ColumnStack.Top()
	if top == nil {
		m.clearNavPlan()
		return nil
	}

	target := p.Current()
	if target == nil {
		m.clearNavPlan()
		return nil
	}

	// Apply ID selection if requested
	if target.ID != "" {
		if !top.SetSelectedByID(target.ID) {
			m.clearNavPlan()
			m.StatusMsg = "Item not found (library may have changed)"
			m.StatusIsErr = true
			return ClearStatusCmd(5 * time.Second)
		}
	}

	p.Advance()

	if p.IsComplete() {
		m.clearNavPlan()
		m.updateInspector()
		return nil
	}

	// More steps: drill to next level
	result := m.drillSelected()
	if result == nil {
		m.clearNavPlan()
		m.StatusMsg = "Navigation failed"
		m.StatusIsErr = true
		return ClearStatusCmd(5 * time.Second)
	}
	// Update navPlan with await info for next load
	m.navPlan.AwaitKind = result.AwaitKind
	m.navPlan.AwaitID = result.AwaitID
	return result.Cmd
}

// navigateToFilteredItem navigates to a filtered item in its context
func (m *Model) navigateToFilteredItem(item service.FilterItem) tea.Cmd {
	// Build navigation context from the filter item
	navCtx := m.buildNavContext(item)

	// Append synthetic "Playlists" entry at bottom
	playlistsEntry := domain.Library{
		ID:   "__playlists__",
		Name: "Playlists",
		Type: "playlist",
	}
	allEntries := append(m.Libraries, playlistsEntry)

	// Reset stack to library level first
	libCol := components.NewLibraryColumn(allEntries)
	libCol.SetLibraryStates(m.LibraryStates)
	m.Inspector.SetLibraryStates(m.LibraryStates)

	// Find and select the library
	for i, lib := range m.Libraries {
		if lib.ID == navCtx.LibraryID {
			libCol.SetSelectedIndex(i)
			break
		}
	}

	m.ColumnStack.Reset(libCol)

	switch item.Type {
	case domain.MediaTypeMovie:
		lib := m.findLibrary(navCtx.LibraryID)
		if lib == nil {
			return nil
		}

		// Handle mixed libraries differently - they use different cache/API
		if lib.Type == "mixed" {
			m.navPlan = &NavPlan{
				Targets:     []NavTarget{{ID: navCtx.MovieID}},
				CurrentStep: 0,
				AwaitKind:   AwaitMixed,
				AwaitID:     lib.ID,
			}

			mixedCol := components.NewListColumn(components.ColumnTypeMixed, lib.Name)

			// If cached, populate and immediately advance
			if cached := m.LibrarySvc.GetCachedLibraryContent(lib.ID); cached != nil {
				mixedCol.SetItems(cached)
				m.ColumnStack.Push(mixedCol, 0)
				m.updateLayout()
				return m.advanceNavPlanAfterLoad(AwaitMixed, lib.ID)
			}

			mixedCol.SetLoading(true)
			m.ColumnStack.Push(mixedCol, 0)
			m.Loading = true
			m.updateLayout()
			return LoadMixedLibraryCmd(m.LibrarySvc, lib.ID)
		}

		m.navPlan = &NavPlan{
			Targets: []NavTarget{
				{ID: navCtx.MovieID},
			},
			CurrentStep: 0,
			AwaitKind:   AwaitMovies,
			AwaitID:     lib.ID,
		}

		moviesCol := components.NewListColumn(components.ColumnTypeMovies, lib.Name)
		moviesCol.SetLoading(true)
		m.ColumnStack.Push(moviesCol, 0)
		m.Loading = true
		m.updateLayout()
		return LoadMoviesCmd(m.LibrarySvc, navCtx.LibraryID)

	case domain.MediaTypeShow:
		lib := m.findLibrary(navCtx.LibraryID)
		if lib == nil {
			return nil
		}

		show, ok := item.Item.(*domain.Show)
		if !ok {
			return nil
		}

		// Handle mixed libraries differently - they use different cache/API
		if lib.Type == "mixed" {
			m.navPlan = &NavPlan{
				Targets: []NavTarget{
					{ID: show.ID}, // Select show in mixed column
					{},            // Land on seasons (no selection)
				},
				CurrentStep: 0,
				AwaitKind:   AwaitMixed,
				AwaitID:     lib.ID,
			}

			mixedCol := components.NewListColumn(components.ColumnTypeMixed, lib.Name)

			// If cached, populate and immediately advance
			if cached := m.LibrarySvc.GetCachedLibraryContent(lib.ID); cached != nil {
				mixedCol.SetItems(cached)
				m.ColumnStack.Push(mixedCol, 0)
				m.updateLayout()
				return m.advanceNavPlanAfterLoad(AwaitMixed, lib.ID)
			}

			mixedCol.SetLoading(true)
			m.ColumnStack.Push(mixedCol, 0)
			m.Loading = true
			m.updateLayout()
			return LoadMixedLibraryCmd(m.LibrarySvc, lib.ID)
		}

		m.navPlan = &NavPlan{
			Targets: []NavTarget{
				{ID: show.ID}, // Select show
				{},            // Land on seasons (no selection)
			},
			CurrentStep: 0,
			AwaitKind:   AwaitShows,
			AwaitID:     lib.ID,
		}

		showsCol := components.NewListColumn(components.ColumnTypeShows, lib.Name)

		// If cached, populate and immediately advance
		cachedShows := m.LibrarySvc.GetCachedShows(navCtx.LibraryID)
		if cachedShows != nil {
			showsCol.SetItems(cachedShows)
			m.ColumnStack.Push(showsCol, 0)
			m.updateLayout()
			return m.advanceNavPlanAfterLoad(AwaitShows, lib.ID)
		}

		// Not cached - async load
		showsCol.SetLoading(true)
		m.ColumnStack.Push(showsCol, 0)
		m.Loading = true
		m.updateLayout()
		return LoadShowsCmd(m.LibrarySvc, navCtx.LibraryID)

	case domain.MediaTypeEpisode:
		lib := m.findLibrary(navCtx.LibraryID)
		if lib == nil {
			return nil
		}

		// Handle mixed libraries differently - they use different cache/API
		if lib.Type == "mixed" {
			// Build NavPlan: Mixed -> Seasons -> Episodes
			m.navPlan = &NavPlan{
				Targets: []NavTarget{
					{ID: navCtx.ShowID},    // Step 0: Select show in mixed column
					{ID: navCtx.SeasonID},  // Step 1: Select season
					{ID: navCtx.EpisodeID}, // Step 2: Select episode
				},
				CurrentStep: 0,
				AwaitKind:   AwaitMixed,
				AwaitID:     lib.ID,
			}

			mixedCol := components.NewListColumn(components.ColumnTypeMixed, lib.Name)

			// If cached, populate and immediately advance
			if cached := m.LibrarySvc.GetCachedLibraryContent(lib.ID); cached != nil {
				mixedCol.SetItems(cached)
				m.ColumnStack.Push(mixedCol, 0)
				m.updateLayout()
				return m.advanceNavPlanAfterLoad(AwaitMixed, lib.ID)
			}

			mixedCol.SetLoading(true)
			m.ColumnStack.Push(mixedCol, 0)
			m.Loading = true
			m.updateLayout()
			return LoadMixedLibraryCmd(m.LibrarySvc, lib.ID)
		}

		// Build NavPlan: Shows -> Seasons -> Episodes
		m.navPlan = &NavPlan{
			Targets: []NavTarget{
				{ID: navCtx.ShowID},    // Step 0: Select show
				{ID: navCtx.SeasonID},  // Step 1: Select season
				{ID: navCtx.EpisodeID}, // Step 2: Select episode
			},
			CurrentStep: 0,
			AwaitKind:   AwaitShows,
			AwaitID:     lib.ID,
		}

		// Push shows column (will populate when ShowsLoadedMsg arrives)
		showsCol := components.NewListColumn(components.ColumnTypeShows, lib.Name)
		showsCol.SetLoading(true)
		m.ColumnStack.Push(showsCol, 0)
		m.Loading = true
		m.updateLayout()

		return LoadShowsCmd(m.LibrarySvc, navCtx.LibraryID)
	}

	return nil
}
