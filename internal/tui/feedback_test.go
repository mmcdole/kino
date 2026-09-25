package tui

import (
	"strings"
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// fetching is a state whose server attempt is in flight.
func fetching(st catalog.State, attempt uint64) catalog.State {
	st.Fetching, st.Attempt = true, attempt
	return st
}

// showSpinner publishes st and lets its indicator delay elapse.
func showSpinner(m *Model, st catalog.State) *Model {
	m = publish(m, st)
	return updateModel(m, showLoadingMsg{Key: st.Resource.Key(), Attempt: st.Attempt})
}

func TestCachedNavigationIsQuietAndHonorsCountPreference(t *testing.T) {
	for _, showCounts := range []bool{false, true} {
		m := testModel(t)
		m.libraryColumn().SetSize(40, 20)
		m.libraryColumn().SetShowLibraryCounts(showCounts)
		r := catalog.LibraryResource(m.libraries[0])
		m.pushColumn(r, "A")
		req := m.requests.active[viewOwner(r)]
		cached := state(r, 1, "one", "two")
		cached.Snapshot.Validated = false
		m = publish(m, cached)
		m = updateModel(m, loadDoneMsg{Request: req})
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
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	m = publish(m, fetching(catalog.State{Resource: r}, 1))
	if m.activeSyncCount() != 0 {
		t.Fatal("spinner shown before delay")
	}
	m = updateModel(m, showLoadingMsg{Key: r.Key(), Attempt: 1})
	if m.activeSyncCount() != 1 {
		t.Fatal("slow server request has no indicator")
	}
	m = publish(m, state(r, 1, "one"))
	m = updateModel(m, showLoadingMsg{Key: r.Key(), Attempt: 1})
	if m.activeSyncCount() != 0 {
		t.Fatal("late timer revived completed activity")
	}

	m = publish(m, fetching(state(r, 1, "one"), 2))
	m = updateModel(m, showLoadingMsg{Key: r.Key(), Attempt: 1})
	if m.activeSyncCount() != 0 {
		t.Fatal("an earlier attempt's timer activated a newer attempt")
	}
}

func TestProgressDoesNotReplaceCompleteCount(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	st := fetching(state(r, 4, "one", "two"), 1)
	m = showSpinner(m, st)
	st.Progress = catalog.Progress{Loaded: 1, Total: 10}
	m = publish(m, st)
	lib := m.libraryStates[r.LibraryID]
	if !lib.Summary.Known || lib.Summary.Count != 2 || lib.Activity.Loaded != 1 || lib.Activity.Total != 10 {
		t.Fatalf("progress and collection summary conflated: %+v", lib)
	}
	st.Fetching, st.Err = false, media.ErrServerOffline
	m = publish(m, st)
	if m.libraryStates[r.LibraryID].Summary.Count != 2 {
		t.Fatal("failure discarded known count")
	}
}

func TestErrorShowsRetryUntilValidatedResult(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	m = updateModel(m, loadDoneMsg{Request: m.requests.active[viewOwner(r)], Err: media.ErrItemNotFound})
	failed := state(r, 4, "one", "two")
	failed.Err = media.ErrItemNotFound
	m = publish(m, failed)
	if lib := m.libraryStates[r.LibraryID]; lib.Summary.Count != 2 || lib.Error == nil || !m.columns.Top().HasLoadFailed() {
		t.Fatal("failure replaced cached summary or lost its retry state")
	}
	m = publish(m, state(r, 5, "one", "two"))
	if m.libraryStates[r.LibraryID].Error != nil || m.columns.Top().HasLoadFailed() {
		t.Fatal("validated result did not clear error")
	}
}

func TestNetworkIndicatorTracksAllSubscribersAndNavigation(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	m.loadResource(r, catalog.Revalidate, true)
	sync := m.requests.active[syncOwner(r)]
	m = showSpinner(m, fetching(catalog.State{Resource: r}, 1))
	m = updateModel(m, loadDoneMsg{Request: sync})
	if m.activeSyncCount() != 1 {
		t.Fatal("one subscriber stopped another's indicator")
	}
	m.handleBack()
	if m.activeSyncCount() != 0 {
		t.Fatal("abandoned view left library spinning")
	}
}

func TestSpinnerTicksOnlyWhileSomethingAnimates(t *testing.T) {
	m := testModel(t)
	if _, cmd := m.Update(tickMsg{}); cmd != nil || m.ticking {
		t.Fatal("idle model scheduled another tick")
	}
	r := catalog.Resource{Kind: catalog.Seasons, ID: "show", ShowID: "show", LibraryID: "a"}
	m.pushColumn(r, "Show")
	m = showSpinner(m, fetching(catalog.State{Resource: r}, 1))
	if !m.ticking {
		t.Fatal("a visible spinner did not start ticking")
	}
	m = publish(m, state(r, 1, "s1"))
	if _, cmd := m.Update(tickMsg{}); cmd != nil || m.ticking {
		t.Fatal("tick continued after the spinner stopped")
	}
}
