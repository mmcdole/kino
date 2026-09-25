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

func testModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(context.Background(), nil, nil, search.NewIndex(), config.UIConfig{})
	t.Cleanup(m.requests.cancel)
	m.Libraries = []domain.Library{{ID: "a", Name: "A", Type: "movie"}, {ID: "b", Name: "B", Type: "movie"}}
	m.libraryColumn().SetItems(components.WrapLibraries(m.allLibraryEntries()))
	return m
}
func snapshot(r catalog.Resource, revision uint64, ids ...string) catalog.Snapshot {
	result := catalog.Snapshot{Resource: r, Revision: revision, Validated: true, CachedList: domain.CachedList{FetchedAt: time.Now()}}
	for _, id := range ids {
		result.Items = append(result.Items, &domain.MediaItem{ID: id, Title: id})
	}
	return result
}

// state is a published collection state holding a validated snapshot.
func state(r catalog.Resource, revision uint64, ids ...string) catalog.State {
	return catalog.State{Resource: r, Known: true, Snapshot: snapshot(r, revision, ids...)}
}
func updateModel(m *Model, msg tea.Msg) *Model { m.Update(msg); return m }
func publish(m *Model, states ...catalog.State) *Model {
	return updateModel(m, StatesMsg(states))
}

func TestFailureFromAbandonedViewCannotFailCurrentLoad(t *testing.T) {
	m := testModel(t)
	a, b := catalog.LibraryResource(m.Libraries[0]), catalog.LibraryResource(m.Libraries[1])
	m.pushColumn(a, "A")
	old := m.requests.active[viewOwner(a)]
	m.handleBack()
	m.pushColumn(b, "B")
	m = updateModel(m, LoadDoneMsg{Request: old, Err: errors.New("A failed")})
	if !m.ColumnStack.Top().IsLoading() {
		t.Fatal("abandoned request failed the current view")
	}
	if m.notice.Text != "" {
		t.Fatal("abandoned error displayed")
	}
}

func TestBackgroundCompletionUpdatesOpenCachedView(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	view := m.requests.active[viewOwner(r)]
	cached := state(r, 0, "old")
	cached.Snapshot.FromCache, cached.Snapshot.Validated, cached.Fetching, cached.Attempt = true, false, true, 1
	m = publish(m, cached)
	col := m.ColumnStack.Top()
	if col.IsLoading() || !col.IsRefreshing() || col.ItemCount() != 1 {
		t.Fatal("cached refresh hid content or lost its indicator")
	}
	m.loadResource(r, catalog.Revalidate, true)
	background := m.requests.active[syncOwner(r)]
	m = publish(m, state(r, 1, "new", "old"))
	m = updateModel(m, LoadDoneMsg{Request: background})
	if col.ItemCount() != 2 || col.SelectedMediaItem().ID != "old" {
		t.Fatal("sync did not update and preserve selection")
	}
	if !col.IsRefreshing() {
		t.Fatal("one completion cleared another pending request")
	}
	m = updateModel(m, LoadDoneMsg{Request: view})
	if col.IsRefreshing() {
		t.Fatal("completed refresh left spinner running")
	}
}

func TestRefreshFailureRetainsContentAndStopsSpinner(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	req := m.requests.active[viewOwner(r)]
	failed := state(r, 1, "old")
	failed.Err = domain.ErrServerOffline
	m = publish(m, failed)
	m = updateModel(m, LoadDoneMsg{Request: req, Err: domain.ErrServerOffline})
	col := m.ColumnStack.Top()
	if col.IsLoading() || col.IsRefreshing() || col.ItemCount() != 1 || !col.HasLoadFailed() {
		t.Fatal("failed refresh discarded usable view, left spinner, or hid retry")
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
				msg = LoadDoneMsg{Request: req, Err: domain.ErrAuthFailed}
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
	m.pushColumn(r, "A")
	req := m.requests.begin("playback:x", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, ActionMsg{Request: req, Playback: true, Err: errors.New("player unavailable")})
	if !m.ColumnStack.Top().IsLoading() {
		t.Fatal("player error stopped column loading")
	}
}

func TestColdStartupFailureKeepsRetryableRoot(t *testing.T) {
	m := NewModel(context.Background(), nil, nil, search.NewIndex(), config.UIConfig{})
	t.Cleanup(m.requests.cancel)
	m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false)
	r := catalog.Resource{Kind: catalog.Libraries}
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, LoadDoneMsg{Request: req, Err: domain.ErrServerOffline})
	if m.ColumnStack.Top() == nil || m.ColumnStack.Top().IsLoading() {
		t.Fatal("failed startup has no retryable root")
	}
	cmd := m.handleRefresh()
	if cmd == nil {
		t.Fatal("r cannot retry cold startup")
	}
}

func TestRemovedLibraryDetachesRequestsAndNavigation(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A")
	m.loadResource(r, catalog.Revalidate, true)
	late := m.requests.active[syncOwner(r)]
	root := catalog.Resource{Kind: catalog.Libraries}
	m = publish(m, catalog.State{Resource: root, Known: true, Snapshot: snapshot(root, 1)})
	if m.ColumnStack.Len() != 1 {
		t.Fatal("removed library remains open")
	}
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("removed library remains in sync status")
	}
	if m.requests.owns(late) || late.ctx.Err() == nil {
		t.Fatal("removed subscription remains active")
	}
	m = publish(m, state(r, 2, "late"))
	m = updateModel(m, LoadDoneMsg{Request: late})
	if _, ok := m.LibraryStates[r.LibraryID]; ok {
		t.Fatal("late state recreated removed library")
	}
	if _, ok := m.collections[r.Key()]; ok {
		t.Fatal("late state recreated removed collection")
	}
}

func TestPlaylistRemovalsForDifferentItemsAreIndependent(t *testing.T) {
	m := testModel(t)
	first := m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})
	second := m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: "p", ItemID: "b"})
	if first == nil || second == nil {
		t.Fatal("a pending removal blocked a different item")
	}
	if m.requests.active[mutationOwner(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})].ID == 0 {
		t.Fatal("first removal is not pending")
	}
	m.beginMutation(catalog.Mutation{Kind: catalog.RemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})
	if m.notice.Text == "" {
		t.Fatal("duplicate removal was ignored silently")
	}
}
