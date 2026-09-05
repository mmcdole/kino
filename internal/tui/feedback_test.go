package tui

import (
	"strings"
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
)

func showNetwork(m Model, req request) Model {
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadNetwork})
	return updateModel(m, ShowLoadingMsg{Request: req})
}

func TestCachedNavigationIsQuietAndHonorsCountPreference(t *testing.T) {
	for _, showCounts := range []bool{false, true} {
		m := testModel(t)
		m.libraryColumn().SetSize(40, 20)
		m.libraryColumn().SetShowLibraryCounts(showCounts)
		r := catalog.LibraryResource(m.Libraries[0])
		m.pushColumn(r, "A", 0)
		req := m.requests.active[viewOwner(r)]
		cached := snapshot(r, 1, "one", "two")
		cached.FromCache, cached.Validated = true, false
		m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: cached})
		row := m.libraryColumn().View()
		if strings.Contains(row, "✓") || strings.Contains(row, styles.SpinnerFrames[0]) || m.activeSyncCount() != 0 {
			t.Fatal("cache hit announced synchronization")
		}
		if strings.Contains(row, "A (2)") != showCounts {
			t.Fatal("count preference ignored")
		}
		if m.notice.Text != "" {
			t.Fatal("routine read produced a notice")
		}
	}
}

func TestNetworkIndicatorIsDelayedAndLateTimerCannotReviveIt(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadNetwork})
	if m.activeSyncCount() != 0 {
		t.Fatal("spinner shown before delay")
	}
	m = updateModel(m, ShowLoadingMsg{Request: req})
	if m.activeSyncCount() != 1 {
		t.Fatal("slow server request has no indicator")
	}
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: snapshot(r, 1, "one")})
	m = updateModel(m, ShowLoadingMsg{Request: req})
	if m.activeSyncCount() != 0 || m.ColumnStack.Top().IsRefreshing() {
		t.Fatal("late timer revived completed activity")
	}

	m.loadResource(r, catalog.Refresh, false)
	replaced := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: replaced, Stage: loadNetwork})
	m.loadResource(r, catalog.Refresh, false)
	m = updateModel(m, ShowLoadingMsg{Request: replaced})
	if m.activeSyncCount() != 0 {
		t.Fatal("replaced request's timer activated new request")
	}
}

func TestProgressDoesNotReplaceCompleteCount(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadCached, Snapshot: snapshot(r, 4, "one", "two")})
	m = showNetwork(m, req)
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadProgress, Progress: catalog.Progress{Loaded: 1, Total: 10}})
	state := m.LibraryStates[r.LibraryID]
	if !state.Summary.Known || state.Summary.Count != 2 || state.Activity.Loaded != 1 || state.Activity.Total != 10 {
		t.Fatalf("progress and collection summary conflated: %+v", state)
	}
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Err: domain.ErrServerOffline})
	if m.LibraryStates[r.LibraryID].Summary.Count != 2 {
		t.Fatal("failure discarded known count")
	}
}

func TestCachedReadCannotClearRefreshError(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.active[viewOwner(r)]
	cached := snapshot(r, 0, "one")
	cached.FromCache, cached.Validated = true, false
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: cached, Err: domain.ErrServerOffline})
	m.loadResource(r, catalog.Browse, false)
	req = m.requests.active[viewOwner(r)]
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: cached})
	if m.LibraryStates[r.LibraryID].Error == nil || !m.ColumnStack.Top().HasLoadFailed() {
		t.Fatal("cache hit claimed server recovery")
	}
	// A server count check can clear the error while retaining the cached payload.
	m.loadResource(r, catalog.Revalidate, true)
	req = m.requests.active[syncOwner(r)]
	cached.Validated, cached.Revision = true, 1
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: cached})
	if m.LibraryStates[r.LibraryID].Error != nil || m.ColumnStack.Top().HasLoadFailed() {
		t.Fatal("validated result did not clear error")
	}
}

func TestNetworkIndicatorTracksAllSubscribersAndNavigation(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	view := m.requests.active[viewOwner(r)]
	m.loadResource(r, catalog.Revalidate, true)
	sync := m.requests.active[syncOwner(r)]
	m = showNetwork(showNetwork(m, view), sync)
	m = updateModel(m, ResourceMsg{Request: sync, Stage: loadFinished, Snapshot: snapshot(r, 1, "one")})
	if m.activeSyncCount() != 1 {
		t.Fatal("one subscriber stopped another's indicator")
	}
	next, _ := m.handleBack()
	m = next.(Model)
	m = updateModel(m, ShowLoadingMsg{Request: view})
	if m.activeSyncCount() != 0 {
		t.Fatal("abandoned view left library spinning")
	}
}

func TestFailureAfterCachedObservationKeepsSummaryAndShowsRetry(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.Libraries[0])
	m.pushColumn(r, "A", 0)
	req := m.requests.active[viewOwner(r)]
	cached := snapshot(r, 4, "one", "two")
	cached.FromCache, cached.Validated = true, false
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadCached, Snapshot: cached})
	// A missing resource has no usable fallback. A failed fetch's timestamp
	// must not turn an empty failure payload into a new collection summary.
	failed := snapshot(r, 0)
	failed.Validated = false
	m = updateModel(m, ResourceMsg{Request: req, Stage: loadFinished, Snapshot: failed, Err: domain.ErrItemNotFound})
	if m.LibraryStates[r.LibraryID].Summary.Count != 2 || m.LibraryStates[r.LibraryID].Error == nil || !m.ColumnStack.Top().HasLoadFailed() {
		t.Fatal("failed revalidation replaced cached summary or lost its retry state")
	}
}
