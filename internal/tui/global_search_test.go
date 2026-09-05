package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

func TestGlobalSearchRapidTypingKeepsModalAndResultsStable(t *testing.T) {
	m := updateModel(testModel(t), tea.WindowSizeMsg{Width: 120, Height: 40})
	m.GlobalSearch.Show()
	position := func() int { return strings.Index(ansi.Strip(m.View()), "Global Search") }
	anchor := position()
	assertStable := func() {
		t.Helper()
		if position() != anchor {
			t.Fatal("search modal moved as the query changed")
		}
	}
	results := func(prefix string, n int) []search.FilterResult {
		items := make([]search.FilterResult, n)
		for i := range items {
			title := fmt.Sprintf("%s %02d", prefix, i)
			items[i] = search.FilterResult{FilterItem: search.FilterItem{Item: &domain.MediaItem{ID: title, Title: title}, Title: title, LibraryID: "a"}}
		}
		return items
	}
	typeQuery := func(value string) {
		m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		assertStable()
	}
	startQuery := func() request {
		m = updateModel(m, SearchDebounceMsg{Seq: m.searchSeq, Query: m.GlobalSearch.Query()})
		return m.requests.active["search"]
	}

	typeQuery("m")
	first := startQuery()
	m = updateModel(m, SearchResultsMsg{Request: first, Results: results("Movie", 15)})
	assertStable()
	typeQuery("o")
	oldSeq := m.searchSeq
	old := startQuery()
	typeQuery("v")
	m = updateModel(m, ShowSearchLoadingMsg{Seq: oldSeq})
	m = updateModel(m, SearchResultsMsg{Request: old, Results: results("Obsolete", 1)})
	if strings.Contains(m.View(), "Searching") || strings.Contains(m.View(), "Obsolete") || !strings.Contains(m.View(), "Movie 00") {
		t.Fatal("stale search messages flashed or replaced retained results")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.GlobalSearch.IsVisible() {
		t.Fatal("Enter activated a result while the query was pending")
	}
	current := startQuery()
	m = updateModel(m, ShowSearchLoadingMsg{Seq: m.searchSeq})
	if !strings.Contains(m.View(), "Searching") || !strings.Contains(m.View(), "Movie 00") {
		t.Fatal("slow query did not retain results alongside activity feedback")
	}
	assertStable()
	m = updateModel(m, SearchResultsMsg{Request: current, Results: results("Current", 1)})
	m = updateModel(m, ShowSearchLoadingMsg{Seq: m.searchSeq})
	if strings.Contains(m.View(), "Searching") || !strings.Contains(m.View(), "Current 00") {
		t.Fatal("completed query did not settle")
	}
	assertStable()
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	assertStable()
	if m.GlobalSearch.ResultCount() != 0 || m.GlobalSearch.Query() != "" {
		t.Fatal("clearing the query left old results")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, ShowSearchLoadingMsg{Seq: m.searchSeq - 1})
	m = updateModel(m, SearchResultsMsg{Request: current, Results: results("Late", 10)})
	if m.GlobalSearch.IsVisible() || m.GlobalSearch.ResultCount() != 0 {
		t.Fatal("late messages changed a closed search")
	}
}
