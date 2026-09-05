package components

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

func searchResults(n int) []search.FilterResult {
	results := make([]search.FilterResult, n)
	for i := range results {
		title := fmt.Sprintf("Movie %02d", i)
		results[i] = search.FilterResult{FilterItem: search.FilterItem{Item: &domain.MediaItem{ID: title, Title: title}, Title: title, LibraryID: "lib"}}
	}
	return results
}

func searchTitlePosition(t *testing.T, view string) (int, int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(view), "\n") {
		if col := strings.Index(line, "Global Search"); col >= 0 {
			return row, lipgloss.Width(line[:col])
		}
	}
	t.Fatal("search title missing")
	return 0, 0
}

func TestSearchModalStaysAnchoredAcrossResultStates(t *testing.T) {
	m := NewGlobalSearch()
	m.Show()
	m.SetSize(120, 40)
	row, col := searchTitlePosition(t, lipgloss.Place(120, 40, lipgloss.Center, lipgloss.Center, m.View()))
	for _, count := range []int{1, 10, 15, 0} {
		m.input.SetValue("movie")
		m.SetResults(searchResults(count))
		for _, pending := range []bool{false, true} {
			m.SetLoading(pending)
			r, c := searchTitlePosition(t, lipgloss.Place(120, 40, lipgloss.Center, lipgloss.Center, m.View()))
			if r != row || c != col {
				t.Errorf("%d results, pending=%v: modal moved from (%d,%d) to (%d,%d)", count, pending, row, col, r, c)
			}
		}
	}
}

func TestSearchRetainsResultsWhilePendingWithoutActivatingThem(t *testing.T) {
	m := NewGlobalSearch()
	m.Show()
	m.SetSize(80, 24)
	m.SetResults(searchResults(3))
	m.SetLoading(true)
	if !strings.Contains(ansi.Strip(m.View()), "Movie 00") {
		t.Fatal("typing erased the previous results")
	}
	_, _, selected := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected || m.Selected() != nil {
		t.Fatal("pending results can be activated for a different query")
	}
}

func TestSearchFitsTerminalWithLongInput(t *testing.T) {
	for _, size := range [][2]int{{180, 50}, {80, 24}, {40, 16}, {24, 10}} {
		m := NewGlobalSearch()
		m.Show()
		m.SetSize(size[0], size[1])
		m.input.SetValue(strings.Repeat("界", 80))
		m.SetResults(searchResults(20))
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Errorf("terminal %dx%d: rendered %dx%d", size[0], size[1], lipgloss.Width(view), lipgloss.Height(view))
		}
	}
}

func TestSearchScrollingSurvivesResizeAndReindex(t *testing.T) {
	m := NewGlobalSearch()
	m.Show()
	m.SetSize(120, 40)
	m.input.SetValue("movie")
	results := searchResults(20)
	m.SetResults(results)
	for range 15 {
		m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.SetSize(40, 12)
	if !strings.Contains(ansi.Strip(m.View()), "Movie 15") {
		t.Fatal("resize scrolled the selection out of view")
	}
	m.SetLoading(true)
	m.SetResults(append(results[10:], results[:10]...))
	if m.Selected() == nil || m.Selected().Item.GetID() != "Movie 15" || !strings.Contains(ansi.Strip(m.View()), "Movie 15") {
		t.Fatal("reindex changed the selection or scrolled it out of view")
	}
	m.input.SetValue("different")
	m.SetLoading(true)
	m.SetResults(results)
	if m.Selected().Item.GetID() != "Movie 00" {
		t.Fatal("a different query did not select its best match")
	}
}

func TestSearchActivityDoesNotFlashForCompletedQueries(t *testing.T) {
	m := NewGlobalSearch()
	m.Show()
	m.SetSize(80, 24)
	m.input.SetValue("movie")
	m.SetLoading(true)
	if strings.Contains(m.View(), "Searching") || strings.Contains(m.View(), "No matches") {
		t.Fatal("pending query immediately displayed transient feedback")
	}
	m.ShowLoading()
	if !strings.Contains(m.View(), "Searching") {
		t.Fatal("slow query has no activity feedback")
	}
	m.SetResults(nil)
	m.ShowLoading()
	if strings.Contains(m.View(), "Searching") || !strings.Contains(m.View(), "No matches") {
		t.Fatal("late indicator obscured a completed query")
	}
}
