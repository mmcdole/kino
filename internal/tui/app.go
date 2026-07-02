package tui

import (
	"errors"
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

// authFailedStatusMsg tells the user how to recover from a revoked/expired
// token. Shown persistently (not auto-cleared) since action is required.
const authFailedStatusMsg = "Session expired or revoked — press L to log out, then run kino to sign in again"

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateBrowsing ApplicationState = iota
	StateHelp
	StateConfirmLogout
	StateConfirmDeletePlaylist
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
	SyncGen       int                                    // Current sync generation; messages from older generations are dropped

	// Navigation plan for deep linking
	navPlan *NavPlan

	// Playlist navigation context (when viewing playlist items)
	currentPlaylistID string

	// Pending playlist deletion awaiting confirmation
	pendingDeletePlaylistID   string
	pendingDeletePlaylistName string

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

		// New sync generation: any still-running chains from before this
		// reload are stale and their messages will be dropped
		m.SyncGen++

		// Initialize all states to Syncing (including playlists)
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range msg.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.LibraryStates[playlistsLibraryID] = components.LibrarySyncState{Status: components.StatusSyncing}
		m.SyncingCount = len(msg.Libraries) + 1 // +1 for playlists
		m.MultiLibSync = true
		m.Inspector.SetLibraryStates(m.LibraryStates)
		m.Loading = true

		syncCmds := []tea.Cmd{
			SyncAllLibrariesCmd(m.LibraryService, msg.Libraries, m.SyncGen),
			SyncPlaylistsCmd(m.PlaylistService, playlistsLibraryID, m.SyncGen),
		}

		// Refresh-all with the user somewhere deeper: keep their position.
		// Update the root column in place and reload the top column's
		// content in the background instead of resetting to the root.
		if msg.Refresh && m.ColumnStack.Len() > 1 {
			libCol := m.libraryColumn()
			var drilledID string
			if libCol != nil {
				if sel := libCol.SelectedLibrary(); sel != nil {
					drilledID = sel.ID
				}
				libCol.ReplaceItems(m.allLibraryEntries())
				libCol.SetLibraryStates(m.LibraryStates)
			}

			// The library the user is inside may have been removed
			// server-side — that's the one case where resetting is the
			// only sane answer
			if drilledID != "" && drilledID != playlistsLibraryID && m.findLibrary(drilledID) == nil {
				m.StatusMsg = "Library no longer exists on server"
				m.StatusIsErr = true
				syncCmds = append(syncCmds, ClearStatusCmd(5*time.Second))
			} else {
				if reload := m.reloadTopColumnCmd(); reload != nil {
					syncCmds = append(syncCmds, reload)
				}
				return m, tea.Batch(syncCmds...)
			}
		}

		// Initial load (or unrecoverable refresh): build the root column
		libCol := components.NewLibraryColumn(m.allLibraryEntries())
		libCol.SetLibraryStates(m.LibraryStates)
		libCol.SetShowWatchStatus(m.UIConfig.ShowWatchStatus)
		libCol.SetShowLibraryCounts(m.UIConfig.ShowLibraryCounts)
		m.ColumnStack.Reset(libCol)

		return m, tea.Batch(syncCmds...)

	case MoviesLoadedMsg:
		m.Loading = false

		// If manual load succeeded and library was in error state, clear it
		if state, ok := m.LibraryStates[msg.LibraryID]; ok && state.Status == components.StatusError {
			state.Status = components.StatusIdle
			state.Error = nil
			m.LibraryStates[msg.LibraryID] = state
			m.updateLibraryStates()
		}

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.LibraryID) {
			return m, nil
		}

		// Update top column with movies
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Movies)
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

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.LibraryID) {
			return m, nil
		}

		// Update top column with shows
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Shows)
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

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.LibraryID) {
			return m, nil
		}

		// Update top column with mixed content
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Items)
		}

		m.updateInspector()

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitMixed, msg.LibraryID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case SeasonsLoadedMsg:
		m.Loading = false

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.ShowID) {
			return m, nil
		}

		// Update top column with seasons
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Seasons)
		}

		m.updateInspector()

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitSeasons, msg.ShowID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case EpisodesLoadedMsg:
		m.Loading = false

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.SeasonID) {
			return m, nil
		}

		// Update top column with episodes
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Episodes)
		}

		m.updateInspector()

		// Advance nav plan if waiting for this load
		if cmd := m.advanceNavPlanAfterLoad(AwaitEpisodes, msg.SeasonID); cmd != nil {
			return m, cmd
		}
		return m, nil

	case PlaybackStartedMsg:
		m.StatusMsg = "Launched: " + msg.Item.Title
		return m, ClearStatusCmd(3 * time.Second)

	case MarkWatchedMsg:
		m.StatusMsg = "Marked watched: " + msg.Title
		m.applyWatchState(msg.ItemID, true)
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case MarkUnwatchedMsg:
		m.StatusMsg = "Marked unwatched: " + msg.Title
		m.applyWatchState(msg.ItemID, false)
		cmds = append(cmds, ClearStatusCmd(3*time.Second))
		return m, tea.Batch(cmds...)

	case ErrMsg:
		m.clearNavPlan()
		m.StatusIsErr = true
		m.Loading = false
		// A failed refresh must not leave the column spinner running, and a
		// failed initial load must show a retry hint, not spin forever
		if top := m.ColumnStack.Top(); top != nil {
			top.SetRefreshing(false)
			if top.IsLoading() {
				top.SetLoadFailed()
			}
		}
		if errors.Is(msg.Err, domain.ErrAuthFailed) {
			// Actionable, persistent message: the token was revoked/expired
			// and the user must re-authenticate
			m.StatusMsg = authFailedStatusMsg
			return m, nil
		}
		m.StatusMsg = msg.Error()
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
		// Drop messages from sync chains superseded by a newer library
		// reload; without this, stale chains corrupt SyncingCount and can
		// wedge the loading state permanently
		if msg.Generation != m.SyncGen {
			return m, nil
		}

		state := m.LibraryStates[msg.LibraryID]

		if msg.Error != nil {
			state.Status = components.StatusError
			state.Error = msg.Error
			m.SyncingCount--
			slog.Error("library sync failed", "libraryID", msg.LibraryID, "error", msg.Error)
			if errors.Is(msg.Error, domain.ErrAuthFailed) {
				m.StatusMsg = authFailedStatusMsg
				m.StatusIsErr = true
			}
		} else {
			state.Loaded = msg.Loaded
			state.Total = msg.Total
			state.FromCache = msg.FromCache

			if msg.Done {
				state.Status = components.StatusSynced
				m.SyncingCount--

				// Trigger delayed cleanup
				cmds = append(cmds, ClearLibraryStatusCmd(msg.LibraryID, 2*time.Second))
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

		// Validate content ID like every other load handler: a slow playlist
		// fetch must not clobber whatever column the user navigated to since
		if !m.validateContentID(playlistsLibraryID) {
			return m, nil
		}

		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Playlists)
		}
		m.updateInspector()
		return m, nil

	case PlaylistItemsLoadedMsg:
		m.Loading = false

		// Validate content ID to prevent race condition
		if !m.validateContentID(msg.PlaylistID) {
			return m, nil
		}

		m.currentPlaylistID = msg.PlaylistID
		if top := m.ColumnStack.Top(); top != nil {
			top.ReplaceItems(msg.Items)
		}
		m.updateInspector()
		return m, nil

	case PlaylistModalDataMsg:
		m.StatusMsg = "" // clear the "Loading playlists..." pending status
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

// validateContentID checks if the top column has the expected content ID.
// Returns false if the column doesn't match (user navigated away before async load completed).
func (m *Model) validateContentID(expectedID string) bool {
	top := m.ColumnStack.Top()
	return top != nil && top.ContentID() == expectedID
}

// updateLibraryStates updates the library states in the library column and inspector
func (m *Model) updateLibraryStates() {
	if libCol := m.libraryColumn(); libCol != nil {
		libCol.SetLibraryStates(m.LibraryStates)
	}
	m.Inspector.SetLibraryStates(m.LibraryStates)
}

// reloadTopColumnCmd returns a command reloading the top column's content
// from the server, landing via ReplaceItems so the cursor and view state
// survive. Used by refresh-all to freshen the visible view without
// resetting navigation. Returns nil at the root (updated in place).
func (m *Model) reloadTopColumnCmd() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}

	lib := m.findLibrary(m.currentLibID)

	switch top.ColumnType() {
	case components.ColumnTypeMovies:
		if lib != nil {
			top.SetRefreshing(true)
			return LoadMoviesCmd(m.LibraryService, *lib)
		}
	case components.ColumnTypeShows:
		if lib != nil {
			top.SetRefreshing(true)
			return LoadShowsCmd(m.LibraryService, *lib)
		}
	case components.ColumnTypeMixed:
		if lib != nil {
			top.SetRefreshing(true)
			return LoadMixedLibraryCmd(m.LibraryService, *lib)
		}
	case components.ColumnTypeSeasons:
		if m.currentShowID != "" {
			top.SetRefreshing(true)
			return LoadSeasonsCmd(m.LibraryService, m.currentLibID, m.currentShowID)
		}
	case components.ColumnTypeEpisodes:
		if seasonCol := m.ColumnStack.Get(m.ColumnStack.Len() - 2); seasonCol != nil {
			if season := seasonCol.SelectedSeason(); season != nil {
				top.SetRefreshing(true)
				return LoadEpisodesCmd(m.LibraryService, m.currentLibID, m.currentShowID, season.ID)
			}
		}
	case components.ColumnTypePlaylists:
		top.SetRefreshing(true)
		return LoadPlaylistsCmd(m.PlaylistService)
	case components.ColumnTypePlaylistItems:
		if m.currentPlaylistID != "" {
			top.SetRefreshing(true)
			return LoadPlaylistItemsCmd(m.PlaylistService, m.currentPlaylistID)
		}
	}
	return nil
}

// applyWatchState patches an item's watch state in the cache and in every
// visible column. This replaces the old invalidate-everything-and-refetch
// approach: the UI updates instantly and no network requests are issued.
func (m *Model) applyWatchState(itemID string, played bool) {
	m.LibraryService.SetWatchState(itemID, played)

	// Patch the item wherever a column renders it, and adjust unwatched
	// counters on visible show/season rows if an episode flipped state.
	var patched *domain.MediaItem
	flipped := false
	for i := 0; i < m.ColumnStack.Len(); i++ {
		if col := m.ColumnStack.Get(i); col != nil {
			if item, f := col.ApplyWatchState(itemID, played); item != nil {
				patched = item
				flipped = flipped || f
			}
		}
	}

	if flipped && patched != nil && patched.ShowID != "" {
		delta := 1
		if played {
			delta = -1
		}
		for i := 0; i < m.ColumnStack.Len(); i++ {
			if col := m.ColumnStack.Get(i); col != nil {
				col.AdjustUnwatchedCounts(patched.ShowID, patched.ParentID, delta)
			}
		}
	}

	m.updateInspector()
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
