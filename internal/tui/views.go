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
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return styles.SpinnerStyle.Render(frames[frame%len(frames)])
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
		switch top.ColumnType() {
		case components.ColumnTypePlaylists:
			center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Delete")
		case components.ColumnTypePlaylistItems:
			center = styles.AccentStyle.Render("x") + styles.DimStyle.Render(" Remove")
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
