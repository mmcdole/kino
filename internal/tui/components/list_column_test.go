package components

import (
	"errors"
	"strings"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func testMovies(titles ...string) []domain.ListItem {
	items := make([]domain.ListItem, len(titles))
	for i, t := range titles {
		items[i] = &domain.MediaItem{ID: "id-" + t, Title: t, Type: domain.MediaTypeMovie}
	}
	return items
}

func selectedID(t *testing.T, c *ListColumn) string {
	t.Helper()
	item := c.SelectedMediaItem()
	if item == nil {
		t.Fatal("no selected media item")
	}
	return item.ID
}

// ReplaceItems must keep the cursor on the same item when content is swapped
// (background refresh), even when new items shift its position.
func TestReplaceItemsPreservesCursorByID(t *testing.T) {
	c := NewListColumn("Movies", ColumnOptions{SortFields: MovieSortOptions(), DefaultSort: SortTitle})
	c.SetSize(40, 20)
	c.SetItems(testMovies("Alpha", "Bravo", "Charlie"))

	c.SetSelectedIndex(1) // Bravo
	if got := selectedID(t, c); got != "id-Bravo" {
		t.Fatalf("setup: selected %q", got)
	}

	// Refresh adds a new item that sorts before Bravo
	c.ReplaceItems(testMovies("Alpha", "Anchor", "Bravo", "Charlie"))

	if got := selectedID(t, c); got != "id-Bravo" {
		t.Fatalf("cursor moved off Bravo after refresh: selected %q", got)
	}
	if c.IsRefreshing() {
		t.Fatal("refreshing flag not cleared")
	}
}

// When the selected item disappears, the cursor falls back to a clamped index
// rather than jumping to the top.
func TestReplaceItemsFallbackClampedIndex(t *testing.T) {
	c := NewListColumn("Movies", ColumnOptions{SortFields: MovieSortOptions(), DefaultSort: SortTitle})
	c.SetSize(40, 20)
	c.SetItems(testMovies("Alpha", "Bravo", "Charlie"))
	c.SetSelectedIndex(2) // Charlie

	c.ReplaceItems(testMovies("Alpha", "Bravo"))

	if idx := c.SelectedIndex(); idx != 1 {
		t.Fatalf("expected clamped cursor 1, got %d", idx)
	}
}

// Sort survives a refresh swap.
func TestReplaceItemsPreservesSort(t *testing.T) {
	c := NewListColumn("Movies", ColumnOptions{SortFields: MovieSortOptions(), DefaultSort: SortTitle})
	c.SetSize(40, 20)
	c.SetItems(testMovies("Alpha", "Bravo"))
	c.ApplySort(SortTitle, SortDesc)

	c.ReplaceItems(testMovies("Alpha", "Bravo", "Charlie"))

	field, dir := c.SortState()
	if field != SortTitle || dir != SortDesc {
		t.Fatalf("sort state lost: field=%v dir=%v", field, dir)
	}
	// Descending: Charlie should be first
	c.SetSelectedIndex(0)
	if got := selectedID(t, c); got != "id-Charlie" {
		t.Fatalf("descending sort not applied to new items: first is %q", got)
	}
}

// An empty column (fresh drill-in) behaves exactly like SetItems.
func TestReplaceItemsOnEmptyColumn(t *testing.T) {
	c := NewListColumn("Movies", ColumnOptions{SortFields: MovieSortOptions(), DefaultSort: SortTitle})
	c.SetSize(40, 20)
	c.SetFeedback(CollectionFeedback{Pending: true})

	c.ReplaceItems(testMovies("Alpha", "Bravo"))

	if c.IsLoading() {
		t.Fatal("loading flag not cleared")
	}
	if c.SelectedIndex() != 0 || c.ItemCount() != 2 {
		t.Fatalf("expected fresh load semantics, cursor=%d count=%d", c.SelectedIndex(), c.ItemCount())
	}
}

func TestRowsFollowItemTypeNotColumn(t *testing.T) {
	episode := &domain.MediaItem{ID: "e", Title: "Pilot", Type: domain.MediaTypeEpisode, ShowTitle: "Show", SeasonNum: 1, EpisodeNum: 1}
	for _, test := range []struct {
		opts ColumnOptions
		want string
	}{
		{ColumnOptions{}, "S01E01 Pilot"},
		{ColumnOptions{ShowParent: true}, "Show - S01E01 Pilot"},
	} {
		c := NewListColumn("test", test.opts)
		c.SetSize(60, 10)
		c.SetItems(nil)
		c.ReplaceItems([]domain.ListItem{episode, &domain.Show{ID: "s", Title: "Mixed Show", Year: 2020}})
		view := c.View()
		if !strings.Contains(view, test.want) || !strings.Contains(view, "Mixed Show (2020)") {
			t.Fatalf("rows ignored item types: %s", view)
		}
	}
}

func TestLoadFailureAndRetryKeepCachedContentVisible(t *testing.T) {
	c := NewListColumn("Movies", ColumnOptions{SortFields: MovieSortOptions(), DefaultSort: SortTitle})
	c.SetSize(50, 20)
	c.SetFeedback(CollectionFeedback{Pending: true})
	c.SetFeedback(CollectionFeedback{Error: errors.New("offline")})
	if c.IsLoading() || !strings.Contains(c.View(), "press r to retry") {
		t.Fatal("cold failure did not become retryable")
	}
	c.SetFeedback(CollectionFeedback{Pending: true})
	if !c.IsLoading() || c.HasLoadFailed() {
		t.Fatal("retry retained failure")
	}
	c.SetItems(testMovies("Alpha"))
	c.SetFeedback(CollectionFeedback{Pending: true})
	c.SetFeedback(CollectionFeedback{Error: errors.New("offline")})
	if !strings.Contains(c.View(), "Refresh failed") || !strings.Contains(c.View(), "Alpha") {
		t.Fatal("refresh failure hid cached content or retry state")
	}
	c.SetFeedback(CollectionFeedback{Pending: true})
	if !c.IsRefreshing() || c.HasLoadFailed() {
		t.Fatal("refresh retry state inconsistent")
	}
	c.ReplaceItems(nil)
	c.SetFeedback(CollectionFeedback{Pending: true})
	if c.IsLoading() || !c.IsRefreshing() {
		t.Fatal("empty snapshot treated as no snapshot")
	}
}
