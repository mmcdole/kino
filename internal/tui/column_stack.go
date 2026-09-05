package tui

import (
	"github.com/mmcdole/kino/internal/tui/components"
)

// ColumnStack retains each column's selection and scroll position during
// navigation. The top column is focused; the root cannot be popped.
type ColumnStack struct {
	columns []*components.ListColumn
}

// NewColumnStack creates a new empty column stack
func NewColumnStack() *ColumnStack {
	return &ColumnStack{}
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

// Push focuses col and retains the parent column.
func (cs *ColumnStack) Push(col *components.ListColumn) {
	if top := cs.Top(); top != nil {
		top.SetFocused(false)
	}

	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
}

// Pop removes the focused column unless it is the root.
func (cs *ColumnStack) Pop() *components.ListColumn {
	if len(cs.columns) <= 1 {
		return nil
	}

	popped := cs.columns[len(cs.columns)-1]
	popped.SetFocused(false)
	cs.columns = cs.columns[:len(cs.columns)-1]

	if top := cs.Top(); top != nil {
		top.SetFocused(true)
	}

	return popped
}

// Reset focuses col as the root.
func (cs *ColumnStack) Reset(col *components.ListColumn) {
	for _, c := range cs.columns {
		c.SetFocused(false)
	}
	cs.columns = nil
	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
}

// CanGoBack returns true if we can navigate back (not at root)
func (cs *ColumnStack) CanGoBack() bool {
	return len(cs.columns) > 1
}

// UpdateTop replaces the top column with the given column.
func (cs *ColumnStack) UpdateTop(col *components.ListColumn) {
	if len(cs.columns) > 0 {
		cs.columns[len(cs.columns)-1] = col
	}
}

// UpdateSpinnerFrame updates the spinner frame for all columns
func (cs *ColumnStack) UpdateSpinnerFrame(frame int) {
	for _, col := range cs.columns {
		col.SetSpinnerFrame(frame)
	}
}
