package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// SortSelection represents the user's sort choice
type SortSelection struct {
	Field     SortField
	Direction SortDirection
}

// SortModal is a small popup for choosing sort order
type SortModal struct {
	options     []SortField
	cursor      int
	activeField SortField
	activeDir   SortDirection
}

// Show displays the modal with the given options and current sort state
func (m *SortModal) Show(options []SortField, activeField SortField, activeDir SortDirection) {
	m.options = options
	m.activeField = activeField
	m.activeDir = activeDir
	// Position cursor on the active field
	m.cursor = 0
	for i, opt := range options {
		if opt == activeField {
			m.cursor = i
			break
		}
	}
}

// HandleKeyMsg processes a key press. Submit carries the chosen sort.
func (m *SortModal) HandleKeyMsg(msg tea.KeyMsg) (Outcome, SortSelection) {
	switch {
	case key.Matches(msg, SortModalKeys.Down):
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
		return Continue, SortSelection{}
	case key.Matches(msg, SortModalKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return Continue, SortSelection{}
	case key.Matches(msg, SortModalKeys.Left):
		return Submit, SortSelection{Field: m.options[m.cursor], Direction: SortAsc}
	case key.Matches(msg, SortModalKeys.Right):
		return Submit, SortSelection{Field: m.options[m.cursor], Direction: SortDesc}
	case key.Matches(msg, SortModalKeys.Enter):
		chosen := m.options[m.cursor]
		dir := DefaultDirection(chosen)
		if chosen == m.activeField {
			if m.activeDir == SortAsc {
				dir = SortDesc
			} else {
				dir = SortAsc
			}
		}
		return Submit, SortSelection{Field: chosen, Direction: dir}
	case key.Matches(msg, SortModalKeys.Escape), key.Matches(msg, SortModalKeys.Close):
		return Cancel, SortSelection{}
	}
	return Continue, SortSelection{}
}

// View renders the sort modal
func (m SortModal) View() string {
	if len(m.options) == 0 {
		return ""
	}

	var lines []string
	for i, opt := range m.options {
		selected := i == m.cursor
		isActive := opt == m.activeField

		// Build the line content
		var prefix string
		if isActive {
			prefix = "✓ "
		} else {
			prefix = "  "
		}

		label := opt.String()

		var suffix string
		if isActive {
			if m.activeDir == SortAsc {
				suffix = " ↑"
			} else {
				suffix = " ↓"
			}
		}

		text := prefix + label + suffix

		// Style the line
		if selected {
			line := lipgloss.NewStyle().
				Foreground(styles.White).
				Background(styles.SlateLight).
				Render(styles.Pad(text, 20))
			lines = append(lines, line)
		} else if isActive {
			line := lipgloss.NewStyle().
				Foreground(styles.PlexOrange).
				Render(styles.Pad(text, 20))
			lines = append(lines, line)
		} else {
			line := lipgloss.NewStyle().
				Foreground(styles.LightGray).
				Render(styles.Pad(text, 20))
			lines = append(lines, line)
		}
	}

	hintText := "← asc   desc →"
	pad := max((20-lipgloss.Width(hintText))/2, 0)
	hint := styles.DimStyle.Render(strings.Repeat(" ", pad) + hintText)
	content := strings.Join(lines, "\n") + "\n\n" + hint

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.PlexOrange).
		Background(styles.SlateDark).
		Padding(0, 1).
		Render(styles.ModalTitleStyle.Render("Sort by") + "\n" + content)

	return modal
}
