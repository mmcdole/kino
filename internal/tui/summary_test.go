package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/tui/components"
)

type summaryCatalog struct {
	Catalog
	asked []string
}

func (c *summaryCatalog) Summary(_ context.Context, id string) (string, error) {
	c.asked = append(c.asked, id)
	return "Synopsis of " + id, nil
}

func summaryModel(t *testing.T, items ...media.ListItem) (*Model, *summaryCatalog) {
	t.Helper()
	m := testModel(t)
	svc := &summaryCatalog{}
	m.catalog = svc
	m.opts.FetchSummaries = true
	col := components.NewListColumn("A", components.ColumnOptions{})
	col.SetItems(items)
	m.columns.Push(col)
	updateModel(m, tea.WindowSizeMsg{Width: 200, Height: 50})
	return m, svc
}

// fire delivers a scheduled debounce as if its delay had passed, runs the
// request and applies its result.
func fire(m *Model, id string) {
	msg := m.startSummary(summaryDebounceMsg{Seq: m.summarySeq, ID: id})
	if msg == nil {
		return
	}
	updateModel(m, msg())
}

func TestInspectorFetchesMissingSummary(t *testing.T) {
	movie := &media.Item{ID: "m1", Title: "Movie", Type: media.TypeMovie}
	m, svc := summaryModel(t, movie)
	if m.summaryWanted != "" {
		t.Fatal("asked for a summary with the inspector closed")
	}

	m.handleToggleInspector()
	updateModel(m, tea.WindowSizeMsg{Width: 200, Height: 50})
	if m.summaryWanted != "m1" {
		t.Fatalf("wanted = %q", m.summaryWanted)
	}
	fire(m, "m1")
	if len(svc.asked) != 1 || !strings.Contains(m.inspector.View(), "Synopsis of m1") {
		t.Fatalf("asked %v; inspector:\n%s", svc.asked, m.inspector.View())
	}
	if movie.Summary != "" {
		t.Fatal("the column's item was changed")
	}

	// Remembered: closing and reopening doesn't ask again.
	m.handleToggleInspector()
	updateModel(m, struct{}{})
	m.handleToggleInspector()
	updateModel(m, struct{}{})
	if m.summaryWanted != "" || len(svc.asked) != 1 {
		t.Fatalf("asked again: %v", svc.asked)
	}
}

// Moving on before the delay drops the earlier item's request.
func TestSummarySkipsItemsScrolledPast(t *testing.T) {
	m, svc := summaryModel(t,
		&media.Item{ID: "a", Title: "A", Type: media.TypeMovie},
		&media.Item{ID: "b", Title: "B", Type: media.TypeMovie},
	)
	m.handleToggleInspector()
	updateModel(m, struct{}{})
	stale := summaryDebounceMsg{Seq: m.summarySeq, ID: "a"}
	m.columns.Top().SetSelectedIndex(1)
	updateModel(m, struct{}{})
	if cmd := m.startSummary(stale); cmd != nil {
		t.Fatal("stale debounce started a request")
	}
	fire(m, "b")
	if strings.Join(svc.asked, ",") != "b" {
		t.Fatalf("asked %v", svc.asked)
	}
}

func TestSummaryNotFetchedWhenListed(t *testing.T) {
	m, _ := summaryModel(t, &media.Show{ID: "s", Title: "S", Summary: "Already here"})
	m.handleToggleInspector()
	updateModel(m, struct{}{})
	if m.summaryWanted != "" {
		t.Fatal("asked for a summary the listing carried")
	}
	m.opts.FetchSummaries = false
	m.columns.Top().SetItems([]media.ListItem{&media.Item{ID: "x", Title: "X"}})
	updateModel(m, struct{}{})
	if m.summaryWanted != "" {
		t.Fatal("asked a server whose listings carry summaries")
	}
}
