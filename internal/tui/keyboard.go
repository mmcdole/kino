package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/tui/components"
)

// handleKeyMsg handles keyboard input
func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd

	// Ctrl+C always quits, even inside modals and text inputs
	if msg.String() == "ctrl+c" {
		m.requests.cancel()
		return tea.Quit
	}

	// Handle state-specific keys
	switch m.State {
	case StateHelp:
		// Any key returns to browsing, as the help screen promises
		m.State = StateBrowsing
		return nil

	case StateConfirmLogout:
		switch {
		case key.Matches(msg, Keys.Confirm):
			// User confirmed logout
			m.loggingOut = true
			return LogoutCmd()
		case key.Matches(msg, Keys.Deny):
			// User cancelled
			m.State = StateBrowsing
		}
		return nil

	case StateConfirmDeletePlaylist:
		switch {
		case key.Matches(msg, Keys.Confirm):
			m.State = StateBrowsing
			if m.pendingDeletePlaylistID != "" {
				id := m.pendingDeletePlaylistID
				m.pendingDeletePlaylistID = ""
				m.pendingDeletePlaylistName = ""
				return m.beginMutation(catalog.Mutation{Kind: catalog.DeletePlaylist, PlaylistID: id})
			}
		case key.Matches(msg, Keys.Deny), key.Matches(msg, Keys.Escape):
			m.State = StateBrowsing
			m.pendingDeletePlaylistID = ""
			m.pendingDeletePlaylistName = ""
		}
		return nil
	}

	// Route to active modal if any
	if handled, cmd := m.routeToModal(msg); handled {
		return cmd
	}

	// Global keys
	switch {
	case key.Matches(msg, Keys.Quit):
		m.requests.cancel()
		return tea.Quit
	case key.Matches(msg, Keys.Help):
		return m.handleHelp()
	case key.Matches(msg, Keys.Escape):
		return m.handleEscape()
	case key.Matches(msg, Keys.Filter):
		return m.handleFilter()
	case key.Matches(msg, Keys.GlobalSearch):
		return m.handleGlobalSearch()
	case key.Matches(msg, Keys.Sort):
		return m.handleSort()
	case key.Matches(msg, Keys.Back):
		return m.handleBack()
	case key.Matches(msg, Keys.Right):
		return m.handleDrillIn()
	case key.Matches(msg, Keys.Enter):
		return m.handleEnter()
	case key.Matches(msg, Keys.Refresh):
		return m.handleRefresh()
	case key.Matches(msg, Keys.RefreshAll):
		return m.handleRefreshAll()
	case key.Matches(msg, Keys.MarkWatched):
		return m.handleMarkWatched()
	case key.Matches(msg, Keys.MarkUnwatched):
		return m.handleMarkUnwatched()
	case key.Matches(msg, Keys.Play):
		return m.handlePlay()
	case key.Matches(msg, Keys.ToggleInspector):
		return m.handleToggleInspector()
	case key.Matches(msg, Keys.Logout):
		return m.handleLogout()
	case key.Matches(msg, Keys.PlaylistModal):
		return m.handlePlaylistModal()
	case key.Matches(msg, Keys.Delete):
		return m.handleDelete()
	case key.Matches(msg, Keys.NewPlaylist):
		return m.handleNewPlaylist()
	}

	// Let the focused column handle remaining keys (j/k/g/G navigation)
	if top := m.ColumnStack.Top(); top != nil {
		if _, cmd := top.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// routeToModal routes key input to active modals
// Returns (handled, cmd) where handled is true if a modal consumed the input
func (m *Model) routeToModal(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.GlobalSearch.IsVisible() {
		return true, m.handleGlobalSearchInput(msg)
	}
	if m.SortModal.IsVisible() {
		return m.handleSortModalInput(msg)
	}
	if m.PlaylistModal.IsVisible() {
		return m.handlePlaylistModalInput(msg)
	}
	if m.InputModal.IsVisible() {
		return m.handleInputModalInput(msg)
	}
	if top := m.ColumnStack.Top(); top != nil && top.IsFilterTyping() {
		return m.handleFilterTypingInput(msg)
	}
	return false, nil
}

// ----------------------------------------------------------------------------
// Global key handlers
// ----------------------------------------------------------------------------

// handleHelp shows the help screen
func (m *Model) handleHelp() tea.Cmd {
	m.State = StateHelp
	return nil
}

// handleEscape clears active filter or cancels nav plan
func (m *Model) handleEscape() tea.Cmd {
	if top := m.ColumnStack.Top(); top != nil && top.IsFiltering() {
		top.ClearFilter()
		return nil
	}
	if m.navPlan != nil {
		m.clearNavPlan()
		return m.notify(NoticeInfo, "Navigation cancelled")
	}
	// Esc dismisses a persistent alert once the user has read it
	if m.notice.Kind == NoticeAlert && m.notice.Text != "" {
		m.clearNotice()
		return nil
	}
	return nil
}

// handleFilter toggles filter mode in the current column
func (m *Model) handleFilter() tea.Cmd {
	if top := m.ColumnStack.Top(); top != nil {
		top.ToggleFilter()
	}
	return nil
}

// handleGlobalSearch opens the global search modal
func (m *Model) handleGlobalSearch() tea.Cmd {
	m.GlobalSearch.Show()
	m.GlobalSearch.SetSize(m.Width, m.Height)
	return m.GlobalSearch.Init()
}

// handleDrillIn handles drilling into the selected item (l key)
func (m *Model) handleDrillIn() tea.Cmd {
	// Manual navigation cancels any pending search-navigation plan; a stale
	// plan resuming on a later load would teleport the user
	m.clearNavPlan()
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	if !top.CanDrillInto() {
		if item := top.SelectedMediaItem(); item != nil {
			return tea.Batch(
				m.notify(NoticeInfo, "Launching: "+item.Title),
				m.beginPlayback(*item, item.ShouldResume()),
			)
		}
		return nil
	}
	return m.drillSelected()
}

// handleEnter handles the enter key press
func (m *Model) handleEnter() tea.Cmd {
	m.clearNavPlan()
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	if top.CanDrillInto() {
		return m.drillSelected()
	}
	if item := top.SelectedMediaItem(); item != nil {
		return tea.Batch(
			m.notify(NoticeInfo, "Launching: "+item.Title),
			m.beginPlayback(*item, item.ShouldResume()),
		)
	}
	return nil
}

// handleSort opens the sort modal for movies/shows columns
func (m *Model) handleSort() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	var opts []components.SortField
	switch top.ColumnType() {
	case components.ColumnTypeMovies:
		opts = components.MovieSortOptions()
	case components.ColumnTypeShows:
		opts = components.ShowSortOptions()
	case components.ColumnTypeEpisodes:
		opts = components.EpisodeSortOptions()
	case components.ColumnTypeMixed:
		opts = components.MixedSortOptions()
	}
	if opts == nil {
		return m.notAvailableHere("Sort (s)")
	}
	field, dir := top.SortState()
	m.SortModal.Show(opts, field, dir)
	return nil
}

// Refresh uses the same resource path as browsing, preserving visible data.
func (m *Model) handleRefresh() tea.Cmd {
	r, ok := m.topResource()
	if !ok {
		return nil
	}
	if r.Kind == catalog.Libraries {
		lib := m.ColumnStack.Top().SelectedLibrary()
		if lib == nil {
			return m.loadResource(r, catalog.Refresh, false)
		}
		if lib.ID == playlistsLibraryID {
			r = catalog.Resource{Kind: catalog.Playlists}
		} else {
			r = catalog.LibraryResource(*lib)
		}
		return m.loadResource(r, catalog.Refresh, true)
	}
	return m.loadResource(r, catalog.Refresh, false)
}

func (m *Model) handleRefreshAll() tea.Cmd {
	m.clearNavPlan()
	cmds := []tea.Cmd{
		m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Refresh, false),
		m.syncLibraries(catalog.Refresh),
	}
	for i := 1; i < m.ColumnStack.Len(); i++ {
		r, _ := m.resource(m.ColumnStack.Get(i).ContentID())
		if r.Kind == catalog.Seasons || r.Kind == catalog.Episodes || r.Kind == catalog.PlaylistItems {
			cmds = append(cmds, m.loadResource(r, catalog.Refresh, false))
		}
	}
	return tea.Batch(cmds...)
}

// handleMarkWatched marks the selected item as watched
func (m *Model) handleMarkWatched() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Mark watched (w)")
	}
	r, _ := m.topResource()
	return m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: item.ID, ShowID: item.ShowID, SeasonID: item.ParentID, Title: item.Title, LibraryID: r.LibraryID, Played: true})
}

// handleMarkUnwatched marks the selected item as unwatched
func (m *Model) handleMarkUnwatched() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Mark unwatched (u)")
	}
	r, _ := m.topResource()
	return m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: item.ID, ShowID: item.ShowID, SeasonID: item.ParentID, Title: item.Title, LibraryID: r.LibraryID})
}

// handlePlay plays the selected item from the beginning
func (m *Model) handlePlay() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Play (p)")
	}
	return tea.Batch(
		m.notify(NoticeInfo, "Launching: "+item.Title),
		m.beginPlayback(*item, false),
	)
}

// notAvailableHere emits a short status explaining that a key does nothing
// for the current selection, instead of silently ignoring it
func (m *Model) notAvailableHere(action string) tea.Cmd {
	return m.notify(NoticeInfo, action+" is not available for this item")
}

// handleToggleInspector toggles the inspector panel visibility
func (m *Model) handleToggleInspector() tea.Cmd {
	m.ShowInspector = !m.ShowInspector
	m.updateLayout()
	return nil
}

// handleLogout shows the logout confirmation
func (m *Model) handleLogout() tea.Cmd {
	m.State = StateConfirmLogout
	return nil
}

// handlePlaylistModal opens the playlist modal for the selected item
func (m *Model) handlePlaylistModal() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Playlists (space)")
	}
	m.PlaylistModal.BeginLoading(item)
	m.PlaylistModal.SetSize(m.Width, m.Height)
	req := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	return LoadPlaylistModalDataCmd(m.Catalog, req, *item)
}

// handleDelete handles deletion of playlists or playlist items
func (m *Model) handleDelete() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil {
		return nil
	}
	switch top.ColumnType() {
	case components.ColumnTypePlaylistItems:
		item := top.SelectedMediaItem()
		if r, ok := m.topResource(); item != nil && ok {
			return m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: r.ID, ItemID: item.ID})
		}
	case components.ColumnTypePlaylists:
		// Deleting a playlist is irreversible and server-side: confirm first
		if playlist := top.SelectedPlaylist(); playlist != nil {
			m.State = StateConfirmDeletePlaylist
			m.pendingDeletePlaylistID = playlist.ID
			m.pendingDeletePlaylistName = playlist.Title
			return nil
		}
	default:
		return m.notify(NoticeInfo, "Remove (x) only works in playlists")
	}
	return nil
}

// handleNewPlaylist opens the new-playlist name input (playlists column only)
func (m *Model) handleNewPlaylist() tea.Cmd {
	top := m.ColumnStack.Top()
	if top == nil || top.ColumnType() != components.ColumnTypePlaylists {
		return nil
	}
	m.InputModal.Show("New Playlist")
	return nil
}

// ----------------------------------------------------------------------------
// Modal input handlers
// ----------------------------------------------------------------------------

// handleGlobalSearchInput handles input when global search is visible
func (m *Model) handleGlobalSearchInput(msg tea.KeyMsg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	var selected bool

	m.GlobalSearch, cmd, selected = m.GlobalSearch.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	if m.GlobalSearch.QueryChanged() {
		cmds = append(cmds, m.scheduleSearch())
	}

	if !m.GlobalSearch.IsVisible() {
		m.requests.stop("search")
		m.searchSeq++
	}
	if selected {
		if result := m.GlobalSearch.Selected(); result != nil {
			m.GlobalSearch.Hide()
			m.requests.stop("search")
			m.searchSeq++
			if navCmd := m.navigateToSearchResult(*result); navCmd != nil {
				cmds = append(cmds, navCmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

// handleSortModalInput handles input when sort modal is visible
func (m *Model) handleSortModalInput(msg tea.KeyMsg) (bool, tea.Cmd) {
	handled, selection := m.SortModal.HandleKeyMsg(msg)
	if handled {
		if selection != nil {
			if top := m.ColumnStack.Top(); top != nil {
				top.ApplySort(selection.Field, selection.Direction)
			}
		}
		return true, nil
	}
	return true, nil
}

// handlePlaylistModalInput handles input when playlist modal is visible
func (m *Model) handlePlaylistModalInput(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.PlaylistModal.IsLoading() {
		if msg.String() == "esc" {
			m.cancelPendingModal()
		}
		return true, nil
	}

	handled, shouldClose, shouldCreate := m.PlaylistModal.HandleKeyMsg(msg)
	if !handled {
		return false, nil
	}

	if shouldCreate {
		return true, m.applyPlaylistCreate()
	}
	if shouldClose {
		return true, m.applyPlaylistChanges()
	}
	return true, nil
}

// applyPlaylistCreate creates a new playlist and applies checkbox changes
func (m *Model) applyPlaylistCreate() tea.Cmd {
	title := m.PlaylistModal.NewPlaylistTitle()
	item := m.PlaylistModal.Item()
	changes := m.PlaylistModal.GetChanges()
	m.PlaylistModal.Hide()

	if title == "" || item == nil {
		return nil
	}

	cmds := []tea.Cmd{m.beginMutation(catalog.Mutation{Kind: catalog.CreatePlaylist, Title: title, ItemIDs: []string{item.ID}})}
	for _, change := range changes {
		if change.Add {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.AddToPlaylist, PlaylistID: change.PlaylistID, ItemIDs: []string{item.ID}}))
		} else {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		}
	}
	return tea.Batch(cmds...)
}

// applyPlaylistChanges applies pending playlist checkbox changes
func (m *Model) applyPlaylistChanges() tea.Cmd {
	changes := m.PlaylistModal.GetChanges()
	item := m.PlaylistModal.Item()
	m.PlaylistModal.Hide()

	if len(changes) == 0 || item == nil {
		return nil
	}

	var cmds []tea.Cmd
	for _, change := range changes {
		if change.Add {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.AddToPlaylist, PlaylistID: change.PlaylistID, ItemIDs: []string{item.ID}}))
		} else {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		}
	}
	return tea.Batch(cmds...)
}

// handleInputModalInput handles input when input modal is visible
func (m *Model) handleInputModalInput(msg tea.KeyMsg) (bool, tea.Cmd) {
	var cmd tea.Cmd
	var submitted bool

	m.InputModal, cmd, submitted = m.InputModal.Update(msg)
	if submitted {
		title := m.InputModal.Value()
		m.InputModal.Hide()
		if title != "" {
			return true, m.beginMutation(catalog.Mutation{Kind: catalog.CreatePlaylist, Title: title})
		}
		return true, nil
	}
	if cmd != nil {
		return true, cmd
	}
	return true, nil
}

// handleFilterTypingInput handles input when filter typing mode is active
func (m *Model) handleFilterTypingInput(msg tea.KeyMsg) (bool, tea.Cmd) {
	top := m.ColumnStack.Top()
	if top == nil {
		return false, nil
	}
	top.Update(msg)
	return true, nil
}
