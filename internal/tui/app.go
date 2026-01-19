package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drake/goplex/internal/domain"
	"github.com/drake/goplex/internal/service"
	"github.com/drake/goplex/internal/tui/components"
	"github.com/drake/goplex/internal/tui/styles"
)

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateBrowsing ApplicationState = iota
	StateSearching
	StateSettings
	StateAuthRequired
	StateHelp
)

// Layout proportions
const (
	SidebarWidthPercent   = 20
	InspectorWidthPercent = 30
	MinSidebarWidth       = 20
	MinInspectorWidth     = 25

	// Vertical layout: status bar + help bar (breadcrumb moved to Browser pane title)
	ChromeHeight = 2
)

// Model is the main Bubble Tea model for the application
type Model struct {
	// Application state
	State        ApplicationState
	Ready        bool

	// Services
	LibrarySvc   *service.LibraryService
	PlaybackSvc  *service.PlaybackService
	SearchSvc    *service.SearchService

	// UI Components
	Sidebar      components.Sidebar
	Grid         components.Grid
	Inspector    components.Inspector
	Omnibar      components.Omnibar

	// Navigation
	NavStack     []NavContext
	CurrentNav   NavContext
	FocusPane    Pane

	// Data
	Libraries    []domain.Library

	// Dimensions
	Width        int
	Height       int

	// UI state
	StatusMsg       string
	StatusIsErr     bool
	Loading         bool
	SpinnerFrame    int

	// Sync state
	LibraryStates    map[string]components.LibrarySyncState // Tracks progress per library
	SyncingCount     int                                    // Libraries still syncing
	TotalSyncedItems int                                    // Grand total across all

	// Pending cursor position for navigation after load
	pendingCursor int
}

// NewModel creates a new application model
func NewModel(
	librarySvc *service.LibraryService,
	playbackSvc *service.PlaybackService,
	searchSvc *service.SearchService,
) Model {
	return Model{
		State:         StateBrowsing,
		LibrarySvc:    librarySvc,
		PlaybackSvc:   playbackSvc,
		SearchSvc:     searchSvc,
		Sidebar:       components.NewSidebar(),
		Grid:          components.NewGrid(),
		Inspector:     components.NewInspector(),
		Omnibar:       components.NewOmnibar(),
		FocusPane:     PaneSidebar,
		LibraryStates: make(map[string]components.LibrarySyncState),
		CurrentNav: NavContext{
			Level: BrowseLevelLibrary,
		},
	}
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		LoadLibrariesCmd(m.LibrarySvc),
		TickCmd(100*time.Millisecond),
	)
}

// Update handles all messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Ready = true
		m.updateLayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case TickMsg:
		m.SpinnerFrame++
		if m.SyncingCount > 0 {
			m.Sidebar.SetSpinnerFrame(m.SpinnerFrame)
			m.Sidebar.RefreshItems()
		}
		return m, TickCmd(100 * time.Millisecond)

	case LibrariesLoadedMsg:
		m.Libraries = msg.Libraries
		m.Sidebar.SetLibraries(msg.Libraries)

		// Initialize all states to Syncing
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range msg.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.SyncingCount = len(msg.Libraries)
		m.Sidebar.SetLibraryStates(m.LibraryStates)

		// Auto-select first library
		if len(msg.Libraries) > 0 {
			m.Sidebar.SetFocused(true)
			m.CurrentNav = NavContext{
				Level:       BrowseLevelLibrary,
				LibraryID:   msg.Libraries[0].ID,
				LibraryName: msg.Libraries[0].Name,
			}
		}

		// Start parallel sync of ALL libraries
		m.Loading = true
		return m, SyncAllLibrariesCmd(m.LibrarySvc, m.SearchSvc, msg.Libraries, false)

	case MoviesLoadedMsg:
		m.Grid.SetMovies(msg.Movies)
		m.CurrentNav.LibraryID = msg.LibraryID
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.Sidebar.SetLibraryStates(m.LibraryStates)
		}

		// Apply pending cursor position from filter navigation
		if m.pendingCursor > 0 {
			m.Grid.SetCursor(m.pendingCursor)
			m.pendingCursor = 0
		}
		m.updateBreadcrumb()
		m.updateInspector()
		// Index movies for global filter
		m.indexMoviesForFilter(msg.Movies, msg.LibraryID)
		return m, nil

	case ShowsLoadedMsg:
		m.Grid.SetShows(msg.Shows)
		m.CurrentNav.LibraryID = msg.LibraryID
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.Sidebar.SetLibraryStates(m.LibraryStates)
		}

		// Apply pending cursor position from filter navigation
		if m.pendingCursor > 0 {
			m.Grid.SetCursor(m.pendingCursor)
			m.pendingCursor = 0
		}
		m.updateBreadcrumb()
		m.updateInspector()
		// Index shows for global filter
		m.indexShowsForFilter(msg.Shows, msg.LibraryID)
		return m, nil

	case SeasonsLoadedMsg:
		m.Grid.SetSeasons(msg.Seasons)
		m.Loading = false
		// Apply pending cursor position from filter navigation
		if m.pendingCursor > 0 {
			m.Grid.SetCursor(m.pendingCursor)
			m.pendingCursor = 0
		}
		m.updateBreadcrumb()
		m.updateInspector()
		return m, nil

	case EpisodesLoadedMsg:
		m.Grid.SetEpisodes(msg.Episodes)
		m.Loading = false
		// Apply pending cursor position from filter navigation
		if m.pendingCursor > 0 {
			m.Grid.SetCursor(m.pendingCursor)
			m.pendingCursor = 0
		}
		m.updateBreadcrumb()
		m.updateInspector()
		// Index episodes for global filter
		m.indexEpisodesForFilter(msg.Episodes, msg.SeasonID)
		return m, nil

	case OnDeckLoadedMsg:
		m.Grid.SetOnDeck(msg.Items)
		m.Loading = false
		m.updateBreadcrumb()
		m.updateInspector()
		return m, nil

	case SearchResultsMsg:
		m.Omnibar.SetResults(msg.Results)
		return m, nil

	case PlaybackStartedMsg:
		m.StatusMsg = "Launched: " + msg.Item.Title
		return m, nil

	case MarkWatchedMsg:
		m.StatusMsg = "Marked watched: " + msg.Title
		// Refresh to update watch status indicators
		cmds = append(cmds, m.refreshCurrentView())
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case MarkUnwatchedMsg:
		m.StatusMsg = "Marked unwatched: " + msg.Title
		// Refresh to update watch status indicators
		cmds = append(cmds, m.refreshCurrentView())
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case ErrMsg:
		m.StatusMsg = msg.Error()
		m.StatusIsErr = true
		m.Loading = false
		cmds = append(cmds, ClearStatusCmd(5*time.Second))
		return m, tea.Batch(cmds...)

	case StatusMsg:
		m.StatusMsg = msg.Message
		m.StatusIsErr = msg.IsError
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case ClearStatusMsg:
		m.StatusMsg = ""
		m.StatusIsErr = false
		return m, nil

	case GlobalSearchReadyMsg:
		m.Omnibar.SetLoading(false)
		if msg.SkippedLibraries > 0 {
			m.StatusMsg = fmt.Sprintf("Search ready (%d libraries still syncing)", msg.SkippedLibraries)
		}
		return m, nil

	case LibrarySyncProgressMsg:
		state := m.LibraryStates[msg.LibraryID]

		if msg.Error != nil {
			state.Status = components.StatusError
			state.Error = msg.Error
		} else {
			state.Loaded = msg.Loaded
			state.Total = msg.Total
			state.FromDisk = msg.FromDisk

			if msg.Done {
				state.Status = components.StatusSynced
				m.TotalSyncedItems += msg.Loaded
				m.SyncingCount--

				// Trigger delayed cleanup (flash pattern: show checkmark briefly, then clear)
				cmds = append(cmds, ClearLibraryStatusCmd(msg.LibraryID, 2*time.Second))

				// If this is the selected library, populate the grid
				if msg.LibraryID == m.CurrentNav.LibraryID {
					lib := m.findLibrary(msg.LibraryID)
					if lib != nil {
						cmds = append(cmds, m.loadLibraryContent(*lib))
					}
				}
			}
		}

		m.LibraryStates[msg.LibraryID] = state
		m.Sidebar.SetLibraryStates(m.LibraryStates)

		// If there's a continuation command, run it to get the next chunk
		if msg.NextCmd != nil {
			if cmd, ok := msg.NextCmd.(tea.Cmd); ok {
				cmds = append(cmds, cmd)
			}
		}

		// Check if all done
		if m.SyncingCount == 0 {
			m.Loading = false
			m.StatusMsg = fmt.Sprintf("Synced %d items", m.TotalSyncedItems)
			cmds = append(cmds, ClearStatusCmd(3*time.Second))
		}

		return m, tea.Batch(cmds...)

	case ClearLibraryStatusMsg:
		if state, ok := m.LibraryStates[msg.LibraryID]; ok {
			// Only clear if still showing Synced (not re-syncing or errored)
			if state.Status == components.StatusSynced {
				state.Status = components.StatusIdle
				m.LibraryStates[msg.LibraryID] = state
				m.Sidebar.SetLibraryStates(m.LibraryStates)
			}
		}
		return m, nil
	}

	// Update focused component
	switch m.FocusPane {
	case PaneSidebar:
		m.Sidebar, _ = m.Sidebar.Update(msg)
	case PaneBrowser:
		m.Grid, _ = m.Grid.Update(msg)
	case PaneInspector:
		m.Inspector, _ = m.Inspector.Update(msg)
	}

	// Update omnibar if visible
	if m.Omnibar.IsVisible() {
		var selected bool
		var cmd tea.Cmd
		m.Omnibar, cmd, selected = m.Omnibar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Handle real-time filtering when query changes
		if m.Omnibar.IsFilterMode() && m.Omnibar.QueryChanged() {
			query := m.Omnibar.Query()
			results := m.SearchSvc.FilterLocal(query)
			m.Omnibar.SetFilterResults(results)
		}

		if selected {
			if m.Omnibar.IsFilterMode() {
				// Filter mode: navigate to item in context
				result := m.Omnibar.SelectedFilterResult()
				if result != nil {
					m.Omnibar.Hide()
					navCmd := m.navigateToFilteredItem(*result)
					if navCmd != nil {
						cmds = append(cmds, navCmd)
					}
				}
			} else {
				// Search mode: play the result
				result := m.Omnibar.SelectedResult()
				if result != nil {
					m.Omnibar.Hide()
					var cmd tea.Cmd
					m, cmd = m.handleItemSelection(*result)
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle state-specific keys
	switch m.State {
	case StateHelp:
		if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
			m.State = StateBrowsing
		}
		return m, nil
	}

	// Handle omnibar if visible
	if m.Omnibar.IsVisible() {
		var cmd tea.Cmd
		var selected bool
		var cmds []tea.Cmd
		m.Omnibar, cmd, selected = m.Omnibar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Handle real-time filtering when query changes
		if m.Omnibar.IsFilterMode() && m.Omnibar.QueryChanged() {
			query := m.Omnibar.Query()
			results := m.SearchSvc.FilterLocal(query)
			m.Omnibar.SetFilterResults(results)
		}

		if selected {
			if m.Omnibar.IsFilterMode() {
				// Filter mode: navigate to item in context
				if result := m.Omnibar.SelectedFilterResult(); result != nil {
					m.Omnibar.Hide()
					navCmd := m.navigateToFilteredItem(*result)
					if navCmd != nil {
						cmds = append(cmds, navCmd)
					}
				}
			} else {
				// Search mode: play the result
				if result := m.Omnibar.SelectedResult(); result != nil {
					m.Omnibar.Hide()
					cmds = append(cmds, PlayItemCmd(m.PlaybackSvc, *result, false))
				}
			}
		}
		return m, tea.Batch(cmds...)
	}

	// Handle Grid filter typing mode - let Grid process its own input
	if m.FocusPane == PaneBrowser && m.Grid.IsFilterTyping() {
		oldCursor := m.Grid.Cursor()
		m.Grid, _ = m.Grid.Update(msg)
		if oldCursor != m.Grid.Cursor() {
			m.updateInspector()
		}
		return m, nil
	}

	// Global keys
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.State = StateHelp
		return m, nil

	case "/":
		// Activate filter when Grid is focused
		if m.FocusPane == PaneBrowser {
			m.Grid.ToggleFilter()
			return m, nil
		}
		return m, nil

	case "s":
		// Global search via Omnibar - load all content first
		m.Omnibar.ShowFilterMode()
		m.Omnibar.SetSize(m.Width, m.Height)
		m.Omnibar.SetLoading(true)
		return m, tea.Batch(
			m.Omnibar.Init(),
			LoadAllForGlobalSearchCmd(m.LibrarySvc, m.SearchSvc, m.Libraries),
		)

	case "h", "left":
		m.moveFocus(-1)
		return m, nil

	case "l", "right":
		m.moveFocus(1)
		return m, nil

	case "enter":
		return m.handleEnter()

	case "shift+enter":
		// Play from beginning (no resume)
		if m.FocusPane == PaneBrowser {
			item := m.Grid.SelectedItem()
			if mediaItem, ok := item.(domain.MediaItem); ok {
				return m, PlayItemCmd(m.PlaybackSvc, mediaItem, false)
			}
		}
		return m, nil

	case "backspace":
		return m.handleBack()

	case "o":
		// Jump to On Deck
		m.Loading = true
		m.CurrentNav = NavContext{Level: BrowseLevelLibrary, LibraryName: "On Deck"}
		return m, LoadOnDeckCmd(m.LibrarySvc)

	case "r":
		// Refresh single selected library (force)
		if lib := m.Sidebar.SelectedLibrary(); lib != nil {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
			m.SyncingCount++
			m.Loading = true
			m.Sidebar.SetLibraryStates(m.LibraryStates)
			return m, SyncLibraryCmd(m.LibrarySvc, m.SearchSvc, *lib, true)
		}
		return m, nil

	case "R":
		// Refresh ALL libraries (force)
		m.SearchSvc.ClearFilterIndex()
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range m.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.SyncingCount = len(m.Libraries)
		m.TotalSyncedItems = 0
		m.Loading = true
		m.Sidebar.SetLibraryStates(m.LibraryStates)
		return m, SyncAllLibrariesCmd(m.LibrarySvc, m.SearchSvc, m.Libraries, true)

	case "w":
		// Mark as watched (Browser action)
		if m.FocusPane == PaneBrowser {
			if item := m.Grid.SelectedMediaItem(); item != nil {
				return m, MarkWatchedCmd(m.PlaybackSvc, item.ID, item.Title)
			}
		}
		return m, nil

	case "u":
		// Mark as unwatched (Browser action)
		if m.FocusPane == PaneBrowser {
			if item := m.Grid.SelectedMediaItem(); item != nil {
				return m, MarkUnwatchedCmd(m.PlaybackSvc, item.ID, item.Title)
			}
		}
		return m, nil

	case "p":
		// Play from beginning (Browser action)
		if m.FocusPane == PaneBrowser {
			if item := m.Grid.SelectedMediaItem(); item != nil {
				return m, PlayItemCmd(m.PlaybackSvc, *item, false)
			}
		}
		return m, nil
	}

	// Let components handle remaining keys
	switch m.FocusPane {
	case PaneSidebar:
		oldIndex := m.Sidebar.SelectedIndex()
		m.Sidebar, _ = m.Sidebar.Update(msg)
		newIndex := m.Sidebar.SelectedIndex()

		if oldIndex != newIndex {
			lib := m.Sidebar.SelectedLibrary()
			if lib != nil {
				m.Loading = true
				cmds = append(cmds, m.loadLibraryContent(*lib))
			}
		}

	case PaneBrowser:
		oldCursor := m.Grid.Cursor()
		m.Grid, _ = m.Grid.Update(msg)
		newCursor := m.Grid.Cursor()

		if oldCursor != newCursor {
			m.updateInspector()
		}

	case PaneInspector:
		m.Inspector, _ = m.Inspector.Update(msg)
	}

	return m, tea.Batch(cmds...)
}

// handleEnter handles the enter key press
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.FocusPane {
	case PaneSidebar:
		lib := m.Sidebar.SelectedLibrary()
		if lib != nil {
			m.FocusPane = PaneBrowser
			m.Grid.SetFocused(true)
			m.Sidebar.SetFocused(false)
			m.Loading = true
			return m, m.loadLibraryContent(*lib)
		}

	case PaneBrowser:
		item := m.Grid.SelectedItem()
		if item == nil {
			return m, nil
		}

		switch v := item.(type) {
		case domain.MediaItem:
			return m.handleItemSelection(v)

		case domain.Show:
			// Drill into show -> seasons
			m.pushNavContext()
			m.CurrentNav.Level = BrowseLevelShow
			m.CurrentNav.ShowID = v.ID
			m.CurrentNav.ShowTitle = v.Title
			m.Loading = true
			return m, LoadSeasonsCmd(m.LibrarySvc, v.ID)

		case domain.Season:
			// Drill into season -> episodes
			m.pushNavContext()
			m.CurrentNav.Level = BrowseLevelSeason
			m.CurrentNav.SeasonID = v.ID
			m.CurrentNav.SeasonNum = v.SeasonNum
			m.Loading = true
			return m, LoadEpisodesCmd(m.LibrarySvc, v.ID)
		}
	}

	return m, nil
}

// handleItemSelection handles selection of a playable item
func (m Model) handleItemSelection(item domain.MediaItem) (Model, tea.Cmd) {
	// Resume from saved position if available, otherwise play from start
	resume := item.ViewOffset > 0 && !item.IsPlayed
	return m, PlayItemCmd(m.PlaybackSvc, item, resume)
}

// handleBack handles the backspace key (navigation back)
func (m Model) handleBack() (tea.Model, tea.Cmd) {
	if len(m.NavStack) == 0 {
		return m, nil
	}

	// Pop navigation context
	m.CurrentNav = m.NavStack[len(m.NavStack)-1]
	m.NavStack = m.NavStack[:len(m.NavStack)-1]

	// Restore cursor position
	m.Grid.SetCursor(m.CurrentNav.CursorPos)

	// Reload content for the level
	m.Loading = true
	switch m.CurrentNav.Level {
	case BrowseLevelLibrary:
		lib := m.findLibrary(m.CurrentNav.LibraryID)
		if lib != nil {
			return m, m.loadLibraryContent(*lib)
		}
	case BrowseLevelShow:
		return m, LoadSeasonsCmd(m.LibrarySvc, m.CurrentNav.ShowID)
	case BrowseLevelSeason:
		return m, LoadEpisodesCmd(m.LibrarySvc, m.CurrentNav.SeasonID)
	}

	return m, nil
}

// pushNavContext saves the current navigation context
func (m *Model) pushNavContext() {
	m.CurrentNav.CursorPos = m.Grid.Cursor()
	m.NavStack = append(m.NavStack, m.CurrentNav)
}

// moveFocus moves focus between panes
func (m *Model) moveFocus(direction int) {
	newPane := int(m.FocusPane) + direction
	if newPane < 0 {
		newPane = 0
	}
	if newPane > int(PaneInspector) {
		newPane = int(PaneInspector)
	}

	m.FocusPane = Pane(newPane)
	m.Sidebar.SetFocused(m.FocusPane == PaneSidebar)
	m.Grid.SetFocused(m.FocusPane == PaneBrowser)
	m.Inspector.SetFocused(m.FocusPane == PaneInspector)
}

// loadLibraryContent loads content for a library
func (m *Model) loadLibraryContent(lib domain.Library) tea.Cmd {
	m.CurrentNav = NavContext{
		Level:       BrowseLevelLibrary,
		LibraryID:   lib.ID,
		LibraryName: lib.Name,
	}
	m.NavStack = nil // Clear navigation history when switching libraries

	if lib.Type == "movie" {
		return LoadMoviesCmd(m.LibrarySvc, lib.ID)
	}
	return LoadShowsCmd(m.LibrarySvc, lib.ID)
}

// refreshCurrentView refreshes the current view
func (m *Model) refreshCurrentView() tea.Cmd {
	m.LibrarySvc.RefreshAll()
	m.Loading = true

	switch m.CurrentNav.Level {
	case BrowseLevelLibrary:
		lib := m.findLibrary(m.CurrentNav.LibraryID)
		if lib != nil {
			return m.loadLibraryContent(*lib)
		}
	case BrowseLevelShow:
		return LoadSeasonsCmd(m.LibrarySvc, m.CurrentNav.ShowID)
	case BrowseLevelSeason:
		return LoadEpisodesCmd(m.LibrarySvc, m.CurrentNav.SeasonID)
	}

	return LoadLibrariesCmd(m.LibrarySvc)
}

// findLibrary finds a library by ID
func (m Model) findLibrary(id string) *domain.Library {
	for _, lib := range m.Libraries {
		if lib.ID == id {
			return &lib
		}
	}
	return nil
}

// updateInspector updates the inspector with the selected item
func (m *Model) updateInspector() {
	item := m.Grid.SelectedItem()
	m.Inspector.SetItem(item)
}

// updateBreadcrumb updates the Grid's breadcrumb based on CurrentNav
func (m *Model) updateBreadcrumb() {
	m.Grid.SetBreadcrumb(m.CurrentNav.Breadcrumb())
}

// updateLayout updates component sizes based on window size
func (m *Model) updateLayout() {
	if m.Width == 0 || m.Height == 0 {
		return
	}

	// Use full terminal width for panels
	availableWidth := m.Width

	// Calculate panel widths using proportions
	// Note: lipgloss Width(w).Height(h) means TOTAL size INCLUDING borders
	sidebarWidth := availableWidth * SidebarWidthPercent / 100
	if sidebarWidth < MinSidebarWidth {
		sidebarWidth = MinSidebarWidth
	}
	inspectorWidth := availableWidth * InspectorWidthPercent / 100
	if inspectorWidth < MinInspectorWidth {
		inspectorWidth = MinInspectorWidth
	}
	browserWidth := availableWidth - sidebarWidth - inspectorWidth

	contentHeight := m.Height - ChromeHeight

	m.Sidebar.SetSize(sidebarWidth, contentHeight)
	m.Grid.SetSize(browserWidth, contentHeight)
	m.Inspector.SetSize(inspectorWidth, contentHeight)
	m.Omnibar.SetSize(m.Width, m.Height)
}

// View renders the application
func (m Model) View() string {
	if !m.Ready {
		return "Loading..."
	}

	// Handle modal states
	if m.State == StateHelp {
		return m.renderHelp()
	}

	// Main layout (breadcrumb is now inside the Grid component)
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.Sidebar.View(),
		m.Grid.View(),
		m.Inspector.View(),
	)

	// Status bar
	statusText := m.StatusMsg
	if m.Loading {
		statusText = RenderSpinner(m.SpinnerFrame) + " Loading..."
	}
	statusBar := RenderStatusBar(statusText, "", m.Width)

	// Help bar
	helpBar := RenderHelp(KeyHelpForPane(m.FocusPane), m.Width)

	// Combine all
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		statusBar,
		helpBar,
	)

	// Overlay omnibar if visible
	if m.Omnibar.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.Omnibar.View())
	}

	return view
}

// renderHelp renders the help screen
func (m Model) renderHelp() string {
	help := `
GOPLEX - Plex TUI

NAVIGATION
  h / l        Switch pane focus (Sidebar ↔ Browser ↔ Inspector)
  j / k        Navigate items up/down
  g / G        Jump to first/last item
  Enter        Play/resume item OR drill into Show/Season
  Shift+Enter  Play from beginning
  Backspace    Go back one level

BROWSER ACTIONS
  w            Mark item as watched
  u            Mark item as unwatched
  p            Play from beginning (same as Shift+Enter)

SEARCH & VIEW
  /            Filter current list (fuzzy match)
  s            Global filter (cached items)
  o            Jump to On Deck
  r            Refresh current view

OTHER
  q            Quit
  ?            Show/hide this help
  Esc          Close modal / Cancel filter

Press any key to return...
`

	return lipgloss.Place(m.Width, m.Height,
		lipgloss.Center, lipgloss.Center,
		styles.ModalStyle.Render(help))
}

// indexMoviesForFilter indexes movies for the global filter
func (m *Model) indexMoviesForFilter(movies []domain.MediaItem, libID string) {
	libName := m.findLibraryName(libID)
	items := make([]service.FilterItem, len(movies))

	for i, movie := range movies {
		items[i] = service.FilterItem{
			Item:  movie,
			Title: movie.Title,
			Type:  domain.MediaTypeMovie,
			NavContext: service.NavigationContext{
				LibraryID:   libID,
				LibraryName: libName,
				ItemIndex:   i,
			},
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// indexShowsForFilter indexes shows for the global filter
func (m *Model) indexShowsForFilter(shows []domain.Show, libID string) {
	libName := m.findLibraryName(libID)
	items := make([]service.FilterItem, len(shows))

	for i, show := range shows {
		items[i] = service.FilterItem{
			Item:  show,
			Title: show.Title,
			Type:  domain.MediaTypeShow,
			NavContext: service.NavigationContext{
				LibraryID:   libID,
				LibraryName: libName,
				ItemIndex:   i,
			},
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// indexEpisodesForFilter indexes episodes for the global filter
func (m *Model) indexEpisodesForFilter(episodes []domain.MediaItem, seasonID string) {
	if len(episodes) == 0 {
		return
	}

	// Get context from first episode
	first := episodes[0]
	libName := m.findLibraryName(first.LibraryID)

	items := make([]service.FilterItem, len(episodes))
	for i, ep := range episodes {
		items[i] = service.FilterItem{
			Item:  ep,
			Title: ep.ShowTitle + " " + ep.EpisodeCode() + " " + ep.Title,
			Type:  domain.MediaTypeEpisode,
			NavContext: service.NavigationContext{
				LibraryID:   ep.LibraryID,
				LibraryName: libName,
				ShowID:      ep.ShowID,
				ShowTitle:   ep.ShowTitle,
				SeasonID:    ep.ParentID,
				SeasonNum:   ep.SeasonNum,
				ItemIndex:   i,
			},
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// findLibraryName returns the library name for a given ID
func (m Model) findLibraryName(libID string) string {
	for _, lib := range m.Libraries {
		if lib.ID == libID {
			return lib.Name
		}
	}
	return ""
}

// navigateToFilteredItem navigates to a filtered item in its context
func (m *Model) navigateToFilteredItem(item service.FilterItem) tea.Cmd {
	// Clear navigation stack
	m.NavStack = nil

	// Move focus to browser
	m.FocusPane = PaneBrowser
	m.Sidebar.SetFocused(false)
	m.Grid.SetFocused(true)
	m.Inspector.SetFocused(false)

	switch item.Type {
	case domain.MediaTypeMovie:
		// Navigate to library and highlight movie
		m.CurrentNav = NavContext{
			Level:       BrowseLevelLibrary,
			LibraryID:   item.NavContext.LibraryID,
			LibraryName: item.NavContext.LibraryName,
		}
		m.pendingCursor = item.NavContext.ItemIndex
		m.Loading = true

		// Select the library in sidebar
		m.selectLibraryInSidebar(item.NavContext.LibraryID)

		return LoadMoviesCmd(m.LibrarySvc, item.NavContext.LibraryID)

	case domain.MediaTypeShow:
		// Navigate to library and highlight show
		m.CurrentNav = NavContext{
			Level:       BrowseLevelLibrary,
			LibraryID:   item.NavContext.LibraryID,
			LibraryName: item.NavContext.LibraryName,
		}
		m.pendingCursor = item.NavContext.ItemIndex
		m.Loading = true

		// Select the library in sidebar
		m.selectLibraryInSidebar(item.NavContext.LibraryID)

		return LoadShowsCmd(m.LibrarySvc, item.NavContext.LibraryID)

	case domain.MediaTypeEpisode:
		// Build navigation stack: library -> show -> season
		// First, push library level
		libCtx := NavContext{
			Level:       BrowseLevelLibrary,
			LibraryID:   item.NavContext.LibraryID,
			LibraryName: item.NavContext.LibraryName,
			CursorPos:   0, // Will be at show position
		}
		m.NavStack = append(m.NavStack, libCtx)

		// Then, push show level
		showCtx := NavContext{
			Level:       BrowseLevelShow,
			LibraryID:   item.NavContext.LibraryID,
			LibraryName: item.NavContext.LibraryName,
			ShowID:      item.NavContext.ShowID,
			ShowTitle:   item.NavContext.ShowTitle,
			CursorPos:   0, // Will be at season position
		}
		m.NavStack = append(m.NavStack, showCtx)

		// Set current nav to season level
		m.CurrentNav = NavContext{
			Level:       BrowseLevelSeason,
			LibraryID:   item.NavContext.LibraryID,
			LibraryName: item.NavContext.LibraryName,
			ShowID:      item.NavContext.ShowID,
			ShowTitle:   item.NavContext.ShowTitle,
			SeasonID:    item.NavContext.SeasonID,
			SeasonNum:   item.NavContext.SeasonNum,
		}
		m.pendingCursor = item.NavContext.ItemIndex
		m.Loading = true

		// Select the library in sidebar
		m.selectLibraryInSidebar(item.NavContext.LibraryID)

		return LoadEpisodesCmd(m.LibrarySvc, item.NavContext.SeasonID)
	}

	return nil
}

// selectLibraryInSidebar selects a library in the sidebar by ID
func (m *Model) selectLibraryInSidebar(libID string) {
	for i, lib := range m.Libraries {
		if lib.ID == libID {
			m.Sidebar.SetSelectedIndex(i)
			break
		}
	}
}
