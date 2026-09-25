package tui

// Layout proportions for Miller Columns
const (
	// 3-Column Smart Ratios (Inspector visible)
	parentColumnPercent3   = 25 // Parent context
	inspectorColumnPercent = 30 // Inspector (summary)

	// 3-Column Focus Mode (Inspector hidden) - show more navigation context
	grandparentColumnPercent = 25 // Grandparent context
	parentColumnPercent2     = 30 // Parent context

	// Root level (single column + inspector)
	rootColumnPercent = 40

	minColumnWidth = 15

	// Vertical layout: single footer line
	chromeHeight = 1
)

// columnLayout holds calculated column widths for the View
type columnLayout struct {
	grandparentWidth int // 0 if not shown
	parentWidth      int // 0 if not shown
	activeWidth      int
	inspectorWidth   int // 0 if not shown
}

// calculateColumnLayout computes column widths based on stack depth and inspector visibility
func (m *Model) calculateColumnLayout(availableWidth int) columnLayout {
	stackLen := m.columns.Len()
	layout := columnLayout{}

	// Helper to apply minimum width
	applyMin := func(width int) int {
		return max(width, minColumnWidth)
	}

	switch stackLen {
	case 0:
		// Empty stack - shouldn't happen
		layout.activeWidth = availableWidth

	case 1:
		// Root level: single column (Libraries)
		if m.showInspector {
			layout.activeWidth = applyMin(availableWidth * rootColumnPercent / 100)
			layout.inspectorWidth = availableWidth - layout.activeWidth
		} else {
			layout.activeWidth = availableWidth
		}

	case 2:
		// 2 columns in stack
		if m.showInspector {
			// [Parent | Active | Inspector]
			layout.parentWidth = applyMin(availableWidth * parentColumnPercent3 / 100)
			layout.inspectorWidth = applyMin(availableWidth * inspectorColumnPercent / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth - layout.inspectorWidth)
		} else {
			// [Parent | Active]
			layout.parentWidth = applyMin(availableWidth * parentColumnPercent2 / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth)
		}

	default:
		// 3+ columns in stack
		if m.showInspector {
			// [Parent | Active | Inspector]
			layout.parentWidth = applyMin(availableWidth * parentColumnPercent3 / 100)
			layout.inspectorWidth = applyMin(availableWidth * inspectorColumnPercent / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth - layout.inspectorWidth)
		} else {
			// [Grandparent | Parent | Active]
			layout.grandparentWidth = applyMin(availableWidth * grandparentColumnPercent / 100)
			layout.parentWidth = applyMin(availableWidth * parentColumnPercent2 / 100)
			layout.activeWidth = applyMin(availableWidth - layout.grandparentWidth - layout.parentWidth)
		}
	}

	return layout
}

// updateLayout updates component sizes based on window size
func (m *Model) updateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	contentHeight := m.height - chromeHeight
	m.globalSearch.SetSize(m.width, m.height)
	m.playlistModal.SetSize(m.width, m.height)

	stackLen := m.columns.Len()
	if stackLen == 0 {
		return
	}

	// Calculate layout using shared logic
	layout := m.calculateColumnLayout(m.width)
	topIdx := stackLen - 1

	// Apply calculated sizes to components
	switch stackLen {
	case 1:
		m.columns.Get(0).SetSize(layout.activeWidth, contentHeight)
		if m.showInspector {
			m.inspector.SetSize(layout.inspectorWidth, contentHeight)
		}

	case 2:
		m.columns.Get(topIdx-1).SetSize(layout.parentWidth, contentHeight)
		m.columns.Get(topIdx).SetSize(layout.activeWidth, contentHeight)
		if m.showInspector {
			m.inspector.SetSize(layout.inspectorWidth, contentHeight)
		}

	default: // 3+ columns
		if layout.grandparentWidth > 0 {
			m.columns.Get(topIdx-2).SetSize(layout.grandparentWidth, contentHeight)
		}
		m.columns.Get(topIdx-1).SetSize(layout.parentWidth, contentHeight)
		m.columns.Get(topIdx).SetSize(layout.activeWidth, contentHeight)
		if m.showInspector {
			m.inspector.SetSize(layout.inspectorWidth, contentHeight)
		}
	}
}
