package tui

import (
	"github.com/mmcdole/kino/internal/tui/components"
)

// columnStack retains each column's selection and scroll position during
// navigation. The top column is focused; the root cannot be popped.
type columnStack struct {
	columns []*components.ListColumn
}

// newColumnStack creates a new empty column stack
func newColumnStack() *columnStack {
	return &columnStack{}
}

// Len returns the number of columns in the stack
func (cs *columnStack) Len() int {
	return len(cs.columns)
}

// Get returns the column at the given index (0 = bottom/oldest)
func (cs *columnStack) Get(idx int) *components.ListColumn {
	if idx < 0 || idx >= len(cs.columns) {
		return nil
	}
	return cs.columns[idx]
}

// Top returns the topmost (current/focused) column
func (cs *columnStack) Top() *components.ListColumn {
	if len(cs.columns) == 0 {
		return nil
	}
	return cs.columns[len(cs.columns)-1]
}

// Push focuses col and retains the parent column.
func (cs *columnStack) Push(col *components.ListColumn) {
	if top := cs.Top(); top != nil {
		top.SetFocused(false)
	}

	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
}

// Pop removes the focused column unless it is the root.
func (cs *columnStack) Pop() *components.ListColumn {
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
func (cs *columnStack) Reset(col *components.ListColumn) {
	for _, c := range cs.columns {
		c.SetFocused(false)
	}
	cs.columns = nil
	col.SetFocused(true)
	cs.columns = append(cs.columns, col)
}

// UpdateSpinnerFrame updates the spinner frame for all columns
func (cs *columnStack) UpdateSpinnerFrame(frame int) {
	for _, col := range cs.columns {
		col.SetSpinnerFrame(frame)
	}
}
