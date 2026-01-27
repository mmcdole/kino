package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
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

// columnLoadSpec contains everything needed to push and load a library column
type columnLoadSpec struct {
	colType   components.ColumnType
	name      string
	awaitKind NavAwaitKind
	awaitID   string
	getCached func() interface{} // Returns nil if not cached, otherwise a slice for SetItems
	loadCmd   tea.Cmd
}

// pushAndLoadColumn pushes a column and either populates from cache or triggers async load.
// This consolidates the repeated cache-check-and-load pattern used throughout navigation.
func (m *Model) pushAndLoadColumn(spec columnLoadSpec, cursor int) *drillResult {
	col := components.NewListColumn(spec.colType, spec.name)
	m.ColumnStack.Push(col, cursor)
	m.updateLayout()

	if cached := spec.getCached(); cached != nil {
		col.SetItems(cached)
		m.updateInspector()
		if m.navPlan != nil {
			return &drillResult{
				AwaitKind: spec.awaitKind,
				AwaitID:   spec.awaitID,
				Cmd:       m.advanceNavPlanAfterLoad(spec.awaitKind, spec.awaitID),
			}
		}
		return &drillResult{AwaitKind: AwaitNone}
	}

	col.SetLoading(true)
	m.Loading = true
	return &drillResult{
		AwaitKind: spec.awaitKind,
		AwaitID:   spec.awaitID,
		Cmd:       spec.loadCmd,
	}
}

// navigateToMixedLibraryItem navigates to an item in a mixed library using NavPlan.
// This consolidates the 3 near-identical mixed library navigation blocks.
func (m *Model) navigateToMixedLibraryItem(lib *domain.Library, targets []NavTarget) tea.Cmd {
	m.navPlan = &NavPlan{
		Targets:     targets,
		CurrentStep: 0,
		AwaitKind:   AwaitMixed,
		AwaitID:     lib.ID,
	}

	mixedCol := components.NewListColumn(components.ColumnTypeMixed, lib.Name)

	if cached, ok := m.LibQueries.GetCachedMixedContent(lib.ID); ok {
		mixedCol.SetItems(cached)
		m.ColumnStack.Push(mixedCol, 0)
		m.updateLayout()
		m.currentLibID = lib.ID // Track context for hierarchical caching
		return m.advanceNavPlanAfterLoad(AwaitMixed, lib.ID)
	}

	mixedCol.SetLoading(true)
	m.ColumnStack.Push(mixedCol, 0)
	m.Loading = true
	m.updateLayout()
	return LoadMixedLibraryCmd(m.LibCommands, lib.ID)
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
	EpisodeID   string
}

// buildNavContext constructs navigation context from a filter result
func (m *Model) buildNavContext(item search.FilterItem) NavigationContext {
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
		if v.ID == playlistsLibraryID {
			col := components.NewListColumn(components.ColumnTypePlaylists, "Playlists")
			m.ColumnStack.Push(col, cursor)
			m.updateLayout()

			// Check cache first
			if cached, ok := m.PlaylistQueries.GetCachedPlaylists(); ok {
				col.SetItems(cached)
				m.updateInspector()
				return &drillResult{AwaitKind: AwaitNone}
			}

			col.SetLoading(true)
			m.Loading = true
			return &drillResult{
				AwaitKind: AwaitNone,
				Cmd:       LoadPlaylistsCmd(m.PlaylistCmds),
			}
		}

		// Track library context for hierarchical caching
		m.currentLibID = v.ID
		m.currentShowID = "" // Reset show context when entering a library

		// Build column spec based on library type
		var spec columnLoadSpec
		switch v.Type {
		case "movie":
			spec = columnLoadSpec{
				colType:   components.ColumnTypeMovies,
				name:      v.Name,
				awaitKind: AwaitMovies,
				awaitID:   v.ID,
				getCached: func() interface{} {
					if c, ok := m.LibQueries.GetCachedMovies(v.ID); ok {
						return c
					}
					return nil
				},
				loadCmd: LoadMoviesCmd(m.LibCommands, v.ID),
			}
		case "show":
			spec = columnLoadSpec{
				colType:   components.ColumnTypeShows,
				name:      v.Name,
				awaitKind: AwaitShows,
				awaitID:   v.ID,
				getCached: func() interface{} {
					if c, ok := m.LibQueries.GetCachedShows(v.ID); ok {
						return c
					}
					return nil
				},
				loadCmd: LoadShowsCmd(m.LibCommands, v.ID),
			}
		case "mixed":
			spec = columnLoadSpec{
				colType:   components.ColumnTypeMixed,
				name:      v.Name,
				awaitKind: AwaitMixed,
				awaitID:   v.ID,
				getCached: func() interface{} {
					if c, ok := m.LibQueries.GetCachedMixedContent(v.ID); ok {
						return c
					}
					return nil
				},
				loadCmd: LoadMixedLibraryCmd(m.LibCommands, v.ID),
			}
		default:
			// Unknown library type - treat as mixed
			spec = columnLoadSpec{
				colType:   components.ColumnTypeMixed,
				name:      v.Name,
				awaitKind: AwaitMixed,
				awaitID:   v.ID,
				getCached: func() interface{} {
					if c, ok := m.LibQueries.GetCachedMixedContent(v.ID); ok {
						return c
					}
					return nil
				},
				loadCmd: LoadMixedLibraryCmd(m.LibCommands, v.ID),
			}
		}
		return m.pushAndLoadColumn(spec, cursor)

	case *domain.Show:
		// Track show context for hierarchical caching (episodes need showID)
		m.currentShowID = v.ID

		libID := m.currentLibID
		showID := v.ID
		spec := columnLoadSpec{
			colType:   components.ColumnTypeSeasons,
			name:      v.Title,
			awaitKind: AwaitSeasons,
			awaitID:   v.ID,
			getCached: func() interface{} {
				if c, ok := m.LibQueries.GetCachedSeasons(libID, showID); ok {
					return c
				}
				return nil
			},
			loadCmd: LoadSeasonsCmd(m.LibCommands, libID, showID),
		}
		return m.pushAndLoadColumn(spec, cursor)

	case *domain.Season:
		title := v.ShowTitle
		if v.SeasonNum == 0 {
			title += " - Specials"
		} else {
			title += fmt.Sprintf(" - S%02d", v.SeasonNum)
		}

		libID := m.currentLibID
		showID := m.currentShowID
		seasonID := v.ID
		spec := columnLoadSpec{
			colType:   components.ColumnTypeEpisodes,
			name:      title,
			awaitKind: AwaitEpisodes,
			awaitID:   v.ID,
			getCached: func() interface{} {
				if c, ok := m.LibQueries.GetCachedEpisodes(libID, showID, seasonID); ok {
					return c
				}
				return nil
			},
			loadCmd: LoadEpisodesCmd(m.LibCommands, libID, showID, seasonID),
		}
		return m.pushAndLoadColumn(spec, cursor)

	case *domain.Playlist:
		col := components.NewListColumn(components.ColumnTypePlaylistItems, v.Title)
		m.ColumnStack.Push(col, cursor)
		m.currentPlaylistID = v.ID
		m.updateLayout()

		// Check cache first
		if cached, ok := m.PlaylistQueries.GetCachedPlaylistItems(v.ID); ok {
			col.SetItems(cached)
			m.updateInspector()
			return &drillResult{AwaitKind: AwaitNone}
		}

		col.SetLoading(true)
		m.Loading = true
		return &drillResult{
			AwaitKind: AwaitNone, // Playlists don't use the NavPlan system
			AwaitID:   v.ID,
			Cmd:       LoadPlaylistItemsCmd(m.PlaylistCmds, v.ID),
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

	// Track context when navigating back for hierarchical caching
	if top := m.ColumnStack.Top(); top != nil {
		switch top.ColumnType() {
		case components.ColumnTypeEpisodes:
			// Leaving episodes - clear nothing (still in show context)
		case components.ColumnTypeSeasons:
			// Leaving seasons - clear show context
			m.currentShowID = ""
		case components.ColumnTypeMovies, components.ColumnTypeShows, components.ColumnTypeMixed:
			// Leaving library content - clear both contexts
			m.currentLibID = ""
			m.currentShowID = ""
		}
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

// navigateToSearchResult navigates to a search result item in its library context.
// Called when a user selects an item from global search results in the omnibar.
func (m *Model) navigateToSearchResult(item search.FilterItem) tea.Cmd {
	navCtx := m.buildNavContext(item)

	// Reset stack to library level first
	libCol := components.NewLibraryColumn(m.allLibraryEntries())
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

	lib := m.findLibrary(navCtx.LibraryID)
	if lib == nil {
		return nil
	}

	// Build navigation targets based on media type
	var targets []NavTarget
	switch item.Type {
	case domain.MediaTypeMovie:
		targets = []NavTarget{{ID: navCtx.MovieID}}
	case domain.MediaTypeShow:
		show, ok := item.Item.(*domain.Show)
		if !ok {
			return nil
		}
		targets = []NavTarget{{ID: show.ID}, {}} // Select show, land on seasons
	case domain.MediaTypeEpisode:
		targets = []NavTarget{
			{ID: navCtx.ShowID},
			{ID: navCtx.SeasonID},
			{ID: navCtx.EpisodeID},
		}
	default:
		return nil
	}

	// Mixed libraries use their own navigation path
	if lib.Type == "mixed" {
		return m.navigateToMixedLibraryItem(lib, targets)
	}

	// Typed libraries (movie/show)
	return m.navigateToTypedLibraryItem(lib, navCtx, targets, item.Type)
}

// navigateToTypedLibraryItem navigates to an item in a typed (movie/show) library.
func (m *Model) navigateToTypedLibraryItem(lib *domain.Library, navCtx NavigationContext, targets []NavTarget, mediaType domain.MediaType) tea.Cmd {
	// Track library context for hierarchical caching
	m.currentLibID = lib.ID
	m.currentShowID = "" // Reset show context

	var spec columnLoadSpec

	if mediaType == domain.MediaTypeMovie {
		m.navPlan = &NavPlan{
			Targets:     targets,
			CurrentStep: 0,
			AwaitKind:   AwaitMovies,
			AwaitID:     lib.ID,
		}
		spec = columnLoadSpec{
			colType:   components.ColumnTypeMovies,
			name:      lib.Name,
			awaitKind: AwaitMovies,
			awaitID:   lib.ID,
			getCached: func() interface{} {
				if c, ok := m.LibQueries.GetCachedMovies(lib.ID); ok {
					return c
				}
				return nil
			},
			loadCmd: LoadMoviesCmd(m.LibCommands, navCtx.LibraryID),
		}
	} else {
		// Shows and episodes both start from the shows column
		m.navPlan = &NavPlan{
			Targets:     targets,
			CurrentStep: 0,
			AwaitKind:   AwaitShows,
			AwaitID:     lib.ID,
		}
		spec = columnLoadSpec{
			colType:   components.ColumnTypeShows,
			name:      lib.Name,
			awaitKind: AwaitShows,
			awaitID:   lib.ID,
			getCached: func() interface{} {
				if c, ok := m.LibQueries.GetCachedShows(lib.ID); ok {
					return c
				}
				return nil
			},
			loadCmd: LoadShowsCmd(m.LibCommands, navCtx.LibraryID),
		}
	}

	return m.pushAndLoadColumn(spec, 0).Cmd
}
