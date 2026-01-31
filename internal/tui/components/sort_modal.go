package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// SortField represents a field to sort by
type SortField int

const (
	SortDefault SortField = iota
	SortTitle
	SortDateAdded
	SortLastUpdated // shows only
	SortReleased
	SortDuration
	SortRating
	SortEpisodeNum
)

// String returns the display name for the sort field
func (f SortField) String() string {
	switch f {
	case SortTitle:
		return "Title"
	case SortDateAdded:
		return "Date Added"
	case SortLastUpdated:
		return "Last Updated"
	case SortReleased:
		return "Release Date"
	case SortDuration:
		return "Duration"
	case SortRating:
		return "Rating"
	case SortEpisodeNum:
		return "Episode #"
	default:
		return "Unknown"
	}
}

// SortDirection represents sort direction
type SortDirection int

const (
	SortAsc SortDirection = iota
	SortDesc
)

// DefaultDirection returns the default sort direction for a field
func DefaultDirection(field SortField) SortDirection {
	switch field {
	case SortTitle:
		return SortAsc // A-Z
	case SortEpisodeNum:
		return SortAsc // natural order
	default:
		return SortDesc // newest/highest/longest first
	}
}

// MovieSortOptions returns the available sort options for movies
func MovieSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortReleased, SortDuration, SortRating}
}

// ShowSortOptions returns the available sort options for shows
func ShowSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortLastUpdated, SortReleased, SortRating}
}

// EpisodeSortOptions returns the available sort options for episodes
func EpisodeSortOptions() []SortField {
	return []SortField{SortEpisodeNum, SortTitle, SortDuration, SortDateAdded, SortRating}
}

// MixedSortOptions returns the available sort options for mixed content
func MixedSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortReleased, SortDuration, SortRating}
}

// SortSelection represents the user's sort choice
type SortSelection struct {
	Field     SortField
	Direction SortDirection
}

// SortModal is a small popup for choosing sort order
type SortModal struct {
	visible     bool
	options     []SortField
	cursor      int
	activeField SortField
	activeDir   SortDirection
}

// Show displays the modal with the given options and current sort state
func (m *SortModal) Show(options []SortField, activeField SortField, activeDir SortDirection) {
	m.visible = true
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

// Hide dismisses the modal
func (m *SortModal) Hide() {
	m.visible = false
}

// IsVisible returns whether the modal is shown
func (m SortModal) IsVisible() bool {
	return m.visible
}

// HandleKeyMsg processes a key press, returns (handled, selection).
// If selection is non-nil, the user confirmed a choice.
func (m *SortModal) HandleKeyMsg(msg tea.KeyMsg) (handled bool, selection *SortSelection) {
	if !m.visible {
		return false, nil
	}

	switch {
	case key.Matches(msg, SortModalKeys.Down):
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
		return true, nil
	case key.Matches(msg, SortModalKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return true, nil
	case key.Matches(msg, SortModalKeys.Left):
		chosen := m.options[m.cursor]
		m.visible = false
		return true, &SortSelection{Field: chosen, Direction: SortAsc}
	case key.Matches(msg, SortModalKeys.Right):
		chosen := m.options[m.cursor]
		m.visible = false
		return true, &SortSelection{Field: chosen, Direction: SortDesc}
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
		m.visible = false
		return true, &SortSelection{Field: chosen, Direction: dir}
	case key.Matches(msg, SortModalKeys.Escape), key.Matches(msg, SortModalKeys.Close):
		m.visible = false
		return true, nil
	}

	return true, nil // consume all keys when visible
}

// View renders the sort modal
func (m SortModal) View() string {
	if !m.visible || len(m.options) == 0 {
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
