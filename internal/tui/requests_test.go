package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/components"
)

func testModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(context.Background(), nil, nil, search.NewIndex(), config.UIConfig{})
	t.Cleanup(m.requests.cancel)
	m.Libraries = []domain.Library{{ID: "a", Name: "A", Type: "movie"}, {ID: "b", Name: "B", Type: "movie"}}
	m.libraryColumn().SetItems(components.WrapLibraries(m.allLibraryEntries()))
	return m
}
func snapshot(r catalog.Resource, revision uint64, ids ...string) catalog.Snapshot {
	result := catalog.Snapshot{Resource: r, Revision: revision, CachedList: domain.CachedList{FetchedAt: time.Now()}}
	for _, id := range ids {
		result.Items = append(result.Items, &domain.MediaItem{ID: id, Title: id})
	}
	return result
}
func updateModel(m Model, msg tea.Msg) Model { next, _ := m.Update(msg); return next.(Model) }

func TestFailureFromAbandonedViewCannotFailCurrentLoad(t *testing.T) {
	m := testModel(t)
	a, b := catalog.LibraryResource(m.Libraries[0]), catalog.LibraryResource(m.Libraries[1])
	m.pushColumn(a, "A", 0)
	old := m.requests.active[viewOwner(a)]
	next, _ := m.handleBack()
	m = next.(Model)
	m.pushColumn(b, "B", 1)
	m = updateModel(m, ResourceMsg{Request: old, Stage: loadFinished, Err: errors.New("A failed")})
	if !m.ColumnStack.Top().IsLoading() {
		t.Fatal("abandoned request failed the current view")
	}
	if m.notice.Text != "" {
		t.Fatal("abandoned error displayed")
	}
}

func TestLateSameResourceResponseCannotReplaceNewRefresh(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	old := m.requests.active[viewOwner(r)]
	m.loadResource(r, catalog.Refresh, false)
	fresh := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: fresh, Stage: loadFinished, Snapshot: snapshot(r, 2, "new")})
	m = updateModel(m, ResourceMsg{Request: old, Stage: loadFinished, Snapshot: snapshot(r, 1, "old")})
	if got := m.ColumnStack.Top().SelectedMediaItem(); got == nil || got.ID != "new" {
		t.Fatal("late response replaced new data")
	}
}

func TestBackgroundCompletionUpdatesOpenCachedView(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	view := m.requests.active[viewOwner(r)]
	cached := snapshot(r, 0, "old")
	cached.FromCache = true
	m = updateModel(m, ResourceMsg{Request: view, Stage: loadCached, Snapshot: cached})
	col := m.ColumnStack.Top()
	if col.IsLoading() || !col.IsRefreshing() || col.ItemCount() != 1 {
		t.Fatal("cached refresh hid content or lost its indicator")
	}
	m.loadResource(r, catalog.Revalidate, true)
	background := m.requests.active[syncOwner(r)]
	m = updateModel(m, ResourceMsg{Request: background, Stage: loadFinished, Snapshot: snapshot(r, 1, "new", "old")})
	if col.ItemCount() != 2 || col.SelectedMediaItem().ID != "old" {
		t.Fatal("sync did not update and preserve selection")
	}
	if !col.IsRefreshing() {
		t.Fatal("one completion cleared another pending request")
	}
	m = updateModel(m, ResourceMsg{Request: view, Stage: loadFinished, Snapshot: snapshot(r, 1, "new", "old")})
	if col.IsRefreshing() {
		t.Fatal("completed refresh left spinner running")
	}
}

func TestRefreshFailureRetainsContentAndStopsSpinner(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.active[viewOwner(r)]
	cached := snapshot(r, 1, "old")
	cached.FromCache = true
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadCached, Snapshot: cached})
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: cached, Err: domain.ErrServerOffline})
	col := m.ColumnStack.Top()
	if col.IsLoading() || col.IsRefreshing() || col.ItemCount() != 1 {
		t.Fatal("failed refresh discarded usable view or left spinner")
	}
	if m.notice.Kind != NoticeError {
		t.Fatal("failed refresh was silent")
	}
}

func TestAllOperationErrorsUseAuthenticationAlert(t *testing.T) {
	for _, kind := range []string{"browse", "mutation", "playback", "modal"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t)
			req := m.requests.begin(kind, catalog.Resource{}, catalog.Browse)
			var msg tea.Msg
			switch kind {
			case "browse":
				msg = ResourceMsg{Request: req, Stage: loadFinished, Err: domain.ErrAuthFailed}
			case "modal":
				msg = PlaylistModalDataMsg{Request: req, Err: domain.ErrAuthFailed}
			default:
				msg = ActionMsg{Request: req, Playback: kind == "playback", Err: domain.ErrAuthFailed}
			}
			m = updateModel(m, msg)
			if m.notice.Kind != NoticeAlert || m.notice.Text != authFailedStatusMsg {
				t.Fatalf("%s bypassed error classification: %+v", kind, m.notice)
			}
		})
	}
}

func TestModalDismissalAndReplacementRejectLateResponses(t *testing.T) {
	m := testModel(t)
	old := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m.PlaylistModal.BeginLoading(&domain.MediaItem{ID: "old"})
	m.cancelPendingModal()
	m = updateModel(m, PlaylistModalDataMsg{Request: old, Item: domain.MediaItem{ID: "old"}})
	if m.PlaylistModal.IsVisible() {
		t.Fatal("dismissed modal reopened")
	}
	newer := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, PlaylistModalDataMsg{Request: newer, Item: domain.MediaItem{ID: "new"}})
	m = updateModel(m, PlaylistModalDataMsg{Request: old, Item: domain.MediaItem{ID: "old"}})
	if m.PlaylistModal.Item().ID != "new" {
		t.Fatal("late response reset active modal")
	}
}

func TestUnrelatedActionErrorLeavesColumnLoading(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.begin("playback:x", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, ActionMsg{Request: req, Playback: true, Err: errors.New("player unavailable")})
	if !m.ColumnStack.Top().IsLoading() {
		t.Fatal("player error stopped column loading")
	}
}

func TestWatchChangeRejectsAlreadyQueuedPreMutationSnapshot(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	read := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: read, Stage: loadCached, Snapshot: snapshot(r, 1, "movie")})
	write := m.requests.begin("mutation:watch:movie", catalog.Resource{}, catalog.Browse)
	change := catalog.Change{Applied: true, Mutation: catalog.Mutation{Kind: catalog.Watch, ItemID: "movie", Played: true}, Revisions: map[string]uint64{r.Key(): 2}}
	m = updateModel(m, ActionMsg{Request: write, Change: change})
	m = updateModel(m, ResourceMsg{Request: read, Stage: loadFinished, Snapshot: snapshot(r, 1, "movie")})
	if !m.ColumnStack.Top().SelectedMediaItem().IsPlayed {
		t.Fatal("queued snapshot undid watch mutation")
	}
}

func TestColdStartupFailureKeepsRetryableRoot(t *testing.T) {
	m := NewModel(context.Background(), nil, nil, search.NewIndex(), config.UIConfig{})
	t.Cleanup(m.requests.cancel)
	m.Init()
	r := catalog.Resource{Kind: catalog.Libraries}
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Err: domain.ErrServerOffline})
	if m.ColumnStack.Top() == nil || m.ColumnStack.Top().IsLoading() {
		t.Fatal("failed startup has no retryable root")
	}
	_, cmd := m.handleRefresh()
	if cmd == nil {
		t.Fatal("r cannot retry cold startup")
	}
}

func TestRejectedSnapshotCannotOverwriteCurrentLibraryCount(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	old := m.requests.active[viewOwner(r)]
	m.loadResource(r, catalog.Revalidate, true)
	newer := m.requests.active[syncOwner(r)]
	m = updateModel(m, ResourceMsg{Request: newer, Stage: loadFinished, Snapshot: snapshot(r, 2, "one", "two")})
	m = updateModel(m, ResourceMsg{Request: old, Stage: loadFinished, Snapshot: snapshot(r, 1, "old"), Err: domain.ErrServerOffline})
	state := m.LibraryStates[r.LibraryID]
	if state.Total != 2 || state.Error != nil || state.Status == components.StatusSyncing {
		t.Fatalf("stale response corrupted status: %+v", state)
	}
	if m.notice.Text != "" || m.ColumnStack.Top().HasLoadFailed() {
		t.Fatal("obsolete error displayed")
	}
}

func TestRemovedLibraryDetachesRequestsAndNavigation(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	m.loadResource(r, catalog.Revalidate, true)
	late := m.requests.active[syncOwner(r)]
	root := catalog.Resource{Kind: catalog.Libraries}
	m.loadResource(root, catalog.Refresh, false)
	req := m.requests.active[viewOwner(root)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: snapshot(root, 1)})
	if m.ColumnStack.Len() != 1 {
		t.Fatal("removed library remains open")
	}
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("removed library remains in sync status")
	}
	if m.requests.owns(late) || late.ctx.Err() == nil {
		t.Fatal("removed subscription remains active")
	}
	m = updateModel(m, ResourceMsg{Request: late, Stage: loadFinished, Snapshot: snapshot(r, 2, "late")})
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("late response recreated removed library")
	}
}
