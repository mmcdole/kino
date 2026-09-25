package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

func TestMutationAdvancesPendingNavigation(t *testing.T) {
	m := testModel(t)
	cache := store.NewMemory()
	backend := &browsingBackend{gate: make(chan struct{})}
	close(backend.gate)
	svc := catalog.NewService(context.Background(), backend, cache)
	defer svc.Close()
	m.Catalog = svc
	r := catalog.LibraryResource(m.Libraries[0])
	m = await(t, m, svc, start(m.pushColumn(r, "A")))
	m.navPlan = &NavPlan{Targets: []string{"movie"}, AwaitKey: r.Key()}
	m = await(t, m, svc, start(m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: "movie", LibraryID: r.LibraryID, Played: true})))
	if !m.ColumnStack.Top().SelectedMediaItem().IsPlayed {
		t.Fatal("mutation did not reach the open column")
	}
	if m.navPlan != nil {
		t.Fatal("mutation snapshot did not advance pending navigation")
	}
}

func TestUncertainMutationDoesNotRestoreRemovedLibrary(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	write := m.requests.begin("mutation:watch:movie", catalog.Resource{}, catalog.Browse)
	root := catalog.Resource{Kind: catalog.Libraries}
	m = publish(m, catalog.State{Resource: root, Known: true, Snapshot: snapshot(root, 1)})
	m = updateModel(m, ActionMsg{Request: write, Change: catalog.Change{Resources: []catalog.Resource{r}}, Err: domain.ErrServerOffline})
	if _, ok := m.collections[r.Key()]; ok {
		t.Fatal("mutation recovery recreated a removed collection")
	}
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("mutation recovery recreated a removed library's feedback")
	}
}

func TestWatchCompletionAfterBackUpdatesParent(t *testing.T) {
	m := testModel(t)
	cache := store.NewMemory()
	svc := catalog.NewService(context.Background(), &browsingBackend{}, cache)
	defer svc.Close()
	m.Catalog = svc
	shows := catalog.Resource{Kind: catalog.Shows, ID: "a", LibraryID: "a"}
	seasons := catalog.Resource{Kind: catalog.Seasons, ID: "show", ShowID: "show", LibraryID: "a"}
	episodes := catalog.Resource{Kind: catalog.Episodes, ID: "season", ShowID: "show", LibraryID: "a"}
	for _, entry := range []struct {
		r    catalog.Resource
		item domain.ListItem
	}{
		{shows, &domain.Show{ID: "show", Title: "Show", EpisodeCount: 1, UnwatchedCount: 1}},
		{seasons, &domain.Season{ID: "season", ShowID: "show", EpisodeCount: 1, UnwatchedCount: 1}},
		{episodes, &domain.MediaItem{ID: "episode", ShowID: "show", ParentID: "season"}},
	} {
		if err := cache.Save(entry.r.Key(), domain.CachedList{Items: []domain.ListItem{entry.item}, FetchedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		m = await(t, m, svc, start(m.pushColumn(entry.r, "Content")))
	}
	cmd := m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: "episode", ShowID: "show", SeasonID: "season", LibraryID: "a", Played: true})
	next, _ := m.handleBack()
	m = next.(Model)
	m = await(t, m, svc, start(cmd))
	if m.ColumnStack.Top().SelectedItem().(*domain.Season).UnwatchedCount != 0 || m.ColumnStack.Get(1).SelectedItem().(*domain.Show).UnwatchedCount != 0 {
		t.Fatal("successful episode watch after Back leaves visible parent counts unchanged")
	}
}

func TestFilterKeepsInspectorOnSelectedItem(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	m = publish(m, state(r, 1, "Alpha", "Beta"))
	m.Inspector.SetSize(40, 20)
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	if m.ColumnStack.Top().SelectedItem().GetID() != "Beta" {
		t.Fatal("filter did not select Beta")
	}
	if !strings.Contains(m.Inspector.View(), "Beta") {
		t.Fatal("selected Beta at cursor 0 but inspector still renders Alpha")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.Inspector.View(), "Alpha") {
		t.Fatal("clearing the filter did not restore inspector selection")
	}
}

func TestSameRevisionDoesNotRebuildColumn(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	m = publish(m, state(r, 2, "movie"))
	selected := m.ColumnStack.Top().SelectedItem()
	m = publish(m, state(r, 2, "movie"))
	if m.ColumnStack.Top().SelectedItem() != selected {
		t.Fatal("an unchanged revision rebuilt the column")
	}
	next, _ := m.handleBack()
	m = next.(Model)
	m.pushColumn(r, "A")
	if !m.ColumnStack.Top().HasContent() {
		t.Fatal("reopened column did not show the retained snapshot")
	}
}
