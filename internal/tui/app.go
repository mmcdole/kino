package tui

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/library"
	"github.com/mmcdole/kino/internal/player"
	"github.com/mmcdole/kino/internal/playlist"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateBrowsing ApplicationState = iota
	StateHelp
	StateConfirmLogout
)

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
	RootColumnPercent    = 40
	RootInspectorPercent = 60

	MinColumnWidth = 15

	// Vertical layout: single footer line
	ChromeHeight = 1

	// Synthetic library entry for playlists
	playlistsLibraryID = "__playlists__"
)

// playlistsLibraryEntry returns the synthetic library entry for playlists
func playlistsLibraryEntry() domain.Library {
	return domain.Library{
		ID:   playlistsLibraryID,
		Name: "Playlists",
		Type: "playlist",
	}
}

// allLibraryEntries returns libraries plus the synthetic Playlists entry
func (m *Model) allLibraryEntries() []domain.Library {
	return append(m.Libraries, playlistsLibraryEntry())
}

// Model is the main Bubble Tea model for the application
type Model struct {
	// Application state
	State ApplicationState
	Ready bool

	// Cache reads (View-safe)
	Store domain.Store

	// Network coordination (concrete types, not interfaces)
	LibraryService  *library.Service
	PlaylistService *playlist.Service

	// Other services
	SearchSvc   *search.Service
	PlaybackSvc *player.Service

	// UI Components - Miller Columns
	ColumnStack   *ColumnStack             // Stack of navigable list columns
	Inspector     components.Inspector     // View projection (always shows details for middle column selection)
	GlobalSearch  components.GlobalSearch  // Search modal
	SortModal     components.SortModal     // Sort field selector
	PlaylistModal components.PlaylistModal // Playlist management modal
	InputModal    components.InputModal    // Simple text input modal

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
	SyncingCount  int                                    // Libraries still syncing
	MultiLibSync  bool                                   // True when syncing multiple libraries (R / startup)

	// Navigation plan for deep linking
	navPlan *NavPlan

	// Playlist navigation context (when viewing playlist items)
	currentPlaylistID string

	// Navigation context for hierarchical cache keys (cascade invalidation)
	currentLibID  string // Set when entering a library
	currentShowID string // Set when entering a show

	// UI preferences from config
	UIConfig config.UIConfig
}

// NewModel creates a new application model
func NewModel(
	store domain.Store,
	librarySvc *library.Service,
	playlistSvc *playlist.Service,
	searchSvc *search.Service,
	playbackSvc *player.Service,
	uiConfig config.UIConfig,
) Model {
	return Model{
		State:           StateBrowsing,
		Store:           store,
		LibraryService:  librarySvc,
		PlaylistService: playlistSvc,
		SearchSvc:       searchSvc,
		PlaybackSvc:     playbackSvc,
		ColumnStack:     NewColumnStack(),
		Inspector:       components.NewInspector(),
		GlobalSearch:    components.NewGlobalSearch(),
		PlaylistModal:   components.NewPlaylistModal(),
		InputModal:      components.NewInputModal(),
		LibraryStates:   make(map[string]components.LibrarySyncState),
		ShowInspector:   false, // Inspector hidden by default - show 3 nav columns
		UIConfig:        uiConfig,
	}
}

// Init initializes the application
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		LoadLibrariesCmd(m.LibraryService),
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
		// Always propagate spinner frame - columns render spinner only when their loading flag is true
		m.ColumnStack.UpdateSpinnerFrame(m.SpinnerFrame)
		return m, TickCmd(100 * time.Millisecond)

	case LibrariesLoadedMsg:
		m.Libraries = msg.Libraries

		// Initialize all states to Syncing (including playlists)
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range msg.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.LibraryStates[playlistsLibraryID] = components.LibrarySyncState{Status: components.StatusSyncing}
		m.SyncingCount = len(msg.Libraries) + 1 // +1 for playlists
		m.MultiLibSync = true

		// Create the library column as the root
		libCol := components.NewLibraryColumn(m.allLibraryEntries())
		libCol.SetLibraryStates(m.LibraryStates)
		libCol.SetShowWatchStatus(m.UIConfig.ShowWatchStatus)
		m.Inspector.SetLibraryStates(m.LibraryStates)
		m.ColumnStack.Reset(libCol)

		// Start parallel sync of ALL libraries + playlists
		m.Loading = true
		return m, tea.Batch(
			SyncAllLibrariesCmd(m.LibraryService, msg.Libraries),
			SyncPlaylistsCmd(m.PlaylistService, playlistsLibraryID),
		)

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

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitShows, msg.LibraryID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case MixedLibraryLoadedMsg:
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
			state.FromCache = msg.FromCache

			if msg.Done {
				state.Status = components.StatusSynced
				m.SyncingCount--

				// Trigger delayed cleanup
				cmds = append(cmds, ClearLibraryStatusCmd(msg.LibraryID, 2*time.Second))

				// If we're at library level and this is selected library, show its content
				if m.ColumnStack.Len() == 1 {
					if libCol := m.ColumnStack.Top(); libCol != nil {
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
			cmds = append(cmds, msg.NextCmd)
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
				return m, LoadPlaylistItemsCmd(m.PlaylistService, m.currentPlaylistID)
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
			if top := m.ColumnStack.Top(); top != nil && top.ColumnType() == components.ColumnTypePlaylists {
				return m, LoadPlaylistsCmd(m.PlaylistService)
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
			cmds = append(cmds, LoadPlaylistsCmd(m.PlaylistService))
			cmds = append(cmds, ClearStatusCmd(3*time.Second))
		}
		return m, tea.Batch(cmds...)
	}

	// Update the focused column (top of stack)
	if top := m.ColumnStack.Top(); top != nil {
		oldCursor := top.SelectedIndex()
		newCol, cmd := top.Update(msg)
		m.ColumnStack.columns[len(m.ColumnStack.columns)-1] = newCol
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if oldCursor != top.SelectedIndex() {
			m.updateInspector()
		}
	}

	return m, tea.Batch(cmds...)
}

// libraryColumn returns the library column (index 0) or nil if not available
func (m *Model) libraryColumn() *components.ListColumn {
	return m.ColumnStack.Get(0)
}

// updateLibraryStates updates the library states in the library column and inspector
func (m *Model) updateLibraryStates() {
	if libCol := m.libraryColumn(); libCol != nil {
		libCol.SetLibraryStates(m.LibraryStates)
	}
	m.Inspector.SetLibraryStates(m.LibraryStates)
}

// refreshCurrentView refreshes the current view
func (m *Model) refreshCurrentView() tea.Cmd {
	m.LibraryService.InvalidateAll()
	m.Loading = true

	top := m.ColumnStack.Top()
	if top == nil {
		return LoadLibrariesCmd(m.LibraryService)
	}

	// Get context from column stack to reload
	switch top.ColumnType() {
	case components.ColumnTypeMovies:
		if libCol := m.libraryColumn(); libCol != nil {
			if lib := libCol.SelectedLibrary(); lib != nil {
				return LoadMoviesCmd(m.LibraryService, lib.ID)
			}
		}
	case components.ColumnTypeShows:
		if libCol := m.libraryColumn(); libCol != nil {
			if lib := libCol.SelectedLibrary(); lib != nil {
				return LoadShowsCmd(m.LibraryService, lib.ID)
			}
		}
	case components.ColumnTypeSeasons:
		// Get show from parent column - needs libID for hierarchical cache
		if showCol := m.ColumnStack.Get(m.ColumnStack.Len() - 2); showCol != nil {
			if show := showCol.SelectedShow(); show != nil {
				return LoadSeasonsCmd(m.LibraryService, m.currentLibID, show.ID)
			}
		}
	case components.ColumnTypeEpisodes:
		// Get season from parent column - needs full ancestry for hierarchical cache
		if seasonCol := m.ColumnStack.Get(m.ColumnStack.Len() - 2); seasonCol != nil {
			if season := seasonCol.SelectedSeason(); season != nil {
				return LoadEpisodesCmd(m.LibraryService, m.currentLibID, m.currentShowID, season.ID)
			}
		}
	case components.ColumnTypeMixed:
		if libCol := m.libraryColumn(); libCol != nil {
			if lib := libCol.SelectedLibrary(); lib != nil {
				return LoadMixedLibraryCmd(m.LibraryService, lib.ID)
			}
		}
	}

	return LoadLibrariesCmd(m.LibraryService)
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
