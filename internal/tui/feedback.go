package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/tui/components"
)

const loadingIndicatorDelay = 200 * time.Millisecond

func showLoadingCmd(req request) tea.Cmd {
	return tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg { return ShowLoadingMsg{Request: req} })
}

func (m *Model) updateResourceFeedback(r catalog.Resource) {
	result := m.collection(r)
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
	feedback := components.CollectionFeedback{
		Pending: pending, Activity: activity, Error: result.Error,
		Summary: components.CollectionSummary{Count: len(result.Snapshot.Items), Known: result.Known,
			Stale: result.Snapshot.Stale || result.Error != nil || result.Snapshot.Revision < result.RequiredRevision},
	}
	for i := 0; i < m.ColumnStack.Len(); i++ {
		if col := m.ColumnStack.Get(i); col.ContentID() == r.Key() {
			col.SetFeedback(feedback)
		}
	}
	if id := libraryStateID(r); id != "" {
		m.LibraryStates[id] = feedback
		m.updateLibraryStates()
	}
}
