package styles

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	PlexOrange = lipgloss.Color("#E5A00D")
	SlateDark  = lipgloss.Color("#1F2937")
	SlateLight = lipgloss.Color("#374151")
	DimGray    = lipgloss.Color("#6B7280")
	LightGray  = lipgloss.Color("#9CA3AF")
	White      = lipgloss.Color("#F9FAFB")
	Green      = lipgloss.Color("#10B981")
	Red        = lipgloss.Color("#EF4444")
	Blue       = lipgloss.Color("#3B82F6")
)

// Borders
var (
	ActiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PlexOrange)

	InactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(DimGray)

	NoBorder = lipgloss.NewStyle().
			Border(lipgloss.HiddenBorder())
)

// Text styles
var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(White).
			Bold(true)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(LightGray)

	DimStyle = lipgloss.NewStyle().
			Foreground(DimGray)

	AccentStyle = lipgloss.NewStyle().
			Foreground(PlexOrange)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Red)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Green)

	HighlightStyle = lipgloss.NewStyle().
			Foreground(White).
			Background(PlexOrange).
			Padding(0, 1)
)

// Raw watch status characters (unstyled)
const (
	UnplayedChar   = "●"
	InProgressChar = "◐"
	PlayedChar     = "✓"
)

// Watch status indicator styles
var (
	UnplayedStyle   = lipgloss.NewStyle().Foreground(PlexOrange)
	InProgressStyle = lipgloss.NewStyle().Foreground(PlexOrange)
	PlayedStyle     = lipgloss.NewStyle().Foreground(Green)
)

// Pre-rendered watch status indicators (for non-selection contexts)
var (
	UnplayedDot   = UnplayedStyle.Render(UnplayedChar)
	InProgressDot = InProgressStyle.Render(InProgressChar)
	PlayedCheck   = PlayedStyle.Render(PlayedChar)
)

// Panel styles
var (
	SidebarStyle = lipgloss.NewStyle().
			Padding(1, 2)

	BrowserStyle = lipgloss.NewStyle().
			Padding(1, 2)

	InspectorStyle = lipgloss.NewStyle().
			Padding(1, 2)
)

// List item styles
var (
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(White).
				Background(SlateLight).
				Padding(0, 1)

	NormalItemStyle = lipgloss.NewStyle().
			Foreground(LightGray).
			Padding(0, 1)

	FocusedItemStyle = lipgloss.NewStyle().
				Foreground(PlexOrange).
				Bold(true).
				Padding(0, 1)
)

// Modal styles
var (
	ModalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PlexOrange).
			Padding(1, 2).
			Background(SlateDark)

	ModalTitleStyle = lipgloss.NewStyle().
			Foreground(White).
			Bold(true).
			MarginBottom(1)
)

// Help styles
var (
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(PlexOrange)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(DimGray)
)

// Progress bar styles
var (
	ProgressFullStyle = lipgloss.NewStyle().
				Foreground(PlexOrange)

	ProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(DimGray)
)

// Badge styles
var (
	BadgeStyle = lipgloss.NewStyle().
			Foreground(White).
			Background(PlexOrange).
			Padding(0, 1)

	DimBadgeStyle = lipgloss.NewStyle().
			Foreground(LightGray).
			Background(SlateLight).
			Padding(0, 1)
)

// Grid cell style
var (
	GridCellStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(DimGray).
			Padding(0, 1).
			Width(20)

	GridCellSelectedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(PlexOrange).
				Padding(0, 1).
				Width(20)
)

// Spinner style
var (
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(PlexOrange)
)

// Filter styles
var (
	FilterStyle = lipgloss.NewStyle().
			Foreground(PlexOrange)

	FilterPromptStyle = lipgloss.NewStyle().
				Foreground(PlexOrange).
				Bold(true)
)

// Match highlight styles for search results
var (
	MatchHighlightStyle = lipgloss.NewStyle().
				Foreground(PlexOrange).
				Bold(true)

	MatchHighlightSelectedStyle = lipgloss.NewStyle().
					Foreground(PlexOrange).
					Background(SlateLight).
					Bold(true)
)

// Helper functions

// Truncate truncates a string to the given width with ellipsis
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		if width > len(s) {
			return s
		}
		return s[:width]
	}
	return s[:width-3] + "..."
}

// Pad pads a string to the given width
func Pad(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + spaces(width-len(s))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

// RenderProgressBar renders a progress bar
func RenderProgressBar(percent float64, width int) string {
	if width < 3 {
		return ""
	}

	filled := int(float64(width) * percent / 100)
	if filled > width {
		filled = width
	}

	bar := ""
	for i := 0; i < filled; i++ {
		bar += ProgressFullStyle.Render("█")
	}
	for i := filled; i < width; i++ {
		bar += ProgressEmptyStyle.Render("░")
	}

	return bar
}

// RenderWatchStatus renders the watch status indicator
func RenderWatchStatus(isPlayed bool, viewOffset int64) string {
	if isPlayed {
		return PlayedCheck
	}
	if viewOffset > 0 {
		return InProgressDot
	}
	return UnplayedDot
}

// RenderListRow renders a complete list row with uniform background when selected.
// This function styles each part explicitly to avoid ANSI reset code issues.
// parts is a slice of {text, fgColor} pairs. Use nil for default foreground.
func RenderListRow(parts []RowPart, selected bool, width int) string {
	bg := SlateLight
	defaultFg := LightGray
	selectedFg := White

	var result string
	visibleLen := 0

	for _, part := range parts {
		style := lipgloss.NewStyle()
		if part.Foreground != nil {
			style = style.Foreground(*part.Foreground)
		} else if selected {
			style = style.Foreground(selectedFg)
		} else {
			style = style.Foreground(defaultFg)
		}
		if selected {
			style = style.Background(bg)
		}
		result += style.Render(part.Text)
		visibleLen += lipgloss.Width(part.Text)
	}

	// Add padding to fill width (subtract 2 for left/right margin)
	paddingNeeded := width - visibleLen - 2
	if paddingNeeded > 0 {
		padStyle := lipgloss.NewStyle()
		if selected {
			padStyle = padStyle.Background(bg)
		}
		result += padStyle.Render(spaces(paddingNeeded))
	}

	// Add margins
	marginStyle := lipgloss.NewStyle()
	if selected {
		marginStyle = marginStyle.Background(bg)
	}
	margin := marginStyle.Render(" ")

	return margin + result + margin
}

// RowPart represents a part of a row with optional foreground color
type RowPart struct {
	Text       string
	Foreground *lipgloss.Color
}
