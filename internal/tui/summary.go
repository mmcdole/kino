package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
)

// summaryDelay is how long the selection must rest under an open inspector
// before its synopsis is asked for, so scrolling doesn't ask for every row.
const summaryDelay = 150 * time.Millisecond

const summaryOwner = "summary"

type summaryDebounceMsg struct {
	Seq uint64
	ID  string
}

type summaryMsg struct {
	Request request
	ID      string
	Text    string
	Err     error
}

// missingSummary returns the ID of a movie, episode or show with no synopsis
// that hasn't been asked for yet, or "".
func (m *Model) missingSummary(item media.ListItem) string {
	var id, text string
	switch v := item.(type) {
	case *media.Item:
		id, text = v.ID, v.Summary
	case *media.Show:
		id, text = v.ID, v.Summary
	default:
		return ""
	}
	if text != "" {
		return ""
	}
	if _, asked := m.summaries[id]; asked {
		return ""
	}
	return id
}

// withSummary returns item with its fetched synopsis, leaving the column's
// value alone.
func (m *Model) withSummary(item media.ListItem) media.ListItem {
	switch v := item.(type) {
	case *media.Item:
		if text := m.summaries[v.ID]; v.Summary == "" && text != "" {
			c := *v
			c.Summary = text
			return &c
		}
	case *media.Show:
		if text := m.summaries[v.ID]; v.Summary == "" && text != "" {
			c := *v
			c.Summary = text
			return &c
		}
	}
	return item
}

// wantSummary schedules a synopsis request for the inspected item once the
// selection rests, and drops one for an item no longer inspected.
func (m *Model) wantSummary() tea.Cmd {
	id := ""
	if m.opts.FetchSummaries && m.showInspector {
		if col := m.columns.Top(); col != nil {
			id = m.missingSummary(col.SelectedItem())
		}
	}
	if id == m.summaryWanted {
		return nil
	}
	m.summaryWanted = id
	m.summarySeq++
	m.requests.stop(summaryOwner)
	if id == "" {
		return nil
	}
	seq := m.summarySeq
	return tea.Tick(summaryDelay, func(time.Time) tea.Msg { return summaryDebounceMsg{Seq: seq, ID: id} })
}

func (m *Model) startSummary(msg summaryDebounceMsg) tea.Cmd {
	if msg.Seq != m.summarySeq || msg.ID != m.summaryWanted {
		return nil
	}
	req := m.requests.begin(summaryOwner, catalog.Resource{}, catalog.Browse)
	svc := m.catalog
	return func() tea.Msg {
		text, err := svc.Summary(req.ctx, msg.ID)
		return summaryMsg{Request: req, ID: msg.ID, Text: text, Err: err}
	}
}

// finishSummary keeps a fetched synopsis. A failed one is asked for again
// only once the item is selected again.
func (m *Model) finishSummary(msg summaryMsg) {
	if !m.requests.owns(msg.Request) {
		return
	}
	m.requests.finish(msg.Request)
	if msg.Err == nil {
		m.summaries[msg.ID] = msg.Text
	}
}
