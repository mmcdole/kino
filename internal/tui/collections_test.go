package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
)

func TestFailureFromAbandonedViewCannotFailCurrentLoad(t *testing.T) {
	m := testModel(t)
	a, b := catalog.LibraryResource(m.libraries[0]), catalog.LibraryResource(m.libraries[1])
	m.pushColumn(a, "A")
	old := m.requests.active[viewOwner(a)]
	m.handleBack()
	m.pushColumn(b, "B")
	m = updateModel(m, loadDoneMsg{Request: old, Err: errors.New("A failed")})
	if !m.columns.Top().IsLoading() {
		t.Fatal("abandoned request failed the current view")
	}
	if m.notice.Text != "" {
		t.Fatal("abandoned error displayed")
	}
}

func TestBackgroundCompletionUpdatesOpenCachedView(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	view := m.requests.active[viewOwner(r)]
	cached := state(r, 0, "old")
	cached.Snapshot.Validated, cached.Fetching, cached.Attempt = false, true, 1
	m = publish(m, cached)
	col := m.columns.Top()
	if col.IsLoading() || !col.IsRefreshing() || col.ItemCount() != 1 {
		t.Fatal("cached refresh hid content or lost its indicator")
	}
	m.loadResource(r, catalog.Revalidate, true)
	background := m.requests.active[syncOwner(r)]
	m = publish(m, state(r, 1, "new", "old"))
	m = updateModel(m, loadDoneMsg{Request: background})
	if col.ItemCount() != 2 || col.SelectedMediaItem().ID != "old" {
		t.Fatal("sync did not update and preserve selection")
	}
	if !col.IsRefreshing() {
		t.Fatal("one completion cleared another pending request")
	}
	m = updateModel(m, loadDoneMsg{Request: view})
	if col.IsRefreshing() {
		t.Fatal("completed refresh left spinner running")
	}
}

func TestRefreshFailureRetainsContentAndStopsSpinner(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	req := m.requests.active[viewOwner(r)]
	failed := state(r, 1, "old")
	failed.Err = domain.ErrServerOffline
	m = publish(m, failed)
	m = updateModel(m, loadDoneMsg{Request: req, Err: domain.ErrServerOffline})
	col := m.columns.Top()
	if col.IsLoading() || col.IsRefreshing() || col.ItemCount() != 1 || !col.HasLoadFailed() {
		t.Fatal("failed refresh discarded usable view, left spinner, or hid retry")
	}
	if m.notice.Kind != noticeError {
		t.Fatal("failed refresh was silent")
	}
}

func TestColdStartupFailureKeepsRetryableRoot(t *testing.T) {
	m := NewModel(context.Background(), nil, nil, nil, search.NewIndex(), Options{})
	t.Cleanup(m.requests.cancel)
	m.loadResource(catalog.Resource{Kind: catalog.Libraries}, catalog.Revalidate, false)
	r := catalog.Resource{Kind: catalog.Libraries}
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, loadDoneMsg{Request: req, Err: domain.ErrServerOffline})
	if m.columns.Top() == nil || m.columns.Top().IsLoading() {
		t.Fatal("failed startup has no retryable root")
	}
	cmd := m.handleRefresh()
	if cmd == nil {
		t.Fatal("r cannot retry cold startup")
	}
}
