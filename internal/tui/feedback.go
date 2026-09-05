package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/tui/components"
)

// Request activity never overwrites the last complete collection summary.
// Errors survive cached reads and clear only after successful server validation.
type resourceResult struct {
	Summary components.CollectionSummary
	Error   error
}

const loadingIndicatorDelay = 200 * time.Millisecond

func showLoadingCmd(req request) tea.Cmd {
	return tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg { return ShowLoadingMsg{Request: req} })
}

func (m *Model) updateResourceFeedback(r catalog.Resource) {
	result := m.resourceResults[r.Key()]
	var pending bool
	var activity components.LoadActivity
	var progressID uint64
	for _, owner := range []string{viewOwner(r), syncOwner(r)} {
		req, ok := m.requests.active[owner]
		if !ok {
			continue
		}
		pending = true
		if !req.IndicatorVisible {
			continue
		}
		activity.Visible = true
		if req.ID > progressID {
			progressID = req.ID
			activity.Loaded, activity.Total = req.Progress.Loaded, req.Progress.Total
		}
	}
	for i := 0; i < m.ColumnStack.Len(); i++ {
		col := m.ColumnStack.Get(i)
		if col.ContentID() == r.Key() {
			col.SetRequestState(pending, activity.Visible, result.Error != nil)
		}
	}
	if id := libraryStateID(r); id != "" {
		summary := result.Summary
		summary.Stale = summary.Stale || result.Error != nil
		m.LibraryStates[id] = components.LibraryState{Summary: summary, Activity: activity, Error: result.Error}
		m.updateLibraryStates()
	}
}
