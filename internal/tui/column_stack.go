package tui

import (
	"github.com/mmcdole/kino/internal/tui/components"
)

// ColumnStack manages the stack of navigable columns in Miller Columns layout.
// The stack contains list columns only - the Inspector is a separate view projection.
//
// Visual representation:
//   Root:     [Empty | Libraries | Inspector]
//   Library:  [Libraries | Movies | Inspector]
//   Show:     [TV Shows | Breaking Bad | Inspector]
//   Season:   [Breaking Bad | Season 1 | Inspector]
//
// The "middle" column (top of stack) is always focused.
// The "left" column shows parent context.
// The "right" column (Inspector) shows details for the selection in middle column.
type ColumnStack struct {
	columns     []*components.ListColumn
	cursorStack []int // Saved cursor positions for back navigation
}

// NewColumnStack creates a new empty column stack
func NewColumnStack() *ColumnStack {
	return &ColumnStack{
		columns:     make([]*components.ListColumn, 0),
		cursorStack: make([]int, 0),
	}
}

// Len returns the number of columns in the stack
func (cs *ColumnStack) Len() int {
	return len(cs.columns)
}

// Get returns the column at the given index (0 = bottom/oldest)
func (cs *ColumnStack) Get(idx int) *components.ListColumn {
	if idx < 0 || idx >= len(cs.columns) {
		return nil
	}
	return cs.columns[idx]
}

// Top returns the topmost (current/focused) column
func (cs *ColumnStack) Top() *components.ListColumn {
	if len(cs.columns) == 0 {
		return nil
	}
	return cs.columns[len(cs.columns)-1]
}

// Push adds a new column to the stack, saving the current cursor position
func (cs *ColumnStack) Push(col *components.ListColumn, saveCursor int) {
	// Save current cursor position for back navigation
	cs.cursorStack = append(cs.cursorStack, saveCursor)

	// Unfocus current top
	if top := cs.Top(); top != nil {
		top.SetFocused(false)
	}

	// Add new column and focus it
	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
}

// Pop removes and returns the top column, along with the saved cursor position.
// Returns nil if stack would become empty (must have at least 1 column).
func (cs *ColumnStack) Pop() (*components.ListColumn, int) {
	if len(cs.columns) <= 1 {
		// Don't pop the last column (root level)
		return nil, 0
	}

	// Remove top column
	popped := cs.columns[len(cs.columns)-1]
	popped.SetFocused(false)
	cs.columns = cs.columns[:len(cs.columns)-1]

	// Restore saved cursor position
	savedCursor := 0
	if len(cs.cursorStack) > 0 {
		savedCursor = cs.cursorStack[len(cs.cursorStack)-1]
		cs.cursorStack = cs.cursorStack[:len(cs.cursorStack)-1]
	}

	// Focus new top
	if top := cs.Top(); top != nil {
		top.SetFocused(true)
	}

	return popped, savedCursor
}

// Replace replaces the top column with a new one (used when switching libraries)
func (cs *ColumnStack) Replace(col *components.ListColumn) {
	if len(cs.columns) == 0 {
		// Just push if empty
		col.SetFocused(true)
		cs.columns = append(cs.columns, col)
		return
	}

	// Unfocus and replace
	cs.columns[len(cs.columns)-1].SetFocused(false)
	col.SetFocused(true)
	cs.columns[len(cs.columns)-1] = col
}

// Clear removes all columns from the stack
func (cs *ColumnStack) Clear() {
	for _, col := range cs.columns {
		col.SetFocused(false)
	}
	cs.columns = nil
	cs.cursorStack = nil
}

// Reset resets the stack to a single column (used when switching libraries)
func (cs *ColumnStack) Reset(col *components.ListColumn) {
	cs.Clear()
	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
	cs.cursorStack = nil
}

// SetSizes updates the size of all columns
func (cs *ColumnStack) SetSizes(width, height int) {
	for _, col := range cs.columns {
		col.SetSize(width, height)
	}
}

// Parent returns the parent column (second from top), or nil if at root
func (cs *ColumnStack) Parent() *components.ListColumn {
	if len(cs.columns) < 2 {
		return nil
	}
	return cs.columns[len(cs.columns)-2]
}

// CanGoBack returns true if we can navigate back (not at root)
func (cs *ColumnStack) CanGoBack() bool {
	return len(cs.columns) > 1
}

// Depth returns the navigation depth (0 = root, 1 = first drill, etc.)
func (cs *ColumnStack) Depth() int {
	if len(cs.columns) == 0 {
		return 0
	}
	return len(cs.columns) - 1
}

// UpdateSpinnerFrame updates the spinner frame for all columns
func (cs *ColumnStack) UpdateSpinnerFrame(frame int) {
	for _, col := range cs.columns {
		col.SetSpinnerFrame(frame)
	}
}
