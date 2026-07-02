package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/tui/components"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// RenderSpinner renders a loading spinner
func RenderSpinner(frame int) string {
	return styles.SpinnerStyle.Render(styles.SpinnerFrames[frame%len(styles.SpinnerFrames)])
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

	if m.State == StateConfirmDeletePlaylist {
		return m.renderDeletePlaylistConfirmation()
	}

	contentHeight := m.Height - ChromeHeight
	stackLen := m.ColumnStack.Len()
	layout := m.calculateColumnLayout(m.Width)

	var content string

	if stackLen == 0 {
		content = ""
	} else {
		topIdx := stackLen - 1
		currentCol := m.ColumnStack.Get(topIdx)

		// Build columns list based on what's visible
		var columnViews []string

		// Add grandparent column if visible (3+ columns, inspector hidden)
		if layout.grandparentWidth > 0 {
			grandparentCol := m.ColumnStack.Get(topIdx - 2)
			grandparentCol.SetSize(layout.grandparentWidth, contentHeight)
			columnViews = append(columnViews, grandparentCol.View())
		}

		// Add parent column if visible (2+ columns)
		if layout.parentWidth > 0 {
			parentCol := m.ColumnStack.Get(topIdx - 1)
			parentCol.SetSize(layout.parentWidth, contentHeight)
			columnViews = append(columnViews, parentCol.View())
		}

		// Active column is always visible
		currentCol.SetSize(layout.activeWidth, contentHeight)
		columnViews = append(columnViews, currentCol.View())

		// Add inspector if visible
		if layout.inspectorWidth > 0 {
			m.Inspector.SetSize(layout.inspectorWidth, contentHeight)
			m.Inspector.SetItem(currentCol.SelectedItem())
			columnViews = append(columnViews, m.Inspector.View())
		}

		content = lipgloss.JoinHorizontal(lipgloss.Top, columnViews...)
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
	if m.GlobalSearch.IsVisible() {
		view = lipgloss.Place(m.Width, m.Height,
			lipgloss.Center, lipgloss.Center,
			m.GlobalSearch.View())
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

// renderFooter renders a single-line minimal footer.
//
// Feedback scope rules (see docs/design-review.md and notice.go):
//   - Row-scoped work (library sync) renders on the row; the footer only
//     carries a compact activity segment on the right, never full-width text.
//   - Column-scoped work (loads, refreshes, failures) renders in the column.
//   - The footer's left side is exclusively the notification slot: transient
//     events and persistent alerts.
func (m Model) renderFooter() string {
	// Left side: current notification, styled by kind
	var left string
	if m.notice.Text != "" {
		switch m.notice.Kind {
		case NoticeAlert:
			left = styles.AlertStyle.Render(m.notice.Text) + styles.DimStyle.Render("  esc to dismiss")
		case NoticeError:
			left = styles.ErrorStyle.Render(m.notice.Text)
		case NoticeSuccess:
			left = styles.SuccessStyle.Render(m.notice.Text)
		default:
			left = styles.DimStyle.Render(m.notice.Text)
		}
	}

	// Center section: context-specific hints based on column type
	var center string
	if top := m.ColumnStack.Top(); top != nil {
		switch top.ColumnType() {
		case components.ColumnTypePlaylists:
			center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Delete")
		case components.ColumnTypePlaylistItems:
			center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Remove")
		}
	}

	// Right side: compact background-sync segment + "? help" hint
	right := styles.AccentStyle.Render("?") + styles.DimStyle.Render(" help")
	if n := m.activeSyncCount(); n > 0 {
		right = RenderSpinner(m.SpinnerFrame) + styles.DimStyle.Render(fmt.Sprintf(" %d syncing", n)) + "   " + right
	}

	// Layout: left + centered hints + right
	leftWidth := lipgloss.Width(left)
	centerWidth := lipgloss.Width(center)
	rightWidth := lipgloss.Width(right)

	totalContent := leftWidth + centerWidth + rightWidth
	if totalContent >= m.Width {
		// Not enough space - just left + right
		gap := max(0, m.Width-leftWidth-rightWidth)
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
  Backspace  Back                  w      Mark watched
  g/Home     First item            u      Mark unwatched
  G/End      Last item
  PgUp/PgDn  Scroll page         PLAYLISTS
  Ctrl+u/d   Scroll half page      Space  Add/remove item
                                   x      Delete / remove
SEARCH & VIEW
  /          Filter              OTHER
  f          Global search         r      Refresh view
  s          Sort                  R      Refresh all
  i          Toggle inspector      q      Quit
                                   L      Logout
                                   Esc    Close / Cancel

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

// renderDeletePlaylistConfirmation renders the playlist delete confirmation
func (m Model) renderDeletePlaylistConfirmation() string {
	name := styles.Truncate(m.pendingDeletePlaylistName, 30)
	modal := fmt.Sprintf(`
        Delete Playlist?

  %q
  will be permanently deleted
  from the server.

      [Y] Yes      [N] No
`, name)

	return lipgloss.Place(m.Width, m.Height,
		lipgloss.Center, lipgloss.Center,
		styles.ModalStyle.Render(modal))
}
