package styles

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

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
)

// Borders
var (
	ActiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(PlexOrange)

	InactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(DimGray)
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

// List item styles
var (
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(White).
				Background(SlateLight).
				Padding(0, 1)

	NormalItemStyle = lipgloss.NewStyle().
			Foreground(LightGray).
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

// Badge styles
var (
	DimBadgeStyle = lipgloss.NewStyle().
		Foreground(LightGray).
		Background(SlateLight).
		Padding(0, 1)
)

// Spinner style
var (
	SpinnerStyle = lipgloss.NewStyle().
		Foreground(PlexOrange)
)

// SpinnerFrames contains the animation frames for the loading spinner
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Filter styles
var (
	FilterStyle = lipgloss.NewStyle().
			Foreground(PlexOrange)

	FilterPromptStyle = lipgloss.NewStyle().
				Foreground(PlexOrange).
				Bold(true)
)

// Helper functions

// Truncate truncates a string to the given display width with ellipsis.
// Display-width aware: byte slicing splits multibyte runes (mojibake) and
// miscounts CJK/emoji cell widths.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return runewidth.Truncate(s, width, "")
	}
	return runewidth.Truncate(s, width, "...")
}

// Pad pads a string to the given display width
func Pad(s string, width int) string {
	if runewidth.StringWidth(s) >= width {
		return runewidth.Truncate(s, width, "")
	}
	return runewidth.FillRight(s, width)
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
