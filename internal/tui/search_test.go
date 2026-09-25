package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/store"
)

func TestGlobalSearchRapidTypingKeepsModalAndResultsStable(t *testing.T) {
	m := updateModel(testModel(t), tea.WindowSizeMsg{Width: 120, Height: 40})
	m.handleGlobalSearch()
	position := func() int { return strings.Index(ansi.Strip(m.View()), "Global Search") }
	anchor := position()
	assertStable := func() {
		t.Helper()
		if position() != anchor {
			t.Fatal("search modal moved as the query changed")
		}
	}
	results := func(prefix string, n int) []search.Result {
		items := make([]search.Result, n)
		for i := range items {
			title := fmt.Sprintf("%s %02d", prefix, i)
			items[i] = search.Result{Entry: search.Entry{Item: &media.Item{ID: title, Title: title}, Title: title, LibraryID: "a"}}
		}
		return items
	}
	typeQuery := func(value string) {
		m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		assertStable()
	}
	startQuery := func() request {
		m = updateModel(m, searchDebounceMsg{Seq: m.searchSeq, Query: m.globalSearch.Query()})
		return m.requests.active["search"]
	}

	typeQuery("m")
	first := startQuery()
	m = updateModel(m, searchResultsMsg{Request: first, Results: results("Movie", 15)})
	assertStable()
	typeQuery("o")
	oldSeq := m.searchSeq
	old := startQuery()
	typeQuery("v")
	m = updateModel(m, showSearchLoadingMsg{Seq: oldSeq})
	m = updateModel(m, searchResultsMsg{Request: old, Results: results("Obsolete", 1)})
	if strings.Contains(m.View(), "Searching") || strings.Contains(m.View(), "Obsolete") || !strings.Contains(m.View(), "Movie 00") {
		t.Fatal("stale search messages flashed or replaced retained results")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlaySearch {
		t.Fatal("Enter activated a result while the query was pending")
	}
	current := startQuery()
	m = updateModel(m, showSearchLoadingMsg{Seq: m.searchSeq})
	if !strings.Contains(m.View(), "Searching") || !strings.Contains(m.View(), "Movie 00") {
		t.Fatal("slow query did not retain results alongside activity feedback")
	}
	assertStable()
	m = updateModel(m, searchResultsMsg{Request: current, Results: results("Current", 1)})
	m = updateModel(m, showSearchLoadingMsg{Seq: m.searchSeq})
	if strings.Contains(m.View(), "Searching") || !strings.Contains(m.View(), "Current 00") {
		t.Fatal("completed query did not settle")
	}
	assertStable()
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	assertStable()
	if m.globalSearch.ResultCount() != 0 || m.globalSearch.Query() != "" {
		t.Fatal("clearing the query left old results")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(m, showSearchLoadingMsg{Seq: m.searchSeq - 1})
	m = updateModel(m, searchResultsMsg{Request: current, Results: results("Late", 10)})
	if m.overlay == overlaySearch || m.globalSearch.ResultCount() != 0 {
		t.Fatal("late messages changed a closed search")
	}
}

// A search result in a library whose snapshot is cached and fresh must still
// be selected: the catalog answers from cache without publishing a new state.
func TestSearchNavigationCompletesFromFreshCache(t *testing.T) {
	m := testModel(t)
	cache := store.NewMemory()
	backend := &browsingBackend{gate: make(chan struct{})} // any fetch would block
	svc := catalog.NewService(context.Background(), backend, cache)
	defer svc.Close()
	m.catalog = svc
	r := catalog.LibraryResource(m.libraries[0])
	if err := cache.Save(r.Key(), media.CachedList{
		Items: []media.ListItem{
			&media.Item{ID: "first", Title: "First"},
			&media.Item{ID: "target", Title: "Prince of Darkness"},
		},
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// Background sync has already published the library's snapshot.
	m = await(t, m, svc, start(m.loadResource(r, catalog.Browse, true)))

	entry := search.Entry{Item: &media.Item{ID: "target", Title: "Prince of Darkness"}, LibraryID: r.LibraryID, Type: media.TypeMovie}
	m = await(t, m, svc, start(m.navigateToSearchResult(entry)))
	if m.pendingSelect != nil {
		t.Fatal("navigation still waiting for a state the catalog never publishes")
	}
	if got := m.columns.Top().SelectedMediaItem(); got == nil || got.ID != "target" {
		t.Fatalf("search result not selected: %+v", got)
	}
}

// A show search result is selected in its library and then opened.
func TestSearchNavigationOpensShow(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	entry := search.Entry{Item: &media.Show{ID: "show", Title: "Show"}, LibraryID: r.LibraryID, Type: media.TypeShow}
	m.navigateToSearchResult(entry)

	st := state(r, 1)
	st.Snapshot.Items = []media.ListItem{&media.Show{ID: "other", Title: "Other"}, &media.Show{ID: "show", Title: "Show"}}
	m = publish(m, st)
	if m.pendingSelect != nil {
		t.Fatal("selection still pending after its content arrived")
	}
	seasons, ok := m.resource(m.columns.Top().ContentID())
	if !ok || seasons.Kind != catalog.Seasons || seasons.ShowID != "show" {
		t.Fatalf("show not opened: %+v", seasons)
	}
}
