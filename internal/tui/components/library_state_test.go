package components

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
)

func TestLibraryCountVisibilityIsIndependentOfActivity(t *testing.T) {
	for _, show := range []bool{false, true} {
		for _, count := range []int{0, 12} {
			for _, active := range []bool{false, true} {
				c := NewListColumn(ColumnTypeLibraries, "Libraries")
				c.SetShowLibraryCounts(show)
				c.SetItems([]domain.ListItem{&domain.Library{ID: "lib", Name: "Movies"}})
				c.SetSize(40, 20)
				c.SetLibraryStates(map[string]CollectionFeedback{"lib": {
					Summary:  CollectionSummary{Known: true, Count: count},
					Activity: LoadActivity{Visible: active, Loaded: 3, Total: 20},
				}})
				view := c.View()
				if strings.Contains(view, "Movies (") != show || strings.Contains(view, "3/20") || strings.Contains(view, "✓") {
					t.Fatalf("count visibility depends on activity: show=%v active=%v count=%d: %s", show, active, count, view)
				}
			}
		}
	}
}

func TestInspectorDistinguishesUnknownZeroAndProgress(t *testing.T) {
	i := NewInspector()
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {Activity: LoadActivity{Visible: true, Loaded: 3, Total: 20}}})
	if strings.Contains(i.renderLibraryFeedback("lib"), "Items:") {
		t.Fatal("unknown count rendered as a partial or zero count")
	}
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {
		Summary:  CollectionSummary{Known: true, Count: 0},
		Activity: LoadActivity{Visible: true, Loaded: 3, Total: 20},
	}})
	view := i.renderLibraryFeedback("lib")
	if !strings.Contains(view, "Items: 0") || !strings.Contains(view, "Loading: 3/20") {
		t.Fatal("zero and progress not distinguished")
	}
	i.SetLibraryStates(map[string]CollectionFeedback{"lib": {
		Summary: CollectionSummary{Known: true, Count: 12, Stale: true}, Error: errors.New("offline"),
	}})
	view = i.renderLibraryFeedback("lib")
	if !strings.Contains(view, "Items: 12 (cached)") || !strings.Contains(view, "r to retry") {
		t.Fatal("offline fallback or recovery hint missing")
	}
}

func TestColumnIndicatorDoesNotMoveTitleOrHideContent(t *testing.T) {
	c := NewListColumn(ColumnTypeMovies, "Movies")
	c.SetSize(40, 20)
	c.SetItems(testMovies("Alpha"))
	c.SetFeedback(CollectionFeedback{Pending: true})
	before := strings.Split(c.renderContent(), "\n")[0]
	c.SetFeedback(CollectionFeedback{Pending: true, Activity: LoadActivity{Visible: true}})
	during := strings.Split(c.renderContent(), "\n")[0]
	beforePrefix := before[:strings.Index(before, "Movies")]
	duringPrefix := during[:strings.Index(during, "Movies")]
	if lipgloss.Width(beforePrefix) != lipgloss.Width(duringPrefix) {
		t.Fatal("loading indicator moved the title")
	}
	if !strings.Contains(c.View(), "Alpha") {
		t.Fatal("refresh hides cached items")
	}
}

func TestFailedRefreshKeepsKnownEmptyCollectionAndRetry(t *testing.T) {
	c := NewListColumn(ColumnTypeMovies, "Movies")
	c.SetSize(40, 20)
	c.SetItems(nil)
	c.SetFeedback(CollectionFeedback{Error: errors.New("offline")})
	view := c.View()
	if !strings.Contains(view, "No items") || !strings.Contains(view, "Refresh failed") || !strings.Contains(view, "r to retry") {
		t.Fatal("known empty collection lost its meaning after failed refresh")
	}
}
