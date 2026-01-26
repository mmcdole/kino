package tui

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
	"github.com/mmcdole/kino/internal/tui/components"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateBrowsing ApplicationState = iota
	StateSearching
	StateSettings
	StateAuthRequired
	StateHelp
	StateConfirmLogout
)

// NavAwaitKind specifies what async load the plan is waiting for
type NavAwaitKind int

const (
	AwaitNone NavAwaitKind = iota
	AwaitMovies   // AwaitID = LibraryID
	AwaitShows    // AwaitID = LibraryID
	AwaitMixed    // AwaitID = LibraryID (mixed content library)
	AwaitSeasons  // AwaitID = ShowID
	AwaitEpisodes // AwaitID = SeasonID
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

// Layout proportions for Miller Columns
const (
	// 3-Column Smart Ratios (Inspector visible)
	ParentColumnPercent3   = 25 // Parent context
	ActiveColumnPercent3   = 35 // Active/focused
	InspectorColumnPercent = 30 // Inspector (summary)

	// 3-Column Focus Mode (Inspector hidden) - show more navigation context
	GrandparentColumnPercent = 25 // Grandparent context
	ParentColumnPercent2     = 30 // Parent context
	ActiveColumnPercent2     = 45 // Active/focused

	// Root level (single column + inspector)
	RootColumnPercent   = 40
	RootInspectorPercent = 60

	MinColumnWidth = 15

	// Vertical layout: single footer line
	ChromeHeight = 1
)

// Model is the main Bubble Tea model for the application
type Model struct {
	// Application state
	State ApplicationState
	Ready bool

	// Services
	LibrarySvc  *service.LibraryService
	PlaybackSvc *service.PlaybackService
	SearchSvc   *service.SearchService
	PlaylistSvc *service.PlaylistService

	// UI Components - Miller Columns
	ColumnStack   *ColumnStack              // Stack of navigable list columns
	Inspector     components.Inspector      // View projection (always shows details for middle column selection)
	Omnibar       components.Omnibar        // Search modal
	SortModal     components.SortModal      // Sort field selector
	PlaylistModal components.PlaylistModal // Playlist management modal
	InputModal    components.InputModal     // Simple text input modal

	// Data
	Libraries []domain.Library

	// Dimensions
	Width  int
	Height int

	// UI state
	StatusMsg     string
	StatusIsErr   bool
	Loading       bool
	SpinnerFrame  int
	ShowInspector bool // Toggle inspector visibility (default true)

	// Sync state
	LibraryStates map[string]components.LibrarySyncState // Tracks progress per library
	SyncingCount int  // Libraries still syncing
	MultiLibSync bool // True when syncing multiple libraries (R / startup)

	// Navigation plan for deep linking
	navPlan *NavPlan

	// Playlist navigation context (when viewing playlist items)
	currentPlaylistID string
}

// NewModel creates a new application model
func NewModel(
	librarySvc *service.LibraryService,
	playbackSvc *service.PlaybackService,
	searchSvc *service.SearchService,
	playlistSvc *service.PlaylistService,
) Model {
	return Model{
		State:         StateBrowsing,
		LibrarySvc:    librarySvc,
		PlaybackSvc:   playbackSvc,
		SearchSvc:     searchSvc,
		PlaylistSvc:   playlistSvc,
		ColumnStack:   NewColumnStack(),
		Inspector:     components.NewInspector(),
		Omnibar:       components.NewOmnibar(),
		PlaylistModal: components.NewPlaylistModal(),
		InputModal:    components.NewInputModal(),
		LibraryStates: make(map[string]components.LibrarySyncState),
		ShowInspector: false, // Inspector hidden by default - show 3 nav columns
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
			m.ColumnStack.UpdateSpinnerFrame(m.SpinnerFrame)
		}
		return m, TickCmd(100 * time.Millisecond)

	case LibrariesLoadedMsg:
		m.Libraries = msg.Libraries

		// Initialize all states to Syncing
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range msg.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.SyncingCount = len(msg.Libraries)
		m.MultiLibSync = true

		// Append synthetic "Playlists" entry at bottom
		playlistsEntry := domain.Library{
			ID:   "__playlists__",
			Name: "Playlists",
			Type: "playlist",
		}
		allEntries := append(msg.Libraries, playlistsEntry)

		// Create the library column as the root
		libCol := components.NewLibraryColumn(allEntries)
		libCol.SetLibraryStates(m.LibraryStates)
		m.Inspector.SetLibraryStates(m.LibraryStates)
		m.ColumnStack.Reset(libCol)

		// Start parallel sync of ALL libraries
		m.Loading = true
		return m, SyncAllLibrariesCmd(m.LibrarySvc, m.SearchSvc, msg.Libraries, false)

	case MoviesLoadedMsg:
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.updateLibraryStates()
		}

		// Update top column with movies
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Movies)
		}

		m.updateInspector()
		// Index movies for global filter
		m.indexMoviesForFilter(msg.Movies, msg.LibraryID)

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitMovies, msg.LibraryID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case ShowsLoadedMsg:
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.updateLibraryStates()
		}

		// Update top column with shows
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Shows)
		}

		m.updateInspector()
		// Index shows for global filter
		m.indexShowsForFilter(msg.Shows, msg.LibraryID)

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitShows, msg.LibraryID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case LibraryContentLoadedMsg:
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.updateLibraryStates()
		}

		// Update top column with mixed content
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Items)
		}

		m.updateInspector()
		// Index items for global filter
		m.indexMixedContentForFilter(msg.Items, msg.LibraryID)

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitMixed, msg.LibraryID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case SeasonsLoadedMsg:
		m.Loading = false

		// Update top column with seasons
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Seasons)
		}

		m.updateInspector()

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitSeasons, msg.ShowID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case EpisodesLoadedMsg:
		m.Loading = false

		// Update top column with episodes
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Episodes)
		}

		m.updateInspector()

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitEpisodes, msg.SeasonID); cmd != nil {
			return m, cmd
		}
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
		m.clearNavPlan()
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
			m.SyncingCount--
			slog.Error("library sync failed", "libraryID", msg.LibraryID, "error", msg.Error)
		} else {
			state.Loaded = msg.Loaded
			state.Total = msg.Total
			state.FromDisk = msg.FromDisk

			if msg.Done {
				state.Status = components.StatusSynced
				m.SyncingCount--

				// Trigger delayed cleanup
				cmds = append(cmds, ClearLibraryStatusCmd(msg.LibraryID, 2*time.Second))

				// If we're at library level and this is selected library, show its content
				if m.ColumnStack.Len() == 1 {
					if libCol, ok := m.ColumnStack.Top().(*components.ListColumn); ok {
						if lib := libCol.SelectedLibrary(); lib != nil && lib.ID == msg.LibraryID {
							// Don't auto-drill; user must press l/Enter
						}
					}
				}
			}
		}

		m.LibraryStates[msg.LibraryID] = state
		m.updateLibraryStates()

		// If there's a continuation command, run it
		if msg.NextCmd != nil {
			if cmd, ok := msg.NextCmd.(tea.Cmd); ok {
				cmds = append(cmds, cmd)
			}
		}

		// Check if all done
		if m.SyncingCount == 0 {
			m.Loading = false
		}

		return m, tea.Batch(cmds...)


	case ClearLibraryStatusMsg:
		if state, ok := m.LibraryStates[msg.LibraryID]; ok {
			if state.Status == components.StatusSynced {
				state.Status = components.StatusIdle
				m.LibraryStates[msg.LibraryID] = state
				m.updateLibraryStates()
			}
		}
		return m, nil

	case LogoutCompleteMsg:
		if msg.Error != nil {
			m.StatusMsg = fmt.Sprintf("Logout failed: %v", msg.Error)
			m.StatusIsErr = true
			m.State = StateBrowsing
			return m, ClearStatusCmd(5 * time.Second)
		}
		// Logout successful - quit the application
		fmt.Println("\nLogged out. Run 'kino' to set up again.")
		return m, tea.Quit

	case PlaylistsLoadedMsg:
		m.Loading = false
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Playlists)
		}
		m.updateInspector()
		return m, nil

	case PlaylistItemsLoadedMsg:
		m.Loading = false
		m.currentPlaylistID = msg.PlaylistID
		if top := m.ColumnStack.Top(); top != nil {
			top.SetItems(msg.Items)
		}
		m.updateInspector()
		return m, nil

	case PlaylistModalDataMsg:
		m.PlaylistModal.Show(msg.Playlists, msg.Membership, msg.Item)
		m.PlaylistModal.SetSize(m.Width, m.Height)
		return m, nil

	case PlaylistUpdatedMsg:
		if msg.Error != nil {
			m.StatusMsg = fmt.Sprintf("Playlist update failed: %v", msg.Error)
			m.StatusIsErr = true
		} else {
			m.StatusMsg = "Playlist updated"
			// Refresh playlist items if viewing a playlist
			if m.currentPlaylistID != "" {
				return m, LoadPlaylistItemsCmd(m.PlaylistSvc, m.currentPlaylistID)
			}
		}
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case PlaylistCreatedMsg:
		if msg.Error != nil {
			m.StatusMsg = fmt.Sprintf("Failed to create playlist: %v", msg.Error)
			m.StatusIsErr = true
		} else {
			m.StatusMsg = fmt.Sprintf("Created playlist: %s", msg.Playlist.Title)
			// Refresh playlists if viewing playlists
			if top := m.ColumnStack.Top(); top != nil {
				if lc, ok := top.(*components.ListColumn); ok && lc.ColumnType() == components.ColumnTypePlaylists {
					return m, LoadPlaylistsCmd(m.PlaylistSvc)
				}
			}
		}
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case PlaylistDeletedMsg:
		if msg.Error != nil {
			m.StatusMsg = fmt.Sprintf("Failed to delete playlist: %v", msg.Error)
			m.StatusIsErr = true
			cmds = append(cmds, ClearStatusCmd(3*time.Second))
		} else {
			m.StatusMsg = "Playlist deleted"
			// Clear current playlist ID and refresh the playlists
			m.currentPlaylistID = ""
			cmds = append(cmds, LoadPlaylistsCmd(m.PlaylistSvc))
			cmds = append(cmds, ClearStatusCmd(3*time.Second))
		}
		return m, tea.Batch(cmds...)
	}

	// Update the focused column (top of stack)
	if top := m.ColumnStack.Top(); top != nil {
		oldCursor := top.SelectedIndex()
		var cmd tea.Cmd
		newCol, cmd := top.Update(msg)
		// Replace the top column with updated version
		if lc, ok := newCol.(*components.ListColumn); ok {
			m.ColumnStack.columns[len(m.ColumnStack.columns)-1] = lc
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if oldCursor != top.SelectedIndex() {
			m.updateInspector()
		}
	}

	// Update omnibar if visible
	if m.Omnibar.IsVisible() {
		var selected bool
		var cmd tea.Cmd
		m.Omnibar, cmd, selected = m.Omnibar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Handle real-time filtering
		if m.Omnibar.IsFilterMode() && m.Omnibar.QueryChanged() {
			query := m.Omnibar.Query()
			results := m.SearchSvc.FilterLocal(query)
			m.Omnibar.SetFilterResults(results)
		}

		if selected {
			if m.Omnibar.IsFilterMode() {
				result := m.Omnibar.SelectedFilterResult()
				if result != nil {
					m.Omnibar.Hide()
					navCmd := m.navigateToFilteredItem(*result)
					if navCmd != nil {
						cmds = append(cmds, navCmd)
					}
				}
			} else {
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

	case StateConfirmLogout:
		switch msg.String() {
		case "y", "Y":
			// User confirmed logout
			return m, LogoutCmd()
		case "n", "N", "esc":
			// User cancelled
			m.State = StateBrowsing
		}
		return m, nil
	}

	// Handle omnibar if visible
	if m.Omnibar.IsVisible() {
		var cmd tea.Cmd
		var selected bool
		m.Omnibar, cmd, selected = m.Omnibar.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if m.Omnibar.IsFilterMode() && m.Omnibar.QueryChanged() {
			query := m.Omnibar.Query()
			results := m.SearchSvc.FilterLocal(query)
			m.Omnibar.SetFilterResults(results)
		}

		if selected {
			if m.Omnibar.IsFilterMode() {
				if result := m.Omnibar.SelectedFilterResult(); result != nil {
					m.Omnibar.Hide()
					navCmd := m.navigateToFilteredItem(*result)
					if navCmd != nil {
						cmds = append(cmds, navCmd)
					}
				}
			} else {
				if result := m.Omnibar.SelectedResult(); result != nil {
					m.Omnibar.Hide()
					cmds = append(cmds, PlayItemCmd(m.PlaybackSvc, *result, false))
				}
			}
		}
		return m, tea.Batch(cmds...)
	}

	// Handle sort modal if visible
	if m.SortModal.IsVisible() {
		handled, selection := m.SortModal.HandleKey(msg.String())
		if handled {
			if selection != nil {
				// Apply sort to current column
				if top := m.ColumnStack.Top(); top != nil {
					if lc, ok := top.(*components.ListColumn); ok {
						lc.ApplySort(selection.Field, selection.Direction)
						m.updateInspector()
					}
				}
			}
			return m, nil
		}
	}

	// Handle playlist modal if visible
	if m.PlaylistModal.IsVisible() {
		handled, shouldClose, shouldCreate := m.PlaylistModal.HandleKeyMsg(msg)
		if handled {
			if shouldCreate {
				// Create new playlist with current item, plus apply any checkbox changes
				title := m.PlaylistModal.NewPlaylistTitle()
				item := m.PlaylistModal.Item()
				changes := m.PlaylistModal.GetChanges()
				m.PlaylistModal.Hide()

				if title != "" && item != nil {
					var batchCmds []tea.Cmd
					batchCmds = append(batchCmds, CreatePlaylistCmd(m.PlaylistSvc, title, []string{item.ID}))
					// Also apply any checkbox changes
					for _, change := range changes {
						if change.Add {
							batchCmds = append(batchCmds, AddToPlaylistCmd(m.PlaylistSvc, change.PlaylistID, []string{item.ID}))
						} else {
							batchCmds = append(batchCmds, RemoveFromPlaylistCmd(m.PlaylistSvc, change.PlaylistID, item.ID))
						}
					}
					return m, tea.Batch(batchCmds...)
				}
			}
			if shouldClose {
				// Apply pending changes
				changes := m.PlaylistModal.GetChanges()
				item := m.PlaylistModal.Item()
				m.PlaylistModal.Hide()

				if len(changes) > 0 && item != nil {
					// Apply changes (add/remove from playlists)
					var batchCmds []tea.Cmd
					for _, change := range changes {
						if change.Add {
							batchCmds = append(batchCmds, AddToPlaylistCmd(m.PlaylistSvc, change.PlaylistID, []string{item.ID}))
						} else {
							batchCmds = append(batchCmds, RemoveFromPlaylistCmd(m.PlaylistSvc, change.PlaylistID, item.ID))
						}
					}
					if len(batchCmds) > 0 {
						return m, tea.Batch(batchCmds...)
					}
				}
			}
			return m, nil
		}
	}

	// Handle input modal if visible
	if m.InputModal.IsVisible() {
		var cmd tea.Cmd
		var submitted bool
		m.InputModal, cmd, submitted = m.InputModal.Update(msg)
		if submitted {
			title := m.InputModal.Value()
			m.InputModal.Hide()
			if title != "" {
				return m, CreatePlaylistCmd(m.PlaylistSvc, title, []string{})
			}
		}
		if cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	// Handle filter typing mode
	if top := m.ColumnStack.Top(); top != nil {
		if lc, ok := top.(*components.ListColumn); ok && lc.IsFilterTyping() {
			oldCursor := lc.SelectedIndex()
			newCol, _ := lc.Update(msg)
			m.ColumnStack.columns[len(m.ColumnStack.columns)-1] = newCol
			if oldCursor != top.SelectedIndex() {
				m.updateInspector()
			}
			return m, nil
		}
	}

	// Global keys
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.State = StateHelp
		return m, nil

	case "esc":
		// Clear active filter if any
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok && lc.IsFiltering() {
				lc.ClearFilter()
				return m, nil
			}
		}
		// Cancel active nav plan if any
		if m.navPlan != nil {
			m.clearNavPlan()
			m.StatusMsg = "Navigation cancelled"
			return m, ClearStatusCmd(2 * time.Second)
		}
		return m, nil

	case "/":
		// Activate filter in middle column
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				lc.ToggleFilter()
			}
		}
		return m, nil

	case "f":
		// Global search via Omnibar
		m.Omnibar.ShowFilterMode()
		m.Omnibar.SetSize(m.Width, m.Height)
		m.Omnibar.SetLoading(true)
		return m, tea.Batch(
			m.Omnibar.Init(),
			LoadAllForGlobalSearchCmd(m.LibrarySvc, m.SearchSvc, m.Libraries),
		)

	case "s":
		// Sort modal (only for movies/shows columns)
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				var opts []components.SortField
				switch lc.ColumnType() {
				case components.ColumnTypeMovies:
					opts = components.MovieSortOptions()
				case components.ColumnTypeShows:
					opts = components.ShowSortOptions()
				}
				if opts != nil {
					field, dir := lc.SortState()
					m.SortModal.Show(opts, field, dir)
				}
			}
		}
		return m, nil

	case "h", "left", "backspace":
		// Go back (pop column stack)
		return m.handleBack()

	case "l", "right":
		// Drill into (push new column)
		return m.handleDrillIn()

	case "enter":
		// Enter can drill in OR play depending on selection
		return m.handleEnter()

	case "r":
		// Refresh single selected library
		if m.ColumnStack.Len() >= 1 {
			if libCol, ok := m.ColumnStack.Get(0).(*components.ListColumn); ok {
				if lib := libCol.SelectedLibrary(); lib != nil {
					m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
					m.SyncingCount++
					m.Loading = true
					m.MultiLibSync = false
					m.updateLibraryStates()
					return m, SyncLibraryCmd(m.LibrarySvc, m.SearchSvc, *lib, true)
				}
			}
		}
		return m, nil

	case "R":
		// Refresh ALL libraries
		m.SearchSvc.ClearFilterIndex()
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range m.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.SyncingCount = len(m.Libraries)
		m.Loading = true
		m.MultiLibSync = true
		m.updateLibraryStates()

		// Append synthetic "Playlists" entry at bottom
		playlistsEntry := domain.Library{
			ID:   "__playlists__",
			Name: "Playlists",
			Type: "playlist",
		}
		allEntries := append(m.Libraries, playlistsEntry)

		// Reset to library view
		libCol := components.NewLibraryColumn(allEntries)
		libCol.SetLibraryStates(m.LibraryStates)
		m.Inspector.SetLibraryStates(m.LibraryStates)
		m.ColumnStack.Reset(libCol)

		return m, SyncAllLibrariesCmd(m.LibrarySvc, m.SearchSvc, m.Libraries, true)

	case "w":
		// Mark as watched
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				if item := lc.SelectedMediaItem(); item != nil {
					return m, MarkWatchedCmd(m.PlaybackSvc, item.ID, item.Title)
				}
			}
		}
		return m, nil

	case "u":
		// Mark as unwatched
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				if item := lc.SelectedMediaItem(); item != nil {
					return m, MarkUnwatchedCmd(m.PlaybackSvc, item.ID, item.Title)
				}
			}
		}
		return m, nil

	case "p":
		// Play from beginning
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				if item := lc.SelectedMediaItem(); item != nil {
					return m, PlayItemCmd(m.PlaybackSvc, *item, false)
				}
			}
		}
		return m, nil

	case "i":
		// Toggle inspector visibility
		m.ShowInspector = !m.ShowInspector
		m.updateLayout()
		return m, nil

	case "L":
		// Logout (Shift+L) - show confirmation modal
		m.State = StateConfirmLogout
		return m, nil

	case " ":
		// Space: Open playlist modal for selected playable item
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				item := lc.SelectedMediaItem()
				if item != nil && m.PlaylistSvc != nil {
					return m, LoadPlaylistModalDataCmd(m.PlaylistSvc, item)
				}
			}
		}
		return m, nil

	case "x":
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok {
				switch lc.ColumnType() {
				case components.ColumnTypePlaylistItems:
					// Remove item from playlist
					if item := lc.SelectedMediaItem(); item != nil && m.currentPlaylistID != "" {
						return m, RemoveFromPlaylistCmd(m.PlaylistSvc, m.currentPlaylistID, item.ID)
					}
				case components.ColumnTypePlaylists:
					// Delete playlist
					if playlist := lc.SelectedPlaylist(); playlist != nil {
						return m, DeletePlaylistCmd(m.PlaylistSvc, playlist.ID)
					}
				}
			}
		}
		return m, nil

	case "n":
		// Plex doesn't support empty playlists - show hint to use Space instead
		if top := m.ColumnStack.Top(); top != nil {
			if lc, ok := top.(*components.ListColumn); ok && lc.ColumnType() == components.ColumnTypePlaylists {
				m.StatusMsg = "Use Space on an item to create a playlist"
				return m, ClearStatusCmd(3 * time.Second)
			}
		}
	}

	// Let the focused column handle remaining keys (j/k/g/G navigation)
	if top := m.ColumnStack.Top(); top != nil {
		oldCursor := top.SelectedIndex()
		newCol, cmd := top.Update(msg)
		// Replace the top column
		if lc, ok := newCol.(*components.ListColumn); ok {
			m.ColumnStack.columns[len(m.ColumnStack.columns)-1] = lc
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if oldCursor != top.SelectedIndex() {
			m.updateInspector()
		}
	}

	return m, tea.Batch(cmds...)
}

// handleDrillIn handles drilling into the selected item (l key)
func (m Model) handleDrillIn() (tea.Model, tea.Cmd) {
	top := m.ColumnStack.Top()
	if top == nil {
		return m, nil
	}

	if !top.CanDrillInto() {
		// Can't drill into leaf items - play instead
		if lc, ok := top.(*components.ListColumn); ok {
			if item := lc.SelectedMediaItem(); item != nil {
				resume := item.ViewOffset > 0 && !item.IsPlayed
				return m, PlayItemCmd(m.PlaybackSvc, *item, resume)
			}
		}
		return m, nil
	}

	return m.drillIntoSelection()
}

// handleEnter handles the enter key press
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	top := m.ColumnStack.Top()
	if top == nil {
		return m, nil
	}

	if top.CanDrillInto() {
		return m.drillIntoSelection()
	}

	// Not drillable - play the item
	if lc, ok := top.(*components.ListColumn); ok {
		if item := lc.SelectedMediaItem(); item != nil {
			resume := item.ViewOffset > 0 && !item.IsPlayed
			return m, PlayItemCmd(m.PlaybackSvc, *item, resume)
		}
	}

	return m, nil
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
				Cmd:       LoadLibraryContentCmd(m.LibrarySvc, v.ID),
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
				Cmd:       LoadLibraryContentCmd(m.LibrarySvc, v.ID),
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
	if top := m.ColumnStack.Top(); top != nil {
		if lc, ok := top.(*components.ListColumn); ok && lc.ColumnType() == components.ColumnTypePlaylistItems {
			m.currentPlaylistID = ""
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

// handleItemSelection handles selection of a playable item
func (m Model) handleItemSelection(item domain.MediaItem) (Model, tea.Cmd) {
	resume := item.ViewOffset > 0 && !item.IsPlayed
	return m, PlayItemCmd(m.PlaybackSvc, item, resume)
}

// updateLibraryStates updates the library states in the library column and inspector
func (m *Model) updateLibraryStates() {
	// Find the library column (should be at index 0)
	if m.ColumnStack.Len() > 0 {
		if libCol, ok := m.ColumnStack.Get(0).(*components.ListColumn); ok {
			libCol.SetLibraryStates(m.LibraryStates)
		}
	}
	m.Inspector.SetLibraryStates(m.LibraryStates)
}

// refreshCurrentView refreshes the current view
func (m *Model) refreshCurrentView() tea.Cmd {
	m.LibrarySvc.RefreshAll()
	m.Loading = true

	top := m.ColumnStack.Top()
	if top == nil {
		return LoadLibrariesCmd(m.LibrarySvc)
	}

	// Get context from column stack to reload
	if lc, ok := top.(*components.ListColumn); ok {
		switch lc.ColumnType() {
		case components.ColumnTypeMovies:
			// Find the library from the library column
			if m.ColumnStack.Len() > 0 {
				if libCol, ok := m.ColumnStack.Get(0).(*components.ListColumn); ok {
					if lib := libCol.SelectedLibrary(); lib != nil {
						return LoadMoviesCmd(m.LibrarySvc, lib.ID)
					}
				}
			}
		case components.ColumnTypeShows:
			if m.ColumnStack.Len() > 0 {
				if libCol, ok := m.ColumnStack.Get(0).(*components.ListColumn); ok {
					if lib := libCol.SelectedLibrary(); lib != nil {
						return LoadShowsCmd(m.LibrarySvc, lib.ID)
					}
				}
			}
		case components.ColumnTypeSeasons:
			// Get show from parent column
			if m.ColumnStack.Len() > 1 {
				if showCol, ok := m.ColumnStack.Get(m.ColumnStack.Len()-2).(*components.ListColumn); ok {
					if show := showCol.SelectedShow(); show != nil {
						return LoadSeasonsCmd(m.LibrarySvc, show.ID)
					}
				}
			}
		case components.ColumnTypeEpisodes:
			// Get season from parent column
			if m.ColumnStack.Len() > 1 {
				if seasonCol, ok := m.ColumnStack.Get(m.ColumnStack.Len()-2).(*components.ListColumn); ok {
					if season := seasonCol.SelectedSeason(); season != nil {
						return LoadEpisodesCmd(m.LibrarySvc, season.ID)
					}
				}
			}
		case components.ColumnTypeMixed:
			// Get library from the library column
			if m.ColumnStack.Len() > 0 {
				if libCol, ok := m.ColumnStack.Get(0).(*components.ListColumn); ok {
					if lib := libCol.SelectedLibrary(); lib != nil {
						return LoadLibraryContentCmd(m.LibrarySvc, lib.ID)
					}
				}
			}
		}
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

// updateInspector updates the inspector with the selected item from middle column
func (m *Model) updateInspector() {
	if top := m.ColumnStack.Top(); top != nil {
		m.Inspector.SetItem(top.SelectedItem())
	} else {
		m.Inspector.SetItem(nil)
	}
}

// updateLayout updates component sizes based on window size
func (m *Model) updateLayout() {
	if m.Width == 0 || m.Height == 0 {
		return
	}

	availableWidth := m.Width
	contentHeight := m.Height - ChromeHeight

	m.Omnibar.SetSize(m.Width, m.Height)

	stackLen := m.ColumnStack.Len()
	if stackLen == 0 {
		return
	}

	// Calculate column widths based on stack depth and inspector visibility
	if stackLen == 1 {
		// Root level: single column (Libraries)
		if m.ShowInspector {
			leftWidth := availableWidth * RootColumnPercent / 100
			if leftWidth < MinColumnWidth {
				leftWidth = MinColumnWidth
			}
			rightWidth := availableWidth - leftWidth
			m.ColumnStack.Get(0).SetSize(leftWidth, contentHeight)
			m.Inspector.SetSize(rightWidth, contentHeight)
		} else {
			m.ColumnStack.Get(0).SetSize(availableWidth, contentHeight)
		}
	} else if stackLen == 2 {
		// 2 columns in stack
		topIdx := stackLen - 1
		if m.ShowInspector {
			parentWidth := availableWidth * ParentColumnPercent3 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			inspectorWidth := availableWidth * InspectorColumnPercent / 100
			if inspectorWidth < MinColumnWidth {
				inspectorWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth - inspectorWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}
			m.ColumnStack.Get(topIdx - 1).SetSize(parentWidth, contentHeight)
			m.ColumnStack.Get(topIdx).SetSize(activeWidth, contentHeight)
			m.Inspector.SetSize(inspectorWidth, contentHeight)
		} else {
			parentWidth := availableWidth * ParentColumnPercent2 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}
			m.ColumnStack.Get(topIdx - 1).SetSize(parentWidth, contentHeight)
			m.ColumnStack.Get(topIdx).SetSize(activeWidth, contentHeight)
		}
	} else {
		// 3+ columns in stack
		topIdx := stackLen - 1
		if m.ShowInspector {
			parentWidth := availableWidth * ParentColumnPercent3 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			inspectorWidth := availableWidth * InspectorColumnPercent / 100
			if inspectorWidth < MinColumnWidth {
				inspectorWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth - inspectorWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}
			m.ColumnStack.Get(topIdx - 1).SetSize(parentWidth, contentHeight)
			m.ColumnStack.Get(topIdx).SetSize(activeWidth, contentHeight)
			m.Inspector.SetSize(inspectorWidth, contentHeight)
		} else {
			// 3-Column Navigation: [Grandparent | Parent | Active]
			grandparentWidth := availableWidth * GrandparentColumnPercent / 100
			if grandparentWidth < MinColumnWidth {
				grandparentWidth = MinColumnWidth
			}
			parentWidth := availableWidth * ParentColumnPercent2 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			activeWidth := availableWidth - grandparentWidth - parentWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}
			m.ColumnStack.Get(topIdx - 2).SetSize(grandparentWidth, contentHeight)
			m.ColumnStack.Get(topIdx - 1).SetSize(parentWidth, contentHeight)
			m.ColumnStack.Get(topIdx).SetSize(activeWidth, contentHeight)
		}
	}
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

	if m.State == StateConfirmLogout {
		return m.renderLogoutConfirmation()
	}

	availableWidth := m.Width
	contentHeight := m.Height - ChromeHeight
	stackLen := m.ColumnStack.Len()

	var content string

	if stackLen == 0 {
		// No columns - shouldn't happen
		content = ""
	} else if stackLen == 1 {
		// Root level: single column (Libraries)
		col := m.ColumnStack.Get(0)

		if m.ShowInspector {
			// [Libraries | Inspector]
			leftWidth := availableWidth * RootColumnPercent / 100
			if leftWidth < MinColumnWidth {
				leftWidth = MinColumnWidth
			}
			rightWidth := availableWidth - leftWidth

			col.SetSize(leftWidth, contentHeight)
			m.Inspector.SetSize(rightWidth, contentHeight)
			m.Inspector.SetItem(col.SelectedItem())

			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				col.View(),
				m.Inspector.View(),
			)
		} else {
			// [Libraries] - full width
			col.SetSize(availableWidth, contentHeight)
			content = col.View()
		}
	} else if stackLen == 2 {
		// 2 columns in stack
		topIdx := stackLen - 1
		parentCol := m.ColumnStack.Get(topIdx - 1)
		currentCol := m.ColumnStack.Get(topIdx)

		if m.ShowInspector {
			// 3-Column: [Parent | Active | Inspector]
			parentWidth := availableWidth * ParentColumnPercent3 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			inspectorWidth := availableWidth * InspectorColumnPercent / 100
			if inspectorWidth < MinColumnWidth {
				inspectorWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth - inspectorWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}

			parentCol.SetSize(parentWidth, contentHeight)
			currentCol.SetSize(activeWidth, contentHeight)
			m.Inspector.SetSize(inspectorWidth, contentHeight)
			m.Inspector.SetItem(currentCol.SelectedItem())

			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				parentCol.View(),
				currentCol.View(),
				m.Inspector.View(),
			)
		} else {
			// 2-Column: [Parent | Active]
			parentWidth := availableWidth * ParentColumnPercent2 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}

			parentCol.SetSize(parentWidth, contentHeight)
			currentCol.SetSize(activeWidth, contentHeight)

			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				parentCol.View(),
				currentCol.View(),
			)
		}
	} else {
		// 3+ columns in stack
		topIdx := stackLen - 1
		currentCol := m.ColumnStack.Get(topIdx)
		parentCol := m.ColumnStack.Get(topIdx - 1)

		if m.ShowInspector {
			// 3-Column: [Parent | Active | Inspector]
			parentWidth := availableWidth * ParentColumnPercent3 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			inspectorWidth := availableWidth * InspectorColumnPercent / 100
			if inspectorWidth < MinColumnWidth {
				inspectorWidth = MinColumnWidth
			}
			activeWidth := availableWidth - parentWidth - inspectorWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}

			parentCol.SetSize(parentWidth, contentHeight)
			currentCol.SetSize(activeWidth, contentHeight)
			m.Inspector.SetSize(inspectorWidth, contentHeight)
			m.Inspector.SetItem(currentCol.SelectedItem())

			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				parentCol.View(),
				currentCol.View(),
				m.Inspector.View(),
			)
		} else {
			// 3-Column Navigation: [Grandparent | Parent | Active]
			grandparentCol := m.ColumnStack.Get(topIdx - 2)

			grandparentWidth := availableWidth * GrandparentColumnPercent / 100
			if grandparentWidth < MinColumnWidth {
				grandparentWidth = MinColumnWidth
			}
			parentWidth := availableWidth * ParentColumnPercent2 / 100
			if parentWidth < MinColumnWidth {
				parentWidth = MinColumnWidth
			}
			activeWidth := availableWidth - grandparentWidth - parentWidth
			if activeWidth < MinColumnWidth {
				activeWidth = MinColumnWidth
			}

			grandparentCol.SetSize(grandparentWidth, contentHeight)
			parentCol.SetSize(parentWidth, contentHeight)
			currentCol.SetSize(activeWidth, contentHeight)

			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				grandparentCol.View(),
				parentCol.View(),
				currentCol.View(),
			)
		}
	}

	// Footer
	footer := m.renderFooter()

	// Combine all
	view := lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		footer,
	)

	// Overlay omnibar if visible
	if m.Omnibar.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.Omnibar.View())
	}

	// Overlay sort modal if visible
	if m.SortModal.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.SortModal.View())
	}

	// Overlay playlist modal if visible
	if m.PlaylistModal.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.PlaylistModal.View())
	}

	// Overlay input modal if visible
	if m.InputModal.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.InputModal.View())
	}

	return view
}

// renderFooter renders a single-line minimal footer
func (m Model) renderFooter() string {
	// Left side: spinner + status when loading or status message active
	var left string
	if m.Loading {
		statusText := "Loading..."

		if m.MultiLibSync {
			// Multi-library: stable library completion fraction
			syncingCount := 0
			for _, state := range m.LibraryStates {
				if state.Status == components.StatusSyncing {
					syncingCount++
				}
			}
			done := len(m.LibraryStates) - syncingCount
			statusText = fmt.Sprintf("Syncing %d/%d libraries...", done, len(m.LibraryStates))
		} else {
			// Single library: show name + item progress
			for id, state := range m.LibraryStates {
				if state.Status == components.StatusSyncing {
					libName := ""
					for _, lib := range m.Libraries {
						if lib.ID == id {
							libName = lib.Name
							break
						}
					}
					if state.Total > 0 {
						statusText = fmt.Sprintf("Syncing %s · %d/%d", libName, state.Loaded, state.Total)
					} else if libName != "" {
						statusText = fmt.Sprintf("Syncing %s...", libName)
					}
					break
				}
			}
		}

		left = RenderSpinner(m.SpinnerFrame) + " " + styles.DimStyle.Render(statusText)
	} else if m.StatusMsg != "" {
		if m.StatusIsErr {
			left = styles.ErrorStyle.Render(m.StatusMsg)
		} else {
			left = styles.DimStyle.Render(m.StatusMsg)
		}
	}

	// Center section: context-specific hints based on column type
	var center string
	if top := m.ColumnStack.Top(); top != nil {
		if lc, ok := top.(*components.ListColumn); ok {
			switch lc.ColumnType() {
			case components.ColumnTypePlaylists:
				center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Delete")
			case components.ColumnTypePlaylistItems:
				center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Remove")
			}
		}
	}

	// Right side: "? help" hint
	right := styles.AccentStyle.Render("?") + styles.DimStyle.Render(" help")

	// Layout: left + centered hints + right
	leftWidth := lipgloss.Width(left)
	centerWidth := lipgloss.Width(center)
	rightWidth := lipgloss.Width(right)

	totalContent := leftWidth + centerWidth + rightWidth
	if totalContent >= m.Width {
		// Not enough space - just left + right
		gap := m.Width - leftWidth - rightWidth
		if gap < 0 {
			gap = 0
		}
		return left + strings.Repeat(" ", gap) + right
	}

	// Center the hints in available space
	available := m.Width - leftWidth - rightWidth
	leftPad := (available - centerWidth) / 2
	rightPad := available - centerWidth - leftPad

	return left + strings.Repeat(" ", leftPad) + center + strings.Repeat(" ", rightPad) + right
}

// renderHelp renders the help screen
func (m Model) renderHelp() string {
	help := `
NAVIGATION                      PLAYBACK
  j/k        Up/down               Enter  Play/resume
  h/l        Back/drill in         p      Play from start
  g/Home     First item            w      Mark watched
  G/End      Last item             u      Mark unwatched
  PgUp/PgDn  Scroll page
  Ctrl+u/d   Scroll half page

SEARCH & VIEW                   OTHER
  /          Filter                r      Refresh library
  f          Global search         R      Refresh all
  s          Sort                  q      Quit
  i          Toggle inspector      ?      This help
  Space      Manage playlists      Esc    Close / Cancel
                                   L      Logout

Press any key to return...
`

	return lipgloss.Place(m.Width, m.Height,
		lipgloss.Center, lipgloss.Center,
		styles.ModalStyle.Render(help))
}

// renderLogoutConfirmation renders the logout confirmation modal
func (m Model) renderLogoutConfirmation() string {
	modal := `
              Log Out?

  This will clear your credentials,
  server URL, and all cached data.

        [Y] Yes      [N] No
`

	return lipgloss.Place(m.Width, m.Height,
		lipgloss.Center, lipgloss.Center,
		styles.ModalStyle.Render(modal))
}

// indexMoviesForFilter indexes movies for the global filter
func (m *Model) indexMoviesForFilter(movies []*domain.MediaItem, libID string) {
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
				MovieID:     movie.ID,
			},
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// indexShowsForFilter indexes shows for the global filter
func (m *Model) indexShowsForFilter(shows []*domain.Show, libID string) {
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
				ShowID:      show.ID,
				ShowTitle:   show.Title,
			},
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// indexMixedContentForFilter indexes mixed library content (movies + shows) for the global filter
func (m *Model) indexMixedContentForFilter(content []domain.ListItem, libID string) {
	libName := m.findLibraryName(libID)
	items := make([]service.FilterItem, 0, len(content))

	for _, item := range content {
		switch v := item.(type) {
		case *domain.MediaItem:
			items = append(items, service.FilterItem{
				Item:  v,
				Title: v.Title,
				Type:  domain.MediaTypeMovie,
				NavContext: service.NavigationContext{
					LibraryID:   libID,
					LibraryName: libName,
					MovieID:     v.ID,
				},
			})
		case *domain.Show:
			items = append(items, service.FilterItem{
				Item:  v,
				Title: v.Title,
				Type:  domain.MediaTypeShow,
				NavContext: service.NavigationContext{
					LibraryID:   libID,
					LibraryName: libName,
					ShowID:      v.ID,
					ShowTitle:   v.Title,
				},
			})
		}
	}

	m.SearchSvc.IndexForFilter(items)
}

// indexEpisodesForFilter indexes episodes for the global filter
func (m *Model) indexEpisodesForFilter(episodes []domain.MediaItem, seasonID string) {
	if len(episodes) == 0 {
		return
	}

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
				EpisodeID:   ep.ID,
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


// clearNavPlan clears the current navigation plan
func (m *Model) clearNavPlan() {
	m.navPlan = nil
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
		lc, ok := top.(*components.ListColumn)
		if !ok || !lc.SetSelectedByID(target.ID) {
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
		if lib.ID == item.NavContext.LibraryID {
			libCol.SetSelectedIndex(i)
			break
		}
	}

	m.ColumnStack.Reset(libCol)

	switch item.Type {
	case domain.MediaTypeMovie:
		lib := m.findLibrary(item.NavContext.LibraryID)
		if lib == nil {
			return nil
		}

		m.navPlan = &NavPlan{
			Targets: []NavTarget{
				{ID: item.NavContext.MovieID},
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
		return LoadMoviesCmd(m.LibrarySvc, item.NavContext.LibraryID)

	case domain.MediaTypeShow:
		lib := m.findLibrary(item.NavContext.LibraryID)
		if lib == nil {
			return nil
		}

		show, ok := item.Item.(*domain.Show)
		if !ok {
			return nil
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
		cachedShows := m.LibrarySvc.GetCachedShows(item.NavContext.LibraryID)
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
		return LoadShowsCmd(m.LibrarySvc, item.NavContext.LibraryID)

	case domain.MediaTypeEpisode:
		lib := m.findLibrary(item.NavContext.LibraryID)
		if lib == nil {
			return nil
		}

		// Build NavPlan: Shows -> Seasons -> Episodes
		m.navPlan = &NavPlan{
			Targets: []NavTarget{
				{ID: item.NavContext.ShowID},    // Step 0: Select show
				{ID: item.NavContext.SeasonID},  // Step 1: Select season
				{ID: item.NavContext.EpisodeID}, // Step 2: Select episode
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

		return LoadShowsCmd(m.LibrarySvc, item.NavContext.LibraryID)
	}

	return nil
}
