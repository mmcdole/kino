package tui

// columnLayout holds calculated column widths for the View
type columnLayout struct {
	grandparentWidth int // 0 if not shown
	parentWidth      int // 0 if not shown
	activeWidth      int
	inspectorWidth   int // 0 if not shown
}

// calculateColumnLayout computes column widths based on stack depth and inspector visibility
func (m Model) calculateColumnLayout(availableWidth int) columnLayout {
	stackLen := m.ColumnStack.Len()
	layout := columnLayout{}

	// Helper to apply minimum width
	applyMin := func(width int) int {
		return max(width, MinColumnWidth)
	}

	switch stackLen {
	case 0:
		// Empty stack - shouldn't happen
		layout.activeWidth = availableWidth

	case 1:
		// Root level: single column (Libraries)
		if m.ShowInspector {
			layout.activeWidth = applyMin(availableWidth * RootColumnPercent / 100)
			layout.inspectorWidth = availableWidth - layout.activeWidth
		} else {
			layout.activeWidth = availableWidth
		}

	case 2:
		// 2 columns in stack
		if m.ShowInspector {
			// [Parent | Active | Inspector]
			layout.parentWidth = applyMin(availableWidth * ParentColumnPercent3 / 100)
			layout.inspectorWidth = applyMin(availableWidth * InspectorColumnPercent / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth - layout.inspectorWidth)
		} else {
			// [Parent | Active]
			layout.parentWidth = applyMin(availableWidth * ParentColumnPercent2 / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth)
		}

	default:
		// 3+ columns in stack
		if m.ShowInspector {
			// [Parent | Active | Inspector]
			layout.parentWidth = applyMin(availableWidth * ParentColumnPercent3 / 100)
			layout.inspectorWidth = applyMin(availableWidth * InspectorColumnPercent / 100)
			layout.activeWidth = applyMin(availableWidth - layout.parentWidth - layout.inspectorWidth)
		} else {
			// [Grandparent | Parent | Active]
			layout.grandparentWidth = applyMin(availableWidth * GrandparentColumnPercent / 100)
			layout.parentWidth = applyMin(availableWidth * ParentColumnPercent2 / 100)
			layout.activeWidth = applyMin(availableWidth - layout.grandparentWidth - layout.parentWidth)
		}
	}

	return layout
}

// updateLayout updates component sizes based on window size
func (m *Model) updateLayout() {
	if m.Width == 0 || m.Height == 0 {
		return
	}

	contentHeight := m.Height - ChromeHeight
	m.GlobalSearch.SetSize(m.Width, m.Height)

	stackLen := m.ColumnStack.Len()
	if stackLen == 0 {
		return
	}

	// Calculate layout using shared logic
	layout := m.calculateColumnLayout(m.Width)
	topIdx := stackLen - 1

	// Apply calculated sizes to components
	switch stackLen {
	case 1:
		m.ColumnStack.Get(0).SetSize(layout.activeWidth, contentHeight)
		if m.ShowInspector {
			m.Inspector.SetSize(layout.inspectorWidth, contentHeight)
		}

	case 2:
		m.ColumnStack.Get(topIdx-1).SetSize(layout.parentWidth, contentHeight)
		m.ColumnStack.Get(topIdx).SetSize(layout.activeWidth, contentHeight)
		if m.ShowInspector {
			m.Inspector.SetSize(layout.inspectorWidth, contentHeight)
		}

	default: // 3+ columns
		if layout.grandparentWidth > 0 {
			m.ColumnStack.Get(topIdx-2).SetSize(layout.grandparentWidth, contentHeight)
		}
		m.ColumnStack.Get(topIdx-1).SetSize(layout.parentWidth, contentHeight)
		m.ColumnStack.Get(topIdx).SetSize(layout.activeWidth, contentHeight)
		if m.ShowInspector {
			m.Inspector.SetSize(layout.inspectorWidth, contentHeight)
		}
	}
}
