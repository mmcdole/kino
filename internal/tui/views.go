package tui

import (
	"github.com/mmcdole/kino/internal/tui/styles"
)

// RenderSpinner renders a loading spinner
func RenderSpinner(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return styles.SpinnerStyle.Render(frames[frame%len(frames)])
}

