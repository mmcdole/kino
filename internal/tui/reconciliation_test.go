package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/store"
)

func TestMutationBeforeQueuedInitialLoad(t *testing.T) {
	m := testModel(t)
	cache, err := store.Open("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	backend := &browsingBackend{gate: make(chan struct{})}
	close(backend.gate)
	svc := catalog.NewService(context.Background(), backend, cache)
	defer svc.Close()
	m.Catalog = svc
	r := catalog.LibraryResource(m.Libraries[0])
	cmd := m.pushColumn(r, "A")
	queued := awaitResource(t, cmd)
	for queued.Stage != loadFinished {
		queued = awaitResource(t, queued.Next)
	}
	m.navPlan = &NavPlan{Targets: []string{"movie"}, AwaitKey: r.Key()}
	mutation := m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: "movie", LibraryID: r.LibraryID, Played: true})
	m = updateModel(m, mutation())
	m = updateModel(m, queued)
	if !m.ColumnStack.Top().HasContent() && !m.ColumnStack.Top().IsLoading() {
		t.Fatal("completed read rejected after mutation; column has no content and no replacement load")
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
	m.loadResource(root, catalog.Refresh, false)
	m = updateModel(m, ResourceMsg{Request: m.requests.active[viewOwner(root)], Stage: loadFinished, Snapshot: snapshot(root, 1)})
	m = updateModel(m, ActionMsg{Request: write, Change: catalog.Change{Resources: []catalog.Resource{r}}, Err: domain.ErrServerOffline})
	if m.collections[r.Key()] != nil {
		t.Fatal("mutation recovery recreated a removed collection")
	}
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("mutation recovery recreated a removed library's feedback")
	}
}

func TestWatchCompletionAfterBackUpdatesParent(t *testing.T) {
	m := testModel(t)
	cache, err := store.Open("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
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
		msg := awaitResource(t, m.pushColumn(entry.r, "Content"))
		m = updateModel(m, msg)
		if msg.Stage != loadFinished {
			t.Fatal("expected a fresh cache hit")
		}
	}
	cmd := m.beginMutation(catalog.Mutation{Kind: catalog.Watch, ItemID: "episode", LibraryID: "a", Played: true})
	next, _ := m.handleBack()
	m = next.(Model)
	m = updateModel(m, cmd())
	if m.ColumnStack.Top().SelectedSeason().UnwatchedCount != 0 || m.ColumnStack.Get(1).SelectedShow().UnwatchedCount != 0 {
		t.Fatal("successful episode watch after Back leaves visible parent counts unchanged")
	}
}

func TestFilterKeepsInspectorOnSelectedItem(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: snapshot(r, 1, "Alpha", "Beta")})
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

func TestObsoleteCompletionRecoversMissingSnapshot(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	old := m.requests.active[viewOwner(r)]
	m.collection(r).RequiredRevision = 2
	m = updateModel(m, ResourceMsg{Request: old, Stage: loadFinished, Snapshot: snapshot(r, 1, "old")})
	recovery, ok := m.requests.active[syncOwner(r)]
	if !ok || recovery.Revision != 2 || !m.ColumnStack.Top().IsLoading() {
		t.Fatal("obsolete completion did not request its missing replacement")
	}
	m = updateModel(m, ResourceMsg{Request: recovery, Stage: loadFinished, Err: domain.ErrServerOffline})
	if !m.ColumnStack.Top().HasLoadFailed() || m.requests.owns(recovery) {
		t.Fatal("failed recovery must stop and expose retry, not loop")
	}
}

func TestFailedAttemptReportsErrorDespiteObsoleteFallback(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: snapshot(r, 2, "current")})
	m.loadResource(r, catalog.Refresh, false)
	req = m.requests.active[viewOwner(r)]
	fallback := snapshot(r, 1, "old")
	fallback.FromCache, fallback.Validated = true, false
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: fallback, Err: domain.ErrServerOffline})
	if m.ColumnStack.Top().SelectedItem().GetID() != "current" || !m.ColumnStack.Top().HasLoadFailed() || !errors.Is(m.collection(r).Error, domain.ErrServerOffline) {
		t.Fatal("an obsolete fallback must not hide a current failure or replace retained content")
	}
}

func TestDuplicateRevisionFinishesSubscriberWithoutReplacingProjection(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	view := m.requests.active[viewOwner(r)]
	m.loadResource(r, catalog.Revalidate, true)
	background := m.requests.active[syncOwner(r)]
	m = updateModel(m, ResourceMsg{Request: view, Stage: loadFinished, Snapshot: snapshot(r, 2, "movie")})
	selected := m.ColumnStack.Top().SelectedItem()
	m = updateModel(m, ResourceMsg{Request: background, Stage: loadFinished, Snapshot: snapshot(r, 2, "movie")})
	if m.ColumnStack.Top().SelectedItem() != selected || m.ColumnStack.Top().IsRefreshing() {
		t.Fatal("duplicate revision rebuilt content or failed to complete its subscriber")
	}
	next, _ := m.handleBack()
	m = next.(Model)
	m.pushColumn(r, "A")
	if !m.ColumnStack.Top().HasContent() {
		t.Fatal("reopened column did not receive the accepted snapshot")
	}
}
