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

	switch m.overlay {
	case overlayHelp:
		// Any key returns to browsing, as the help screen promises
		m.overlay = overlayNone
		return nil
	case overlayConfirmLogout:
		switch {
		case key.Matches(msg, Keys.Confirm):
			m.loggingOut = true
			return logoutCmd(m.Session)
		case key.Matches(msg, Keys.Deny):
			m.overlay = overlayNone
		}
		return nil
	case overlayConfirmDelete:
		playlist := m.confirmDelete
		switch {
		case key.Matches(msg, Keys.Confirm):
			m.overlay, m.confirmDelete = overlayNone, nil
			return m.beginMutation(catalog.Mutation{Kind: catalog.DeletePlaylist, PlaylistID: playlist.ID})
		case key.Matches(msg, Keys.Deny), key.Matches(msg, Keys.Escape):
			m.overlay, m.confirmDelete = overlayNone, nil
		}
		return nil
	case overlaySearch:
		return m.handleGlobalSearchInput(msg)
	case overlaySort:
		return m.handleSortModalInput(msg)
	case overlayPlaylists:
		return m.handlePlaylistModalInput(msg)
	case overlayInput:
		return m.handleInputModalInput(msg)
	}
	if top := m.ColumnStack.Top(); top != nil && top.IsFilterTyping() {
		top.Update(msg)
		return nil
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

// ----------------------------------------------------------------------------
// Global key handlers
// ----------------------------------------------------------------------------

// handleHelp shows the help screen
func (m *Model) handleHelp() tea.Cmd {
	m.overlay = overlayHelp
	return nil
}

// handleEscape clears active filter or cancels a pending search selection
func (m *Model) handleEscape() tea.Cmd {
	if top := m.ColumnStack.Top(); top != nil && top.IsFiltering() {
		top.ClearFilter()
		return nil
	}
	if m.pendingSelect != nil {
		m.pendingSelect = nil
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
	m.GlobalSearch.Reset()
	m.GlobalSearch.SetSize(m.Width, m.Height)
	m.overlay = overlaySearch
	return m.GlobalSearch.Init()
}

// handleDrillIn handles drilling into the selected item (l key)
func (m *Model) handleDrillIn() tea.Cmd {
	// Manual navigation cancels any pending search selection; a stale
	// selection resuming on a later load would teleport the user
	m.pendingSelect = nil
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
	m.pendingSelect = nil
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
	opts := top.SortFields()
	if opts == nil {
		return m.notAvailableHere("Sort (s)")
	}
	field, dir := top.SortState()
	m.SortModal.Show(opts, field, dir)
	m.overlay = overlaySort
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
	m.pendingSelect = nil
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
	m.overlay = overlayConfirmLogout
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
	m.overlay = overlayPlaylists
	req := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	return playlistMembershipCmd(m.Catalog, req, *item)
}

// handleDelete handles deletion of playlists or playlist items
func (m *Model) handleDelete() tea.Cmd {
	top := m.ColumnStack.Top()
	r, ok := m.topResource()
	if top == nil || !ok {
		return nil
	}
	switch r.Kind {
	case catalog.PlaylistItems:
		if item := top.SelectedMediaItem(); item != nil {
			return m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: r.ID, ItemID: item.ID})
		}
	case catalog.Playlists:
		// Deleting a playlist is irreversible and server-side: confirm first
		if playlist := top.SelectedPlaylist(); playlist != nil {
			m.overlay, m.confirmDelete = overlayConfirmDelete, playlist
			return nil
		}
	default:
		return m.notify(NoticeInfo, "Remove (x) only works in playlists")
	}
	return nil
}

// handleNewPlaylist opens the new-playlist name input (playlists column only)
func (m *Model) handleNewPlaylist() tea.Cmd {
	if r, ok := m.topResource(); !ok || r.Kind != catalog.Playlists {
		return nil
	}
	m.InputModal.Show("New Playlist")
	m.overlay = overlayInput
	return nil
}

// ----------------------------------------------------------------------------
// Modal input handlers
// ----------------------------------------------------------------------------

// handleGlobalSearchInput handles input while global search is open
func (m *Model) handleGlobalSearchInput(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	var outcome components.Outcome
	m.GlobalSearch, cmd, outcome = m.GlobalSearch.Update(msg)
	cmds := []tea.Cmd{cmd}
	switch outcome {
	case components.Cancel:
		m.closeSearch()
	case components.Submit:
		result := m.GlobalSearch.Selected()
		m.closeSearch()
		cmds = append(cmds, m.navigateToSearchResult(*result))
	default:
		if m.GlobalSearch.QueryChanged() {
			cmds = append(cmds, m.scheduleSearch())
		}
	}
	return tea.Batch(cmds...)
}

// closeSearch closes global search and abandons any pending query.
func (m *Model) closeSearch() {
	m.overlay = overlayNone
	m.requests.stop("search")
	m.searchSeq++
}

// handleSortModalInput handles input while the sort modal is open
func (m *Model) handleSortModalInput(msg tea.KeyMsg) tea.Cmd {
	outcome, selection := m.SortModal.HandleKeyMsg(msg)
	if outcome == components.Submit {
		if top := m.ColumnStack.Top(); top != nil {
			top.ApplySort(selection.Field, selection.Direction)
		}
	}
	if outcome != components.Continue {
		m.overlay = overlayNone
	}
	return nil
}

// handlePlaylistModalInput handles input while the playlist modal is open.
// Closing the modal applies its checkbox changes.
func (m *Model) handlePlaylistModalInput(msg tea.KeyMsg) tea.Cmd {
	if m.PlaylistModal.IsLoading() {
		if msg.String() == "esc" {
			m.cancelPendingModal()
		}
		return nil
	}
	outcome, create := m.PlaylistModal.HandleKeyMsg(msg)
	if outcome != components.Submit {
		return nil
	}
	m.overlay = overlayNone
	if create {
		return m.applyPlaylistCreate()
	}
	return m.applyPlaylistChanges()
}

// applyPlaylistCreate creates a new playlist and applies checkbox changes
func (m *Model) applyPlaylistCreate() tea.Cmd {
	title := m.PlaylistModal.NewPlaylistTitle()
	item := m.PlaylistModal.Item()
	changes := m.PlaylistModal.GetChanges()

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

// handleInputModalInput handles input while the new-playlist input is open
func (m *Model) handleInputModalInput(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	var outcome components.Outcome
	m.InputModal, cmd, outcome = m.InputModal.Update(msg)
	switch outcome {
	case components.Submit:
		m.overlay = overlayNone
		if title := m.InputModal.Value(); title != "" {
			return m.beginMutation(catalog.Mutation{Kind: catalog.CreatePlaylist, Title: title})
		}
		return nil
	case components.Cancel:
		m.overlay = overlayNone
		return nil
	}
	return cmd
}
