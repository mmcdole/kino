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
		case key.Matches(msg, keys.Confirm):
			m.loggingOut = true
			return logoutCmd(m.session)
		case key.Matches(msg, keys.Deny):
			m.overlay = overlayNone
		}
		return nil
	case overlayConfirmDelete:
		playlist := m.confirmDelete
		switch {
		case key.Matches(msg, keys.Confirm):
			m.overlay, m.confirmDelete = overlayNone, nil
			return m.beginMutation(catalog.Mutation{Kind: catalog.MutationDeletePlaylist, PlaylistID: playlist.ID})
		case key.Matches(msg, keys.Deny), key.Matches(msg, keys.Escape):
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
	if top := m.columns.Top(); top != nil && top.IsFilterTyping() {
		top.Update(msg)
		return nil
	}

	// Global keys
	switch {
	case key.Matches(msg, keys.Quit):
		m.requests.cancel()
		return tea.Quit
	case key.Matches(msg, keys.Help):
		return m.handleHelp()
	case key.Matches(msg, keys.Escape):
		return m.handleEscape()
	case key.Matches(msg, keys.Filter):
		return m.handleFilter()
	case key.Matches(msg, keys.GlobalSearch):
		return m.handleGlobalSearch()
	case key.Matches(msg, keys.Sort):
		return m.handleSort()
	case key.Matches(msg, keys.Back):
		return m.handleBack()
	case key.Matches(msg, keys.Right):
		return m.handleDrillIn()
	case key.Matches(msg, keys.Enter):
		return m.handleEnter()
	case key.Matches(msg, keys.Refresh):
		return m.handleRefresh()
	case key.Matches(msg, keys.RefreshAll):
		return m.handleRefreshAll()
	case key.Matches(msg, keys.MarkWatched):
		return m.handleMarkWatched()
	case key.Matches(msg, keys.MarkUnwatched):
		return m.handleMarkUnwatched()
	case key.Matches(msg, keys.Play):
		return m.handlePlay()
	case key.Matches(msg, keys.ToggleInspector):
		return m.handleToggleInspector()
	case key.Matches(msg, keys.Logout):
		return m.handleLogout()
	case key.Matches(msg, keys.PlaylistModal):
		return m.handlePlaylistModal()
	case key.Matches(msg, keys.Delete):
		return m.handleDelete()
	case key.Matches(msg, keys.NewPlaylist):
		return m.handleNewPlaylist()
	}

	// Let the focused column handle remaining keys (j/k/g/G navigation)
	if top := m.columns.Top(); top != nil {
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
	if top := m.columns.Top(); top != nil && top.IsFiltering() {
		top.ClearFilter()
		return nil
	}
	if m.pendingSelect != nil {
		m.pendingSelect = nil
		return m.notify(noticeInfo, "Navigation cancelled")
	}
	// Esc dismisses a persistent alert once the user has read it
	if m.notice.Kind == noticeAlert && m.notice.Text != "" {
		m.clearNotice()
		return nil
	}
	return nil
}

// handleFilter toggles filter mode in the current column
func (m *Model) handleFilter() tea.Cmd {
	if top := m.columns.Top(); top != nil {
		top.ToggleFilter()
	}
	return nil
}

// handleGlobalSearch opens the global search modal
func (m *Model) handleGlobalSearch() tea.Cmd {
	m.globalSearch.Reset()
	m.globalSearch.SetSize(m.width, m.height)
	m.overlay = overlaySearch
	return m.globalSearch.Init()
}

// handleDrillIn handles drilling into the selected item (l key)
func (m *Model) handleDrillIn() tea.Cmd {
	// Manual navigation cancels any pending search selection; a stale
	// selection resuming on a later load would teleport the user
	m.pendingSelect = nil
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	if !top.CanDrillInto() {
		if item := top.SelectedMediaItem(); item != nil {
			return tea.Batch(
				m.notify(noticeInfo, "Launching: "+item.Title),
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
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	if top.CanDrillInto() {
		return m.drillSelected()
	}
	if item := top.SelectedMediaItem(); item != nil {
		return tea.Batch(
			m.notify(noticeInfo, "Launching: "+item.Title),
			m.beginPlayback(*item, item.ShouldResume()),
		)
	}
	return nil
}

// handleSort opens the sort modal for movies/shows columns
func (m *Model) handleSort() tea.Cmd {
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	opts := top.SortFields()
	if opts == nil {
		return m.notAvailableHere("Sort (s)")
	}
	field, dir := top.SortState()
	m.sortModal.Show(opts, field, dir)
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
		lib := m.columns.Top().SelectedLibrary()
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
	for i := 1; i < m.columns.Len(); i++ {
		r, _ := m.resource(m.columns.Get(i).ContentID())
		if r.Kind == catalog.Seasons || r.Kind == catalog.Episodes || r.Kind == catalog.PlaylistItems {
			cmds = append(cmds, m.loadResource(r, catalog.Refresh, false))
		}
	}
	return tea.Batch(cmds...)
}

// handleMarkWatched marks the selected item as watched
func (m *Model) handleMarkWatched() tea.Cmd {
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Mark watched (w)")
	}
	r, _ := m.topResource()
	return m.beginMutation(catalog.Mutation{Kind: catalog.MutationWatch, ItemID: item.ID, ShowID: item.ShowID, SeasonID: item.ParentID, Title: item.Title, LibraryID: r.LibraryID, Played: true})
}

// handleMarkUnwatched marks the selected item as unwatched
func (m *Model) handleMarkUnwatched() tea.Cmd {
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Mark unwatched (u)")
	}
	r, _ := m.topResource()
	return m.beginMutation(catalog.Mutation{Kind: catalog.MutationWatch, ItemID: item.ID, ShowID: item.ShowID, SeasonID: item.ParentID, Title: item.Title, LibraryID: r.LibraryID})
}

// handlePlay plays the selected item from the beginning
func (m *Model) handlePlay() tea.Cmd {
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Play (p)")
	}
	return tea.Batch(
		m.notify(noticeInfo, "Launching: "+item.Title),
		m.beginPlayback(*item, false),
	)
}

// notAvailableHere emits a short status explaining that a key does nothing
// for the current selection, instead of silently ignoring it
func (m *Model) notAvailableHere(action string) tea.Cmd {
	return m.notify(noticeInfo, action+" is not available for this item")
}

// handleToggleInspector toggles the inspector panel visibility
func (m *Model) handleToggleInspector() tea.Cmd {
	m.showInspector = !m.showInspector
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
	top := m.columns.Top()
	if top == nil {
		return nil
	}
	item := top.SelectedMediaItem()
	if item == nil {
		return m.notAvailableHere("Playlists (space)")
	}
	m.playlistModal.BeginLoading(item)
	m.playlistModal.SetSize(m.width, m.height)
	m.overlay = overlayPlaylists
	req := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	return playlistMembershipCmd(m.catalog, req, *item)
}

// handleDelete handles deletion of playlists or playlist items
func (m *Model) handleDelete() tea.Cmd {
	top := m.columns.Top()
	r, ok := m.topResource()
	if top == nil || !ok {
		return nil
	}
	switch r.Kind {
	case catalog.PlaylistItems:
		if item := top.SelectedMediaItem(); item != nil {
			return m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: r.ID, ItemID: item.ID})
		}
	case catalog.Playlists:
		// Deleting a playlist is irreversible and server-side: confirm first
		if playlist := top.SelectedPlaylist(); playlist != nil {
			m.overlay, m.confirmDelete = overlayConfirmDelete, playlist
			return nil
		}
	default:
		return m.notify(noticeInfo, "Remove (x) only works in playlists")
	}
	return nil
}

// handleNewPlaylist opens the new-playlist name input (playlists column only)
func (m *Model) handleNewPlaylist() tea.Cmd {
	if r, ok := m.topResource(); !ok || r.Kind != catalog.Playlists {
		return nil
	}
	m.inputModal.Show("New Playlist")
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
	m.globalSearch, cmd, outcome = m.globalSearch.Update(msg)
	cmds := []tea.Cmd{cmd}
	switch outcome {
	case components.Cancel:
		m.closeSearch()
	case components.Submit:
		result := m.globalSearch.Selected()
		m.closeSearch()
		cmds = append(cmds, m.navigateToSearchResult(*result))
	default:
		if m.globalSearch.QueryChanged() {
			cmds = append(cmds, m.scheduleSearch())
		}
	}
	return tea.Batch(cmds...)
}

// handleSortModalInput handles input while the sort modal is open
func (m *Model) handleSortModalInput(msg tea.KeyMsg) tea.Cmd {
	outcome, selection := m.sortModal.HandleKeyMsg(msg)
	if outcome == components.Submit {
		if top := m.columns.Top(); top != nil {
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
	if m.playlistModal.IsLoading() {
		if msg.String() == "esc" {
			m.cancelPendingModal()
		}
		return nil
	}
	outcome, create := m.playlistModal.HandleKeyMsg(msg)
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
	title := m.playlistModal.NewPlaylistTitle()
	item := m.playlistModal.Item()
	changes := m.playlistModal.GetChanges()

	if title == "" || item == nil {
		return nil
	}

	cmds := []tea.Cmd{m.beginMutation(catalog.Mutation{Kind: catalog.MutationCreatePlaylist, Title: title, ItemID: item.ID})}
	for _, change := range changes {
		if change.Add {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.MutationAddToPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		} else {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		}
	}
	return tea.Batch(cmds...)
}

// applyPlaylistChanges applies pending playlist checkbox changes
func (m *Model) applyPlaylistChanges() tea.Cmd {
	changes := m.playlistModal.GetChanges()
	item := m.playlistModal.Item()

	if len(changes) == 0 || item == nil {
		return nil
	}

	var cmds []tea.Cmd
	for _, change := range changes {
		if change.Add {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.MutationAddToPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		} else {
			cmds = append(cmds, m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: change.PlaylistID, ItemID: item.ID}))
		}
	}
	return tea.Batch(cmds...)
}

// handleInputModalInput handles input while the new-playlist input is open
func (m *Model) handleInputModalInput(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	var outcome components.Outcome
	m.inputModal, cmd, outcome = m.inputModal.Update(msg)
	switch outcome {
	case components.Submit:
		m.overlay = overlayNone
		if title := m.inputModal.Value(); title != "" {
			return m.beginMutation(catalog.Mutation{Kind: catalog.MutationCreatePlaylist, Title: title})
		}
		return nil
	case components.Cancel:
		m.overlay = overlayNone
		return nil
	}
	return cmd
}
