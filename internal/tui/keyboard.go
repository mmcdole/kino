package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/tui/components"
)

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle state-specific keys
	switch m.State {
	case StateHelp:
		if key.Matches(msg, Keys.Escape, Keys.Help, Keys.Quit) {
			m.State = StateBrowsing
		}
		return m, nil

	case StateConfirmLogout:
		switch {
		case key.Matches(msg, Keys.Confirm):
			// User confirmed logout
			return m, LogoutCmd(m.SessionSvc)
		case key.Matches(msg, Keys.Deny):
			// User cancelled
			m.State = StateBrowsing
		}
		return m, nil
	}

	// Route to active modal if any
	if handled, newModel, cmd := m.routeToModal(msg); handled {
		return newModel, cmd
	}

	// Global keys
	switch {
	case key.Matches(msg, Keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, Keys.Help):
		m.State = StateHelp
		return m, nil

	case key.Matches(msg, Keys.Escape):
		// Clear active filter if any
		if top := m.ColumnStack.Top(); top != nil && top.IsFiltering() {
			top.ClearFilter()
			return m, nil
		}
		// Cancel active nav plan if any
		if m.navPlan != nil {
			m.clearNavPlan()
			m.StatusMsg = "Navigation cancelled"
			return m, ClearStatusCmd(2 * time.Second)
		}
		return m, nil

	case key.Matches(msg, Keys.Filter):
		// Activate filter in middle column
		if top := m.ColumnStack.Top(); top != nil {
			top.ToggleFilter()
		}
		return m, nil

	case key.Matches(msg, Keys.GlobalSearch):
		// Global search via Omnibar
		m.GlobalSearch.Show()
		m.GlobalSearch.SetSize(m.Width, m.Height)
		return m, m.GlobalSearch.Init()

	case key.Matches(msg, Keys.Sort):
		// Sort modal (only for movies/shows columns)
		if top := m.ColumnStack.Top(); top != nil {
			var opts []components.SortField
			switch top.ColumnType() {
			case components.ColumnTypeMovies:
				opts = components.MovieSortOptions()
			case components.ColumnTypeShows:
				opts = components.ShowSortOptions()
			}
			if opts != nil {
				field, dir := top.SortState()
				m.SortModal.Show(opts, field, dir)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.Back):
		// Go back (pop column stack)
		return m.handleBack()

	case key.Matches(msg, Keys.Right):
		// Drill into (push new column)
		return m.handleDrillIn()

	case key.Matches(msg, Keys.Enter):
		// Enter can drill in OR play depending on selection
		return m.handleEnter()

	case key.Matches(msg, Keys.Refresh):
		// Refresh single selected library
		if libCol := m.libraryColumn(); libCol != nil {
			if lib := libCol.SelectedLibrary(); lib != nil {
				m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
				m.SyncingCount++
				m.Loading = true
				m.MultiLibSync = false
				m.updateLibraryStates()
				return m, SyncLibraryCmd(m.LibrarySvc, *lib, true)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.RefreshAll):
		// Refresh ALL libraries
		m.LibraryStates = make(map[string]components.LibrarySyncState)
		for _, lib := range m.Libraries {
			m.LibraryStates[lib.ID] = components.LibrarySyncState{Status: components.StatusSyncing}
		}
		m.SyncingCount = len(m.Libraries)
		m.Loading = true
		m.MultiLibSync = true
		m.updateLibraryStates()

		// Append synthetic "Playlists" entry at bottom
		allEntries := append(m.Libraries, playlistsLibraryEntry())

		// Reset to library view
		libCol := components.NewLibraryColumn(allEntries)
		libCol.SetLibraryStates(m.LibraryStates)
		m.Inspector.SetLibraryStates(m.LibraryStates)
		m.ColumnStack.Reset(libCol)

		return m, SyncAllLibrariesCmd(m.LibrarySvc, m.Libraries, true)

	case key.Matches(msg, Keys.MarkWatched):
		// Mark as watched
		if top := m.ColumnStack.Top(); top != nil {
			if item := top.SelectedMediaItem(); item != nil {
				return m, MarkWatchedCmd(m.PlaybackSvc, item.ID, item.Title)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.MarkUnwatched):
		// Mark as unwatched
		if top := m.ColumnStack.Top(); top != nil {
			if item := top.SelectedMediaItem(); item != nil {
				return m, MarkUnwatchedCmd(m.PlaybackSvc, item.ID, item.Title)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.Play):
		// Play from beginning
		if top := m.ColumnStack.Top(); top != nil {
			if item := top.SelectedMediaItem(); item != nil {
				return m, PlayItemCmd(m.PlaybackSvc, *item, false)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.ToggleInspector):
		// Toggle inspector visibility
		m.ShowInspector = !m.ShowInspector
		m.updateLayout()
		return m, nil

	case key.Matches(msg, Keys.Logout):
		// Logout (Shift+L) - show confirmation modal
		m.State = StateConfirmLogout
		return m, nil

	case key.Matches(msg, Keys.PlaylistModal):
		// Space: Open playlist modal for selected playable item
		if top := m.ColumnStack.Top(); top != nil {
			item := top.SelectedMediaItem()
			if item != nil && m.PlaylistSvc != nil {
				return m, LoadPlaylistModalDataCmd(m.PlaylistSvc, item)
			}
		}
		return m, nil

	case key.Matches(msg, Keys.Delete):
		if top := m.ColumnStack.Top(); top != nil {
			switch top.ColumnType() {
			case components.ColumnTypePlaylistItems:
				// Remove item from playlist
				if item := top.SelectedMediaItem(); item != nil && m.currentPlaylistID != "" {
					return m, RemoveFromPlaylistCmd(m.PlaylistSvc, m.currentPlaylistID, item.ID)
				}
			case components.ColumnTypePlaylists:
				// Delete playlist
				if playlist := top.SelectedPlaylist(); playlist != nil {
					return m, DeletePlaylistCmd(m.PlaylistSvc, playlist.ID)
				}
			}
		}
		return m, nil

	case key.Matches(msg, Keys.NewPlaylist):
		// Plex doesn't support empty playlists - show hint to use Space instead
		if top := m.ColumnStack.Top(); top != nil && top.ColumnType() == components.ColumnTypePlaylists {
			m.StatusMsg = "Use Space on an item to create a playlist"
			return m, ClearStatusCmd(3 * time.Second)
		}
	}

	// Let the focused column handle remaining keys (j/k/g/G navigation)
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

// routeToModal routes key input to active modals
// Returns (handled, model, cmd) where handled is true if a modal consumed the input
func (m Model) routeToModal(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle omnibar if visible
	if m.GlobalSearch.IsVisible() {
		var cmd tea.Cmd
		var selected bool
		m.GlobalSearch, cmd, selected = m.GlobalSearch.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if m.GlobalSearch.QueryChanged() {
			query := m.GlobalSearch.Query()
			results := m.SearchSvc.FilterLocal(query, nil, m.Libraries)
			m.GlobalSearch.SetResults(results)
		}

		if selected {
			if result := m.GlobalSearch.Selected(); result != nil {
				m.GlobalSearch.Hide()
				navCmd := m.navigateToSearchResult(*result)
				if navCmd != nil {
					cmds = append(cmds, navCmd)
				}
			}
		}
		return true, m, tea.Batch(cmds...)
	}

	// Handle sort modal if visible
	if m.SortModal.IsVisible() {
		handled, selection := m.SortModal.HandleKey(msg.String())
		if handled {
			if selection != nil {
				// Apply sort to current column
				if top := m.ColumnStack.Top(); top != nil {
					top.ApplySort(selection.Field, selection.Direction)
					m.updateInspector()
				}
			}
			return true, m, nil
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
					return true, m, tea.Batch(batchCmds...)
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
						return true, m, tea.Batch(batchCmds...)
					}
				}
			}
			return true, m, nil
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
				return true, m, CreatePlaylistCmd(m.PlaylistSvc, title, []string{})
			}
		}
		if cmd != nil {
			return true, m, cmd
		}
		return true, m, nil
	}

	// Handle filter typing mode
	if top := m.ColumnStack.Top(); top != nil && top.IsFilterTyping() {
		oldCursor := top.SelectedIndex()
		newCol, _ := top.Update(msg)
		m.ColumnStack.columns[len(m.ColumnStack.columns)-1] = newCol
		if oldCursor != top.SelectedIndex() {
			m.updateInspector()
		}
		return true, m, nil
	}

	return false, m, nil
}

// handleDrillIn handles drilling into the selected item (l key)
func (m Model) handleDrillIn() (tea.Model, tea.Cmd) {
	top := m.ColumnStack.Top()
	if top == nil {
		return m, nil
	}

	if !top.CanDrillInto() {
		// Can't drill into leaf items - play instead
		if item := top.SelectedMediaItem(); item != nil {
			resume := item.ShouldResume()
			return m, PlayItemCmd(m.PlaybackSvc, *item, resume)
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
	if item := top.SelectedMediaItem(); item != nil {
		resume := item.ShouldResume()
		return m, PlayItemCmd(m.PlaybackSvc, *item, resume)
	}

	return m, nil
}
