package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/tui/components"
)

const loadingIndicatorDelay = 200 * time.Millisecond

// showLoadingCmd reveals the activity indicator for one server attempt if it
// is still running after the delay, so quick loads never flash a spinner.
func showLoadingCmd(key string, attempt uint64) tea.Cmd {
	return tea.Tick(loadingIndicatorDelay, func(time.Time) tea.Msg { return ShowLoadingMsg{Key: key, Attempt: attempt} })
}

// updateResourceFeedback derives one feedback value from the collection's
// state and this model's subscriptions, and hands it to every place that
// shows the collection.
func (m *Model) updateResourceFeedback(r catalog.Resource) {
	key := r.Key()
	st := m.collections[key]
	_, view := m.requests.active[viewOwner(r)]
	_, sync := m.requests.active[syncOwner(r)]
	pending := view || sync

	var activity components.LoadActivity
	if pending && st.Fetching && st.Attempt != 0 && m.indicators[key] == st.Attempt {
		activity = components.LoadActivity{Visible: true, Loaded: st.Progress.Loaded, Total: st.Progress.Total}
	}
	feedback := components.CollectionFeedback{
		Pending: pending, Activity: activity, Error: st.Err,
		Summary: components.CollectionSummary{Count: len(st.Snapshot.Items), Known: st.Known, Stale: st.Snapshot.Stale || st.Err != nil},
	}
	for i := 0; i < m.ColumnStack.Len(); i++ {
		if col := m.ColumnStack.Get(i); col.ContentID() == key {
			col.SetFeedback(feedback)
		}
	}
	if id := libraryStateID(r); id != "" {
		m.LibraryStates[id] = feedback
		m.updateLibraryStates()
	}
}
